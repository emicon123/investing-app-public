package rebalancecalc

import (
	"sort"

	"github.com/shopspring/decimal"
)

// calcRow holds the per-row working fields of the allocation (C_i/N_i/M_i/P_i/O_i/DO_i/DQ_i/DR_i),
// named after the variables of the original algorithm (ported from the Java
// AbstractProportionalStrategy) so the maths stays traceable against it.
type calcRow struct {
	row PositionRow
	c   decimal.Decimal // C_i — target fraction (0..1)
	n   decimal.Decimal // N_i — current value PLN
	m   decimal.Decimal // M_i — projected value after a naive per-asset deposit slice
	p   decimal.Decimal // P_i — target value under the post-deposit total
	o   decimal.Decimal // O_i — deficit (positive) or surplus (negative)
	do  decimal.Decimal // DO_i — allocated PLN
	dq  decimal.Decimal // DQ_i — allocated "native" (== DO_i; the shared algorithm never converts
	// currency — AbstractProportionalStrategy.allocatePln sets `DQ_i = DO_i` unconditionally,
	// confirmed by direct read; both apps' Position.effectivePurchaseCurrency() hardcode PLN)
	dr decimal.Decimal // DR_i — units to buy
}

// AllocateProportional runs the five shared allocation steps
// (buildRows→computeTargetValues→positiveDeficitSum+allocatePln→computeUnits→makeItem) with
// decimal.Decimal math using the same scales/rounding as the original BigDecimal code
// (RoundingMode.HALF_UP -> decimal.Round / decimal.DivRound, both round-half-away-from-zero), plus
// the sort and the one genuinely-varying step (allocateToAccounts) as an explicit parameter — see
// this package's own doc comment for why no interface/base-struct is needed here.
//
// fxRates is accepted for signature parity with the original calculate(...) method (which threaded
// fxRates through to an abstract getNativePricePln hook) but is not referenced in this function's
// own body: allocatePln performs NO currency conversion (DQ_i = DO_i unconditionally — see the
// calcRow.dq field comment), and each row's own native price is already resolved into
// PriceNativePln by ComputeValues before AllocateProportional is ever called. A future row whose
// purchase currency genuinely diverges from PLN would resolve that at the ComputeValues layer, not
// here.
//
// allocateToAccounts is called unconditionally (never skipped, even when accountBalances is empty).
// In the Java original portfolioB always called it while portfolioC's base-class guard (`if (balances ==
// null || balances.isEmpty()) return buyList;`) always fired — externally indistinguishable from
// calling portfolioC's identity closure with an empty map, so one Go shape serves both apps without
// changing either one's observable behavior.
func AllocateProportional(
	rows []PositionRow,
	deployable decimal.Decimal,
	fxRates map[string]decimal.Decimal,
	totalPortfolioValuePln decimal.Decimal,
	allocateToAccounts func(buyList []BuyListItem, balances map[string]decimal.Decimal) []BuyListItem,
	accountBalances map[string]decimal.Decimal,
) []BuyListItem {
	if deployable.Sign() <= 0 {
		return nil
	}

	working := make([]*calcRow, 0, len(rows))
	for _, r := range rows {
		targetPct := decimal.Zero
		if r.TargetAllocationPct != nil {
			targetPct = *r.TargetAllocationPct
		}
		c := targetPct.DivRound(hundred, 10)
		working = append(working, &calcRow{row: r, c: c, n: r.ValuePln})
	}
	if len(working) == 0 {
		return nil
	}

	a := deployable

	// computeTargetValues: M_i = N_i + C_i*A; M_total = ΣM_i; P_i = C_i*M_total; O_i = P_i - N_i.
	for _, wr := range working {
		wr.m = wr.n.Add(wr.c.Mul(a))
	}
	mTotal := decimal.Zero
	for _, wr := range working {
		mTotal = mTotal.Add(wr.m)
	}
	for _, wr := range working {
		wr.p = wr.c.Mul(mTotal)
	}
	for _, wr := range working {
		wr.o = wr.p.Sub(wr.n)
	}

	// positiveDeficitSum
	positive := decimal.Zero
	for _, wr := range working {
		if wr.o.Sign() > 0 {
			positive = positive.Add(wr.o)
		}
	}
	if positive.Sign() == 0 {
		return nil // all positions over-allocated — never a sell, buy-only DCA
	}

	// allocatePln: DO_i = A*O_i/positive (0 when O_i<=0); DQ_i = DO_i (no conversion).
	for _, wr := range working {
		if wr.o.Sign() > 0 {
			wr.do = a.Mul(wr.o).DivRound(positive, 10)
		} else {
			wr.do = decimal.Zero
		}
		wr.dq = wr.do
	}

	// computeUnits: DR_i = DQ_i / priceNativePln (0 on missing/non-positive price — defensive
	// fallback, even though ComputeValues already resolves a usable price for every tradable row it
	// returns).
	for _, wr := range working {
		if wr.row.PriceNativePln.Sign() > 0 {
			wr.dr = wr.dq.DivRound(wr.row.PriceNativePln, 8)
		} else {
			wr.dr = decimal.Zero
		}
	}

	buyList := make([]BuyListItem, 0, len(working))
	for _, wr := range working {
		if wr.do.Sign() <= 0 {
			continue
		}
		buyList = append(buyList, buildItem(wr, totalPortfolioValuePln))
	}

	sort.SliceStable(buyList, func(i, j int) bool {
		return buyList[i].DeviationPct.Cmp(buyList[j].DeviationPct) < 0
	})

	return allocateToAccounts(buyList, accountBalances)
}

// buildItem is the direct port of AbstractProportionalStrategy.makeItem.
func buildItem(wr *calcRow, totalPortfolioValuePln decimal.Decimal) BuyListItem {
	currentPct := decimal.Zero
	if totalPortfolioValuePln.Sign() > 0 {
		currentPct = wr.n.DivRound(totalPortfolioValuePln, 8).Mul(hundred)
	}
	targetPctDisplay := wr.c.Mul(hundred).Round(4)
	deviationPct := decimal.RequireFromString("-1")
	if targetPctDisplay.Sign() > 0 {
		deviationPct = currentPct.DivRound(targetPctDisplay, 8).Sub(decimal.NewFromInt(1))
	}
	return BuyListItem{
		Ticker:           wr.row.Ticker,
		Name:             wr.row.Name,
		Exchange:         wr.row.Exchange,
		PurchaseCurrency: "PLN",
		AllocatedPln:     wr.do.Round(4),
		AllocatedNative:  wr.dq.Round(4),
		UnitsToBuy:       wr.dr.Round(8),
		CurrentPct:       currentPct.Round(4),
		TargetPct:        targetPctDisplay,
		DeviationPct:     deviationPct.Round(4),
	}
}
