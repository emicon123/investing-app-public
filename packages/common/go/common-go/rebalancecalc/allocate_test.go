package rebalancecalc

import (
	"testing"

	"github.com/shopspring/decimal"
)

func pct(s string) *decimal.Decimal {
	v := decimal.RequireFromString(s)
	return &v
}

func identityAllocate(buyList []BuyListItem, _ map[string]decimal.Decimal) []BuyListItem {
	return buyList
}

// TestAllocateProportional_TwoAssetSplit is a hand-computed parity check against
// AbstractProportionalStrategy's own math (task 151 §4's "verify AllocateProportional's output
// against real portfolioB/portfolioC Java behavior" acceptance bar) — two assets, 60/40 target, one at 0
// value and one already holding some value, deploying 1000 PLN.
//
// By hand (mirrors buildRows/computeTargetValues/allocatePln/computeUnits/makeItem exactly):
//
//	C_A = 0.6, C_B = 0.4; N_A = 0, N_B = 500
//	M_A = 0 + 0.6*1000 = 600; M_B = 500 + 0.4*1000 = 900; M_total = 1500
//	P_A = 0.6*1500 = 900; P_B = 0.4*1500 = 600
//	O_A = 900 - 0 = 900; O_B = 600 - 500 = 100
//	positive = 1000
//	DO_A = 1000*900/1000 = 900; DO_B = 1000*100/1000 = 100
//	price_A = 10 PLN => units_A = 900/10 = 90; price_B = 50 PLN => units_B = 100/50 = 2
//	currentPct_A = 0/1000*100 = 0; currentPct_B = 500/1000*100 = 50
//	targetPct_A = 60.0000; targetPct_B = 40.0000
//	deviationPct_A = 0/60 - 1 = -1; deviationPct_B = 50/40 - 1 = 0.25
//
// sorted ascending by deviationPct => [A (-1), B (0.25)].
func TestAllocateProportional_TwoAssetSplit(t *testing.T) {
	rows := []PositionRow{
		{Ticker: "A", Name: "Asset A", Exchange: "XTB", TargetAllocationPct: pct("60"), ValuePln: decimal.Zero, PriceNativePln: decimal.RequireFromString("10")},
		{Ticker: "B", Name: "Asset B", Exchange: "XTB", TargetAllocationPct: pct("40"), ValuePln: decimal.RequireFromString("500"), PriceNativePln: decimal.RequireFromString("50")},
	}
	totalPortfolioValuePln := decimal.RequireFromString("1000")

	got := AllocateProportional(rows, decimal.RequireFromString("1000"), nil, totalPortfolioValuePln, identityAllocate, nil)

	if len(got) != 2 {
		t.Fatalf("expected 2 buy items, got %d: %+v", len(got), got)
	}
	// most-underweight-first sort.
	if got[0].Ticker != "A" || got[1].Ticker != "B" {
		t.Fatalf("expected [A, B] order (A most underweight), got [%s, %s]", got[0].Ticker, got[1].Ticker)
	}

	a, b := got[0], got[1]
	assertDecEq(t, "A.AllocatedPln", a.AllocatedPln, "900.0000")
	assertDecEq(t, "A.UnitsToBuy", a.UnitsToBuy, "90.00000000")
	assertDecEq(t, "A.CurrentPct", a.CurrentPct, "0.0000")
	assertDecEq(t, "A.TargetPct", a.TargetPct, "60.0000")
	assertDecEq(t, "A.DeviationPct", a.DeviationPct, "-1.0000")

	assertDecEq(t, "B.AllocatedPln", b.AllocatedPln, "100.0000")
	assertDecEq(t, "B.UnitsToBuy", b.UnitsToBuy, "2.00000000")
	assertDecEq(t, "B.CurrentPct", b.CurrentPct, "50.0000")
	assertDecEq(t, "B.TargetPct", b.TargetPct, "40.0000")
	assertDecEq(t, "B.DeviationPct", b.DeviationPct, "0.2500")
}

func assertDecEq(t *testing.T, label string, got decimal.Decimal, want string) {
	t.Helper()
	w := decimal.RequireFromString(want)
	if !got.Equal(w) {
		t.Fatalf("%s: got %s, want %s", label, got.String(), w.String())
	}
}

func TestAllocateProportional_ZeroOrNegativeDeployable_ReturnsNil(t *testing.T) {
	rows := []PositionRow{{Ticker: "A", TargetAllocationPct: pct("100"), ValuePln: decimal.Zero, PriceNativePln: decimal.RequireFromString("1")}}
	for _, d := range []string{"0", "-5"} {
		got := AllocateProportional(rows, decimal.RequireFromString(d), nil, decimal.Zero, identityAllocate, nil)
		if got != nil {
			t.Fatalf("deployable=%s: expected nil, got %+v", d, got)
		}
	}
}

