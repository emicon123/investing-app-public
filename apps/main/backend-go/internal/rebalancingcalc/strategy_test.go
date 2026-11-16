package rebalancingcalc

import (
	"math/rand"
	"testing"

	"github.com/shopspring/decimal"
)

func target(pct string) *decimal.Decimal {
	d := decimal.RequireFromString(pct)
	return &d
}

func plnRates() map[string]decimal.Decimal {
	return map[string]decimal.Decimal{
		"PLN": decimal.NewFromInt(1),
		"EUR": decimal.RequireFromString("4.3"),
		"USD": decimal.RequireFromString("4.0"),
		"CHF": decimal.RequireFromString("4.6"),
	}
}

// threeAssetRows sums to exactly 100% target, matching the Step-6 invariant every real
// portfolio must satisfy — three assets at 50/30/20, one already sitting exactly on target,
// one underweight, one overweight.
func threeAssetRows() []PositionRow {
	yt := func(s string) *string { return &s }
	return []PositionRow{
		{Ticker: "A", Name: "Asset A", Exchange: "XTB", PriceCurrency: "PLN", PurchaseCurrency: "PLN", TargetAllocationPct: target("50"), YahooTicker: yt("A.YAHOO")},
		{Ticker: "B", Name: "Asset B", Exchange: "XTB", PriceCurrency: "PLN", PurchaseCurrency: "PLN", TargetAllocationPct: target("30"), YahooTicker: yt("B.YAHOO")},
		{Ticker: "C", Name: "Asset C", Exchange: "XTB", PriceCurrency: "PLN", PurchaseCurrency: "PLN", TargetAllocationPct: target("20"), YahooTicker: yt("C.YAHOO")},
	}
}

func TestCalculateBuyList_ProportionalAllocation_KnownValues(t *testing.T) {
	rows := threeAssetRows()
	// Current values: A=500 (way underweight vs its 50% target of a ~1000 total),
	// B=300 (exactly on target), C=300 (overweight vs its 20% target).
	valuePln := map[string]decimal.Decimal{
		"A": decimal.NewFromInt(500),
		"B": decimal.NewFromInt(300),
		"C": decimal.NewFromInt(300),
	}
	pricePlnByTicker := map[string]decimal.Decimal{
		"A.YAHOO": decimal.NewFromInt(10),
		"B.YAHOO": decimal.NewFromInt(10),
		"C.YAHOO": decimal.NewFromInt(10),
	}
	total := decimal.NewFromInt(1100) // 500+300+300 + budget(0)? total computed by caller normally
	budget := decimal.NewFromInt(100)

	buyList, err := calculateBuyList(budget, rows, valuePln, plnRates(), pricePlnByTicker, total, portfolioConfig{convertChfNative: true})
	if err != nil {
		t.Fatalf("calculateBuyList: %v", err)
	}

	// O_total = (500+300+300) + 100 = 1200. Target values: A=600, B=360, C=240.
	// Q_i = target - current: A=100, B=60, C=-60. Q_pos = 160.
	// R_i = 100 * Q_i / 160: A = 62.5, B = 37.5, C = 0 (excluded, Q_i<0).
	byTicker := map[string]decimal.Decimal{}
	for _, item := range buyList {
		byTicker[item.Ticker] = item.AllocatedPln
	}
	if len(buyList) != 2 {
		t.Fatalf("expected 2 buy-list items (C over-target excluded), got %d: %+v", len(buyList), buyList)
	}
	if got := byTicker["A"]; !got.Equal(decimal.RequireFromString("62.5")) {
		t.Fatalf("expected A allocatedPln=62.5, got %s", got)
	}
	if got := byTicker["B"]; !got.Equal(decimal.RequireFromString("37.5")) {
		t.Fatalf("expected B allocatedPln=37.5, got %s", got)
	}
	if _, present := byTicker["C"]; present {
		t.Fatalf("C is over-target and must never appear in the buy list (manual-sell-only invariant)")
	}

	// Sum of allocations must equal the budget exactly.
	sum := decimal.Zero
	for _, v := range byTicker {
		sum = sum.Add(v)
	}
	if !sum.Equal(budget) {
		t.Fatalf("expected sum(allocatedPln) == budget (%s), got %s", budget, sum)
	}
}

