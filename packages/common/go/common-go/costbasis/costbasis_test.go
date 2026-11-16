package costbasis

import (
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/positioncalc"
)

// ---------------------------------------------------------------------------------------------
// helpers (test-local; the kernel itself has no notion of "primary", profit or entry price)
// ---------------------------------------------------------------------------------------------

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func ptr(s string) *decimal.Decimal {
	if s == "" {
		return nil
	}
	v := d(s)
	return &v
}

// mk builds a Row; price/cost "" mean nil (unpriced / NULL cost).
func mk(id, asset int64, units, price, cost string) Row {
	return Row{PositionID: id, AssetID: asset, Units: d(units), PricePln: ptr(price), CostPln: ptr(cost)}
}

func byID(rows []Row, id int64) Row {
	for _, r := range rows {
		if r.PositionID == id {
			return r
		}
	}
	panic("row not found")
}

func mustCost(t *testing.T, rows []Row, id int64, want string) {
	t.Helper()
	got := byID(rows, id).CostPln
	if got == nil {
		t.Fatalf("position %d: cost is NULL, want %s", id, want)
	}
	if !got.Equal(d(want)) {
		t.Fatalf("position %d: cost = %s, want %s", id, got, want)
	}
}

func mustNullCost(t *testing.T, rows []Row, id int64) {
	t.Helper()
	if got := byID(rows, id).CostPln; got != nil {
		t.Fatalf("position %d: cost = %s, want NULL", id, got)
	}
}

// sumCost is the meaningful number: Σ recorded cost over the asset's rows.
func sumCost(rows []Row) decimal.Decimal {
	s := decimal.Zero
	for _, r := range rows {
		if r.CostPln != nil {
			s = s.Add(*r.CostPln)
		}
	}
	return s
}

// afterUnits returns a copy of rows with `delta` units added to position id — the caller-side
// "apply the trade's units" step (the kernel returns rows with the units it was given).
func afterUnits(rows []Row, id int64, delta string) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	for i := range out {
		if out[i].PositionID == id {
			out[i].Units = out[i].Units.Add(d(delta))
		}
	}
	return out
}

// withPrice returns a copy with position id's price replaced.
func withPrice(rows []Row, id int64, price string) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	for i := range out {
		if out[i].PositionID == id {
			out[i].PricePln = ptr(price)
		}
	}
	return out
}

func subs(rows []Row) []positioncalc.SubCost {
	out := make([]positioncalc.SubCost, 0, len(rows))
	for _, r := range rows {
		sc := positioncalc.SubCost{TotalSpentPln: r.CostPln}
		if r.PricePln != nil {
			v := r.Units.Mul(*r.PricePln)
			sc.ValuePln = &v
		}
		out = append(out, sc)
	}
	return out
}

// profitPct is positioncalc.GroupProfit over the rows (units as given = post-trade units).
func profitPct(t *testing.T, rows []Row) decimal.Decimal {
	t.Helper()
	p, ok := positioncalc.GroupProfit(subs(rows))
	if !ok {
		t.Fatalf("GroupProfit not ok (Σcost <= 0) for %+v", rows)
	}
	return p
}

// entryPricePln is design §1.1: price_pln(P) / (1 + profit).
func entryPricePln(t *testing.T, rows []Row, primaryID int64) decimal.Decimal {
	t.Helper()
	p := byID(rows, primaryID).PricePln
	if p == nil {
		t.Fatalf("primary %d unpriced", primaryID)
	}
	return p.DivRound(decimal.NewFromInt(1).Add(profitPct(t, rows)), 8)
}

// within asserts |got-want| <= tol * |want| (relative tolerance, e.g. "0.005" = 0.5 %).
func within(t *testing.T, what string, got decimal.Decimal, want, tol string) {
	t.Helper()
	w := d(want)
	if got.Sub(w).Abs().Cmp(w.Abs().Mul(d(tol))) > 0 {
		t.Fatalf("%s = %s, want %s (+/- %s of it)", what, got, want, tol)
	}
}

func near(t *testing.T, what string, got decimal.Decimal, want, absTol string) {
	t.Helper()
	if got.Sub(d(want)).Abs().Cmp(d(absTol)) > 0 {
		t.Fatalf("%s = %s, want %s (+/- %s)", what, got, want, absTol)
	}
}

// ownerAsset is design §1.3-A at "today" prices: 0.5 BTC (cost 200 000) + 5 000 ETN (cost 100 000),
// BTC 480 000 PLN, ETN 48 PLN. V = 480 000, C = 300 000, profit +60 %, entry 300 000 PLN.
func ownerAsset() []Row {
	return []Row{
		mk(1, 1, "0.5", "480000", "200000"),
		mk(2, 1, "5000", "48", "100000"),
	}
}

// ---------------------------------------------------------------------------------------------
// S1 - partial sale: cost x (1-f), entry price and profit unchanged
// ---------------------------------------------------------------------------------------------

