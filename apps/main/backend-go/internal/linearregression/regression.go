package linearregression

import (
	"math"
	"time"

	"investing-app/common-go/httpx"
)

// validIntervals mirrors LinearRegressionService.VALID_INTERVALS.
var validIntervals = map[string]bool{"d": true, "wk": true, "mo": true}

// ValidateInterval rejects anything but d/wk/mo with the legacy literal message — 400
// VALIDATION_ERROR.
func ValidateInterval(interval string) error {
	if !validIntervals[interval] {
		return httpx.NewValidationError("", "interval must be d, wk, or mo; got: "+interval)
	}
	return nil
}

// FitLogLinear fits the log-linear OLS regression over the full point history — the
// direct port of LinearRegressionService.compute's SimpleRegression +
// DescriptiveStatistics pair, in float64 IEEE754 (same arithmetic the Apache Commons
// Math classes perform, so bit-level drift stays within ordinary summation-order
// noise — asserted against hardcoded goldens in regression_test.go).
//
// x is 1-indexed (oldest = 1), y = ln(close). slope/intercept follow the textbook
// normal equations SimpleRegression evaluates; stdDev is the SAMPLE standard deviation
// (n-1) of the ln residuals, NaN when n < 2 (DescriptiveStatistics returns NaN there;
// Go's 0/0 float division yields NaN naturally, no special-casing needed beyond the
// explicit guard for clarity).
func FitLogLinear(points []PricedOHLCPoint) (slope, intercept, stdDev float64) {
	n := float64(len(points))
	var sx, sy, sxx, sxy float64
	for k, p := range points {
		x := float64(k + 1)
		y := math.Log(p.Close)
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	denom := n*sxx - sx*sx
	slope = (n*sxy - sx*sy) / denom
	intercept = (sy - slope*sx) / n

	if len(points) < 2 {
		return slope, intercept, math.NaN()
	}
	var sumSq float64
	mean := 0.0
	residuals := make([]float64, len(points))
	for k, p := range points {
		r := math.Log(p.Close) - (intercept + slope*float64(k+1))
		residuals[k] = r
		mean += r
	}
	mean /= n
	for _, r := range residuals {
		d := r - mean
		sumSq += d * d
	}
	stdDev = math.Sqrt(sumSq / (n - 1))
	return slope, intercept, stdDev
}

// BuildPoint ports LinearRegressionService.buildPoint: trendline = exp(lnTrendline),
// bands = trendline·exp(k·sd) for k = ±1..±4.
func BuildPoint(date time.Time, count int, p PricedOHLCPoint, slope, intercept, stdDev float64) DataPoint {
	logN := math.Log(p.Close)
	lnTrendline := intercept + slope*float64(count)
	trendline := math.Exp(lnTrendline)
	residual := logN - lnTrendline
	close, high, low := p.Close, p.High, p.Low
	return DataPoint{
		Date: date, Count: count,
		Close: &close, High: &high, Low: &low,
		LogN: &logN, Trendline: trendline, LnTrendline: lnTrendline, Residual: &residual,
		Sd1Plus: trendline * math.Exp(stdDev), Sd2Plus: trendline * math.Exp(2*stdDev),
		Sd1Minus: trendline * math.Exp(-stdDev), Sd2Minus: trendline * math.Exp(-2*stdDev),
		Sd3Plus: trendline * math.Exp(3*stdDev), Sd3Minus: trendline * math.Exp(-3*stdDev),
		Sd4Plus: trendline * math.Exp(4*stdDev), Sd4Minus: trendline * math.Exp(-4*stdDev),
	}
}

// BuildFuturePoint ports buildFuturePoint: null actuals, trendline + bands only.
func BuildFuturePoint(date time.Time, count int, slope, intercept, stdDev float64) DataPoint {
	lnTrendline := intercept + slope*float64(count)
	trendline := math.Exp(lnTrendline)
	return DataPoint{
		Date: date, Count: count,
		Trendline: trendline, LnTrendline: lnTrendline,
		Sd1Plus: trendline * math.Exp(stdDev), Sd2Plus: trendline * math.Exp(2*stdDev),
		Sd1Minus: trendline * math.Exp(-stdDev), Sd2Minus: trendline * math.Exp(-2*stdDev),
		Sd3Plus: trendline * math.Exp(3*stdDev), Sd3Minus: trendline * math.Exp(-3*stdDev),
		Sd4Plus: trendline * math.Exp(4*stdDev), Sd4Minus: trendline * math.Exp(-4*stdDev),
	}
}

// AdvanceDate ports LinearRegressionService.advanceDate: wk adds weeks, mo adds months
// with day=1, d adds days.
func AdvanceDate(base time.Time, interval string, steps int) time.Time {
	switch interval {
	case "wk":
		return base.AddDate(0, 0, 7*steps)
	case "mo":
		shifted := base.AddDate(0, steps, 0)
		return time.Date(shifted.Year(), shifted.Month(), 1, 0, 0, 0, 0, time.UTC)
	default: // "d" — validated before use
		return base.AddDate(0, 0, steps)
	}
}
