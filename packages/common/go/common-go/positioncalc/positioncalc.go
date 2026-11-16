// Package positioncalc is the shared position-metrics kernel for portfolioB and portfolioC (ported from the
// Java PositionMetricsCalculator, ADR-094), specialized to each app's own units shape through the
// UnitsTotaler seam. The two apps differ only in how total units are read (portfolioB sums five
// account-type buckets, portfolioC reads a single units column); every other line of the computation is
// identical. Per doc 13 §5 ("one small interface per genuinely-varying step, never a base class"),
// that one varying step is the UnitsTotaler interface below — portfolioB's PositionUnits already
// satisfies it, portfolioC's gains a 3-line method. The per-app DTO assembly (portfolioB's 5-bucket
// SubPositionDTO vs portfolioC's single-units shape) stays per-app; this package returns plain
// scalars/structs only (the settingscore.Store precedent: separate Go modules with their own
// sqlcgen types, so seams use plain Go types). CurrentAndDeviationPct, ComputeEntryPrice and
// EquivalentUnits are the exceptions to "portfolioB/portfolioC-only": MAIN's own
// backend-go/internal/position/computed.go calls them directly, since they are the pieces of this
// package's formula that are byte-identical across all three apps (see their own doc comments) —
// MAIN keeps its own computeFields only for the parts that differ (multi-portfolio totals, the
// macro target override, RETIREMENT gating). The rest of the package (ComputeFields,
// SubPositionValues) stays portfolioB/portfolioC-only. The subasset grouping in subassets.go
// (OrderSubassets, PrimaryOf over the SubassetRow seam) is 3-app as well.
package positioncalc

import "github.com/shopspring/decimal"

var hundred = decimal.NewFromInt(100)

// zeroTargetDeviationSentinel mirrors position.zeroTargetDeviationSentinel in each app's own
// computed.go (ADR-053) — ported here to close a gap where this shared kernel never received
// the ADR-053 fix that MAIN's Go port has. See docs/architecture/05-rebalancing-algorithm.md §7.6.
// MAIN's own former copy of this var (backend-go/internal/position/computed.go) was retired
// 2026-09-23 when CurrentAndDeviationPct became the single source for all three apps.
var zeroTargetDeviationSentinel = decimal.NewFromInt(1)

// UnitsTotaler is the ONE varying step between the two apps' computed-fields
// logic: how total units are read from the decoded position row.
type UnitsTotaler interface{ UnitsTotal() decimal.Decimal }

// Companion carries the scalars ComputeFields needs per non-primary subasset row: its price
// key, its units, and its own cost basis (task 161a-1 — group profit sums the cost of ALL
// subassets, not just the primary's; nil = no recorded cost of its own — its value still counts
// toward the profit, its baseline being assumed covered by a sibling's group-level cost, see
// GroupProfit).
type Companion struct {
	Ticker        string // Subasset.ticker — named in CostlessSubassets, never used for pricing
	YahooTicker   *string
	UnitsTotal    decimal.Decimal
	TotalSpentPln *decimal.Decimal
}

// Computed mirrors both apps' ComputedFields field-for-field (same order, no
// tags) so adapters convert with ComputedFields(sharedResult) in one line.
type Computed struct {
	UnitsTotal, PricePln, PriceNative, ValuePln,
	CurrentPct, DeviationPct, ProfitPct, AvgBuyPricePln,
	AvgBuyPriceNative, EquivalentUnits *decimal.Decimal
	CostlessSubassets []string
}

