package position

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/rebalancingcalc"
	"investing-app/common-go/positioncalc"
)

// buildDTO attaches group-level computed fields and priced subassets to an asset —
// the regrouped port of PositionService.withComputed/computeFields/computeSubPositions
// (task 156): valuePln = Σ subassets, deviationPct on the asset's target, primary
// first. UnitsTotal/pricePln/priceNative stay the primary subasset's own figures (summing
// units across different instruments would be meaningless — and matches the old main-row-only
// semantics, since the primary IS the old main row); profitPct and the entry-price fields are
// asset-level (positioncalc.ComputeEntryPrice, task 161).
func (s *Service) buildDTO(ctx context.Context, a assetInfo, subs []decoded, portfolioTotals map[string]decimal.Decimal,
	targetOverrides map[string]decimal.Decimal, pricePlnMap map[string]decimal.Decimal) (AssetDTO, error) {

	computed, err := s.computeFields(ctx, a, subs, portfolioTotals, targetOverrides, pricePlnMap)
	if err != nil {
		return AssetDTO{}, err
	}
	out := make([]SubassetDTO, 0, len(subs))
	for _, d := range subs {
		sub, err := s.toSubassetDTO(ctx, d, pricePlnMap)
		if err != nil {
			return AssetDTO{}, err
		}
		out = append(out, sub)
	}
	dto := toDTOBase(a, subs)
	dto.Computed = computed
	dto.Subassets = out
	return dto, nil
}

// computeFields computes an asset's derived fields (regrouped from the Java
// PositionService.computeFields). A missing price for the primary's own ticker/currency degrades to
// a partial result (unitsTotal only, everything else nil) rather than failing the request. The
// subasset fold (old companion-value add) only applies once the primary itself is priced, exactly
// as before.
func (s *Service) computeFields(ctx context.Context, a assetInfo, subs []decoded, portfolioTotals map[string]decimal.Decimal,
	targetOverrides map[string]decimal.Decimal, pricePlnMap map[string]decimal.Decimal) (ComputedFields, error) {

	primary := positioncalc.PrimaryOf(subs, a.ID)
	if primary == nil {
		return ComputedFields{}, nil
	}
	p := *primary

	hasTicker := p.YahooTicker != nil && strings.TrimSpace(*p.YahooTicker) != ""
	isCurrency := p.AssetClass == "CURRENCY"
	if !hasTicker && !isCurrency {
		return ComputedFields{}, nil
	}
	unitsTotal := p.UnitsTotal()
	degraded := ComputedFields{UnitsTotal: &unitsTotal}

	var pricePln decimal.Decimal
	if hasTicker {
		v, ok := pricePlnMap[*p.YahooTicker]
		if !ok {
			return degraded, nil
		}
		pricePln = v
	} else {
		rate, err := s.prices.RateToPln(ctx, p.PriceCurrency)
		if err != nil {
			return degraded, nil
		}
		pricePln = rate
	}
	valuePln := unitsTotal.Mul(pricePln)

	// The FX rate lookup runs unconditionally here too, with the same degrade-on-failure handling
	// as the price lookup above — any failure degrades the whole result the same way.
	fxRate, err := s.prices.RateToPln(ctx, p.PriceCurrency)
	if err != nil {
		return degraded, nil
	}
	var priceNative *decimal.Decimal
	if fxRate.Sign() != 0 {
		pn := pricePln.DivRound(fxRate, 4)
		priceNative = &pn
	}

	// Group profit inputs (task 156 / 161a-1): every subasset's OWN value and OWN cost — the
	// primary's value is taken BEFORE the other subassets are folded into valuePln below.
	// (Previously only the primary's total_spent_pln fed the profit while valuePln summed the
	// whole group, so e.g. XRP showed +82.8% instead of the true +10.2%, and a set-primary flip
	// would have switched which row's cost fed the profit.)
	primaryValue := valuePln
	profitSubs := make([]positioncalc.SubCost, 0, len(subs))
	profitSubs = append(profitSubs, positioncalc.SubCost{
		ValuePln: &primaryValue, TotalSpentPln: p.TotalSpentPln, Ticker: p.Ticker, Units: unitsTotal,
	})
	for _, d := range subs {
		if d.IsPrimary {
			continue
		}
		sc := positioncalc.SubCost{TotalSpentPln: d.TotalSpentPln, Ticker: d.Ticker, Units: d.UnitsTotal()}
		if v, ok := subassetValuePln(d, pricePlnMap); ok {
			sc.ValuePln = &v // unpriced -> nil: GroupProfit then drops its cost too (neither side)
			valuePln = valuePln.Add(v)
		}
		profitSubs = append(profitSubs, sc)
	}

	// The overrides map is keyed by the primary subasset's ticker (computeOverrides), so
	// looking it up by p.Ticker resolves exactly the entry the old per-position call
	// (p.Ticker, p.TargetAllocationPct) resolved. assets.key is a logical slug and is
	// never used for this lookup.
	var effectiveTargetAllocationPct *decimal.Decimal
	if a.TargetAllocationPct != nil {
		eff := rebalancingcalc.ResolveTargetPct(p.Ticker, a.TargetAllocationPct, targetOverrides)
		effectiveTargetAllocationPct = &eff
	}

	var currentPct, deviationPct *decimal.Decimal
	if total, ok := portfolioTotals[a.Portfolio]; ok {
		currentPct, deviationPct = positioncalc.CurrentAndDeviationPct(valuePln, total, effectiveTargetAllocationPct)
	}

	// The entry-price block is the shared positioncalc kernel (task 161; one implementation for all
	// three apps). One kernel for every portfolio — RETIREMENT included (ADR-104 lifted ADR-056).
	entry := positioncalc.ComputeEntryPrice(pricePln, fxRate, profitSubs)

	return ComputedFields{
		UnitsTotal: &unitsTotal, PricePln: &pricePln, PriceNative: priceNative, ValuePln: &valuePln,
		CurrentPct: currentPct, EffectiveTargetAllocationPct: effectiveTargetAllocationPct,
		DeviationPct: deviationPct, ProfitPct: entry.ProfitPct,
		AvgBuyPricePln: entry.AvgBuyPricePln, AvgBuyPriceNative: entry.AvgBuyPriceNative,
		EquivalentUnits: positioncalc.EquivalentUnits(valuePln, pricePln), CostlessSubassets: entry.CostlessSubassets,
	}, nil
}

