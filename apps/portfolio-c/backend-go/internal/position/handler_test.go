package position

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/siblingcore"
	scposition "investing-app/common-go/siblingcore/position"
)

func TestUnitsPatch_SingleBucketOnlyWhenPresent(t *testing.T) {
	if unitsPatch(nil) != nil {
		t.Fatal("absent units, no patch")
	}
	v := decimal.RequireFromString("2.5")
	patch := unitsPatch(&v)
	if len(patch) != 1 || !patch[siblingcore.SingleBucket].Equal(decimal.RequireFromString("2.5")) {
		t.Fatalf("patch %v", patch)
	}
}

func TestToWireAsset_UnitsAndPortfolio(t *testing.T) {
	units := siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{siblingcore.SingleBucket: decimal.NewFromInt(7)})
	dto := scposition.AssetDTO{Portfolio: "FIXED", Subassets: []scposition.SubassetDTO{{Units: units, EffectiveUnits: units.Total()}}}
	w := toWireAsset(dto)
	if w.Portfolio != "FIXED" || w.Subassets[0].Units != 7 || w.Subassets[0].EffectiveUnits != 7 {
		t.Fatalf("wire %+v", w)
	}
}

// TestPositionRequests_NumbersReachTheServiceExactly pins task 182 B5 (F3.4): units, cost, avg
// price and the new asset's targets decode as decimals and reach the service input unchanged.
func TestPositionRequests_NumbersReachTheServiceExactly(t *testing.T) {
	var upd wireUpdatePositionRequest
	if err := json.Unmarshal([]byte(`{"units":0.123456789012345678,"totalSpentPln":1234567.8912,`+
		`"avgBuyPrice":0.000000012345678901}`), &upd); err != nil {
		t.Fatal(err)
	}
	in := upd.toUpdateInput()
	if len(in.Units) != 1 || in.Units[siblingcore.SingleBucket].String() != "0.123456789012345678" {
		t.Errorf("units %v", in.Units)
	}
	if in.TotalSpentPln == nil || in.TotalSpentPln.String() != "1234567.8912" ||
		in.AvgBuyPrice == nil || in.AvgBuyPrice.String() != "0.000000012345678901" {
		t.Errorf("cost %v / avg %v", in.TotalSpentPln, in.AvgBuyPrice)
	}
	if empty := (wireUpdatePositionRequest{}).toUpdateInput(); empty.Units != nil || empty.TotalSpentPln != nil || empty.AvgBuyPrice != nil {
		t.Errorf("absent fields must stay nil, got %+v", empty)
	}

	var create wireCreatePositionRequest
	if err := json.Unmarshal([]byte(`{"portfolio":"VARIABLE","ticker":"X","newAsset":{"name":"X",`+
		`"targetAllocationPct":2.123456789012345678,"targetAllocationPctRiskOff":0.1}}`), &create); err != nil {
		t.Fatal(err)
	}
	cin, err := create.toCreateInput()
	if err != nil {
		t.Fatal(err)
	}
	na := cin.NewAsset
	if cin.Portfolio != "VARIABLE" || na == nil || na.TargetAllocationPct.String() != "2.123456789012345678" ||
		na.TargetAllocationPctRiskOff.String() != "0.1" {
		t.Errorf("create input %+v / new asset %+v", cin, na)
	}
}