func TestApplySale_Partial_ScalesCostAndKeepsProfitAndEntry(t *testing.T) {
	tests := []struct {
		name         string
		sale         Sale
		unitsDelta   string // for the sold position
		wantBTC      string
		wantETN      string
		wantSumAfter string
	}{
		{"sell 2500 ETN (f=0.25)", Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")}, "-2500", "150000", "75000", "225000"},
		{"sell 0.1 BTC (f=0.10)", Sale{PositionID: 1, Units: d("0.1"), PricePln: d("480000")}, "-0.1", "180000", "90000", "270000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := ownerAsset()
			out := ApplySale(in, tc.sale)
			mustCost(t, out, 1, tc.wantBTC)
			mustCost(t, out, 2, tc.wantETN)
			if got := sumCost(out); !got.Equal(d(tc.wantSumAfter)) {
				t.Fatalf("Σcost = %s, want %s", got, tc.wantSumAfter)
			}
			post := afterUnits(out, tc.sale.PositionID, tc.unitsDelta)
			if got := profitPct(t, post); !got.Equal(d("0.6")) {
				t.Fatalf("profit after = %s, want unchanged 0.6", got)
			}
			if got := entryPricePln(t, post, 1); !got.Equal(d("300000")) {
				t.Fatalf("entry after = %s, want unchanged 300000", got)
			}
		})
	}
}

