// Package xirr is the one XIRR solver of all three apps (ADR-112): Brent's method on
// [−0.999, 10] for the annualised internal rate of return of dated PLN cash flows, plus
// the deposits-page summary portfolioB and portfolioC show.
//
// Pure and deterministic: nothing here reads a clock. Callers pass the valuation date
// (historiaquery.NowInWarsaw() for the deposits page, the snapshot date for historia).
// Imports only stdlib and shopspring/decimal, so every common-go package may use it.
package xirr

import (
	"errors"
	"math"
	"time"

	"github.com/shopspring/decimal"
)

// Flow is one dated PLN cash flow: deposits NEGATIVE, the terminal portfolio value POSITIVE.
type Flow struct {
	Date   time.Time
	Amount decimal.Decimal
}

// Result is the deposits-page summary shared by portfolioB and portfolioC (formerly
// bisectxirr.XirrResult; field names unchanged).
type Result struct {
	XirrPct          *decimal.Decimal
	TotalInvestedPln decimal.Decimal
	CurrentValuePln  decimal.Decimal
	SimpleReturnPct  *decimal.Decimal
	YearsElapsed     *decimal.Decimal
}

const (
	bracketLower  = -0.999
	bracketUpper  = 10.0
	absAccuracy   = 1e-7
	relAccuracy   = 1e-7
	maxIterations = 1000
	daysPerYear   = 365
)

var (
	errNoBracket    = errors.New("xirr: NPV does not change sign on the bracket")
	errNotConverged = errors.New("xirr: solver did not converge")
)

// Solve returns the annualised internal rate of return of flows as a fraction at scale 6
// (HALF_UP), or nil when there is no meaningful solution: fewer than 2 flows, no sign change
// on [-0.999, 10], solver not converged within 1000 iterations, or root outside (-1, 10).
// Brent's method, absolute/relative accuracy 1e-7. Discounting is anchored on the calendar
// date of flows[0].Date; each flow's exponent is whole days / 365 between calendar dates
// (time.Date(y, m, d) of each instant in its own location). Pure and deterministic.
func Solve(flows []Flow) *decimal.Decimal {
	return solve(flows, maxIterations)
}

// solve is Solve with the iteration limit as a parameter (the test hook for "unconverged").
func solve(flows []Flow, maxIter int) *decimal.Decimal {
	if len(flows) < 2 {
		return nil
	}
	r, err := brent(npvFunc(flows), bracketLower, bracketUpper, absAccuracy, relAccuracy, maxIter)
	if err != nil || r <= -1 || r >= bracketUpper {
		return nil
	}
	// shopspring Round is HALF_UP, the rounding every XIRR column was written with.
	out := decimal.NewFromFloat(r).Round(6)
	return &out
}

// Summarize computes Result for deposit flows (negative amounts, ascending dates) valued at
// currentValue on valuationDate (callers pass historiaquery.NowInWarsaw()). Semantics are
// bisectxirr.ComputeXirrResult's, except: the solver is Solve (Brent) and the valuation date
// is injected instead of time.Now().UTC() (ADR-112).
func Summarize(deposits []Flow, currentValue decimal.Decimal, valuationDate time.Time) Result {
	zero := Result{TotalInvestedPln: decimal.Zero, CurrentValuePln: currentValue}
	if len(deposits) == 0 {
		return zero
	}
	invested := decimal.Zero
	for _, f := range deposits {
		if f.Amount.Sign() < 0 {
			invested = invested.Add(f.Amount.Neg())
		}
	}
	if invested.Sign() == 0 {
		return zero
	}
	simpleReturn := currentValue.Sub(invested).DivRound(invested, 8)

	flows := make([]Flow, len(deposits), len(deposits)+1)
	copy(flows, deposits)
	flows = append(flows, Flow{Date: valuationDate, Amount: currentValue})

	return Result{
		XirrPct:          Solve(flows),
		TotalInvestedPln: invested,
		CurrentValuePln:  currentValue,
		SimpleReturnPct:  &simpleReturn,
		YearsElapsed:     yearsElapsed(deposits[0].Date, valuationDate),
	}
}