// subassetValuePln is the regrouped port of PositionService.computeCompanionValuePln — the
// subasset's own PLN value; ok=false when it cannot be priced (silently skipped, never fails the
// asset's computation).
func subassetValuePln(d decoded, pricePlnMap map[string]decimal.Decimal) (decimal.Decimal, bool) {
	if d.YahooTicker == nil || *d.YahooTicker == "" {
		return decimal.Zero, false
	}
	price, ok := pricePlnMap[*d.YahooTicker]
	if !ok {
		return decimal.Zero, false
	}
	return d.UnitsTotal().Mul(price), true
}

// toSubassetDTO prices one subasset row (regrouped from the Java toSubPositionDto). NOTE (kept from
// the original, not hardened): the final FX rate lookup for priceNative is NOT covered by the
// degrade-to-partial handling — a genuinely failed live FX fetch returns an error from this
// function, which the caller does not catch either, so it surfaces as a 500.
func (s *Service) toSubassetDTO(ctx context.Context, d decoded, pricePlnMap map[string]decimal.Decimal) (SubassetDTO, error) {
	effectiveUnits := d.UnitsTotal()
	var pricePln *decimal.Decimal

	if d.YahooTicker != nil && *d.YahooTicker != "" {
		if v, ok := pricePlnMap[*d.YahooTicker]; ok {
			pricePln = &v
		}
	} else if d.AssetClass == "CURRENCY" {
		if rate, err := s.prices.RateToPln(ctx, d.PriceCurrency); err == nil {
			pricePln = &rate
		}
	}

	var valuePln *decimal.Decimal
	if pricePln != nil {
		vp := effectiveUnits.Mul(*pricePln)
		valuePln = &vp
	}

	fxRate, err := s.prices.RateToPln(ctx, d.PriceCurrency)
	if err != nil {
		return SubassetDTO{}, err
	}
	var priceNative *decimal.Decimal
	if pricePln != nil && fxRate.Sign() != 0 {
		pn := pricePln.DivRound(fxRate, 4)
		priceNative = &pn
	}

	return SubassetDTO{
		ID: d.ID, AssetID: d.AssetID, IsPrimary: d.IsPrimary,
		Ticker: d.Ticker, Name: d.Name, Portfolio: d.Portfolio, Exchange: d.Exchange, AssetClass: d.AssetClass,
		PriceCurrency: d.PriceCurrency, PurchaseCurrency: d.PurchaseCurrency,
		UnitsIke: d.UnitsIke, UnitsIkze: d.UnitsIkze, UnitsRegularEur: d.UnitsRegularEur,
		UnitsRegularUsd: d.UnitsRegularUsd, UnitsRegularPln: d.UnitsRegularPln,
		EffectiveUnits: effectiveUnits, PricePln: pricePln, PriceNative: priceNative, ValuePln: valuePln,
		YahooTicker: d.YahooTicker, CompanyBacked: d.CompanyBacked,
		TotalSpentPln: d.TotalSpentPln, AvgBuyPrice: d.AvgBuyPrice,
	}, nil
}