func TestApplySale_DoesNotMutateInput(t *testing.T) {
	in := ownerAsset()
	_ = ApplySale(in, Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")})
	mustCost(t, in, 1, "200000")
	mustCost(t, in, 2, "100000")
	if in[0].Units.String() != "0.5" || in[1].Units.String() != "5000" {
		t.Fatalf("input units mutated: %+v", in)
	}
}

func TestApplySale_NoOps(t *testing.T) {
	tests := []struct {
		name string
		rows []Row
		sale Sale
	}{
		{"zero units", ownerAsset(), Sale{PositionID: 2, Units: d("0"), PricePln: d("48")}},
		{"negative units", ownerAsset(), Sale{PositionID: 2, Units: d("-1"), PricePln: d("48")}},
		{"zero price", ownerAsset(), Sale{PositionID: 2, Units: d("10"), PricePln: d("0")}},
		{"unknown position", ownerAsset(), Sale{PositionID: 99, Units: d("10"), PricePln: d("48")}},
		{"untracked asset (all NULL cost)", []Row{mk(1, 1, "10", "100", ""), mk(2, 1, "5", "100", "")}, Sale{PositionID: 1, Units: d("4"), PricePln: d("100")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := ApplySale(tc.rows, tc.sale)
			if len(ChangedRows(tc.rows, out)) != 0 {
				t.Fatalf("expected no change, got %+v", ChangedRows(tc.rows, out))
			}
		})
	}
}

// ---------------------------------------------------------------------------------------------
// S2 / S3 / S4 - group fixtures (synthetic, shaped like design §2.2, tolerance +/-0.5 %)
// ---------------------------------------------------------------------------------------------

// btcMain: cost 100 000 sits on BTC-USD (the go-live head row), the ETN row has NULL cost.
// BTC-USD 0.2 @ 300 000 = 60 000; ETN 500 @ 40 = 20 000; V = 80 000; true profit -20 %.
func btcMain() []Row {
	return []Row{
		mk(10, 100, "0.2", "300000", "100000"),
		mk(11, 100, "500", "40", ""),
	}
}

func goldMain() []Row {
	// ETC 1000 @ 40 = 40 000 (cost 50 000 on the head), 4GLD.DE 100 @ 200 = 20 000,
	// IGLN.L 1000 @ 15 = 15 000; V = 75 000; profit +50 %.
	return []Row{
		mk(20, 200, "1000", "40", "50000"),
		mk(21, 200, "100", "200", ""),
		mk(22, 200, "1000", "15", ""),
	}
}

func TestApplySale_GroupFixture_BTC_SellWholeETN(t *testing.T) {
	rows := btcMain()
	if p := profitPct(t, rows); p.Sub(d("-0.20")).Abs().Cmp(d("0.001")) > 0 {
		t.Fatalf("fixture profit before = %s, want about -20 %%", p)
	}
	out := ApplySale(rows, Sale{PositionID: 11, Units: d("500"), PricePln: d("40")})
	within(t, "cost after selling the whole ETN", sumCost(out), "75000", "0.005")
	mustNullCost(t, out, 11) // NULL stays NULL
	post := afterUnits(out, 11, "-500")
	near(t, "profit after", profitPct(t, post), "-0.20", "0.001") // -20 %, NOT the head-only -40 %
}

func TestApplySale_GroupFixture_BTC_SellHead001(t *testing.T) {
	rows := btcMain()
	out := ApplySale(rows, Sale{PositionID: 10, Units: d("0.01"), PricePln: d("300000")}) // 3 000 PLN
	within(t, "cost after selling 0.01 BTC", sumCost(out), "96250", "0.005")
	post := afterUnits(out, 10, "-0.01")
	near(t, "profit after", profitPct(t, post), "-0.20", "0.001") // unchanged by a partial sale
}

func TestApplySale_GroupFixture_Gold_SellAll4GLD_NullCostSibling(t *testing.T) {
	rows := goldMain()
	if p := profitPct(t, rows); p.Sub(d("0.50")).Abs().Cmp(d("0.001")) > 0 {
		t.Fatalf("fixture profit before = %s, want about +50 %%", p)
	}
	out := ApplySale(rows, Sale{PositionID: 21, Units: d("100"), PricePln: d("200")})
	within(t, "gold cost after selling 4GLD.DE", sumCost(out), "36666.67", "0.005")
	mustNullCost(t, out, 21)
	mustNullCost(t, out, 22)
	// the head's cost is the one that scaled
	if byID(out, 20).CostPln == nil {
		t.Fatal("head cost must remain recorded")
	}
	post := afterUnits(out, 21, "-100")
	near(t, "profit after", profitPct(t, post), "0.50", "0.001") // +50 %, unchanged by the sale
}

// ---------------------------------------------------------------------------------------------
// S5 - full sale then rebuy: exact zero, no dust, rebuy starts fresh
// ---------------------------------------------------------------------------------------------

func TestSaleSequence_FullSaleGivesExactZero_ThenRebuyStartsFresh(t *testing.T) {
	// 3 units @ 33.33 with cost 100: selling one third at a time creates rounding dust unless the
	// last sale zeroes the cost exactly.
	rows := []Row{mk(1, 1, "3", "33.33", "100")}
	for range 3 {
		rows = ApplySale(rows, Sale{PositionID: 1, Units: d("1"), PricePln: d("33.33")})
		rows = afterUnits(rows, 1, "-1")
	}
	c := byID(rows, 1).CostPln
	if c == nil || !c.IsZero() {
		t.Fatalf("cost after selling everything = %v, want exactly 0", c)
	}
	if _, ok := positioncalc.GroupProfit(subs(rows)); ok {
		t.Fatal("entry/profit must be absent for an empty asset")
	}

	// Rebuy 1 unit @ 40: fresh start, entry = rebuy price.
	rows = ApplyBuy(rows, 1, d("40"))
	rows = afterUnits(rows, 1, "1")
	rows = withPrice(rows, 1, "40")
	mustCost(t, rows, 1, "40")
	if got := entryPricePln(t, rows, 1); !got.Equal(d("40")) {
		t.Fatalf("entry after rebuy = %s, want 40", got)
	}
}

func TestSaleSequence_FullSaleAcrossRows_AllRecordedCostsExactlyZero(t *testing.T) {
	rows := goldMain()
	rows = ApplySale(rows, Sale{PositionID: 21, Units: d("100"), PricePln: d("200")})
	rows = afterUnits(rows, 21, "-100")
	rows = ApplySale(rows, Sale{PositionID: 22, Units: d("1000"), PricePln: d("15")})
	rows = afterUnits(rows, 22, "-1000")
	if sumCost(rows).IsZero() {
		t.Fatal("cost must remain while the head still holds units")
	}
	rows = ApplySale(rows, Sale{PositionID: 20, Units: d("1000"), PricePln: d("40")})
	rows = afterUnits(rows, 20, "-1000")
	mustCost(t, rows, 20, "0")
	mustNullCost(t, rows, 21)
	mustNullCost(t, rows, 22)
	if !sumCost(rows).IsZero() {
		t.Fatalf("Σcost = %s, want exactly 0", sumCost(rows))
	}
}

// ---------------------------------------------------------------------------------------------
// S6 - partial sale then rebuy: 80k -> sell 0.5 -> rebuy 0.5 @100k = 90 000
// ---------------------------------------------------------------------------------------------

func TestSaleThenRebuy_BlendsToWeightedAverage(t *testing.T) {
	rows := []Row{mk(1, 1, "1", "80000", "80000")}
	rows = ApplySale(rows, Sale{PositionID: 1, Units: d("0.5"), PricePln: d("80000")})
	rows = afterUnits(rows, 1, "-0.5")
	mustCost(t, rows, 1, "40000")
	if got := entryPricePln(t, rows, 1); !got.Equal(d("80000")) {
		t.Fatalf("entry after the partial sale = %s, want unchanged 80000", got)
	}

	rows = ApplyBuy(rows, 1, d("50000")) // 0.5 @ 100 000
	rows = afterUnits(rows, 1, "0.5")
	rows = withPrice(rows, 1, "100000")
	mustCost(t, rows, 1, "90000")
	if got := entryPricePln(t, rows, 1).Round(2); !got.Equal(d("90000")) {
		t.Fatalf("entry after rebuy = %s, want 90000", got)
	}
}

// ---------------------------------------------------------------------------------------------
// S7 - same-asset swap carries cost (D3, gamma = 1): 345 000 must NOT be produced
// ---------------------------------------------------------------------------------------------

func TestPlanTransfer_SameAssetSwap_CarriesCost(t *testing.T) {
	assets := map[int64][]Row{1: ownerAsset()}
	out := PlanTransfer(assets, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")}, // S = 120 000, f = 0.25
		Buys: []Leg{{PositionID: 1, AssetID: 1, Units: d("0.25"), PricePln: d("480000")}},
	})
	rows := out[1]
	mustCost(t, rows, 1, "225000") // 150 000 scaled + 75 000 carried
	mustCost(t, rows, 2, "75000")
	if got := sumCost(rows); !got.Equal(d("300000")) {
		t.Fatalf("asset cost = %s, want unchanged 300000 (a realization rule would give 345000)", got)
	}
	if sumCost(rows).Equal(d("345000")) {
		t.Fatal("345000 must never be produced")
	}
	post := afterUnits(afterUnits(rows, 2, "-2500"), 1, "0.25")
	if got := entryPricePln(t, post, 1); !got.Equal(d("300000")) {
		t.Fatalf("entry after the swap = %s, want 300000", got)
	}
	if got := profitPct(t, post); !got.Equal(d("0.6")) {
		t.Fatalf("profit after the swap = %s, want 0.6", got)
	}
}

