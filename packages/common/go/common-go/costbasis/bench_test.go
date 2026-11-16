package costbasis

import "testing"

// BenchmarkPlanTransfer measures one confirmed transfer over three assets (10 rows): the sale
// scales asset 1, one leg carries cost on the sold asset, two legs are market buys elsewhere.
// No threshold: the number is for comparing runs.
func BenchmarkPlanTransfer(b *testing.B) {
	assets := map[int64][]Row{
		1: {mk(1, 1, "10", "100", "800"), mk(2, 1, "5", "100", "400"), mk(3, 1, "2.5", "100", "150"), mk(4, 1, "1", "", "50")},
		2: {mk(5, 2, "40", "25", "900"), mk(6, 2, "10", "25", "200"), mk(7, 2, "0", "25", "")},
		3: {mk(8, 3, "3", "500", "1200"), mk(9, 3, "1", "500", "400"), mk(10, 3, "0.5", "500", "")},
	}
	t := Transfer{
		Sold: Sale{PositionID: 1, Units: d("4"), PricePln: d("100")},
		Buys: []Leg{
			{PositionID: 2, AssetID: 1, Units: d("1"), PricePln: d("100")},
			{PositionID: 5, AssetID: 2, Units: d("8"), PricePln: d("25")},
			{PositionID: 8, AssetID: 3, Units: d("0.2"), PricePln: d("500")},
		},
	}

	for b.Loop() {
		if len(PlanTransfer(assets, 1, t)) != 3 {
			b.Fatal("transfer must touch the sold asset and both bought assets")
		}
	}
}
