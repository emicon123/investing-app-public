package historiaquery

import (
	"math"
	"time"

	"github.com/shopspring/decimal"
)

// This file holds the inflation math the historia read path uses (cumulative factor,
// annualized, inflation-adjusted deposits and the real return derived from them —
// descended from pl.shared.inflation.InflationMath) as pure functions over
// shopspring/decimal. It is the single copy: root's backend-go/internal/historia
// imports it too.

// YearMonth is a calendar month (year + month, no day) — the key of the inflation index.
type YearMonth struct {
	Year  int
	Month int
}

// YearMonthOf extracts the calendar month of t.
func YearMonthOf(t time.Time) YearMonth {
	y, m, _ := t.Date()
	return YearMonth{Year: y, Month: int(m)}
}

// CumulativeFactor ports InflationMath.cumulativeFactor: the compounded CPI factor
// from fromExclusive (exclusive) to toInclusive (inclusive), skipping months with no
// observation. Returns ONE when the range is empty or inverted.
func CumulativeFactor(fromExclusive, toInclusive YearMonth, mom map[YearMonth]decimal.Decimal) decimal.Decimal {
	if !beforeMonth(fromExclusive, toInclusive) {
		return decimal.NewFromInt(1)
	}
	factor := decimal.NewFromInt(1)
	for cur := nextMonth(fromExclusive); !afterMonth(cur, toInclusive); cur = nextMonth(cur) {
		if cpi, ok := mom[cur]; ok {
			// The original math used 12 significant digits (HALF_UP); 12 decimal places here is the
			// same order of precision for CPI values around 100, far below anything this math can
			// observe.
			factor = factor.Mul(cpi.DivRound(decimal.NewFromInt(100), 12))
		}
	}
	return factor
}

// CumulativeSince is the cumulative inflation fraction (factor minus one) from base
// (exclusive) to asOf's month (inclusive) — the input Annualized turns into a CAGR.
func CumulativeSince(base YearMonth, asOf time.Time, mom map[YearMonth]decimal.Decimal) decimal.Decimal {
	return CumulativeFactor(base, YearMonthOf(asOf), mom).Sub(decimal.NewFromInt(1))
}

// Annualized returns the annualized rate equivalent to the cumulative fraction over base..target.
// Zero or negative spans annualize to ZERO (not nil). Under a year of history, extrapolating via
// (12/months) would amplify a single month's noise into a misleading annual figure, so it returns
// the raw cumulative fraction unextrapolated instead.
func Annualized(cumulative decimal.Decimal, base, target YearMonth) decimal.Decimal {
	months := (target.Year-base.Year)*12 + (target.Month - base.Month)
	if months <= 0 {
		return decimal.Zero
	}
	if months < 12 {
		return cumulative
	}
	f, _ := cumulative.Add(decimal.NewFromInt(1)).Float64()
	return decimal.NewFromFloat(math.Pow(f, 12.0/float64(months)) - 1.0)
}

// InflationAdjustedDeposits is the break-even line of the history chart: every
// PLN cash flow dated on or before asOf, inflated from its own month to asOf's
// month (Σ amountᵢ × CPI factor). It is "how much I would need to hold now for my
// deposits to have kept their purchasing power" — portfolio value above it is a
// real gain, below it a real loss. With zero inflation it equals total deposits,
// which is why it tracks the deposits line. Flows after asOf are ignored so a
// deposit later in the same month does not leak into an earlier snapshot.
// Nil when there is nothing to compare against (no flows or no index data).
func InflationAdjustedDeposits(flows []CashFlow, asOf time.Time, mom map[YearMonth]decimal.Decimal) *decimal.Decimal {
	if len(flows) == 0 || len(mom) == 0 {
		return nil
	}
	cutoff := dateOnly(asOf)
	end := YearMonthOf(cutoff)
	sum := decimal.Zero
	for _, f := range flows {
		if dateOnly(f.Date).After(cutoff) {
			continue
		}
		sum = sum.Add(f.Amount.Mul(CumulativeFactor(YearMonthOf(f.Date), end, mom)))
	}
	return &sum
}

// RealReturn is the inflation-adjusted total return: value against its
// inflation-adjusted deposits (InflationAdjustedDeposits), minus one. Nil when the
// adjusted deposits are unavailable or zero (nothing deposited yet).
func RealReturn(value decimal.Decimal, adjustedDeposits *decimal.Decimal) *decimal.Decimal {
	if adjustedDeposits == nil || adjustedDeposits.IsZero() {
		return nil
	}
	// 8 decimal places, half away from zero (HALF_UP for these positive operands).
	r := value.DivRound(*adjustedDeposits, 8).Sub(decimal.NewFromInt(1))
	return &r
}

func beforeMonth(a, b YearMonth) bool {
	return a.Year < b.Year || (a.Year == b.Year && a.Month < b.Month)
}

func afterMonth(a, b YearMonth) bool {
	return beforeMonth(b, a)
}

func nextMonth(m YearMonth) YearMonth {
	if m.Month == 12 {
		return YearMonth{Year: m.Year + 1, Month: 1}
	}
	return YearMonth{Year: m.Year, Month: m.Month + 1}
}
