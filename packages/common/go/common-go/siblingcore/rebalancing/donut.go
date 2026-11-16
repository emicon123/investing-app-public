package rebalancing

import (
	"github.com/shopspring/decimal"

	"investing-app/common-go/rebalancecalc"
)

var (
	hundred        = decimal.NewFromInt(100)
	donutTolerance = decimal.RequireFromString("0.001")
)

// donutData is each tradable asset's current vs target share; status is ON_TARGET within ±0.1 %
// of the target (relative), OVER/UNDER beyond, and always ON_TARGET for a zero target.
func donutData(rows []rebalancecalc.PositionRow, total decimal.Decimal) []DonutSlice {
	out := make([]DonutSlice, 0, len(rows))
	for _, r := range rows {
		currentPct := decimal.Zero
		if total.Sign() > 0 {
			currentPct = r.ValuePln.DivRound(total, 8).Mul(hundred)
		}
		targetPct := decimal.Zero
		if r.TargetAllocationPct != nil {
			targetPct = *r.TargetAllocationPct
		}
		deviationPct, status := decimal.Zero, "ON_TARGET"
		if targetPct.Sign() > 0 {
			deviationPct = currentPct.DivRound(targetPct, 8).Sub(decimal.NewFromInt(1))
			status = donutStatus(deviationPct)
		}
		out = append(out, DonutSlice{
			Ticker: r.Ticker, Name: r.Name, CurrentPct: currentPct.Round(4), TargetPct: targetPct.Round(4),
			ValuePln: r.ValuePln.Round(4), DeviationPct: deviationPct.Round(4), Status: status,
		})
	}
	return out
}

func donutStatus(deviationPct decimal.Decimal) string {
	switch {
	case deviationPct.Abs().Cmp(donutTolerance) <= 0:
		return "ON_TARGET"
	case deviationPct.Sign() > 0:
		return "OVER"
	}
	return "UNDER"
}
