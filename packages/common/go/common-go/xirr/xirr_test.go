package xirr

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// The golden cases below moved here from the four former XIRR copies (ADR-112):
// common-go/historiaquery/service_test.go, main internal/historia/xirr_inflation_test.go,
// main internal/deposit/xirr_test.go and common-go/bisectxirr/bisectxirr_test.go.

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func flowsOf(dates, amounts []string) []Flow {
	out := make([]Flow, len(dates))
	for i := range dates {
		out[i] = Flow{Date: day(dates[i]), Amount: dec(amounts[i])}
	}
	return out
}

// --- Solve ---------------------------------------------------------------------------

func TestSolve_FewerThanTwoFlowsIsNil(t *testing.T) {
	if got := Solve(nil); got != nil {
		t.Fatalf("0 flows: want nil, got %s", got)
	}
	if got := Solve([]Flow{{Date: time.Now(), Amount: dec("100")}}); got != nil {
		t.Fatalf("1 flow: want nil, got %s", got)
	}
}

func TestSolve_ExactTenPercent(t *testing.T) {
	// 100 in on 2023-01-01, 110 out exactly 365 days later → r = 0.1 exactly.
	got := Solve(flowsOf([]string{"2023-01-01", "2024-01-01"}, []string{"-100", "110"}))
	if got == nil || got.String() != "0.1" {
		t.Fatalf("want 0.1, got %v", got)
	}
}

func TestSolve_LeapYearSpanIsSlightlyUnderTenPercent(t *testing.T) {
	// 2024 has 366 days, so 1000 → 1100 over 2024 is a little under 10 % a year.
	got := Solve(flowsOf([]string{"2024-01-01", "2025-01-01"}, []string{"-1000", "1100"}))
	if got == nil {
		t.Fatal("want a solution")
	}
	if r := got.InexactFloat64(); r < 0.09 || r >= 0.1 {
		t.Fatalf("want ~0.0997, got %s", got)
	}
}

func TestSolve_DoublingInOneYearIsOneHundredPercent(t *testing.T) {
	got := Solve(flowsOf([]string{"2025-01-01", "2026-01-01"}, []string{"-1000", "2000"}))
	if got == nil || !got.Equal(dec("1")) {
		t.Fatalf("want 1, got %v", got)
	}
}

func TestSolve_NoGrowthIsZero(t *testing.T) {
	got := Solve(flowsOf([]string{"2025-01-01", "2026-01-01"}, []string{"-1000", "1000"}))
	if got == nil || !got.IsZero() {
		t.Fatalf("want 0, got %v", got)
	}
}

func TestSolve_UnbracketedIsNil(t *testing.T) {
	// Terminal value zero: NPV is negative on the whole bracket → nil.
	if got := Solve(flowsOf([]string{"2023-01-01", "2024-01-01"}, []string{"-100", "0"})); got != nil {
		t.Fatalf("want nil, got %s", got)
	}
}

func TestSolve_RootAboveTheBracketIsNil(t *testing.T) {
	// ×3 in 30 days is an annualised rate far above 10 (1000 %).
	if got := Solve(flowsOf([]string{"2025-01-01", "2025-01-31"}, []string{"-100", "300"})); got != nil {
		t.Fatalf("want nil, got %s", got)
	}
}

// TestSolve_LossBeyondMinus99_9PercentIsNil pins the one owner-accepted difference of
// ADR-112: an annualised loss worse than −99.9 % has no root on [−0.999, 10], so the
// answer is nil ("—" on the page). The former bisection solver widened its bracket
// towards −1 and answered ≈ −1 (−100 %) for the same series.
func TestSolve_LossBeyondMinus99_9PercentIsNil(t *testing.T) {
	// 1000 → 100 in 73 days (0.2 y): (0.1)^(1/0.2) − 1 = −0.99999.
	flows := flowsOf([]string{"2025-01-01", "2025-03-15"}, []string{"-1000", "100"})
	if got := Solve(flows); got != nil {
		t.Fatalf("want nil for an annualised loss beyond −99.9 %%, got %s", got)
	}
	// Just inside the bracket still solves: 1000 → 500 in 0.2 y is (0.5)^5 − 1 = −0.96875.
	inside := flowsOf([]string{"2025-01-01", "2025-03-15"}, []string{"-1000", "500"})
	if got := Solve(inside); got == nil || !got.Equal(dec("-0.96875")) {
		t.Fatalf("want -0.96875, got %v", got)
	}
}