// yearsElapsed is calendar days / 365 at scale 8, nil when the valuation is not after the
// first deposit's day.
func yearsElapsed(first, valuation time.Time) *decimal.Decimal {
	days := wholeDays(first, valuation)
	if days <= 0 {
		return nil
	}
	v := decimal.NewFromInt(days).DivRound(decimal.NewFromInt(daysPerYear), 8)
	return &v
}

// npvFunc returns NPV(r) of flows discounted to the calendar date of flows[0].
func npvFunc(flows []Flow) func(float64) float64 {
	amounts := make([]float64, len(flows))
	years := make([]float64, len(flows))
	for i, f := range flows {
		amounts[i] = f.Amount.InexactFloat64()
		years[i] = float64(wholeDays(flows[0].Date, f.Date)) / daysPerYear
	}
	return func(r float64) float64 {
		sum := 0.0
		for i := range amounts {
			sum += amounts[i] / math.Pow(1+r, years[i])
		}
		return sum
	}
}

// brent finds a root of f in [lo, hi] with Brent's method. Same-sign (or NaN) endpoints
// answer errNoBracket; leaving the loop without meeting the tolerance answers
// errNotConverged, never the last iterate.
func brent(f func(float64) float64, lo, hi, absAcc, relAcc float64, maxIter int) (float64, error) {
	a, b := lo, hi
	fa, fb := f(a), f(b)
	if fa == 0 {
		return a, nil
	}
	if fb == 0 {
		return b, nil
	}
	if math.IsNaN(fa) || math.IsNaN(fb) || fa*fb > 0 {
		return 0, errNoBracket
	}
	if math.Abs(fa) < math.Abs(fb) {
		a, b, fa, fb = b, a, fb, fa
	}
	c, fc := a, fa
	mflag := true
	var d float64
	for range maxIter {
		s := interpolate(a, b, c, fa, fb, fc)
		if mustBisect(s, a, b, c, d, absAcc, mflag) {
			s = (a + b) / 2
			mflag = true
		} else {
			mflag = false
		}
		fs := f(s)
		if math.IsNaN(fs) {
			return 0, errNoBracket
		}
		d = c
		c, fc = b, fb
		if fa*fs < 0 {
			b, fb = s, fs
		} else {
			a, fa = s, fs
		}
		if math.Abs(fa) < math.Abs(fb) {
			a, b, fa, fb = b, a, fb, fa
		}
		if fb == 0 || math.Abs(b-a) <= math.Max(relAcc*math.Abs(b), absAcc) {
			return b, nil
		}
	}
	return 0, errNotConverged
}

// interpolate is the inverse-quadratic step when the three ordinates differ, else secant.
func interpolate(a, b, c, fa, fb, fc float64) float64 {
	if fa != fc && fb != fc {
		return a*fb*fc/((fa-fb)*(fa-fc)) +
			b*fa*fc/((fb-fa)*(fb-fc)) +
			c*fa*fb/((fc-fa)*(fc-fb))
	}
	return b - fb*(b-a)/(fb-fa)
}

// mustBisect is Brent's rejection test for the interpolated step s. The step must lie
// strictly between (3a+b)/4 and b in either order — a > b after a swap is the normal case
// (F5.3: the former "s < (3a+b)/4 || s > b" rejected every step then) — and must shrink
// fast enough relative to the previous steps.
func mustBisect(s, a, b, c, d, absAcc float64, mflag bool) bool {
	q := (3*a + b) / 4
	lo, hi := min(q, b), max(q, b)
	if s <= lo || s >= hi {
		return true
	}
	if mflag {
		return math.Abs(s-b) >= math.Abs(b-c)/2 || math.Abs(b-c) < absAcc
	}
	return math.Abs(s-b) >= math.Abs(c-d)/2 || math.Abs(c-d) < absAcc
}

// wholeDays is the number of calendar days from from's date to to's date, each taken in its own
// location (calendar-date arithmetic, independent of the time of day).
func wholeDays(from, to time.Time) int64 {
	return int64(dateOnly(to).Sub(dateOnly(from)).Hours() / 24)
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
