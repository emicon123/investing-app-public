package rebalancingcalc

import (
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
)

// This file collapses AbstractProportionalStrategy + MainRebalancingStrategy +
// RetirementRebalancingStrategy + RebalancingStrategy + RebalancingStrategyFactory +
// RebalancingContext into ONE function, calculateBuyList, per doc 13 §5 point 2 and diagram doc
// 150 §0/§5 ("CONCRETE RISK... do not [port as] a Go interface + factory + two structs"). Direct
// line-by-line comparison of MainRebalancingStrategy.calculate (12 named steps) and
// RetirementRebalancingStrategy.calculate (8 named steps) shows the arithmetic is IDENTICAL —
// MAIN's own doc comment says so explicitly ("structurally equivalent... same 12-step vs 8-step
// DCA shape"); the extra MAIN step numbers are cosmetic renames of the same quantities
// (MAIN's O_i/RETIREMENT's M_i, MAIN's Q_i/RETIREMENT's O_i, MAIN's R_i==V_i/RETIREMENT's DO_i).
// The only two genuine behavioral differences are captured by portfolioConfig below:
//
//  1. targetOverrides (MAIN only, via the macro de-risk gate) — RETIREMENT always passes nil.
//  2. convertChfNative — MainRebalancingStrategy's shared convertToNative helper handles CHF
//     purchase currency explicitly; RetirementRebalancingStrategy's own inline switch (Step 7)
//     does NOT have a CHF case and silently falls through to unconverted PLN. This only affects
//     the display-only allocatedNative field (never unitsToBuy, which is always computed
//     independently from the PLN allocation and PLN price per both strategies' own comments) —
//     preserved here exactly rather than "fixed", since RETIREMENT realistically has no
//     CHF-purchase-currency positions and this is a faithful behavior port, not a design choice
//     to relitigate.
type portfolioConfig struct {
	targetOverrides  map[string]decimal.Decimal // nil/empty = disabled (RETIREMENT always nil)
	convertChfNative bool                       // true for MAIN, false for RETIREMENT
}

var (
	hundred            = decimal.NewFromInt(100)
	invariantTolerance = decimal.RequireFromString("0.01")
)

type calcRow struct {
	pos                 PositionRow
	c                   decimal.Decimal // target decimal 0..1
	n                   decimal.Decimal // current value PLN
	o, p, q, r, v, u, w decimal.Decimal
	purchaseCurrency    string
}

// calculateBuyList runs the 8/12-step pure-proportional, strictly buy-only DCA (ADR-045) common
// to both portfolios. Never returns a negative allocatedPln — over-target positions (Q_i <= 0)
// simply receive zero allocation and are left alone; this is the manual-sell-only invariant's
// pure-arithmetic half (architect-output §3 item 4 — the deleted ADR-045 2x-trim rule used to be
// able to emit a negative allocation here; it cannot anymore, by construction).
func calculateBuyList(
	budget decimal.Decimal,
	rows []PositionRow,
	valuePln map[string]decimal.Decimal,
	fxRates map[string]decimal.Decimal,
	pricePlnByTicker map[string]decimal.Decimal,
	totalPortfolioValuePln decimal.Decimal,
	cfg portfolioConfig,
) ([]BuyListItem, error) {
	if budget.Cmp(decimal.Zero) <= 0 {
		return nil, nil
	}

	// ADR-044 §6 coverage assertion (MAIN only, when the gate is enabled): every row's ticker
	// must have an override entry, or a stale/incomplete override map would silently fall back
	// to the risk-on DB value for the missing ticker — fail loudly instead.
	if len(cfg.targetOverrides) > 0 {
		for _, row := range rows {
			if _, ok := cfg.targetOverrides[row.Ticker]; !ok {
				return nil, fmt.Errorf("rebalancingcalc: macro de-risk override map is missing ticker %s — every MAIN asset (primary subasset) must have an interpolated target when the gate is enabled (ADR-044 §6)", row.Ticker)
			}
		}
	}

	working := make([]*calcRow, 0, len(rows))
	for _, row := range rows {
		targetPct := ResolveTargetPct(row.Ticker, row.TargetAllocationPct, cfg.targetOverrides)
		c := targetPct.DivRound(hundred, 10)
		n := valuePln[row.Ticker]
		working = append(working, &calcRow{pos: row, c: c, n: n})
	}
	if len(working) == 0 {
		return nil, nil
	}

	A := budget

	// Step 1/1 — per-asset budget slice, then projected value after deposit.
	for _, wr := range working {
		wr.o = wr.n.Add(wr.c.Mul(A))
	}
	oTotal := decimal.Zero
	for _, wr := range working {
		oTotal = oTotal.Add(wr.o)
	}
	// Step 4/2 — target value under the post-deposit total.
	for _, wr := range working {
		wr.p = wr.c.Mul(oTotal)
	}
	// Step 5/3 — deficit (positive) or surplus (negative).
	for _, wr := range working {
		wr.q = wr.p.Sub(wr.n)
	}
	// Step 6/4 — invariant check: SUM(Q_i) == A.
	sumQ := decimal.Zero
	for _, wr := range working {
		sumQ = sumQ.Add(wr.q)
	}
	if sumQ.Sub(A).Abs().Cmp(invariantTolerance) > 0 {
		return nil, fmt.Errorf("rebalancing invariant violated: SUM(Q_i)=%s != A=%s (diff=%s) — target allocations do not sum to 100%%",
			sumQ.StringFixed(4), A.StringFixed(4), sumQ.Sub(A).Abs().StringFixed(6))
	}
	// Step 7/5 — positive deficit sum.
	qPos := decimal.Zero
	for _, wr := range working {
		if wr.q.Sign() > 0 {
			qPos = qPos.Add(wr.q)
		}
	}
	if qPos.Sign() == 0 {
		return nil, nil // all positions over-allocated — no buy-side allocations (never a sell)
	}

	// Step 8/6 — proportional base allocation, which IS the final PLN allocation (MAIN's
	// separate "V_i = R_i" step is a pure rename with zero behavioral effect).
	for _, wr := range working {
		if wr.q.Sign() > 0 {
			wr.r = A.Mul(wr.q).DivRound(qPos, 10)
		}
		wr.v = wr.r
	}

	// Step 10/7 — convert to purchase currency (informational only; fails loudly on missing FX).
	for _, wr := range working {
		pc := wr.pos.EffectivePurchaseCurrency()
		wr.purchaseCurrency = pc
		native, err := convertToNative(wr.v, pc, fxRates, cfg.convertChfNative)
		if err != nil {
			return nil, err
		}
		wr.u = native
	}

	// Step 11/8 — units to buy. Defensive fallback to zero on a missing/non-positive price:
	// computeValues already fails loudly earlier for any tradable position lacking a price, so this
	// branch is unreachable in practice — kept as the same defensive fallback the original had
	// despite the same upstream guarantee.
	for _, wr := range working {
		priceNativePln, ok := nativePricePln(wr.pos, fxRates, pricePlnByTicker)
		if ok && priceNativePln.Sign() > 0 {
			wr.w = wr.v.DivRound(priceNativePln, 8)
		}
	}

	buyList := make([]BuyListItem, 0, len(working))
	for _, wr := range working {
		if wr.v.Sign() <= 0 {
			continue
		}
		buyList = append(buyList, buildItem(wr, totalPortfolioValuePln))
	}

	// Sort: most underweight first (deviationPct ASC) — identical in both strategies.
	sort.SliceStable(buyList, func(i, j int) bool {
		return buyList[i].DeviationPct.Cmp(buyList[j].DeviationPct) < 0
	})
	return buyList, nil
}

