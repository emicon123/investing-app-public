package linearregression

import (
	"context"
	"time"

	"investing-app/common-go/httpx"
)

// HistoryFetcher is the narrow slice of HistoryService this service (and the
// trade-chart package) needs — declared as an interface so Compute is unit-testable
// with canned OHLC series and no HTTP at all.
type HistoryFetcher interface {
	FetchOHLCByRange(ctx context.Context, yahooTicker, interval string, period1, period2 int64) []PricedOHLCPoint
}

// Service is the linear-regression service layer: handler -> Service -> HistoryService
// (no database — this domain is a pure compute-over-live-fetches shape).
type Service struct {
	history HistoryFetcher
	now     func() time.Time // overridable clock (tests); defaults to time.Now
}

// NewService wires the history facade (explicit constructor wiring, ADR-094 §2).
func NewService(history HistoryFetcher) *Service {
	return &Service{history: history, now: time.Now}
}

// Compute fits the log-linear regression over the full available history and returns
// the [fromDate, toDate]-filtered rows plus `since` extrapolated future rows — the
// direct port of LinearRegressionService.compute. `since` is a future-extrapolation
// count (spec fix 2026-09-15): negative means "no extrapolation", never an error.
func (s *Service) Compute(ctx context.Context, ticker, interval string, fromDate, toDate *time.Time, since int) (Result, error) {
	if err := ValidateInterval(interval); err != nil {
		return Result{}, err
	}

	allPoints := s.fetchOHLC(ctx, ticker, interval, fromDate, toDate)
	if len(allPoints) == 0 {
		return Result{}, httpx.NewValidationError("", "No data found for ticker: "+ticker)
	}
	// One priceSource for the whole response — every point shares it by construction
	// (ADR-085 §8).
	priceSource := allPoints[0].Source

	slope, intercept, stdDev := FitLogLinear(allPoints)

	dataPoints := make([]DataPoint, 0, len(allPoints))
	for k, p := range allPoints {
		if fromDate != nil && p.Date.Before(*fromDate) {
			continue
		}
		if toDate != nil && p.Date.After(*toDate) {
			continue
		}
		dataPoints = append(dataPoints, BuildPoint(p.Date, k+1, p, slope, intercept, stdDev))
	}

	if since > 0 {
		lastDate := allPoints[len(allPoints)-1].Date
		baseK := len(allPoints)
		for i := 1; i <= since; i++ {
			futureDate := AdvanceDate(lastDate, interval, i)
			dataPoints = append(dataPoints, BuildFuturePoint(futureDate, baseK+i, slope, intercept, stdDev))
		}
	}

	return Result{
		Ticker: ticker, Interval: interval,
		Slope: slope, Intercept: intercept, StdDev: stdDev,
		DataPoints: dataPoints, PriceSource: priceSource,
	}, nil
}

// fetchOHLC ports LinearRegressionService.fetchOHLC's window policy: weekly/monthly
// always request the full history (1970 through today — Yahoo returns whatever it has
// from inception, so fromDate/toDate only filter rows afterward, never bound this
// fetch); daily keeps its narrower 5-year default, or the caller's explicit window.
// An explicit period1/period2 range is always requested — never the range=max
// shorthand, which Yahoo silently degrades to coarser-than-requested granularity for
// long histories.
func (s *Service) fetchOHLC(ctx context.Context, ticker, interval string, fromDate, toDate *time.Time) []PricedOHLCPoint {
	yahooInterval := "1" + interval
	today := WarsawTodayAt(s.now())

	if interval != "d" {
		p1 := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
		p2 := today.Add(23*time.Hour + 59*time.Minute + 59*time.Second).Unix()
		return s.history.FetchOHLCByRange(ctx, ticker, yahooInterval, p1, p2)
	}

	from := today.AddDate(-5, 0, 0)
	if fromDate != nil {
		from = *fromDate
	}
	to := today
	if toDate != nil {
		to = *toDate
	}
	p1 := from.Unix()
	p2 := to.Add(23*time.Hour + 59*time.Minute + 59*time.Second).Unix()
	return s.history.FetchOHLCByRange(ctx, ticker, yahooInterval, p1, p2)
}
