package positioncalc

import (
	"testing"

	"github.com/shopspring/decimal"
)

// fixedUnits is a trivial UnitsTotaler stub — ComputeFields is a pure function so no DB/fakes
// are needed beyond returning a fixed units total.
type fixedUnits decimal.Decimal

func (f fixedUnits) UnitsTotal() decimal.Decimal { return decimal.Decimal(f) }

func TestComputeFields_ZeroTargetWithNonzeroValue_UsesSentinelDeviation(t *testing.T) {
	yahoo := "ZT.YAHOO"
	zero := decimal.Zero
	units := fixedUnits(decimal.NewFromInt(10))
	pricePlnByTicker := map[string]decimal.Decimal{yahoo: decimal.NewFromInt(5)} // 10 * 5 = 50 value, target=0
	ratesToPln := map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}
	totalPortfolioValuePln := decimal.NewFromInt(1000)

	computed := ComputeFields("T", &yahoo, "ETF", "PLN", &zero, units, nil, totalPortfolioValuePln, nil, pricePlnByTicker, ratesToPln)

	if computed.DeviationPct == nil {
		t.Fatalf("expected ADR-053 sentinel deviationPct, got nil")
	}
	if !computed.DeviationPct.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("expected ADR-053 sentinel deviationPct=1, got %v", computed.DeviationPct)
	}
}

func TestComputeFields_ZeroTargetWithNothingHeld_NoSentinel(t *testing.T) {
	yahoo := "ZT.YAHOO"
	zero := decimal.Zero
	units := fixedUnits(decimal.Zero)
	pricePlnByTicker := map[string]decimal.Decimal{yahoo: decimal.NewFromInt(5)}
	ratesToPln := map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}
	totalPortfolioValuePln := decimal.NewFromInt(1000)

	computed := ComputeFields("T", &yahoo, "ETF", "PLN", &zero, units, nil, totalPortfolioValuePln, nil, pricePlnByTicker, ratesToPln)

	if computed.DeviationPct != nil {
		t.Fatalf("expected nil deviationPct when nothing held, got %v", computed.DeviationPct)
	}
}

func TestComputeFields_NilTarget_Unaffected(t *testing.T) {
	yahoo := "NT.YAHOO"
	units := fixedUnits(decimal.NewFromInt(10))
	pricePlnByTicker := map[string]decimal.Decimal{yahoo: decimal.NewFromInt(5)}
	ratesToPln := map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}
	totalPortfolioValuePln := decimal.NewFromInt(1000)

	computed := ComputeFields("T", &yahoo, "ETF", "PLN", nil, units, nil, totalPortfolioValuePln, nil, pricePlnByTicker, ratesToPln)

	if computed.DeviationPct != nil {
		t.Fatalf("expected nil deviationPct for nil target, got %v", computed.DeviationPct)
	}
}

func TestComputeFields_NormalNonzeroTarget_ComputesProportionalDeviation(t *testing.T) {
	yahoo := "NORM.YAHOO"
	target := decimal.NewFromInt(10)
	units := fixedUnits(decimal.NewFromInt(10))
	pricePlnByTicker := map[string]decimal.Decimal{yahoo: decimal.NewFromInt(5)} // value=50
	ratesToPln := map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}
	totalPortfolioValuePln := decimal.NewFromInt(1000) // currentPct = 5%

	computed := ComputeFields("T", &yahoo, "ETF", "PLN", &target, units, nil, totalPortfolioValuePln, nil, pricePlnByTicker, ratesToPln)

	if computed.CurrentPct == nil || !computed.CurrentPct.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("expected currentPct=5, got %v", computed.CurrentPct)
	}
	// deviationPct = (currentPct / targetPct) - 1 = (5/10) - 1 = -0.5
	if computed.DeviationPct == nil || !computed.DeviationPct.Equal(decimal.NewFromFloat(-0.5)) {
		t.Fatalf("expected deviationPct=-0.5, got %v", computed.DeviationPct)
	}
}

