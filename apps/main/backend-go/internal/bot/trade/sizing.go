package trade

import (
	"github.com/shopspring/decimal"
)

// Sizing is one position-size decision: USDT-native margin, BTC quantity, and the
// applied (possibly reduced) leverage.
type Sizing struct {
	MarginUsdt      decimal.Decimal
	QuantityBtc     decimal.Decimal
	AppliedLeverage int
}

// confidencePivot is the confidence at which the multiplier is exactly 1.0x.
var confidencePivot = decimal.RequireFromString("0.5")

// Size ports BotPositionSizingService.size: confidence-scaled margin clamped to the
// [min,max] risk band, then the joint leverage×stopDistance ≤ cap rule (reduce
// leverage first down to 1; if still violated, skip — never widen the stop, never
// add margin). Empty = vetoed (caller logs + HOLD signal, no order).
func Size(cfg ExecutionConfig, equityUsdt, confidence decimal.Decimal, confidenceSet bool, netLeverage int, entryPrice decimal.Decimal) (Sizing, bool) {
	if equityUsdt.Cmp(decimal.Zero) <= 0 || entryPrice.Cmp(decimal.Zero) <= 0 || netLeverage < 1 {
		return Sizing{}, false
	}
	c := confidencePivot
	if confidenceSet {
		c = confidence
	}
	multiplier := c.DivRound(confidencePivot, 8)
	multiplier = clampDecimal(multiplier,
		decimal.RequireFromString("0.5"), decimal.RequireFromString("1.5"))
	margin := equityUsdt.Mul(cfg.BaseRiskPct).Mul(multiplier)
	margin = clampDecimal(margin,
		equityUsdt.Mul(cfg.MinRiskPct), equityUsdt.Mul(cfg.MaxRiskPct))
	appliedLeverage := netLeverage
	for appliedLeverage > 1 &&
		decimal.NewFromInt(int64(appliedLeverage)).Mul(cfg.StopDistancePct).Cmp(cfg.LeverageLossCapPct) > 0 {
		appliedLeverage--
	}
	if decimal.NewFromInt(int64(appliedLeverage)).Mul(cfg.StopDistancePct).Cmp(cfg.LeverageLossCapPct) > 0 {
		return Sizing{}, false
	}
	quantity := margin.DivRound(entryPrice, 8)
	if quantity.Cmp(decimal.Zero) <= 0 {
		return Sizing{}, false
	}
	return Sizing{MarginUsdt: margin, QuantityBtc: quantity, AppliedLeverage: appliedLeverage}, true
}

func clampDecimal(v, floor, cap decimal.Decimal) decimal.Decimal {
	if v.Cmp(floor) < 0 {
		return floor
	}
	if v.Cmp(cap) > 0 {
		return cap
	}
	return v
}
