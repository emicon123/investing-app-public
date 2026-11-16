package xirr

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// TestSolve_AgreesWithAnIndependentBisection is the ADR-112 property test (the review's
// oracle, seeded): random deposit series of 1–40 deposits over 0.1–6 years with a terminal
// value of −60 %…+200 % of the invested sum. Inside the bracket Solve must agree with a
// plain bisection to 3·10⁻⁵; outside it (annualised rate ≤ −0.999 or ≥ 10) Solve answers nil.
func TestSolve_AgreesWithAnIndependentBisection(t *testing.T) {
	const (
		cases     = 2500
		tolerance = 3e-5
	)
	rng := rand.New(rand.NewPCG(178, 112))
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	compared, outside, maxDiff := 0, 0, 0.0
	for i := range cases {
		flows := randomSeries(rng, base)
		want := referenceRate(flows)
		got := Solve(flows)
		if want <= bracketLower || want >= bracketUpper {
			outside++
			if got != nil {
				t.Fatalf("case %d: true rate %v is outside the bracket, want nil, got %s", i, want, got)
			}
			continue
		}
		if got == nil {
			t.Fatalf("case %d: true rate %v, Solve answered nil", i, want)
		}
		diff := math.Abs(got.InexactFloat64() - want)
		maxDiff = max(maxDiff, diff)
		if diff > tolerance {
			t.Fatalf("case %d: Solve %s vs bisection %v (|Δ| = %e)", i, got, want, diff)
		}
		compared++
	}
	if compared < 2000 {
		t.Fatalf("only %d of %d cases fell inside the bracket; the generator lost its coverage", compared, cases)
	}
	t.Logf("compared %d cases (max |Δ| %.2e), %d outside the bracket answered nil", compared, maxDiff, outside)
}

// randomSeries draws deposits (negative, 100–10 000 PLN at 2 decimals) on random days of a
// 0.1–6 year span, the first one on day 0, plus the terminal value on the span's last day.
func randomSeries(rng *rand.Rand, base time.Time) []Flow {
	spanDays := int(math.Round((0.1 + rng.Float64()*5.9) * 365))
	n := 1 + rng.IntN(40)
	offsets := make([]int, n)
	for i := 1; i < n; i++ {
		offsets[i] = rng.IntN(spanDays + 1)
	}
	sort.Ints(offsets)
	flows := make([]Flow, 0, n+1)
	invested := decimal.Zero
	for _, off := range offsets {
		amount := decimal.NewFromFloat(100 + rng.Float64()*9900).Round(2)
		invested = invested.Add(amount)
		flows = append(flows, Flow{Date: base.AddDate(0, 0, off), Amount: amount.Neg()})
	}
	growth := decimal.NewFromFloat(1 + (-0.6 + rng.Float64()*2.6))
	value := invested.Mul(growth).Round(2)
	return append(flows, Flow{Date: base.AddDate(0, 0, spanDays), Amount: value})
}

// referenceRate solves NPV(r) = 0 by bisection on (−1, 10⁶). These series have every
// negative flow no later than the single positive one, so NPV·(1+r)^T is decreasing in r
// and the root is unique; the bracket ends are returned when the root lies beyond them.
func referenceRate(flows []Flow) float64 {
	npv := func(r float64) float64 { return independentNPV(flows, r) }
	lo, hi := -1+1e-12, 1e6
	if npv(lo) <= 0 {
		return lo
	}
	if npv(hi) >= 0 {
		return hi
	}
	for range 300 {
		mid := (lo + hi) / 2
		if npv(mid) > 0 {
			lo = mid
		} else {
			hi = mid
		}
		if hi-lo < 1e-13 {
			break
		}
	}
	return (lo + hi) / 2
}