func dp(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

// TestGroupProfit is task 161a-1's table (rule corrected 2026-09-25): profit = Σvalue / Σcost − 1
// where cost sums only the subassets with a recorded cost > 0 and value sums EVERY priced
// subasset, because the go-live import stored the whole group's cost on the head row and left the
// other rows' cost NULL. A costed-but-unpriced subasset contributes neither side.
func TestGroupProfit(t *testing.T) {
	tests := []struct {
		name string
		subs []SubCost
		want string // "" = not ok (profit unknown)
	}{
		{name: "single subasset, plain profit", subs: []SubCost{{ValuePln: dp("120"), TotalSpentPln: dp("100")}}, want: "0.2"},
		{name: "single subasset, loss", subs: []SubCost{{ValuePln: dp("90"), TotalSpentPln: dp("100")}}, want: "-0.1"},
		{
			// XRP-shaped (task 161 D1): primary 1000 for 900 cost, ETN 250 for 225 cost -> the group
			// made 1250/1125 - 1 = +11.11%, NOT the primary-only 1250/900 - 1 = +38.9%.
			name: "XRP-shaped: primary + ETN both with own cost -> group profit",
			subs: []SubCost{{ValuePln: dp("1000"), TotalSpentPln: dp("900")}, {ValuePln: dp("250"), TotalSpentPln: dp("225")}},
			want: "0.11111111",
		},
		{
			// Bitcoin's head row holds the cost of physical
			// BTC + the ETN together (100000); the ETN row's cost is NULL. Old rule gave -40 %.
			name: "Bitcoin-shaped: group cost on the head, ETN cost NULL",
			subs: []SubCost{{ValuePln: dp("60000"), TotalSpentPln: dp("100000")}, {ValuePln: dp("20000"), TotalSpentPln: nil}},
			want: "-0.2",
		},
		{
			name: "Gold-shaped: one head cost, two NULL-cost siblings",
			subs: []SubCost{{ValuePln: dp("40000"), TotalSpentPln: dp("50000")}, {ValuePln: dp("20000"), TotalSpentPln: nil}, {ValuePln: dp("15000"), TotalSpentPln: nil}},
			want: "0.5",
		},
		{
			name: "Quantum-shaped: head cost, one NULL-cost sibling",
			subs: []SubCost{{ValuePln: dp("800"), TotalSpentPln: dp("1000")}, {ValuePln: dp("400"), TotalSpentPln: nil}},
			want: "0.2",
		},
		{
			// XRP-shaped: the companion carries its own recorded post-go-live cost, which must stay in
			// the denominator (the original bug this helper fixed).
			name: "XRP-shaped: companion with its own cost stays in the denominator",
			subs: []SubCost{{ValuePln: dp("330"), TotalSpentPln: dp("300")}, {ValuePln: dp("270"), TotalSpentPln: dp("200")}},
			want: "0.2",
		},
		{
			name: "nil-cost subasset: its value counts, it adds no cost",
			subs: []SubCost{{ValuePln: dp("1200"), TotalSpentPln: dp("1000")}, {ValuePln: dp("300"), TotalSpentPln: nil}},
			want: "0.5",
		},
		{
			name: "zero-cost subasset adds value but no cost",
			subs: []SubCost{{ValuePln: dp("1200"), TotalSpentPln: dp("1000")}, {ValuePln: dp("300"), TotalSpentPln: dp("0")}},
			want: "0.5",
		},
		{
			name: "negative-cost subasset adds value but no cost (never subtracts from the denominator)",
			subs: []SubCost{{ValuePln: dp("1200"), TotalSpentPln: dp("1000")}, {ValuePln: dp("300"), TotalSpentPln: dp("-10")}},
			want: "0.5",
		},
		{
			name: "costed subasset that cannot be priced contributes neither cost nor value",
			subs: []SubCost{{ValuePln: dp("1000"), TotalSpentPln: dp("900")}, {ValuePln: nil, TotalSpentPln: dp("225")}},
			want: "0.11111111",
		},
		{
			name: "unpriced-with-cost beside a priced nil-cost sibling",
			subs: []SubCost{{ValuePln: dp("1000"), TotalSpentPln: dp("900")}, {ValuePln: nil, TotalSpentPln: dp("225")}, {ValuePln: dp("100"), TotalSpentPln: nil}},
			want: "0.22222222",
		},
		{name: "total wipe-out is -100%", subs: []SubCost{{ValuePln: dp("0"), TotalSpentPln: dp("100")}}, want: "-1"},
		{name: "all cost NULL", subs: []SubCost{{ValuePln: dp("1000"), TotalSpentPln: nil}, {ValuePln: dp("250"), TotalSpentPln: nil}}},
		{name: "costless singleton", subs: []SubCost{{ValuePln: dp("100"), TotalSpentPln: nil}}},
		{name: "all cost zero", subs: []SubCost{{ValuePln: dp("1000"), TotalSpentPln: dp("0")}}},
		{name: "only a negative cost", subs: []SubCost{{ValuePln: dp("1000"), TotalSpentPln: dp("-10")}}},
		{name: "no subassets", subs: nil},
		{name: "costed but nothing priceable", subs: []SubCost{{ValuePln: nil, TotalSpentPln: dp("100")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := GroupProfit(tt.subs)
			if tt.want == "" {
				if ok {
					t.Fatalf("expected profit unknown, got %s", got)
				}
				return
			}
			if !ok {
				t.Fatalf("expected profit %s, got unknown", tt.want)
			}
			if !got.Equal(decimal.RequireFromString(tt.want)) {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

// TestGroupProfit_OrderIndependent is the flip guarantee at the pure level: profit reads the
// SET of subassets, so swapping which one is primary (reordering) cannot change it — including
// when the group cost sits on one row and the others carry NULL cost.
func TestGroupProfit_OrderIndependent(t *testing.T) {
	sets := map[string][]SubCost{
		"costed + costed + nil-cost": {{ValuePln: dp("1000"), TotalSpentPln: dp("900")}, {ValuePln: dp("250"), TotalSpentPln: dp("225")}, {ValuePln: dp("777"), TotalSpentPln: nil}},
		"Gold-shaped":                {{ValuePln: dp("40000"), TotalSpentPln: dp("50000")}, {ValuePln: dp("20000"), TotalSpentPln: nil}, {ValuePln: dp("15000"), TotalSpentPln: nil}},
		"with an unpriced costed":    {{ValuePln: dp("1000"), TotalSpentPln: dp("900")}, {ValuePln: nil, TotalSpentPln: dp("225")}, {ValuePln: dp("100"), TotalSpentPln: nil}},
	}
	for name, set := range sets {
		t.Run(name, func(t *testing.T) {
			want, wantOk := GroupProfit(set)
			if !wantOk {
				t.Fatalf("expected a profit for %s", name)
			}
			for _, perm := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
				reordered := []SubCost{set[perm[0]], set[perm[1]], set[perm[2]]}
				got, ok := GroupProfit(reordered)
				if !ok || !got.Equal(want) {
					t.Fatalf("profit must not depend on subasset order %v, got %s/%v vs %s", perm, got, ok, want)
				}
			}
		})
	}
}

// TestGroupProfit_PrimaryFlipDoesNotChangeProfit: the same two rows with the head role swapped —
// group cost on the head (BTC-shaped) vs the NULL-cost row promoted to head — give the identical
// profit (task 161 D3).
func TestGroupProfit_PrimaryFlipDoesNotChangeProfit(t *testing.T) {
	btc, etn := SubCost{ValuePln: dp("60000"), TotalSpentPln: dp("100000")}, SubCost{ValuePln: dp("20000")}
	before, ok1 := GroupProfit([]SubCost{btc, etn}) // BTC primary
	after, ok2 := GroupProfit([]SubCost{etn, btc})  // ETN primary
	if !ok1 || !ok2 || !before.Equal(after) {
		t.Fatalf("a primary flip must not change profit, got %s/%v vs %s/%v", before, ok1, after, ok2)
	}
}

func computeFor(primaryYahoo string, primaryUnits, primaryCost *string, companions []Companion, prices map[string]string) Computed {
	y := primaryYahoo
	pm := map[string]decimal.Decimal{}
	for k, v := range prices {
		pm[k] = decimal.RequireFromString(v)
	}
	var cost *decimal.Decimal
	if primaryCost != nil {
		cost = dp(*primaryCost)
	}
	return ComputeFields("P", &y, "CRYPTO", "PLN", nil, fixedUnits(decimal.RequireFromString(*primaryUnits)), cost,
		decimal.NewFromInt(100000), companions, pm, map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)})
}

// TestComputeFields_GroupProfitUsesAllSubassets: the XRP-shaped case end to end. Primary XRP
// 10u x 100 = 1000 (cost 900) + ETN 5u x 50 = 250 (cost 225): the profit is the GROUP's
// +11.11% (old code: primary cost only -> +38.9%), and valuePln is still the group's 1250.
func TestComputeFields_GroupProfitUsesAllSubassets(t *testing.T) {
	etn := "ETN.Y"
	got := computeFor("XRP.Y", new("10"), new("900"),
		[]Companion{{YahooTicker: &etn, UnitsTotal: decimal.NewFromInt(5), TotalSpentPln: dp("225")}},
		map[string]string{"XRP.Y": "100", "ETN.Y": "50"})

	if got.ValuePln == nil || !got.ValuePln.Equal(decimal.NewFromInt(1250)) {
		t.Fatalf("expected the group value 1250, got %v", got.ValuePln)
	}
	if got.ProfitPct == nil || !got.ProfitPct.Equal(decimal.RequireFromString("0.11111111")) {
		t.Fatalf("expected the GROUP profit 0.11111111, got %v", got.ProfitPct)
	}
	// avgBuyPricePln keeps its pre-161 derivation (primary price / (1 + profit)) on the corrected profit.
	want := decimal.NewFromInt(100).DivRound(decimal.NewFromInt(1).Add(decimal.RequireFromString("0.11111111")), 4)
	if got.AvgBuyPricePln == nil || !got.AvgBuyPricePln.Equal(want) {
		t.Fatalf("expected avgBuyPricePln %s, got %v", want, got.AvgBuyPricePln)
	}
}

// TestComputeFields_FlipKeepsGroupProfit: the same two rows with the primary role swapped give
// the identical profit and group value (task 161 D3 — a flip must never move profit).
func TestComputeFields_FlipKeepsGroupProfit(t *testing.T) {
	prices := map[string]string{"XRP.Y": "100", "ETN.Y": "50"}
	xrp, etn := "XRP.Y", "ETN.Y"
	before := computeFor("XRP.Y", new("10"), new("900"),
		[]Companion{{YahooTicker: &etn, UnitsTotal: decimal.NewFromInt(5), TotalSpentPln: dp("225")}}, prices)
	after := computeFor("ETN.Y", new("5"), new("225"),
		[]Companion{{YahooTicker: &xrp, UnitsTotal: decimal.NewFromInt(10), TotalSpentPln: dp("900")}}, prices)

	if before.ProfitPct == nil || after.ProfitPct == nil || !before.ProfitPct.Equal(*after.ProfitPct) {
		t.Fatalf("profit must be identical across a flip, got %v vs %v", before.ProfitPct, after.ProfitPct)
	}
	if !before.ValuePln.Equal(*after.ValuePln) {
		t.Fatalf("group value must be identical across a flip, got %v vs %v", before.ValuePln, after.ValuePln)
	}
}

// TestComputeFields_SingleSubassetProfitUnchanged pins the pre-161 single-row numbers
// byte-for-byte: valuePln / cost - 1 at 8 decimals, avgBuyPricePln = price / (1 + profit) at 4.
func TestComputeFields_SingleSubassetProfitUnchanged(t *testing.T) {
	got := computeFor("A.Y", new("7"), new("300"), nil, map[string]string{"A.Y": "50"}) // value 350, cost 300
	if got.ProfitPct == nil || got.ProfitPct.String() != "0.16666667" {
		t.Fatalf("expected profit 0.16666667, got %v", got.ProfitPct)
	}
	if got.AvgBuyPricePln == nil || got.AvgBuyPricePln.String() != "42.8571" {
		t.Fatalf("expected avgBuyPricePln 42.8571, got %v", got.AvgBuyPricePln)
	}
}

func TestComputeFields_UnknownCostsStayNil(t *testing.T) {
	etn := "ETN.Y"
	prices := map[string]string{"XRP.Y": "100", "ETN.Y": "50"}
	// Nothing has a cost -> no profit and no avg price (portfolioB's all-NULL case).
	got := computeFor("XRP.Y", new("10"), nil, []Companion{{YahooTicker: &etn, UnitsTotal: decimal.NewFromInt(5)}}, prices)
	if got.ProfitPct != nil || got.AvgBuyPricePln != nil {
		t.Fatalf("all-NULL cost must keep profit/avg nil, got %v / %v", got.ProfitPct, got.AvgBuyPricePln)
	}
	// Only the companion has a cost (2026-09-25 rule): the group's whole value (primary 1000 +
	// companion 250) sits over that group cost — the primary's NULL cost is assumed covered by it.
	got = computeFor("XRP.Y", new("10"), nil, []Companion{{YahooTicker: &etn, UnitsTotal: decimal.NewFromInt(5), TotalSpentPln: dp("225")}}, prices)
	if got.ProfitPct == nil || !got.ProfitPct.Equal(decimal.RequireFromString("4.55555556")) {
		t.Fatalf("expected profit over the whole group value / the companion's cost, got %v", got.ProfitPct)
	}
}

// TestComputeFields_BitcoinShaped_GroupCostOnHead: the go-live shape end to end (2026-09-25 rule).
// The head BTC row carries the WHOLE group's cost (100000); the ETN companion has NULL cost. Profit
// is the group's value over that cost (-20 %, not the old head-only -40 %), and promoting the ETN to
// primary (cost now on the companion) yields the identical profit and group value.
func TestComputeFields_BitcoinShaped_GroupCostOnHead(t *testing.T) {
	prices := map[string]string{"BTC.Y": "100", "ETN.Y": "100"}
	btc, etn := "BTC.Y", "ETN.Y"
	before := computeFor("BTC.Y", new("600"), new("100000"),
		[]Companion{{YahooTicker: &etn, UnitsTotal: decimal.RequireFromString("200")}}, prices)
	after := computeFor("ETN.Y", new("200"), nil,
		[]Companion{{YahooTicker: &btc, UnitsTotal: decimal.RequireFromString("600"), TotalSpentPln: dp("100000")}}, prices)

	want := decimal.RequireFromString("-0.2")
	for label, c := range map[string]Computed{"BTC primary": before, "ETN primary": after} {
		if c.ProfitPct == nil || !c.ProfitPct.Equal(want) {
			t.Fatalf("%s: expected group profit %s, got %v", label, want, c.ProfitPct)
		}
		if c.ValuePln == nil || !c.ValuePln.Equal(decimal.NewFromInt(80000)) {
			t.Fatalf("%s: expected group value 80000, got %v", label, c.ValuePln)
		}
	}
}