func TestPlanTransfer_SameAssetSwap_CarriedCostSplitProRataAcrossLegs(t *testing.T) {
	asset := append(ownerAsset(), mk(3, 1, "10", "100", "")) // a third subasset, NULL cost
	out := PlanTransfer(map[int64][]Row{1: asset}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")}, // S = 120 000
		Buys: []Leg{
			{PositionID: 1, AssetID: 1, Units: d("0.125"), PricePln: d("480000")}, // 60 000
			{PositionID: 3, AssetID: 1, Units: d("600"), PricePln: d("100")},      // 60 000
		},
	})
	rows := out[1]
	// V = 481 000, f = 120/481, released Q = 300 000 - round(300 000 x 361/481) = 74 844.0748;
	// both legs are worth 60 000 so each receives half of it: 37 422.0374 (the NULL row is
	// initialized by the carry alone).
	if got := sumCost(rows); !got.Equal(d("300000")) {
		t.Fatalf("Σcost = %s, want conserved 300000", got)
	}
	near(t, "carried onto the NULL-cost leg row", *byID(rows, 3).CostPln, "37422.0374", "0.0001")
	mustCost(t, rows, 1, "187525.9875") // 150 103.9501 scaled + 37 422.0374 carried
}

// ---------------------------------------------------------------------------------------------
// S8 - mixed swap (gamma = 0.4 + cross-asset leg), top-up (V_same > P), fee imbalance (V_same < P)
// ---------------------------------------------------------------------------------------------

func TestPlanTransfer_MixedSwap_Gamma04_PlusCrossAssetGoldLeg(t *testing.T) {
	gold := []Row{mk(30, 2, "100", "1000", "60000")}
	out := PlanTransfer(map[int64][]Row{1: ownerAsset(), 2: gold}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")}, // S = 120 000, f = 0.25, Q = 75 000
		Buys: []Leg{
			{PositionID: 1, AssetID: 1, Units: d("0.1"), PricePln: d("480000")}, // V_same = 48 000 -> gamma = 0.4
			{PositionID: 30, AssetID: 2, Units: d("72"), PricePln: d("1000")},   // gold, 72 000 at market
		},
	})
	btc := out[1]
	if got := sumCost(btc); !got.Equal(d("255000")) { // 225 000 + 0.4 x 75 000
		t.Fatalf("asset cost = %s, want 255000", got)
	}
	mustCost(t, btc, 1, "180000") // 150 000 scaled + 30 000 carried
	mustCost(t, btc, 2, "75000")
	post := afterUnits(afterUnits(btc, 2, "-2500"), 1, "0.1")
	if got := profitPct(t, post); !got.Equal(d("0.6")) {
		t.Fatalf("profit = %s, want 0.6 (V 408 000 / C 255 000)", got)
	}
	if got := entryPricePln(t, post, 1); !got.Equal(d("300000")) {
		t.Fatalf("entry = %s, want 300000 preserved", got)
	}
	mustCost(t, out[2], 30, "132000") // gold bought at market: 60 000 + 72 000
}

func TestPlanTransfer_TopUp_VSameExceedsProceeds_BlendsExcessAtMarket(t *testing.T) {
	out := PlanTransfer(map[int64][]Row{1: ownerAsset()}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("1000"), PricePln: d("48")},                    // S = 48 000, f = 0.1, Q = 30 000
		Buys: []Leg{{PositionID: 1, AssetID: 1, Units: d("0.15"), PricePln: d("480000")}}, // 72 000 -> 24 000 top-up
	})
	rows := out[1]
	mustCost(t, rows, 1, "234000") // 180 000 + 30 000 carried + 24 000 market top-up
	mustCost(t, rows, 2, "90000")
	if got := sumCost(rows); !got.Equal(d("324000")) {
		t.Fatalf("asset cost = %s, want 324000", got)
	}
}

func TestPlanTransfer_FeeImbalance_UnmatchedShareIsRealized(t *testing.T) {
	out := PlanTransfer(map[int64][]Row{1: ownerAsset()}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")},                   // S = 120 000, Q = 75 000
		Buys: []Leg{{PositionID: 1, AssetID: 1, Units: d("0.2"), PricePln: d("480000")}}, // 96 000 -> gamma 0.8
	})
	rows := out[1]
	if got := sumCost(rows); !got.Equal(d("285000")) { // 225 000 + 0.8 x 75 000
		t.Fatalf("asset cost = %s, want 285000", got)
	}
	post := afterUnits(afterUnits(rows, 2, "-2500"), 1, "0.2")
	if got := profitPct(t, post); !got.Equal(d("0.6")) {
		t.Fatalf("profit = %s, want 0.6 preserved", got)
	}
	if got := entryPricePln(t, post, 1); !got.Equal(d("300000")) {
		t.Fatalf("entry = %s, want 300000 preserved", got)
	}
}

func TestPlanTransfer_TopUp_OnUntrackedAsset_InventsNothing(t *testing.T) {
	asset := []Row{mk(1, 1, "0.5", "480000", ""), mk(2, 1, "5000", "48", "")}
	out := PlanTransfer(map[int64][]Row{1: asset}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("1000"), PricePln: d("48")},
		Buys: []Leg{{PositionID: 1, AssetID: 1, Units: d("0.15"), PricePln: d("480000")}},
	})
	mustNullCost(t, out[1], 1)
	mustNullCost(t, out[1], 2)
}

// ---------------------------------------------------------------------------------------------
// S9 - cross-asset transfer: sold asset x (1-f), bought asset += paid (market)
// ---------------------------------------------------------------------------------------------

