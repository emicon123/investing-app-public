package rebalancecalc

import "github.com/shopspring/decimal"

// ComputationResult is the output of ComputeValues (ported from the Java
// AbstractValueComputationService.ComputationResult; its portfolio-scoped sibling was
// byte-identical once portfolio-scoping happens one layer up, at the caller's own SQL query —
// architect-output §4). pricesRefreshedAt is deliberately omitted from THIS struct: both apps'
// RebalancingResultDto do carry a pricesRefreshedAt field (earliest price fetch time across the
// computation), but it is a pure display concern — "prices last updated at" — with zero effect on
// the rebalancing math itself. Rather than smuggle a price_cache.fetched_at dependency into this
// pure-transform package (which otherwise never sees a raw DB row), the one caller that needs it
// (internal/rebalancing, the only endpoint whose DTO has this field — internal/position and
// internal/deposit's own DTOs have no such field) computes it itself from this function's own
// Tradable rows' tickers, via its own PriceSource's fetched_at read.
type ComputationResult struct {
	ValuePlnByTicker map[string]decimal.Decimal
	Tradable         []PositionRow
}

// ComputeValues values every position (ported from the Java
// AbstractValueComputationService.computeValues). fxRates and pricePlnByTicker are cache-only reads
// — never a live fetch: prices are refreshed by the background scheduler.
//
// Companion rows fold their value into their parent ticker's ValuePln entry and never appear as
// their own PositionRow in the returned Tradable slice — a companion with no resolvable price is
// silently skipped (contributes zero). A tradable row with no resolvable price still appears (with
// PriceNativePln == zero, ValuePln == its companions' value only), never a hard failure at this
// layer.
func ComputeValues(inputs []PositionInput, fxRates, pricePlnByTicker map[string]decimal.Decimal) ComputationResult {
	var tradable, companions []PositionInput
	for _, p := range inputs {
		if p.IsCompanionRow {
			companions = append(companions, p)
		} else {
			tradable = append(tradable, p)
		}
	}

	companionValues := map[string]decimal.Decimal{}
	for _, c := range companions {
		if c.CompanionParentTicker == nil {
			continue
		}
		price, ok := nativePricePln(c.PriceCurrency, c.YahooTicker, fxRates, pricePlnByTicker)
		if !ok {
			continue
		}
		companionValues[*c.CompanionParentTicker] = companionValues[*c.CompanionParentTicker].Add(c.UnitsTotal.Mul(price))
	}

	valuePln := make(map[string]decimal.Decimal, len(tradable))
	rows := make([]PositionRow, 0, len(tradable))
	for _, p := range tradable {
		price, ok := nativePricePln(p.PriceCurrency, p.YahooTicker, fxRates, pricePlnByTicker)
		if !ok {
			price = decimal.Zero
		}
		value := p.UnitsTotal.Mul(price)
		if cv, has := companionValues[p.Ticker]; has {
			value = value.Add(cv)
		}
		valuePln[p.Ticker] = value
		rows = append(rows, PositionRow{
			Ticker: p.Ticker, Name: p.Name, Exchange: p.Exchange,
			TargetAllocationPct: p.TargetAllocationPct, ValuePln: value, PriceNativePln: price,
		})
	}

	return ComputationResult{ValuePlnByTicker: valuePln, Tradable: rows}
}

// TotalPortfolioValuePln sums a ComputationResult's per-ticker values — the one expression the
// rebalancing, XIRR and position services all need.
func TotalPortfolioValuePln(result ComputationResult) decimal.Decimal {
	total := decimal.Zero
	for _, v := range result.ValuePlnByTicker {
		total = total.Add(v)
	}
	return total
}

// nativePricePln is the direct port of AbstractValueComputationService.getNativePricePln — CHF
// is a special case (the FX rate IS the price, defaulting to 1 when the rate is itself missing,
// never a "not found" outcome for CHF), everything else resolves through the cache-only
// pricePlnByTicker map keyed by yahooTicker.
func nativePricePln(priceCurrency string, yahooTicker *string, fxRates, pricePlnByTicker map[string]decimal.Decimal) (decimal.Decimal, bool) {
	if priceCurrency == "CHF" {
		if rate, ok := fxRates["CHF"]; ok {
			return rate, true
		}
		return decimal.NewFromInt(1), true
	}
	if yahooTicker == nil || *yahooTicker == "" {
		return decimal.Zero, false
	}
	price, ok := pricePlnByTicker[*yahooTicker]
	return price, ok
}
