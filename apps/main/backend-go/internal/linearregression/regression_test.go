package linearregression

import (
	"math"
	"testing"
	"time"
)

// Pure-math tests for FitLogLinear — no network, no database. Goldens below were
// computed independently (python3 float64 IEEE754); slope/intercept/stdDev must match
// to 1e-12, the same arithmetic Apache Commons Math's SimpleRegression +
// DescriptiveStatistics perform.

func pricedCloses(dates []time.Time, closes []float64) []PricedOHLCPoint {
	pts := make([]PricedOHLCPoint, len(closes))
	for i, c := range closes {
		pts[i] = PricedOHLCPoint{Date: dates[i], Open: c, Close: c, High: c, Low: c, Source: PriceSourceYahoo}
	}
	return pts
}

func days(n int) []time.Time {
	base := utcDate(2025, time.January, 1)
	out := make([]time.Time, n)
	for i := range out {
		out[i] = base.AddDate(0, 0, i)
	}
	return out
}

func TestFitLogLinear_TwoPointExact(t *testing.T) {
	// ln = [0, 1] over x = [1, 2] -> slope 1, intercept -1, zero residuals.
	pts := pricedCloses(days(2), []float64{1.0, math.E})
	slope, intercept, stdDev := FitLogLinear(pts)
	if slope != 1 || intercept != -1 || stdDev != 0 {
		t.Fatalf("expected (1,-1,0), got (%v,%v,%v)", slope, intercept, stdDev)
	}
}

func TestFitLogLinear_ThreePointGolden(t *testing.T) {
	// Golden: slope=0.4581453659370804, intercept=4.22535875825863,
	// stdDev=0.13567836093256327 (sample, n-1).
	pts := pricedCloses(days(3), []float64{100, 200, 250})
	slope, intercept, stdDev := FitLogLinear(pts)
	if math.Abs(slope-0.4581453659370804) > 1e-12 {
		t.Fatalf("slope: got %.16f", slope)
	}
	if math.Abs(intercept-4.22535875825863) > 1e-12 {
		t.Fatalf("intercept: got %.16f", intercept)
	}
	if math.Abs(stdDev-0.13567836093256327) > 1e-12 {
		t.Fatalf("stdDev: got %.16f", stdDev)
	}
}

func TestFitLogLinear_PerfectExponential(t *testing.T) {
	// close_k = 100*e^{0.05k} -> slope 0.05, intercept ln(100), ~zero residuals.
	closes := make([]float64, 5)
	for k := range closes {
		closes[k] = 100 * math.Exp(0.05*float64(k+1))
	}
	slope, intercept, stdDev := FitLogLinear(pricedCloses(days(5), closes))
	if math.Abs(slope-0.05) > 1e-12 {
		t.Fatalf("slope: got %.16f", slope)
	}
	if math.Abs(intercept-math.Log(100)) > 1e-12 {
		t.Fatalf("intercept: got %.16f", intercept)
	}
	if stdDev > 1e-12 {
		t.Fatalf("perfect fit must have ~zero stdDev, got %v", stdDev)
	}
}

func TestFitLogLinear_SinglePointIsNaN(t *testing.T) {
	// n=1: degenerate like SimpleRegression/DescriptiveStatistics — NaN, never a panic.
	pts := pricedCloses(days(1), []float64{100})
	slope, intercept, stdDev := FitLogLinear(pts)
	if !math.IsNaN(slope) || !math.IsNaN(intercept) || !math.IsNaN(stdDev) {
		t.Fatalf("expected NaN triple, got (%v,%v,%v)", slope, intercept, stdDev)
	}
}

func TestBuildPoint_BandsAreTrendlineTimesExp(t *testing.T) {
	p := PricedOHLCPoint{Date: days(1)[0], Open: 100, Close: 110, High: 115, Low: 99, Source: PriceSourceYahoo}
	dp := BuildPoint(p.Date, 1, p, 0.05, 4.6, 0.1)
	lnTrend := 4.6 + 0.05
	if math.Abs(dp.LnTrendline-lnTrend) > 1e-15 || math.Abs(dp.Trendline-math.Exp(lnTrend)) > 1e-9 {
		t.Fatalf("trendline wrong: %+v", dp)
	}
	if math.Abs(dp.Sd1Plus-dp.Trendline*math.Exp(0.1)) > 1e-9 ||
		math.Abs(dp.Sd4Minus-dp.Trendline*math.Exp(-0.4)) > 1e-9 {
		t.Fatalf("bands wrong: %+v", dp)
	}
	if dp.Close == nil || *dp.Close != 110 || dp.Residual == nil {
		t.Fatalf("actuals must be set on history rows: %+v", dp)
	}
	fp := BuildFuturePoint(p.Date, 9, 0.05, 4.6, 0.1)
	if fp.Close != nil || fp.LogN != nil || fp.Residual != nil {
		t.Fatalf("future rows must have null actuals: %+v", fp)
	}
	if fp.Count != 9 {
		t.Fatalf("future count must continue the 1-indexed series, got %d", fp.Count)
	}
}

func TestAdvanceDate(t *testing.T) {
	base := utcDate(2026, time.March, 15)
	if got := AdvanceDate(base, "wk", 2); !got.Equal(utcDate(2026, time.March, 29)) {
		t.Fatalf("wk: got %v", got)
	}
	if got := AdvanceDate(base, "d", 3); !got.Equal(utcDate(2026, time.March, 18)) {
		t.Fatalf("d: got %v", got)
	}
	// mo: shift months, then pin day=1.
	if got := AdvanceDate(base, "mo", 1); !got.Equal(utcDate(2026, time.April, 1)) {
		t.Fatalf("mo: got %v", got)
	}
}

func TestValidateInterval(t *testing.T) {
	for _, ok := range []string{"d", "wk", "mo"} {
		if err := ValidateInterval(ok); err != nil {
			t.Fatalf("%s must pass: %v", ok, err)
		}
	}
	if err := ValidateInterval("w"); err == nil || err.Error() != "interval must be d, wk, or mo; got: w" {
		t.Fatalf("expected literal message, got %v", err)
	}
}