func TestPlanTransfer_CrossAsset_SoldAtOwnEntryAndBuyAtMarket(t *testing.T) {
	eth := []Row{mk(40, 2, "10", "10000", "90000")}
	out := PlanTransfer(map[int64][]Row{1: ownerAsset(), 2: eth}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")}, // S = 120 000, f = 0.25
		Buys: []Leg{{PositionID: 40, AssetID: 2, Units: d("12"), PricePln: d("10000")}},
	})
	if got := sumCost(out[1]); !got.Equal(d("225000")) {
		t.Fatalf("sold asset cost = %s, want 225000 (x 0.75)", got)
	}
	post := afterUnits(out[1], 2, "-2500")
	if got := entryPricePln(t, post, 1); !got.Equal(d("300000")) {
		t.Fatalf("sold asset entry = %s, want unchanged 300000", got)
	}
	mustCost(t, out[2], 40, "210000") // 90 000 + 12 x 10 000 paid
}

func TestPlanTransfer_CrossAsset_BuyOnUntrackedHoldingAsset_InventsNothing(t *testing.T) {
	eth := []Row{mk(40, 2, "10", "10000", "")}
	out := PlanTransfer(map[int64][]Row{1: ownerAsset(), 2: eth}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")},
		Buys: []Leg{{PositionID: 40, AssetID: 2, Units: d("1"), PricePln: d("10000")}},
	})
	mustNullCost(t, out[2], 40)
}

func TestPlanTransfer_CrossAsset_BuyOnEmptyAsset_StartsTracking(t *testing.T) {
	eth := []Row{mk(40, 2, "0", "10000", "")}
	out := PlanTransfer(map[int64][]Row{1: ownerAsset(), 2: eth}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")},
		Buys: []Leg{{PositionID: 40, AssetID: 2, Units: d("3"), PricePln: d("10000")}},
	})
	mustCost(t, out[2], 40, "30000")
}

func TestPlanTransfer_UnknownSoldAsset_BuysStillMarket(t *testing.T) {
	eth := []Row{mk(40, 2, "10", "10000", "90000")}
	out := PlanTransfer(map[int64][]Row{2: eth}, 1, Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")},
		Buys: []Leg{{PositionID: 40, AssetID: 2, Units: d("1"), PricePln: d("10000")}},
	})
	if _, ok := out[1]; ok {
		t.Fatal("unknown sold asset must not appear in the result")
	}
	mustCost(t, out[2], 40, "100000")
}

// ---------------------------------------------------------------------------------------------
// S10 - same instrument REGULAR -> IKZE: no change
// ---------------------------------------------------------------------------------------------

func TestPlanTransfer_SameInstrumentBucketMove_NoChange(t *testing.T) {
	rows := []Row{mk(1, 1, "100", "10", "900")}
	out := PlanTransfer(map[int64][]Row{1: rows}, 1, Transfer{
		Sold: Sale{PositionID: 1, Units: d("40"), PricePln: d("10")},
		Buys: []Leg{{PositionID: 1, AssetID: 1, Units: d("40"), PricePln: d("10")}},
	})
	mustCost(t, out[1], 1, "900")
	if len(ChangedRows(rows, out[1])) != 0 {
		t.Fatalf("no row may be written, got %+v", ChangedRows(rows, out[1]))
	}
}

func TestPlanTransfer_SameInstrumentBucketMove_AwkwardRounding_TotalExactlyConserved(t *testing.T) {
	rows := []Row{mk(1, 1, "7", "3.1", "100.01"), mk(2, 1, "5", "3.1", "33.33")}
	out := PlanTransfer(map[int64][]Row{1: rows}, 1, Transfer{
		Sold: Sale{PositionID: 1, Units: d("3"), PricePln: d("3.1")},
		Buys: []Leg{{PositionID: 1, AssetID: 1, Units: d("3"), PricePln: d("3.1")}},
	})
	if got := sumCost(out[1]); !got.Equal(d("133.34")) {
		t.Fatalf("Σcost = %s, want exactly 133.34", got)
	}
}

// ---------------------------------------------------------------------------------------------
// S11 - flip of the primary between trades: the kernel is primary-blind, results identical
// ---------------------------------------------------------------------------------------------

func TestSequence_IndependentOfRowOrderAndPrimary(t *testing.T) {
	run := func(rows []Row) []Row {
		rows = ApplySale(rows, Sale{PositionID: 2, Units: d("1000"), PricePln: d("48")})
		rows = afterUnits(rows, 2, "-1000")
		// primary "flips" here - a pure relabel outside the kernel; rows carry no primary flag.
		rows = ApplyBuy(rows, 1, d("24000"))
		rows = afterUnits(rows, 1, "0.05")
		rows = ApplySale(rows, Sale{PositionID: 1, Units: d("0.2"), PricePln: d("480000")})
		return afterUnits(rows, 1, "-0.2")
	}
	forward := run(ownerAsset())
	reversed := run([]Row{ownerAsset()[1], ownerAsset()[0]})
	for _, id := range []int64{1, 2} {
		a, b := byID(forward, id).CostPln, byID(reversed, id).CostPln
		if a == nil || b == nil || !a.Equal(*b) {
			t.Fatalf("position %d: forward cost %v != reversed cost %v", id, a, b)
		}
	}
	// entry price for either primary is p_P x C/V - the ratio of the two equals the price ratio.
	e1, e2 := entryPricePln(t, forward, 1), entryPricePln(t, forward, 2)
	within(t, "entry ratio", e1.DivRound(e2, 8), "10000", "0.000001") // = 480 000 / 48
}

// ---------------------------------------------------------------------------------------------
// S12 - buy rule (design §2 rule 7)
// ---------------------------------------------------------------------------------------------