func TestCalculateBuyList_ZeroOrNegativeBudget_ReturnsEmptyNoError(t *testing.T) {
	rows := threeAssetRows()
	for _, budget := range []decimal.Decimal{decimal.Zero, decimal.NewFromInt(-1)} {
		buyList, err := calculateBuyList(budget, rows, map[string]decimal.Decimal{}, plnRates(), map[string]decimal.Decimal{}, decimal.Zero, portfolioConfig{})
		if err != nil {
			t.Fatalf("budget=%s: unexpected error: %v", budget, err)
		}
		if len(buyList) != 0 {
			t.Fatalf("budget=%s: expected empty buy list, got %+v", budget, buyList)
		}
	}
}

// NOTE: "all positions over-allocated -> Q_pos == 0 -> empty buy list" (Java's
// MainRebalancingStrategy/RetirementRebalancingStrategy log.info branch) is mathematically
// unreachable whenever budget > 0 and the Step-6 invariant holds: Q_pos == 0 requires every
// Q_i <= 0, which would force SUM(Q_i) <= 0 — but the invariant just above guarantees
// SUM(Q_i) == A, and A > 0 is already established by the early return at the top of this
// function. So a positive budget with targets summing to exactly 100% can never produce an
// empty buy list via this path; Java carries the same dead defensive branch, and this Go port
// preserves it byte-for-byte (see the `if qPos.Sign() == 0` guard in calculateBuyList) without a
// test that would require an artificially invariant-violating fixture to exercise it.

func TestCalculateBuyList_InvariantViolation_TargetsDoNotSumTo100(t *testing.T) {
	rows := []PositionRow{
		{Ticker: "A", TargetAllocationPct: target("60")},
		{Ticker: "B", TargetAllocationPct: target("60")}, // sums to 120, not 100
	}
	_, err := calculateBuyList(decimal.NewFromInt(100), rows, map[string]decimal.Decimal{}, plnRates(), map[string]decimal.Decimal{}, decimal.Zero, portfolioConfig{})
	if err == nil {
		t.Fatal("expected an invariant-violation error when targets do not sum to 100")
	}
}

func TestCalculateBuyList_CoverageAssertion_MissingOverrideTickerErrors(t *testing.T) {
	rows := threeAssetRows()
	overrides := map[string]decimal.Decimal{
		"A": decimal.RequireFromString("50"),
		"B": decimal.RequireFromString("30"),
		// "C" deliberately missing.
	}
	_, err := calculateBuyList(decimal.NewFromInt(100), rows, map[string]decimal.Decimal{}, plnRates(),
		map[string]decimal.Decimal{}, decimal.Zero, portfolioConfig{targetOverrides: overrides})
	if err == nil {
		t.Fatal("expected an error when the override map is missing a ticker present in rows")
	}
}

// TestCalculateBuyList_NeverProducesNegativeAllocation is the property test the phase task
// explicitly asks for (Non-negotiable correctness requirements #1): the old 2x-trim rule that
// could emit a negative allocation was deleted by ADR-045 and must never be reintroduced. Runs
// calculateBuyList over many pseudo-random current-value distributions (targets always sum to
// exactly 100, matching the real invariant every portfolio must satisfy) and asserts every
// resulting AllocatedPln/AllocatedNative/UnitsToBuy is >= 0.
func TestCalculateBuyList_NeverProducesNegativeAllocation(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	tickers := []string{"A", "B", "C", "D", "E"}
	targets := []string{"30", "25", "20", "15", "10"} // sums to 100

	for trial := range 200 {
		var rows []PositionRow
		valuePln := map[string]decimal.Decimal{}
		pricePlnByTicker := map[string]decimal.Decimal{}
		total := decimal.Zero
		for i, tk := range tickers {
			yahoo := tk + ".YAHOO"
			rows = append(rows, PositionRow{
				Ticker: tk, Name: tk, Exchange: "XTB", PriceCurrency: "PLN", PurchaseCurrency: "PLN",
				TargetAllocationPct: target(targets[i]), YahooTicker: &yahoo,
			})
			// Random current value between 0 and 10000 PLN, random price between 1 and 500.
			v := decimal.NewFromInt(int64(rng.Intn(10000)))
			valuePln[tk] = v
			total = total.Add(v)
			pricePlnByTicker[yahoo] = decimal.NewFromInt(int64(1 + rng.Intn(500)))
		}
		budget := decimal.NewFromInt(int64(rng.Intn(5000)))
		total = total.Add(budget)

		buyList, err := calculateBuyList(budget, rows, valuePln, plnRates(), pricePlnByTicker, total, portfolioConfig{convertChfNative: true})
		if err != nil {
			t.Fatalf("trial %d: unexpected error: %v", trial, err)
		}
		for _, item := range buyList {
			if item.AllocatedPln.Sign() < 0 {
				t.Fatalf("trial %d: negative AllocatedPln for %s: %s", trial, item.Ticker, item.AllocatedPln)
			}
			if item.AllocatedNative.Sign() < 0 {
				t.Fatalf("trial %d: negative AllocatedNative for %s: %s", trial, item.Ticker, item.AllocatedNative)
			}
			if item.UnitsToBuy.Sign() < 0 {
				t.Fatalf("trial %d: negative UnitsToBuy for %s: %s", trial, item.Ticker, item.UnitsToBuy)
			}
		}
	}
}

