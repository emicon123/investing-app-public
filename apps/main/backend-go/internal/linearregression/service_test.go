package linearregression

import (
	"context"
	"errors"
	"testing"
	"time"

	"investing-app/common-go/httpx"
)

// stubHistoryFetcher is a canned OHLC series — Compute tests never touch HTTP.
type stubHistoryFetcher struct {
	points       []PricedOHLCPoint
	lastP1       int64
	lastP2       int64
	lastInterval string
}

func (f *stubHistoryFetcher) FetchOHLCByRange(_ context.Context, _ string, interval string, p1, p2 int64) []PricedOHLCPoint {
	f.lastInterval = interval
	f.lastP1, f.lastP2 = p1, p2
	return f.points
}

func stubPoints(n int) []PricedOHLCPoint {
	pts := make([]PricedOHLCPoint, n)
	for k := range pts {
		c := 100 + float64(k)*10
		pts[k] = PricedOHLCPoint{Date: days(n)[k], Open: c, Close: c, High: c, Low: c, Source: PriceSourceYahoo}
	}
	return pts
}

func TestCompute_FiltersAndExtrapolates(t *testing.T) {
	stub := &stubHistoryFetcher{points: stubPoints(5)}
	svc := NewService(stub)

	from := days(5)[1]
	to := days(5)[3]
	res, err := svc.Compute(context.Background(), "BTC-USD", "wk", &from, &to, 2)
	if err != nil {
		t.Fatal(err)
	}
	// 3 filtered history rows (counts 2,3,4 — fit stays over the FULL 5) + 2 future.
	if len(res.DataPoints) != 5 {
		t.Fatalf("expected 3+2 rows, got %d", len(res.DataPoints))
	}
	if res.DataPoints[0].Count != 2 || res.DataPoints[2].Count != 4 {
		t.Fatalf("counts must index the full fit: %+v", res.DataPoints)
	}
	if res.DataPoints[0].Close == nil || *res.DataPoints[0].Close != 110 {
		t.Fatalf("history rows carry actuals: %+v", res.DataPoints[0])
	}
	future := res.DataPoints[3]
	if future.Close != nil || future.Count != 6 {
		t.Fatalf("future rows extrapolate with null actuals: %+v", future)
	}
	if !future.Date.Equal(AdvanceDate(days(5)[4], "wk", 1)) {
		t.Fatalf("future dates advance from the last actual point: %v", future.Date)
	}
	if res.Ticker != "BTC-USD" || res.Interval != "wk" || res.PriceSource != PriceSourceYahoo {
		t.Fatalf("unexpected result header: %+v", res)
	}
}

func TestCompute_NegativeSinceMeansNone(t *testing.T) {
	stub := &stubHistoryFetcher{points: stubPoints(3)}
	svc := NewService(stub)
	res, err := svc.Compute(context.Background(), "BTC-USD", "wk", nil, nil, -5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.DataPoints) != 3 {
		t.Fatalf("negative since must not extrapolate and must not error: %+v", res.DataPoints)
	}
}

func TestCompute_BadIntervalAndEmptyAre400(t *testing.T) {
	svc := NewService(&stubHistoryFetcher{points: stubPoints(3)})
	if _, err := svc.Compute(context.Background(), "BTC-USD", "w", nil, nil, 0); !isValidation400(err) {
		t.Fatalf("bad interval must be 400, got %v", err)
	}
	empty := NewService(&stubHistoryFetcher{})
	if _, err := empty.Compute(context.Background(), "NOPE", "wk", nil, nil, 0); !isValidation400(err) {
		t.Fatalf("empty history must be 400, got %v", err)
	}
}

func TestCompute_FetchWindows(t *testing.T) {
	stub := &stubHistoryFetcher{points: stubPoints(2)}
	svc := NewService(stub)
	svc.now = func() time.Time { return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC) }

	_, _ = svc.Compute(context.Background(), "BTC-USD", "wk", nil, nil, 0)
	if stub.lastInterval != "1wk" {
		t.Fatalf("yahoo interval must be 1-prefixed, got %q", stub.lastInterval)
	}
	p1 := time.Unix(stub.lastP1, 0).UTC()
	if p1.Year() != 1970 {
		t.Fatalf("wk/mo must fetch full history from 1970, got %v", p1)
	}

	_, _ = svc.Compute(context.Background(), "BTC-USD", "d", nil, nil, 0)
	if stub.lastInterval != "1d" {
		t.Fatalf("got %q", stub.lastInterval)
	}
	p1d := time.Unix(stub.lastP1, 0).UTC()
	if p1d.Year() != 2021 {
		t.Fatalf("daily default must be a 5-year window, got %v", p1d)
	}
}

func isValidation400(err error) bool {
	var appErr *httpx.AppError
	return errors.As(err, &appErr) && appErr.Status == 400 && appErr.Code == httpx.CodeValidationError
}