func TestSolve_MultipleDepositsNpvVanishes(t *testing.T) {
	flows := flowsOf(
		[]string{"2022-03-10", "2023-07-01", "2024-01-15", "2025-06-30"},
		[]string{"-1000", "-500", "-250", "2100"},
	)
	got := Solve(flows)
	if got == nil {
		t.Fatal("want a solution")
	}
	r := got.InexactFloat64()
	if r <= -1 || r >= 10 {
		t.Fatalf("solution out of range: %f", r)
	}
	// Independent NPV check at the solution. Tolerance: the x-accuracy (1e-7) times the
	// NPV slope (~1e4 here) plus the scale-6 rounding of the returned rate.
	if npv := independentNPV(flows, r); math.Abs(npv) > 1e-2 {
		t.Fatalf("NPV at the solution should vanish, got %e (r=%f)", npv, r)
	}
	if got.Exponent() < -6 {
		t.Fatalf("want scale <= 6, got %s", got)
	}
}

func TestSolve_TwoDepositsWithinTheBracket(t *testing.T) {
	cases := []struct {
		name    string
		flows   []Flow
		lo, hi  float64
		exactly string
	}{
		{"half-year top-up", flowsOf([]string{"2024-01-01", "2024-07-01", "2025-01-01"}, []string{"-1000", "-500", "1800"}), 0.2, 0.3, ""},
		{"typical deposit series", flowsOf([]string{"2024-01-01", "2024-06-01", "2025-01-01"}, []string{"-4000", "-4000", "9000"}), 0.1, 0.25, ""},
		{"known root 10 %", flowsOf([]string{"2025-01-01", "2026-01-01"}, []string{"-100", "110"}), 0, 0, "0.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Solve(c.flows)
			if got == nil {
				t.Fatal("want a solution")
			}
			if c.exactly != "" {
				if got.String() != c.exactly {
					t.Fatalf("want %s, got %s", c.exactly, got)
				}
				return
			}
			if r := got.InexactFloat64(); r < c.lo || r > c.hi {
				t.Fatalf("want %v..%v, got %s", c.lo, c.hi, got)
			}
		})
	}
}

func TestSolve_IntradayTimesUseCalendarDates(t *testing.T) {
	// The exponent is whole calendar days / 365: times of day do not matter.
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("zoneinfo unavailable")
	}
	flows := []Flow{
		{Date: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), Amount: dec("-100")},
		{Date: time.Date(2024, 1, 1, 23, 30, 0, 0, warsaw), Amount: dec("110")},
	}
	if got := Solve(flows); got == nil || got.String() != "0.1" {
		t.Fatalf("want 0.1, got %v", got)
	}
}

// --- brent ---------------------------------------------------------------------------