func TestComputeOverrides_SumConserving(t *testing.T) {
	rows := []PositionRow{
		{Ticker: "A", TargetAllocationPct: target("50"), TargetAllocationPctRiskOff: target("20")},
		{Ticker: "B", TargetAllocationPct: target("30"), TargetAllocationPctRiskOff: target("30")},
		{Ticker: "C", TargetAllocationPct: target("20"), TargetAllocationPctRiskOff: target("50")},
	}
	for _, blendStr := range []string{"0", "0.25", "0.5", "0.75", "1"} {
		blend := decimal.RequireFromString(blendStr)
		overrides := computeOverrides(rows, &blend)
		sum := decimal.Zero
		for _, v := range overrides {
			sum = sum.Add(v)
		}
		if !sum.Equal(decimal.NewFromInt(100)) {
			t.Fatalf("blend=%s: expected sum=100, got %s (overrides=%v)", blendStr, sum, overrides)
		}
	}
}

func TestComputeOverrides_NilBlendIsFullRiskOn(t *testing.T) {
	rows := []PositionRow{
		{Ticker: "A", TargetAllocationPct: target("70"), TargetAllocationPctRiskOff: target("10")},
	}
	overrides := computeOverrides(rows, nil)
	if got := overrides["A"]; !got.Equal(decimal.RequireFromString("70")) {
		t.Fatalf("expected nil blend to resolve to risk-on (70), got %s", got)
	}
}

func TestComputeOverrides_NullRiskOffFallsBackToRiskOn(t *testing.T) {
	rows := []PositionRow{
		{Ticker: "A", TargetAllocationPct: target("40")}, // TargetAllocationPctRiskOff nil
	}
	blend := decimal.RequireFromString("0.3")
	overrides := computeOverrides(rows, &blend)
	// riskOff falls back to riskOn (40) when unset -> effective is always 40 regardless of blend.
	if got := overrides["A"]; !got.Equal(decimal.RequireFromString("40")) {
		t.Fatalf("expected riskOff fallback to riskOn=40 regardless of blend, got %s", got)
	}
}

func TestComputeOverrides_ExcludesNonPrimarySubassets(t *testing.T) {
	// Non-primary subassets carry no target (targets live on the asset, riding on its primary
	// row) and must never appear in the override map: mainPrimaries is the selection.
	rows := []PositionRow{
		{Ticker: "A", Portfolio: PortfolioMAIN, IsPrimary: true, TargetAllocationPct: target("100")},
		{Ticker: "A-sub", Portfolio: PortfolioMAIN, IsPrimary: false, TargetAllocationPct: target("999")},
		{Ticker: "R", Portfolio: PortfolioRETIREMENT, IsPrimary: true, TargetAllocationPct: target("50")},
	}
	overrides := computeOverrides(mainPrimaries(rows), nil)
	if _, present := overrides["A-sub"]; present {
		t.Fatal("non-primary subassets must never appear in the override map")
	}
	if _, present := overrides["R"]; present {
		t.Fatal("RETIREMENT rows must never appear in the MAIN override map")
	}
	if len(overrides) != 1 {
		t.Fatalf("expected exactly 1 entry, got %d: %v", len(overrides), overrides)
	}
}