// ComputeFields computes the derived fields of one position. Both resolvers are cache-only reads
// here (pricePlnByTicker/ratesToPln maps). A missing price OR a missing fx rate degrades every
// computed field to nil except unitsTotal (the original wrapped the whole computation in a
// catch-all).
func ComputeFields(ticker string, yahooTicker *string, assetClass, priceCurrency string,
	targetPct *decimal.Decimal, units UnitsTotaler, totalSpentPln *decimal.Decimal,
	totalPortfolioValuePln decimal.Decimal, companions []Companion,
	pricePlnByTicker, ratesToPln map[string]decimal.Decimal) Computed {
	hasTicker := yahooTicker != nil && *yahooTicker != ""
	isCurrency := assetClass == "CURRENCY"
	unitsTotal := units.UnitsTotal()
	if !hasTicker && !isCurrency {
		return Computed{}
	}

	var pricePln decimal.Decimal
	var ok bool
	if hasTicker {
		pricePln, ok = pricePlnByTicker[*yahooTicker]
	} else {
		pricePln, ok = ratesToPln[priceCurrency]
	}
	if !ok {
		return Computed{UnitsTotal: &unitsTotal}
	}
	valuePln := unitsTotal.Mul(pricePln)

	fxRate, fxOk := ratesToPln[priceCurrency]
	if !fxOk {
		return Computed{UnitsTotal: &unitsTotal}
	}
	var priceNative *decimal.Decimal
	if fxRate.Sign() != 0 {
		v := pricePln.DivRound(fxRate, 4)
		priceNative = &v
	}

	// Group profit inputs (task 161a-1): every subasset's own value and own cost. The primary's
	// value is taken BEFORE the companions are folded into valuePln below.
	subs := make([]SubCost, 0, 1+len(companions))
	primaryValue := valuePln
	subs = append(subs, SubCost{ValuePln: &primaryValue, TotalSpentPln: totalSpentPln, Ticker: ticker, Units: unitsTotal})
	for _, c := range companions {
		sc := SubCost{TotalSpentPln: c.TotalSpentPln, Ticker: c.Ticker, Units: c.UnitsTotal}
		if v, ok := companionValue(c, pricePlnByTicker); ok {
			sc.ValuePln = &v
		}
		subs = append(subs, sc)
	}

	if len(companions) > 0 {
		valuePln = valuePln.Add(companionValuePln(companions, pricePlnByTicker))
	}

	currentPct, deviationPct := CurrentAndDeviationPct(valuePln, totalPortfolioValuePln, targetPct)

	// The entry-price block (task 161): group profit, the entry price in PLN and in the primary's
	// currency at the current fx, and the costless-subassets note — one implementation for all
	// three apps. equivalentUnits needs no cost, only the primary's price.
	entry := ComputeEntryPrice(pricePln, fxRate, subs)

	return Computed{
		UnitsTotal: &unitsTotal, PricePln: &pricePln, PriceNative: priceNative, ValuePln: &valuePln,
		CurrentPct: currentPct, DeviationPct: deviationPct,
		ProfitPct: entry.ProfitPct, AvgBuyPricePln: entry.AvgBuyPricePln, AvgBuyPriceNative: entry.AvgBuyPriceNative,
		EquivalentUnits: EquivalentUnits(valuePln, pricePln), CostlessSubassets: entry.CostlessSubassets,
	}
}

// CurrentAndDeviationPct is the shared core of the currentPct/deviationPct formula, including
// the ADR-053 zero-target sentinel (docs/architecture/05-rebalancing-algorithm.md §7.6) — the one
// piece of ComputeFields/MAIN's computeFields that is byte-identical across all three apps.
// Callers resolve their own portfolio total (MAIN: multi-portfolio map lookup; portfolioB/portfolioC: a
// single scalar) and target (MAIN: the ADR-044 macro de-risk override via
// rebalancingcalc.ResolveTargetPct; portfolioB/portfolioC: the raw column, no override) before calling in —
// this function only knows about the two already-resolved scalars. Extracted 2026-09-23 after the
// same formula had to be fixed independently in this package and in MAIN's own computed.go on the
// same day (the portfolioB/portfolioC porting-gap fix, §7.6) — this is what stops a third copy next time.
// Returns (nil, nil) when totalPortfolioValuePln <= 0.
func CurrentAndDeviationPct(valuePln, totalPortfolioValuePln decimal.Decimal, targetPct *decimal.Decimal) (currentPct, deviationPct *decimal.Decimal) {
	if totalPortfolioValuePln.Sign() <= 0 {
		return nil, nil
	}
	cp := valuePln.DivRound(totalPortfolioValuePln, 8).Mul(hundred)
	currentPct = &cp
	if targetPct != nil && targetPct.Sign() > 0 {
		dp := cp.DivRound(*targetPct, 8).Sub(decimal.NewFromInt(1))
		deviationPct = &dp
	} else if targetPct != nil && cp.Sign() > 0 {
		// ADR-053: an explicit zero target with nonzero value is unambiguously in excess —
		// use a fixed sentinel rather than an unbounded ratio (division by zero otherwise).
		s := zeroTargetDeviationSentinel
		deviationPct = &s
	}
	return currentPct, deviationPct
}

// SubPositionValues is the direct port of PositionService.toSubPositionDto's
// pricing core — the per-app adapters assemble their own (divergent) DTO shapes
// from these scalars.
func SubPositionValues(yahooTicker *string, assetClass, priceCurrency string,
	units UnitsTotaler, pricePlnByTicker, ratesToPln map[string]decimal.Decimal,
) (unitsTotal decimal.Decimal, pricePln, priceNative, valuePln *decimal.Decimal) {
	unitsTotal = units.UnitsTotal()

	if yahooTicker != nil && *yahooTicker != "" {
		if p, ok := pricePlnByTicker[*yahooTicker]; ok {
			pricePln = &p
			v := unitsTotal.Mul(p)
			valuePln = &v
		}
	} else if assetClass == "CURRENCY" {
		if r, ok := ratesToPln[priceCurrency]; ok {
			pricePln = &r
			v := unitsTotal.Mul(r)
			valuePln = &v
		}
	}

	if fxRate, ok := ratesToPln[priceCurrency]; ok && pricePln != nil && fxRate.Sign() != 0 {
		v := pricePln.DivRound(fxRate, 4)
		priceNative = &v
	}
	return unitsTotal, pricePln, priceNative, valuePln
}

