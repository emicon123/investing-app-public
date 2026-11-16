package positioncalc

import "github.com/shopspring/decimal"

// EntryPrice is the entry-price block of an asset's `computed` fields (task 161, design
// docs/architecture/diagrams/161-asset-entry-price.md §1.1) — the one implementation for all three
// apps. Every field is nil when unknown; nothing is ever fabricated.
type EntryPrice struct {
	// ProfitPct is the group profit Σvalue/Σcost − 1 (GroupProfit); nil when Σcost <= 0.
	ProfitPct *decimal.Decimal
	// AvgBuyPricePln is the entry price in PLN per 1 unit of the primary: pricePln / (1 + profit).
	AvgBuyPricePln *decimal.Decimal
	// AvgBuyPriceNative is the same entry price in the primary's price currency at the CURRENT fx.
	AvgBuyPriceNative *decimal.Decimal
	// CostlessSubassets lists (in input order) the tickers of priced subassets that hold units but
	// have no recorded cost. Only ever set together with AvgBuyPricePln.
	CostlessSubassets []string
}

// ComputeEntryPrice derives the entry-price block for one asset from the primary's current PLN
// price, the current PLN rate of the primary's price currency, and every subasset of the asset
// (primary included — the primary is just one of the rows, so a set-primary flip only changes the
// two scalars, never the profit).
//
//	profit            = GroupProfit(subs)                     (nil when Σcost <= 0 -> everything nil)
//	avgBuyPricePln    = pricePln / (1 + profit)               (4 dp)
//	avgBuyPriceNative = pricePln / ((1 + profit) × fxRate)    (4 dp — ONE rounding, from the unrounded
//	                    entry price; equals avgBuyPricePln for a PLN primary, fxRate = 1)
//
// The native line uses the CURRENT fx by design (the PLN cost is fixed, so "the average price in USD
// is the PLN average divided by the current dollar rate"). It is absent when fxRate is 0 — the same
// gate as priceNative — and the entry price itself is absent when profit = -100 % (1 + profit = 0),
// in which case no costless list is emitted either ("only when an entry price exists").
func ComputeEntryPrice(pricePln, fxRate decimal.Decimal, subs []SubCost) EntryPrice {
	profit, ok := GroupProfit(subs)
	if !ok {
		return EntryPrice{}
	}
	out := EntryPrice{ProfitPct: &profit}
	divisor := decimal.NewFromInt(1).Add(profit)
	if divisor.Sign() == 0 {
		return out
	}
	avg := pricePln.DivRound(divisor, 4)
	out.AvgBuyPricePln = &avg
	if fxRate.Sign() != 0 {
		native := pricePln.DivRound(divisor.Mul(fxRate), 4)
		out.AvgBuyPriceNative = &native
	}
	out.CostlessSubassets = costlessSubassets(subs)
	return out
}

// costlessSubassets names, in input order, the PRICED subassets that hold units but have no recorded
// cost (NULL, zero or negative) — the rows whose baseline is "assumed included in the group cost"
// (the go-live import put the whole group's cost on the head row). An unpriced row is skipped (it
// contributes neither value nor cost to the entry price, GroupProfit's rule), and so is a row
// holding no units (sold out). nil, never an empty slice, when there is none.
func costlessSubassets(subs []SubCost) []string {
	var out []string
	for _, s := range subs {
		if s.ValuePln == nil || s.Units.Sign() <= 0 {
			continue
		}
		if s.TotalSpentPln == nil || s.TotalSpentPln.Sign() <= 0 {
			out = append(out, s.Ticker)
		}
	}
	return out
}

// EquivalentUnits is what the asset's whole priced holdings are worth in units of the primary:
// valuePln / pricePln (valuePln = Σ over the priced subassets; 8 dp). It does not depend on any
// cost — it is present whenever the primary is priced — and, when an entry price exists,
// avgBuyPricePln × EquivalentUnits = Σcost. nil when the primary's price is not positive.
func EquivalentUnits(valuePln, pricePln decimal.Decimal) *decimal.Decimal {
	if pricePln.Sign() <= 0 {
		return nil
	}
	u := valuePln.DivRound(pricePln, 8)
	return &u
}