func TestApplyBuy_Rule7(t *testing.T) {
	tests := []struct {
		name     string
		rows     []Row
		position int64
		paid     string
		wantNull bool
		wantCost string
	}{
		{"asset with recorded cost: cost += paid", ownerAsset(), 1, "1000", false, "201000"},
		{"buy into a NULL sibling of a tracked asset initializes it", []Row{mk(1, 1, "0.5", "480000", "200000"), mk(2, 1, "100", "48", "")}, 2, "500", false, "500"},
		{"NULL-cost asset that holds units: no cost invented", []Row{mk(1, 1, "10", "100", "")}, 1, "1000", true, ""},
		{"NULL-cost asset that holds units in a sibling: no cost invented", []Row{mk(1, 1, "0", "100", ""), mk(2, 1, "3", "50", "")}, 1, "1000", true, ""},
		{"empty NULL-cost asset: starts tracking", []Row{mk(1, 1, "0", "100", "")}, 1, "1000", false, "1000"},
		{"empty asset with zeroed cost: starts fresh, no dust", []Row{mk(1, 1, "0", "100", "0")}, 1, "1000", false, "1000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := ApplyBuy(tc.rows, tc.position, d(tc.paid))
			if tc.wantNull {
				mustNullCost(t, out, tc.position)
				return
			}
			mustCost(t, out, tc.position, tc.wantCost)
		})
	}
}

func TestApplyBuy_NoOps(t *testing.T) {
	rows := ownerAsset()
	for name, out := range map[string][]Row{
		"zero paid":        ApplyBuy(rows, 1, d("0")),
		"negative paid":    ApplyBuy(rows, 1, d("-5")),
		"unknown position": ApplyBuy(rows, 99, d("100")),
	} {
		if len(ChangedRows(rows, out)) != 0 {
			t.Fatalf("%s: expected no change, got %+v", name, ChangedRows(rows, out))
		}
	}
}

// ---------------------------------------------------------------------------------------------
// S13 - unpriced sibling and rounding remainder
// ---------------------------------------------------------------------------------------------

func TestApplySale_UnpricedSiblingSkippedInValue_YetItsRecordedCostScales(t *testing.T) {
	rows := []Row{
		mk(1, 1, "10", "10", "200"),
		mk(2, 1, "5", "", ""),   // unpriced, NULL cost: no value, no cost
		mk(3, 1, "5", "", "50"), // unpriced but with a recorded cost: value skipped, cost scales
	}
	out := ApplySale(rows, Sale{PositionID: 1, Units: d("2"), PricePln: d("10")}) // f = 20/100
	mustCost(t, out, 1, "160")
	mustNullCost(t, out, 2)
	mustCost(t, out, 3, "40")
}

func TestApplySale_RoundingRemainderGoesToLargestRow_SumExact(t *testing.T) {
	// V_before = 490 000 (ETN sold at 50, above its 48 cache price); f = 125 000 / 490 000.
	in := ownerAsset()
	out := ApplySale(in, Sale{PositionID: 2, Units: d("2500"), PricePln: d("50")})
	// per-row (rounded to 4 dp): BTC 148 979.5918, ETN 74 489.7959, sum 223 469.3877,
	// target round(300 000 x 365/490) = 223 469.3878 -> the +0.0001 remainder lands on BTC.
	if got := sumCost(out); !got.Equal(d("223469.3878")) {
		t.Fatalf("Σcost = %s, want 223469.3878", got)
	}
	mustCost(t, out, 1, "148979.5919")
	mustCost(t, out, 2, "74489.7959")
}

func TestApplySale_TieOnLargestRowBreaksToLowestPositionID_AndRowOrderIrrelevant(t *testing.T) {
	a := []Row{mk(5, 1, "1", "10", "10.01"), mk(3, 1, "1", "10", "10.01"), mk(9, 1, "1", "10", "10.01")}
	b := []Row{a[2], a[0], a[1]}
	sale := Sale{PositionID: 5, Units: d("1"), PricePln: d("10")} // f = 1/3
	oa, ob := ApplySale(a, sale), ApplySale(b, sale)
	for _, id := range []int64{3, 5, 9} {
		ca, cb := byID(oa, id).CostPln, byID(ob, id).CostPln
		if !ca.Equal(*cb) {
			t.Fatalf("position %d: %s vs %s", id, ca, cb)
		}
	}
	if got := sumCost(oa); !got.Equal(d("20.02")) { // round(30.03 x 20/30)
		t.Fatalf("Σcost = %s, want 20.02", got)
	}
}

// ---------------------------------------------------------------------------------------------
// ChangedRows
// ---------------------------------------------------------------------------------------------

func TestChangedRows_ReportsOnlyRowsWhoseCostDiffers(t *testing.T) {
	before := btcMain()
	after := ApplySale(before, Sale{PositionID: 11, Units: d("574"), PricePln: d("32.6")})
	changed := ChangedRows(before, after)
	if len(changed) != 1 || changed[0].PositionID != 10 {
		t.Fatalf("changed = %+v, want only the head row 10 (the ETN row stays NULL)", changed)
	}
}

// ---------------------------------------------------------------------------------------------
// PlanTrades - a batch of confirmed trades netted PER ASSET (purchase-session completion, 161a-2
// follow-up). Per asset: S = value sold, B = value bought in that asset. min(S, B) CARRIES cost
// (the asset's cost is unchanged by it), a buy excess is a market buy (rule 7), a sale excess is a
// proportional sale. Flows on different assets stay independent.
// ---------------------------------------------------------------------------------------------

func val(s string) *decimal.Decimal { v := d(s); return &v }