// ResolveTargetPct is the direct port of MainRebalancingStrategy.resolveTargetPct /
// RetirementRebalancingStrategy's inline equivalent (RETIREMENT never receives overrides, so its
// own read always falls through to the DB value here too — same function, no behavior split
// needed). Exported so internal/position's own computed-fields path (which needs the identical
// macro-derisk-override lookup for effectiveTargetAllocationPct) shares this one implementation
// instead of maintaining its own copy.
func ResolveTargetPct(ticker string, targetAllocationPct *decimal.Decimal, overrides map[string]decimal.Decimal) decimal.Decimal {
	if len(overrides) == 0 {
		if targetAllocationPct != nil {
			return *targetAllocationPct
		}
		return decimal.Zero
	}
	if v, ok := overrides[ticker]; ok {
		return v
	}
	if targetAllocationPct != nil {
		return *targetAllocationPct
	}
	return decimal.Zero
}

// convertToNative is the direct port of AbstractProportionalStrategy.convertToNative +
// RetirementRebalancingStrategy's own inline Step-7 switch, unified via convertChfNative (see
// portfolioConfig's doc comment above for the one real behavioral difference between the two).
func convertToNative(pln decimal.Decimal, purchaseCurrency string, fxRates map[string]decimal.Decimal, convertChf bool) (decimal.Decimal, error) {
	switch purchaseCurrency {
	case "EUR":
		return divByRate(pln, fxRates, "EUR")
	case "USD":
		return divByRate(pln, fxRates, "USD")
	case "CHF":
		if convertChf {
			return divByRate(pln, fxRates, "CHF")
		}
		return pln, nil
	default:
		return pln, nil
	}
}

func divByRate(pln decimal.Decimal, fxRates map[string]decimal.Decimal, currency string) (decimal.Decimal, error) {
	rate, ok := fxRates[currency]
	if !ok || rate.Cmp(decimal.Zero) <= 0 {
		return decimal.Zero, fxRateUnavailable(currency)
	}
	return pln.DivRound(rate, 10), nil
}

// buildItem is the direct port of AbstractProportionalStrategy.makeItem (and
// RetirementRebalancingStrategy's identical inline construction).
func buildItem(wr *calcRow, totalPortfolioValuePln decimal.Decimal) BuyListItem {
	currentPct := decimal.Zero
	if totalPortfolioValuePln.Cmp(decimal.Zero) > 0 {
		currentPct = wr.n.DivRound(totalPortfolioValuePln, 8).Mul(hundred)
	}
	targetPctDisplay := wr.c.Mul(hundred).Round(4)
	deviationPct := decimal.RequireFromString("-1")
	if targetPctDisplay.Cmp(decimal.Zero) > 0 {
		deviationPct = currentPct.DivRound(targetPctDisplay, 8).Sub(decimal.NewFromInt(1))
	}
	return BuyListItem{
		Ticker:           wr.pos.Ticker,
		Name:             wr.pos.Name,
		Exchange:         wr.pos.Exchange,
		PurchaseCurrency: wr.purchaseCurrency,
		AllocatedPln:     wr.v.Round(4),
		AllocatedNative:  wr.u.Round(4),
		UnitsToBuy:       wr.w.Round(8),
		CurrentPct:       currentPct.Round(4),
		TargetPct:        targetPctDisplay,
		DeviationPct:     deviationPct.Round(4),
	}
}
