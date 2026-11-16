package costmember

import (
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/costbasis"
)

// fakeHolding is a test-local Holding: one unit total and one nullable cost.
type fakeHolding struct {
	units decimal.Decimal
	cost  *decimal.Decimal
}

func (f *fakeHolding) UnitsTotal() decimal.Decimal   { return f.units }
func (f *fakeHolding) CostPln() *decimal.Decimal     { return f.cost }
func (f *fakeHolding) SetCostPln(c *decimal.Decimal) { f.cost = c }

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func dp(s string) *decimal.Decimal {
	v := d(s)
	return &v
}

func member(id, assetID int64, ticker *string, units string, cost *decimal.Decimal) *Member[*fakeHolding] {
	return &Member[*fakeHolding]{ID: id, AssetID: assetID, YahooTicker: ticker, Units: &fakeHolding{units: d(units), cost: cost}}
}

func TestRows_PricesByTickerAndCopiesUnitsAndCost(t *testing.T) {
	prices := map[string]decimal.Decimal{"AAA": d("12.5")}
	members := []*Member[*fakeHolding]{
		member(1, 7, new("AAA"), "3", dp("30")),
		member(2, 7, nil, "1", nil),
		member(3, 7, new(""), "2", nil),
		member(4, 7, new("MISSING"), "4", dp("8")),
	}
	rows := Rows(members, prices)
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}
	if r := rows[0]; r.PositionID != 1 || r.AssetID != 7 || !r.Units.Equal(d("3")) ||
		r.PricePln == nil || !r.PricePln.Equal(d("12.5")) || r.CostPln == nil || !r.CostPln.Equal(d("30")) {
		t.Fatalf("priced row = %+v", r)
	}
	for i, why := range map[int]string{1: "nil ticker", 2: "empty ticker", 3: "missing cache entry"} {
		if rows[i].PricePln != nil {
			t.Errorf("%s: PricePln = %v, want nil", why, *rows[i].PricePln)
		}
	}
	if rows[1].CostPln != nil {
		t.Errorf("NULL cost must stay nil, got %v", *rows[1].CostPln)
	}
}

func TestRows_EmptyInputIsEmptyNonNil(t *testing.T) {
	rows := Rows[*fakeHolding](nil, nil)
	if rows == nil || len(rows) != 0 {
		t.Fatalf("Rows(nil) = %#v, want empty non-nil", rows)
	}
}

func TestRows_CostIsCopiedNotAliased(t *testing.T) {
	m := member(1, 7, nil, "1", dp("10"))
	rows := Rows([]*Member[*fakeHolding]{m}, nil)
	*rows[0].CostPln = d("999")
	if !m.Units.cost.Equal(d("10")) {
		t.Fatalf("mutating the row's cost changed the holding: %v", *m.Units.cost)
	}
}

func TestApply_WritesOnlyChangedMembersAndCopiesCost(t *testing.T) {
	a := member(1, 7, nil, "1", dp("10"))
	b := member(2, 7, nil, "1", dp("20"))
	members := []*Member[*fakeHolding]{a, b}
	before := Rows(members, nil)
	after := Rows(members, nil)
	after[0].CostPln = dp("5")

	changed := Apply(members, before, after)
	if len(changed) != 1 || changed[0] != a {
		t.Fatalf("changed = %v, want only member 1", changed)
	}
	if !a.Units.cost.Equal(d("5")) || !b.Units.cost.Equal(d("20")) {
		t.Fatalf("costs after Apply = %v, %v; want 5, 20", *a.Units.cost, *b.Units.cost)
	}
	*after[0].CostPln = d("999")
	if !a.Units.cost.Equal(d("5")) {
		t.Fatalf("mutating the kernel row changed the holding: %v", *a.Units.cost)
	}
}

func TestApply_NullingACostAndIgnoringUnknownRows(t *testing.T) {
	a := member(1, 7, nil, "1", dp("10"))
	members := []*Member[*fakeHolding]{a}
	before := Rows(members, nil)
	after := []costbasis.Row{{PositionID: 1, AssetID: 7, Units: d("1")}, {PositionID: 99, AssetID: 7, CostPln: dp("1")}}
	changed := Apply(members, append(before, costbasis.Row{PositionID: 99}), after)
	if len(changed) != 1 || changed[0] != a || a.Units.cost != nil {
		t.Fatalf("changed = %v, cost = %v; want member 1 with NULL cost", changed, a.Units.cost)
	}
}

func TestApply_NoChangeReturnsNil(t *testing.T) {
	members := []*Member[*fakeHolding]{member(1, 7, nil, "1", dp("10"))}
	rows := Rows(members, nil)
	if changed := Apply(members, rows, Rows(members, nil)); changed != nil {
		t.Fatalf("changed = %v, want nil", changed)
	}
}

// Sold asset 1 first, then the legs' assets in leg order (3, 2), asset 3 not repeated although
// two legs touch it.
func TestPlanTransfer_SoldAssetFirstThenLegAssetsInOrderDeduped(t *testing.T) {
	s11 := member(11, 1, new("AAA"), "10", dp("1000"))
	b21 := member(21, 2, new("BBB"), "2", dp("200"))
	c31 := member(31, 3, nil, "0", nil)
	c32 := member(32, 3, nil, "0", nil)
	members := map[int64][]*Member[*fakeHolding]{1: {s11}, 2: {b21}, 3: {c31, c32}}
	prices := map[string]decimal.Decimal{"AAA": d("100"), "BBB": d("100")}

	changed := PlanTransfer(members, prices, 1, costbasis.Transfer{
		Sold: costbasis.Sale{PositionID: 11, Units: d("5"), PricePln: d("100")},
		Buys: []costbasis.Leg{
			{PositionID: 31, AssetID: 3, Units: d("1"), PricePln: d("50")},
			{PositionID: 21, AssetID: 2, Units: d("1"), PricePln: d("100")},
			{PositionID: 32, AssetID: 3, Units: d("2"), PricePln: d("10")},
		},
	})

	var ids []int64
	for _, m := range changed {
		ids = append(ids, m.ID)
	}
	want := []int64{11, 31, 32, 21}
	if len(ids) != len(want) {
		t.Fatalf("changed ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("changed ids = %v, want %v", ids, want)
		}
	}
	for m, cost := range map[*Member[*fakeHolding]]string{s11: "500", b21: "300", c31: "50", c32: "20"} {
		if m.Units.cost == nil || !m.Units.cost.Equal(d(cost)) {
			t.Errorf("position %d cost = %v, want %s", m.ID, m.Units.cost, cost)
		}
	}
}

func TestPlanTransfer_UnknownSoldAssetStillAppliesOtherLegs(t *testing.T) {
	b21 := member(21, 2, nil, "0", nil)
	members := map[int64][]*Member[*fakeHolding]{2: {b21}}
	changed := PlanTransfer(members, nil, 1, costbasis.Transfer{
		Sold: costbasis.Sale{PositionID: 11, Units: d("1"), PricePln: d("1")},
		Buys: []costbasis.Leg{{PositionID: 21, AssetID: 2, Units: d("1"), PricePln: d("40")}},
	})
	if len(changed) != 1 || changed[0] != b21 || !b21.Units.cost.Equal(d("40")) {
		t.Fatalf("changed = %v, cost = %v; want member 21 at 40", changed, b21.Units.cost)
	}
}