// btcSession is the group shape of a BTC asset: the go-live group cost (75 000) sits on the head
// row, the ETN row has NULL cost. BTC-USD 0.1 @ 400 000 = 40 000, ETN 400 @ 50 = 20 000, V = 60 000.
func btcSession() []Row {
	return []Row{
		mk(10, 100, "0.1", "400000", "75000"),
		mk(11, 100, "400", "50", ""),
	}
}

func TestPlanTrades_SessionSwapWholeETNIntoPhysical_CostUnchanged(t *testing.T) {
	rows := btcSession()
	before := profitPct(t, rows) // -20 %
	out := PlanTrades(map[int64][]Row{100: rows},
		[]Sale{{PositionID: 11, Units: d("400"), PricePln: d("50"), ValuePln: val("20000")}},
		[]Leg{{PositionID: 10, AssetID: 100, Units: d("0.05"), PricePln: d("400000"), ValuePln: val("20000")}})
	a := out[100]
	if got := sumCost(a); !got.Equal(d("75000")) {
		t.Fatalf("asset cost = %s, want unchanged 75000 (a swap must not realize the loss)", got)
	}
	mustCost(t, a, 10, "75000") // the carried cost lands on the BOUGHT row
	mustNullCost(t, a, 11)
	post := afterUnits(afterUnits(a, 11, "-400"), 10, "0.05")
	near(t, "profit after", profitPct(t, post), before.String(), "0.0001")
	near(t, "profit is -20 %", profitPct(t, post), "-0.20", "0.0005")
}

// flatAsset: head 1 @ 50 000 (cost 60 000) + ETN 1 000 @ 50 (NULL cost); V = 100 000, profit +66.7 %.
func flatAsset() []Row {
	return []Row{mk(1, 1, "1", "50000", "60000"), mk(2, 1, "1000", "50", "")}
}

func TestPlanTrades_BuyExceedsSale_OnlyTheExcessIsAMarketBuy(t *testing.T) {
	out := PlanTrades(map[int64][]Row{1: flatAsset()},
		[]Sale{{PositionID: 2, Units: d("200"), PricePln: d("50")}},               // S = 10 000
		[]Leg{{PositionID: 1, AssetID: 1, Units: d("0.3"), PricePln: d("50000")}}) // B = 15 000
	if got := sumCost(out[1]); !got.Equal(d("65000")) { // 60 000 + (15 000 - 10 000)
		t.Fatalf("asset cost = %s, want 65000 (cost += 5 000 only)", got)
	}
}

func TestPlanTrades_SaleExceedsBuy_ScalesByTheNetSoldShare(t *testing.T) {
	out := PlanTrades(map[int64][]Row{1: flatAsset()},
		[]Sale{{PositionID: 2, Units: d("300"), PricePln: d("50")}},               // S = 15 000
		[]Leg{{PositionID: 1, AssetID: 1, Units: d("0.2"), PricePln: d("50000")}}) // B = 10 000
	// cost x (V - (S - B)) / V = 60 000 x 95 000 / 100 000
	if got := sumCost(out[1]); !got.Equal(d("57000")) {
		t.Fatalf("asset cost = %s, want 57000", got)
	}
}

func TestPlanTrades_TwoAssets_DonorGetsCompensatingBuy_RecipientBuysAtMarket(t *testing.T) {
	// asset 1 (donor): IKE-ETN sold 5 000, compensating REGULAR buy of the same ETN row 5 000.
	// asset 2 (recipient): buys 5 000 at market. Rows are per instrument; buckets are invisible here.
	donor := []Row{mk(1, 1, "100", "100", "8000"), mk(2, 1, "100", "100", "")} // V = 20 000
	recipient := []Row{mk(3, 2, "10", "100", "600")}
	out := PlanTrades(map[int64][]Row{1: donor, 2: recipient},
		[]Sale{{PositionID: 2, Units: d("50"), PricePln: d("100")}},
		[]Leg{
			{PositionID: 2, AssetID: 1, Units: d("50"), PricePln: d("100")}, // compensating buy, same row
			{PositionID: 3, AssetID: 2, Units: d("50"), PricePln: d("100")}, // recipient buy
		})
	if got := sumCost(out[1]); !got.Equal(d("8000")) {
		t.Fatalf("donor asset cost = %s, want unchanged 8000", got)
	}
	mustCost(t, out[2], 3, "5600") // 600 + 5 000 at market
}

func TestPlanTrades_CarryLandsOnTheBoughtRow_WhenBoughtRowHasNullCost(t *testing.T) {
	rows := []Row{mk(1, 1, "10", "100", "1000"), mk(2, 1, "10", "100", "")}
	out := PlanTrades(map[int64][]Row{1: rows},
		[]Sale{{PositionID: 1, Units: d("5"), PricePln: d("100")}},
		[]Leg{{PositionID: 2, AssetID: 1, Units: d("5"), PricePln: d("100")}})
	if got := sumCost(out[1]); !got.Equal(d("1000")) {
		t.Fatalf("Σcost = %s, want unchanged 1000", got)
	}
	mustCost(t, out[1], 1, "750")
	mustCost(t, out[1], 2, "250") // initialized by the carry alone
}

