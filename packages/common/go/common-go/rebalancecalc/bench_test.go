package rebalancecalc

import (
	"fmt"
	"testing"

	"github.com/shopspring/decimal"
)

// BenchmarkAllocateProportional measures one allocation over a 30-position book: targets sum to
// 100 %, current values spread above and below target, one account-less identity allocator (the
// portfolioC shape). No threshold: the number is for comparing runs.
func BenchmarkAllocateProportional(b *testing.B) {
	rows := make([]PositionRow, 30)
	for i := range rows {
		target := decimal.NewFromInt(3) // 20 x 3 %
		if i >= 20 {
			target = decimal.RequireFromString("4") // 10 x 4 % -> 100 %
		}
		rows[i] = PositionRow{
			Ticker:              fmt.Sprintf("T%02d", i),
			Name:                fmt.Sprintf("Asset %02d", i),
			Exchange:            "XTB",
			TargetAllocationPct: &target,
			ValuePln:            decimal.NewFromInt(int64(200 + 137*(i%7))),
			PriceNativePln:      decimal.NewFromInt(int64(10 + 3*i)),
		}
	}
	deployable := decimal.NewFromInt(5000)
	total := decimal.NewFromInt(30000)

	for b.Loop() {
		if len(AllocateProportional(rows, deployable, nil, total, identityAllocate, nil)) == 0 {
			b.Fatal("an under-weight book must produce buy items")
		}
	}
}
