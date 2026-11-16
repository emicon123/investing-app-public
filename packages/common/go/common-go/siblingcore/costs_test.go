package siblingcore_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/siblingcore"
)

func TestMembersByAsset_GroupsByAssetOrderedByIDOverCopies(t *testing.T) {
	subs := []siblingcore.Subasset{
		{ID: 9, AssetID: 1, Ticker: "B", Holding: siblingcore.Holding{Units: siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{"A": dec("2")})}},
		{ID: 3, AssetID: 1, Ticker: "A", YahooTicker: new("A.WA")},
		{ID: 5, AssetID: 2, Ticker: "C"},
	}
	m := siblingcore.MembersByAsset(subs)
	if len(m[1]) != 2 || m[1][0].ID != 3 || m[1][1].ID != 9 || len(m[2]) != 1 || *m[1][0].YahooTicker != "A.WA" {
		t.Fatalf("members %+v", m)
	}
	c := dec("1")
	m[1][1].Units.SetCostPln(&c)
	if subs[0].Holding.TotalSpentPln != nil {
		t.Fatal("members must hold copies, not the caller's rows")
	}
	if !m[1][1].Units.UnitsTotal().Equal(dec("2")) {
		t.Fatalf("units %s", m[1][1].Units.UnitsTotal())
	}
}

type costLog struct{ ids []int64 }

func (l *costLog) WriteCost(_ context.Context, id int64, _ *decimal.Decimal) error {
	l.ids = append(l.ids, id)
	return nil
}

func TestPersistCosts_SkipsTheRowsTheDeltaWriteCarries(t *testing.T) {
	m := siblingcore.MembersByAsset([]siblingcore.Subasset{{ID: 1, AssetID: 1}, {ID: 2, AssetID: 1}, {ID: 3, AssetID: 1}})
	var l costLog
	if err := siblingcore.PersistCosts(context.Background(), &l, m[1], 2); err != nil {
		t.Fatal(err)
	}
	if len(l.ids) != 2 || l.ids[0] != 1 || l.ids[1] != 3 {
		t.Fatalf("written %v", l.ids)
	}
}
