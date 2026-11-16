package position

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/siblingcore"
	scposition "investing-app/common-go/siblingcore/position"
	"portfolio-b/backend-go/internal/book"
)

func dp(s string) *decimal.Decimal { v := decimal.RequireFromString(s); return &v }

func TestUnitsPatch_OnlyTheAccountFieldsPresent(t *testing.T) {
	patch := unitsPatch(wireUpdatePositionRequest{UnitsIkzeMama: dp("1.5"), UnitsInne: dp("0")})
	if len(patch) != 2 || !patch[book.IkzeMama].Equal(decimal.RequireFromString("1.5")) || !patch[book.Inne].IsZero() {
		t.Fatalf("patch %v", patch)
	}
	if len(unitsPatch(wireUpdatePositionRequest{})) != 0 {
		t.Fatal("no units fields, no patch")
	}
}

func TestToWireAsset_FiveAccountFieldsFromTheBuckets(t *testing.T) {
	units := siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{
		book.IkeTata: decimal.NewFromInt(1), book.IkeMama: decimal.NewFromInt(2), book.IkzeTata: decimal.NewFromInt(3),
		book.IkzeMama: decimal.NewFromInt(4), book.Inne: decimal.NewFromInt(5),
	})
	w := toWireAsset(scposition.AssetDTO{Subassets: []scposition.SubassetDTO{{Units: units, EffectiveUnits: units.Total()}}})
	s := w.Subassets[0]
	if s.UnitsIkeTata != 1 || s.UnitsIkeMama != 2 || s.UnitsIkzeTata != 3 || s.UnitsIkzeMama != 4 || s.UnitsInne != 5 || s.EffectiveUnits != 15 {
		t.Fatalf("wire %+v", s)
	}
}

// TestPositionRequests_NumbersReachTheServiceExactly pins task 182 B5 (F3.4): units, cost, avg
// price and the new asset's targets decode as decimals and reach the service input unchanged.
func TestPositionRequests_NumbersReachTheServiceExactly(t *testing.T) {
	var upd wireUpdatePositionRequest
	if err := json.Unmarshal([]byte(`{"unitsIkeTata":0.123456789012345678,"unitsInne":12345678.123456789012345678,`+
		`"totalSpentPln":1234567.8912,"avgBuyPrice":0.000000012345678901}`), &upd); err != nil {
		t.Fatal(err)
	}
	in := upd.toUpdateInput()
	if len(in.Units) != 2 || in.Units[book.IkeTata].String() != "0.123456789012345678" ||
		in.Units[book.Inne].String() != "12345678.123456789012345678" {
		t.Errorf("units %v", in.Units)
	}
	if in.TotalSpentPln == nil || in.TotalSpentPln.String() != "1234567.8912" ||
		in.AvgBuyPrice == nil || in.AvgBuyPrice.String() != "0.000000012345678901" {
		t.Errorf("cost %v / avg %v", in.TotalSpentPln, in.AvgBuyPrice)
	}
	if empty := (wireUpdatePositionRequest{}).toUpdateInput(); empty.TotalSpentPln != nil || empty.AvgBuyPrice != nil {
		t.Errorf("absent cost fields must stay nil, got %+v", empty)
	}

	var create wireCreatePositionRequest
	if err := json.Unmarshal([]byte(`{"ticker":"X","newAsset":{"name":"X","targetAllocationPct":2.123456789012345678,`+
		`"targetAllocationPctRiskOff":0.1}}`), &create); err != nil {
		t.Fatal(err)
	}
	na := create.toCreateInput().NewAsset
	if na == nil || na.TargetAllocationPct.String() != "2.123456789012345678" || na.TargetAllocationPctRiskOff.String() != "0.1" {
		t.Errorf("new asset targets %+v", na)
	}
}
