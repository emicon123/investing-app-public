package xirr

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// BenchmarkSolve measures the production-shaped solve: 39 monthly-ish deposits over five years
// plus the terminal portfolio value (40 flows). No threshold: the number is for comparing runs.
func BenchmarkSolve(b *testing.B) {
	start := time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)
	flows := make([]Flow, 0, 40)
	invested := decimal.Zero
	for i := range 39 {
		amount := decimal.NewFromInt(int64(-1000 - 25*i))
		invested = invested.Add(amount.Neg())
		flows = append(flows, Flow{Date: start.AddDate(0, 0, 46*i), Amount: amount})
	}
	flows = append(flows, Flow{Date: start.AddDate(5, 0, 0), Amount: invested.Mul(decimal.RequireFromString("1.35"))})

	for b.Loop() {
		if Solve(flows) == nil {
			b.Fatal("benchmark flows must converge")
		}
	}
}