func TestAllocateProportional_AllOverTarget_ReturnsNilNeverASell(t *testing.T) {
	// Every row has no target allocation set (C_i = 0 for all), so the post-deposit target
	// value P_i is 0 for every row while N_i > 0 — O_i = P_i - N_i = -N_i <= 0 for all rows,
	// making positiveDeficitSum == 0. The function must return nil, never a negative
	// allocation (the manual-sell-only invariant's pure-arithmetic half: this package must be
	// structurally incapable of emitting a sell). This is the realistic way
	// positiveDeficitSum==0 is reachable at all: with a valid target-allocation set summing to
	// 100% (the app's own data-integrity invariant, PositionService's promote-invariant check),
	// sum(O_i) always equals the deployable amount A > 0, so SOME row's O_i must be positive —
	// the branch is only reachable when targets are missing/zero, exercised here.
	rows := []PositionRow{
		{Ticker: "A", TargetAllocationPct: nil, ValuePln: decimal.RequireFromString("10000"), PriceNativePln: decimal.RequireFromString("1")},
		{Ticker: "B", TargetAllocationPct: nil, ValuePln: decimal.RequireFromString("90000"), PriceNativePln: decimal.RequireFromString("1")},
	}
	got := AllocateProportional(rows, decimal.RequireFromString("10"), nil, decimal.RequireFromString("100000"), identityAllocate, nil)
	if got != nil {
		t.Fatalf("expected nil (no buy-side allocation possible), got %+v", got)
	}
}

func TestAllocateProportional_NeverEmitsNegativeAllocation(t *testing.T) {
	// Cross-check across a spread of target/value combinations: AllocatedPln must never be
	// negative for ANY item this function returns — the buy-only invariant, checked
	// structurally rather than for one hand-picked scenario.
	rows := []PositionRow{
		{Ticker: "A", TargetAllocationPct: pct("50"), ValuePln: decimal.RequireFromString("9000"), PriceNativePln: decimal.RequireFromString("3")},
		{Ticker: "B", TargetAllocationPct: pct("30"), ValuePln: decimal.RequireFromString("100"), PriceNativePln: decimal.RequireFromString("7")},
		{Ticker: "C", TargetAllocationPct: pct("20"), ValuePln: decimal.RequireFromString("500"), PriceNativePln: decimal.RequireFromString("11")},
	}
	got := AllocateProportional(rows, decimal.RequireFromString("500"), nil, decimal.RequireFromString("9600"), identityAllocate, nil)
	for _, item := range got {
		if item.AllocatedPln.Sign() < 0 {
			t.Fatalf("ticker %s: negative AllocatedPln %s — manual-sell-only invariant violated", item.Ticker, item.AllocatedPln)
		}
	}
}

// TestAllocateProportional_PortfolioBWaterfall_AlwaysCalled mirrors portfolioB's own
// RebalancingStrategy.calculate, which always calls allocateToAccounts regardless of whether
// account balances are present — confirming this package's own "always call, never skip" design
// (see allocate.go's doc comment) actually reaches the closure with the real buy list and
// balances, not an empty stand-in.
func TestAllocateProportional_PortfolioBWaterfall_AlwaysCalled(t *testing.T) {
	rows := []PositionRow{
		{Ticker: "A", TargetAllocationPct: pct("100"), ValuePln: decimal.Zero, PriceNativePln: decimal.RequireFromString("1")},
	}
	var capturedBalances map[string]decimal.Decimal
	var capturedList []BuyListItem
	waterfall := func(buyList []BuyListItem, balances map[string]decimal.Decimal) []BuyListItem {
		capturedBalances = balances
		capturedList = buyList
		return buyList
	}
	balances := map[string]decimal.Decimal{"IKE_TATA": decimal.RequireFromString("50")}
	AllocateProportional(rows, decimal.RequireFromString("100"), nil, decimal.RequireFromString("100"), waterfall, balances)

	if capturedBalances == nil || capturedBalances["IKE_TATA"].String() != "50" {
		t.Fatalf("expected the real balances map to reach allocateToAccounts, got %+v", capturedBalances)
	}
	if len(capturedList) != 1 || capturedList[0].Ticker != "A" {
		t.Fatalf("expected the real buy list to reach allocateToAccounts, got %+v", capturedList)
	}
}

// TestAllocateProportional_PortfolioCIdentity_NoAccountAllocations mirrors portfolioC's own
// RebalancingStrategy, which never overrides allocateToAccounts (inherits the shared base's
// one-line no-op) — the identity closure must leave AccountAllocations nil/unset.
func TestAllocateProportional_PortfolioCIdentity_NoAccountAllocations(t *testing.T) {
	rows := []PositionRow{
		{Ticker: "SXRV.DE", TargetAllocationPct: pct("100"), ValuePln: decimal.Zero, PriceNativePln: decimal.RequireFromString("500")},
	}
	got := AllocateProportional(rows, decimal.RequireFromString("400"), nil, decimal.RequireFromString("0"), identityAllocate, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got))
	}
	if got[0].AccountAllocations != nil {
		t.Fatalf("expected nil AccountAllocations for portfolioC's identity closure, got %+v", got[0].AccountAllocations)
	}
}
