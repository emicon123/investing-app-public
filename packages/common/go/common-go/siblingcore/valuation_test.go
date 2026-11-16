package siblingcore_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/positioncalc"
	"investing-app/common-go/siblingcore"
)

func TestSubasset_OrderSubassetsPrimaryFirstThenTicker(t *testing.T) {
	subs := []siblingcore.Subasset{
		{ID: 1, AssetID: 7, Ticker: "C"}, {ID: 2, AssetID: 7, Ticker: "B", IsPrimary: true},
		{ID: 3, AssetID: 8, Ticker: "A"}, {ID: 4, AssetID: 7, Ticker: "A"},
	}
	got := positioncalc.OrderSubassets(subs, 7)
	if len(got) != 3 || got[0].ID != 2 || got[1].ID != 4 || got[2].ID != 1 {
		t.Fatalf("order: %+v", got)
	}
	if p := positioncalc.PrimaryOf(subs, 7); p == nil || p.ID != 2 {
		t.Fatalf("primary: %+v", p)
	}
}

func TestPositionInputs_TargetsFromTheAssetAndSiblingsFoldIntoThePrimary(t *testing.T) {
	sixty := dec("60")
	assets := []siblingcore.Asset{{ID: 7, TargetAllocationPct: &sixty}}
	units := siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{"A": dec("1"), "B": dec("2")})
	subs := []siblingcore.Subasset{
		{AssetID: 7, Ticker: "SIB", YahooTicker: new("S.WA"), Holding: siblingcore.Holding{Units: units}},
		{AssetID: 7, Ticker: "PRI", IsPrimary: true, PriceCurrency: "PLN"},
		{AssetID: 9, Ticker: "ORPHAN"},
	}
	in := siblingcore.PositionInputs(assets, subs)
	if len(in) != 3 || in[0].Ticker != "SIB" || !in[0].IsCompanionRow || in[0].CompanionParentTicker == nil || *in[0].CompanionParentTicker != "PRI" {
		t.Fatalf("sibling: %+v", in[0])
	}
	if !in[0].UnitsTotal.Equal(dec("3")) || in[0].TargetAllocationPct == nil || !in[0].TargetAllocationPct.Equal(sixty) {
		t.Fatalf("sibling units/target: %+v", in[0])
	}
	if in[1].IsCompanionRow || in[1].CompanionParentTicker != nil {
		t.Fatalf("primary: %+v", in[1])
	}
	if in[2].TargetAllocationPct != nil || in[2].CompanionParentTicker != nil {
		t.Fatalf("orphan: %+v", in[2])
	}
}