func TestBrent_NoBracketAndLinearRoot(t *testing.T) {
	if _, err := brent(func(x float64) float64 { return x*x + 1 }, -0.999, 10, 1e-7, 1e-7, 1000); err == nil {
		t.Fatal("want errNoBracket for x²+1")
	}
	r, err := brent(func(x float64) float64 { return 2*x - 1 }, -0.999, 10, 1e-7, 1e-7, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(r-0.5) > 1e-7 {
		t.Fatalf("want 0.5, got %f", r)
	}
}

// TestMustBisect_AcceptsAStepWhenTheEndsAreSwapped is the F5.3 unit case. Whenever
// |f(a)| < |f(b)| Brent swaps the ends, so a > b and (3a+b)/4 > b. The former acceptance
// test "s < (3a+b)/4 || s > b" rejected every interpolated step in that state; the fixed
// test accepts any s strictly between (3a+b)/4 and b, in either order.
func TestMustBisect_AcceptsAStepWhenTheEndsAreSwapped(t *testing.T) {
	// a = 10, b = −0.999: (3a+b)/4 = 7.25; the secant step −0.5 lies between b and 7.25.
	if mustBisect(-0.5, 10, -0.999, 10, 0, absAccuracy, true) {
		t.Fatal("a step between b and (3a+b)/4 must be accepted when a > b")
	}
	if !mustBisect(8, 10, -0.999, 10, 0, absAccuracy, true) || !mustBisect(-1.5, 10, -0.999, 10, 0, absAccuracy, true) {
		t.Fatal("steps outside ((3a+b)/4, b) must still bisect")
	}
	if mustBisect(1.5, -0.999, 2, -0.999, 0, absAccuracy, true) {
		t.Fatal("the a < b case must keep accepting a step between (3a+b)/4 and b")
	}
}

// TestBrent_ConvergesFasterThanBisection is the F5.3 regression test on a real series:
// with the old acceptance test this series took 27 iterations (plain bisection of
// [−0.999, 10] down to 1e-7 needs ≥ 27); the fixed Brent needs 12.
func TestBrent_ConvergesFasterThanBisection(t *testing.T) {
	flows := flowsOf(
		[]string{"2020-01-01", "2023-02-03", "2024-07-05"},
		[]string{"-3017.56", "-7541.85", "14859.5"},
	)
	npv := npvFunc(flows)
	calls := 0
	r, err := brent(func(r float64) float64 { calls++; return npv(r) }, bracketLower, bracketUpper, absAccuracy, relAccuracy, maxIterations)
	if err != nil {
		t.Fatal(err)
	}
	if iterations := calls - 2; iterations >= 20 {
		t.Fatalf("want < 20 iterations, got %d", iterations)
	}
	if math.Abs(independentNPV(flows, r)) > 1e-3 {
		t.Fatalf("not a root: r=%v", r)
	}
}

// TestSolve_UnconvergedIsNil: leaving the loop without meeting the tolerance is
// errNotConverged, which Solve answers as nil — never the last iterate (F5.3).
func TestSolve_UnconvergedIsNil(t *testing.T) {
	flows := flowsOf([]string{"2022-03-10", "2024-01-15", "2025-06-30"}, []string{"-1000", "-250", "2100"})
	if got := solve(flows, 3); got != nil {
		t.Fatalf("want nil after 3 iterations, got %s", got)
	}
	if _, err := brent(npvFunc(flows), bracketLower, bracketUpper, absAccuracy, relAccuracy, 3); !errors.Is(err, errNotConverged) {
		t.Fatalf("want errNotConverged, got %v", err)
	}
	if got := solve(flows, maxIterations); got == nil {
		t.Fatal("the same series converges with the production limit")
	}
}

// --- Summarize -----------------------------------------------------------------------

func TestSummarize_NoFlowsIsZeroed(t *testing.T) {
	got := Summarize(nil, dec("100"), day("2026-01-01"))
	if got.XirrPct != nil || got.SimpleReturnPct != nil || got.YearsElapsed != nil {
		t.Fatalf("want nil computed fields, got %+v", got)
	}
	if !got.CurrentValuePln.Equal(dec("100")) || !got.TotalInvestedPln.IsZero() {
		t.Fatalf("want value passed through and zero invested, got %+v", got)
	}
}

func TestSummarize_AllPositiveFlowsHaveNothingInvested(t *testing.T) {
	got := Summarize([]Flow{{Date: day("2025-01-01"), Amount: dec("50")}}, dec("50"), day("2026-01-01"))
	if got.XirrPct != nil || !got.TotalInvestedPln.IsZero() || got.SimpleReturnPct != nil {
		t.Fatalf("want zero invested and no rates, got %+v", got)
	}
}

func TestSummarize_SingleDepositDoublesInOneYear(t *testing.T) {
	deposits := []Flow{{Date: day("2025-01-01"), Amount: dec("-1000")}}
	got := Summarize(deposits, dec("2000"), day("2026-01-01"))
	if got.XirrPct == nil || !got.XirrPct.Equal(dec("1")) {
		t.Fatalf("xirr: want 1, got %v", got.XirrPct)
	}
	if got.TotalInvestedPln.String() != "1000" || !got.CurrentValuePln.Equal(dec("2000")) {
		t.Fatalf("totals: got %+v", got)
	}
	if got.SimpleReturnPct == nil || got.SimpleReturnPct.String() != "1" {
		t.Fatalf("simple return: want 1, got %v", got.SimpleReturnPct)
	}
	if got.YearsElapsed == nil || got.YearsElapsed.String() != "1" {
		t.Fatalf("years: want 1, got %v", got.YearsElapsed)
	}
	if len(deposits) != 1 || cap(deposits) != 1 {
		t.Fatal("Summarize must not grow the caller's slice")
	}
}

func TestSummarize_FormulasAtScaleEight(t *testing.T) {
	// (V − I)/I DivRound 8 and days/365 DivRound 8.
	deposits := flowsOf([]string{"2025-01-01", "2025-04-01"}, []string{"-1000", "-2000"})
	got := Summarize(deposits, dec("3100"), day("2025-07-01"))
	if got.SimpleReturnPct == nil || got.SimpleReturnPct.String() != "0.03333333" {
		t.Fatalf("simple return: want 0.03333333, got %v", got.SimpleReturnPct)
	}
	if got.YearsElapsed == nil || got.YearsElapsed.String() != "0.49589041" { // 181/365
		t.Fatalf("years: want 0.49589041, got %v", got.YearsElapsed)
	}
	want := Solve(append(append([]Flow{}, deposits...), Flow{Date: day("2025-07-01"), Amount: dec("3100")}))
	if got.XirrPct == nil || want == nil || !got.XirrPct.Equal(*want) {
		t.Fatalf("xirr: want Solve's %v, got %v", want, got.XirrPct)
	}
}

func TestSummarize_SameDayValuationHasNoYearsElapsed(t *testing.T) {
	got := Summarize([]Flow{{Date: day("2026-01-01"), Amount: dec("-1000")}}, dec("1000"), day("2026-01-01"))
	if got.YearsElapsed != nil {
		t.Fatalf("want nil years for a same-day valuation, got %s", got.YearsElapsed)
	}
	if got.SimpleReturnPct == nil || !got.SimpleReturnPct.IsZero() {
		t.Fatalf("want 0 simple return, got %v", got.SimpleReturnPct)
	}
}

// TestSummarize_ValuationDateIsTheCallersCalendarDay pins the ADR-112 fix of the former
// time.Now().UTC() valuation: 00:30 on 2 Jan in Warsaw is still 1 Jan in UTC, and the
// day count must follow the injected Warsaw instant.
func TestSummarize_ValuationDateIsTheCallersCalendarDay(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("zoneinfo unavailable")
	}
	valuation := time.Date(2026, 1, 2, 0, 30, 0, 0, warsaw)
	got := Summarize([]Flow{{Date: day("2025-01-02"), Amount: dec("-100")}}, dec("110"), valuation)
	if got.YearsElapsed == nil || got.YearsElapsed.String() != "1" {
		t.Fatalf("years: want 1 (365 Warsaw days), got %v", got.YearsElapsed)
	}
	if got.XirrPct == nil || got.XirrPct.String() != "0.1" {
		t.Fatalf("xirr: want 0.1, got %v", got.XirrPct)
	}
}

func TestSummarize_LossBeyondMinus99_9PercentKeepsTheOtherFields(t *testing.T) {
	got := Summarize([]Flow{{Date: day("2025-01-01"), Amount: dec("-1000")}}, dec("100"), day("2025-03-15"))
	if got.XirrPct != nil {
		t.Fatalf("want nil xirr (shown as —), got %s", got.XirrPct)
	}
	if got.SimpleReturnPct == nil || got.SimpleReturnPct.String() != "-0.9" || got.YearsElapsed == nil {
		t.Fatalf("simple return / years must still be computed, got %+v", got)
	}
}

// independentNPV recomputes NPV(r) from scratch (calendar days / 365 from flows[0]).
func independentNPV(flows []Flow, r float64) float64 {
	y0, m0, d0 := flows[0].Date.Date()
	start := time.Date(y0, m0, d0, 0, 0, 0, 0, time.UTC)
	npv := 0.0
	for _, f := range flows {
		y, m, d := f.Date.Date()
		days := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Sub(start).Hours() / 24
		npv += f.Amount.InexactFloat64() / math.Pow(1+r, days/365)
	}
	return npv
}