// companionValue is one non-primary subasset's PLN value: units x cached PLN price. ok is false
// when no price is resolvable (no yahoo ticker, or not in the cache).
func companionValue(c Companion, pricePlnByTicker map[string]decimal.Decimal) (decimal.Decimal, bool) {
	if c.YahooTicker == nil || *c.YahooTicker == "" {
		return decimal.Zero, false
	}
	price, ok := pricePlnByTicker[*c.YahooTicker]
	if !ok {
		return decimal.Zero, false
	}
	return c.UnitsTotal.Mul(price), true
}

// companionValuePln is the direct port of
// PositionMetricsCalculator.computeCompanionValuePln — a companion with no
// resolvable price contributes nothing (silently skipped, never a hard failure
// for the parent's own computed fields). Sole caller is ComputeFields.
func companionValuePln(companions []Companion,
	pricePlnByTicker map[string]decimal.Decimal) decimal.Decimal {
	total := decimal.Zero
	for _, c := range companions {
		if v, ok := companionValue(c, pricePlnByTicker); ok {
			total = total.Add(v)
		}
	}
	return total
}

// SubCost is one subasset's contribution to its asset's group profit: its own current PLN
// value and its own cost basis. Either may be nil (unpriced / no recorded cost).
//
// Ticker (Subasset.ticker) and Units (units held, every bucket) are read ONLY by EntryPrice, to
// name the priced subassets that hold units but carry no cost; GroupProfit ignores them.
type SubCost struct {
	ValuePln      *decimal.Decimal
	TotalSpentPln *decimal.Decimal
	Ticker        string
	Units         decimal.Decimal
}

// GroupProfit is the asset-level profit fraction, profit = Σvalue / Σcost − 1 (0.1 = +10 %),
// task 161a-1 (rule corrected 2026-09-25).
//
//   - cost  = Σ TotalSpentPln over the subassets whose TotalSpentPln > 0;
//   - value = Σ ValuePln over ALL priced subassets — including those with a nil / zero / negative
//     cost. Why: the go-live import (main's V21 backfill, from the owner's Google Sheet) stored
//     the WHOLE GROUP's cost on the head row (e.g. BTC-USD's total_spent_pln is the cost of
//     physical BTC plus the PL.ETNVIRBTCP ETN together); the non-primary rows carry NULL cost
//     because their baseline cost already sits inside that group cost. Dropping their VALUE while
//     their COST stays in the denominator made profit far too negative (Bitcoin −34.2 % instead of
//     −6.0 %, Gold −18.8 % instead of +49.4 %, Quantum −30.7 % instead of +19.0 % on 2026-09-25
//     data). A companion that DOES have its own recorded cost (the XRP-type case, a post-go-live
//     purchase) still contributes that cost to the denominator — the original bug this helper fixed.
//   - a subasset with a cost > 0 but no price (ValuePln == nil) contributes NEITHER its cost nor a
//     value, so a cost is never counted without its value;
//   - ok is false (profit unknown, no number fabricated) when cost <= 0 — so an app whose costs are
//     all NULL (portfolioB) or a costless singleton keeps profit nil exactly as before.
//
// The result reads the SET of subassets, so a set-primary flip (which only relabels the same rows)
// cannot change it. The 8-decimal DivRound matches the pre-161 single-row formula, so a
// single-subasset asset yields byte-identical results.
//
// Known limitation: a subasset held with NO recorded cost that is NOT covered by a sibling's
// group-level baseline (e.g. bought after go-live without its cost being entered) adds its value to
// the numerator only and inflates profit. Task 161's per-subasset baselines remove it.
func GroupProfit(subs []SubCost) (profitPct decimal.Decimal, ok bool) {
	sumValue, sumCost := decimal.Zero, decimal.Zero
	for _, s := range subs {
		if s.ValuePln == nil {
			continue // unpriced: neither value nor cost (never a cost without its value)
		}
		sumValue = sumValue.Add(*s.ValuePln)
		if s.TotalSpentPln != nil && s.TotalSpentPln.Sign() > 0 {
			sumCost = sumCost.Add(*s.TotalSpentPln)
		}
	}
	if sumCost.Sign() <= 0 {
		return decimal.Zero, false
	}
	return sumValue.DivRound(sumCost, 8).Sub(decimal.NewFromInt(1)), true
}