func TestPlanTrades_BuysOnly_EqualsIndependentBuys(t *testing.T) {
	tracked := []Row{mk(1, 1, "10", "100", "1000"), mk(2, 1, "10", "100", "")}
	untracked := []Row{mk(3, 2, "10", "100", "")}
	empty := []Row{mk(4, 3, "0", "100", "")}
	out := PlanTrades(map[int64][]Row{1: tracked, 2: untracked, 3: empty}, nil, []Leg{
		{PositionID: 2, AssetID: 1, Units: d("5"), PricePln: d("100")},
		{PositionID: 3, AssetID: 2, Units: d("5"), PricePln: d("100")},
		{PositionID: 4, AssetID: 3, Units: d("5"), PricePln: d("100")},
	})
	mustCost(t, out[1], 2, "500")
	mustCost(t, out[1], 1, "1000")
	mustNullCost(t, out[2], 3) // rule 7: untracked asset that holds units
	mustCost(t, out[3], 4, "500")
}

func TestPlanTrades_SeveralSalesOfOneAsset_AreAggregated(t *testing.T) {
	rows := []Row{mk(1, 1, "10", "100", "800"), mk(2, 1, "10", "100", "200")} // V = 2 000, C = 1 000
	out := PlanTrades(map[int64][]Row{1: rows}, []Sale{
		{PositionID: 1, Units: d("2"), PricePln: d("100")},
		{PositionID: 2, Units: d("3"), PricePln: d("100")},
	}, nil)
	// S = 500 -> cost x 0.75
	mustCost(t, out[1], 1, "600")
	mustCost(t, out[1], 2, "150")
}

func TestPlanTrades_SellingEverythingLeavesExactZero(t *testing.T) {
	rows := []Row{mk(1, 1, "3", "33.33", "100"), mk(2, 1, "0", "33.33", "")}
	out := PlanTrades(map[int64][]Row{1: rows}, []Sale{{PositionID: 1, Units: d("3"), PricePln: d("33.33")}}, nil)
	mustCost(t, out[1], 1, "0")
}

func TestPlanTrades_WholeAssetSwappedIntoAnotherRow_CarriesEverything(t *testing.T) {
	rows := []Row{mk(1, 1, "10", "100", "1000"), mk(2, 1, "0", "100", "")}
	out := PlanTrades(map[int64][]Row{1: rows},
		[]Sale{{PositionID: 1, Units: d("10"), PricePln: d("100")}},
		[]Leg{{PositionID: 2, AssetID: 1, Units: d("10"), PricePln: d("100")}})
	mustCost(t, out[1], 1, "0")
	mustCost(t, out[1], 2, "1000")
}

func TestPlanTrades_ExactAmountsOverrideUnitsTimesPrice(t *testing.T) {
	// the session path derives price = value / units at 8 dp, so units x price drifts from the value
	// actually paid; the exact value must be the one booked.
	rows := []Row{mk(1, 1, "3", "10", "")}
	empty := []Row{mk(2, 2, "0", "10", "")}
	out := PlanTrades(map[int64][]Row{1: rows, 2: empty}, nil,
		[]Leg{{PositionID: 2, AssetID: 2, Units: d("3"), PricePln: d("654.62333333"), ValuePln: val("1963.87")}})
	mustCost(t, out[2], 2, "1963.87")
}

func TestPlanTrades_IgnoresSalesOfUnknownPositionsAndInvalidSales(t *testing.T) {
	rows := ownerAsset()
	out := PlanTrades(map[int64][]Row{1: rows}, []Sale{
		{PositionID: 99, Units: d("1"), PricePln: d("48")},
		{PositionID: 2, Units: d("0"), PricePln: d("48")},
		{PositionID: 2, Units: d("5"), PricePln: d("0")},
	}, nil)
	if len(ChangedRows(rows, out[1])) != 0 {
		t.Fatalf("nothing applicable: expected no cost change, got %+v", ChangedRows(rows, out[1]))
	}
	if _, ok := out[99]; ok {
		t.Fatal("a sale of an unknown position must not create a result entry")
	}
}

func TestPlanTrades_IndependentOfSaleAndLegOrder(t *testing.T) {
	assets := func() map[int64][]Row { return map[int64][]Row{1: ownerAsset()} }
	sales := []Sale{
		{PositionID: 2, Units: d("1000"), PricePln: d("48")},
		{PositionID: 1, Units: d("0.05"), PricePln: d("480000")},
	}
	legs := []Leg{
		{PositionID: 1, AssetID: 1, Units: d("0.02"), PricePln: d("480000")},
		{PositionID: 2, AssetID: 1, Units: d("300"), PricePln: d("48")},
	}
	a := PlanTrades(assets(), sales, legs)
	b := PlanTrades(assets(), []Sale{sales[1], sales[0]}, []Leg{legs[1], legs[0]})
	for _, id := range []int64{1, 2} {
		ca, cb := byID(a[1], id).CostPln, byID(b[1], id).CostPln
		if ca == nil || cb == nil || !ca.Equal(*cb) {
			t.Fatalf("position %d: %v vs %v", id, ca, cb)
		}
	}
}

func TestPlanTransfer_IsPlanTradesWithOneSale(t *testing.T) {
	tr := Transfer{
		Sold: Sale{PositionID: 2, Units: d("2500"), PricePln: d("48")},
		Buys: []Leg{{PositionID: 1, AssetID: 1, Units: d("0.25"), PricePln: d("480000")}},
	}
	a := PlanTransfer(map[int64][]Row{1: ownerAsset()}, 1, tr)
	b := PlanTrades(map[int64][]Row{1: ownerAsset()}, []Sale{tr.Sold}, tr.Buys)
	for _, id := range []int64{1, 2} {
		if !byID(a[1], id).CostPln.Equal(*byID(b[1], id).CostPln) {
			t.Fatalf("position %d differs between PlanTransfer and PlanTrades", id)
		}
	}
}
