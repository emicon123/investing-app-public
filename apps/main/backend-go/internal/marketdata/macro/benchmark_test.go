package macro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Canned Yahoo v8 chart response: 3 weekly candles, one null close (market holiday week).
const cannedChart = `{"chart":{"result":[{"timestamp":[1310085600,1310690400,1311295200],` +
	`"indicators":{"quote":[{"close":[15.4,null,15.9]}]}}],"error":null}}`

func chartServer(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if !strings.Contains(r.URL.RawQuery, "interval=1wk") || !strings.Contains(r.URL.RawQuery, "period1=") {
			t.Errorf("expected explicit 1wk period1/period2 query, got %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedChart))
	}))
}

func testBenchmark(t *testing.T, now time.Time) (*BenchmarkService, *int) {
	t.Helper()
	var hits int
	srv := chartServer(t, &hits)
	t.Cleanup(srv.Close)
	b := NewBenchmarkService()
	b.baseURL = srv.URL
	b.nowFunc = func() time.Time { return now }
	return b, &hits
}

func TestBenchmark_ParsesWeeklyClosesSkippingNulls(t *testing.T) {
	b, hits := testBenchmark(t, time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	pts, err := b.Get(context.Background(), "BTC-USD")
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("expected 2 points (null skipped), got %d", len(pts))
	}
	if pts[0].Symbol != "BTC-USD" || pts[0].Close != 15.4 || pts[0].Source != "YAHOO" {
		t.Fatalf("unexpected first point: %+v", pts[0])
	}
	if got := pts[0].Date.Format("2006-01-02"); got != "2011-07-08" {
		t.Fatalf("expected UTC calendar date 2011-07-08, got %s", got)
	}
	if *hits != 1 {
		t.Fatalf("expected 1 upstream hit, got %d", *hits)
	}
	// Second call within 12h → cache hit, no new upstream request.
	if _, err := b.Get(context.Background(), "BTC-USD"); err != nil {
		t.Fatal(err)
	}
	if *hits != 1 {
		t.Fatalf("expected cache hit (still 1 upstream hit), got %d", *hits)
	}
}

func TestBenchmark_CacheExpiresAfter12h(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	var hits int
	srv := chartServer(t, &hits)
	t.Cleanup(srv.Close)
	b := NewBenchmarkService()
	b.baseURL = srv.URL
	current := now
	b.nowFunc = func() time.Time { return current }
	if _, err := b.Get(context.Background(), "^NDX"); err != nil {
		t.Fatal(err)
	}
	current = now.Add(12*time.Hour + time.Minute) // past TTL
	if _, err := b.Get(context.Background(), "^NDX"); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("expected refetch after TTL, hits=%d", hits)
	}
}

func TestBenchmark_UnsupportedSymbol(t *testing.T) {
	b, hits := testBenchmark(t, time.Now())
	if b.Supports("AAPL") {
		t.Fatalf("AAPL must not be whitelisted")
	}
	for _, s := range []string{"BTC-USD", "^NDX", "GC=F", "^HSI"} {
		if !b.Supports(s) {
			t.Fatalf("%s must be whitelisted", s)
		}
	}
	if _, err := b.Get(context.Background(), "AAPL"); err == nil {
		t.Fatalf("expected error for unsupported symbol")
	}
	if *hits != 0 {
		t.Fatalf("unsupported symbol must never reach Yahoo, hits=%d", *hits)
	}
}

func TestBenchmark_FetchFailure_ServesEmptyNotError(t *testing.T) {
	b := NewBenchmarkService()
	b.baseURL = "http://127.0.0.1:1" // nothing listens — fail-soft
	pts, err := b.Get(context.Background(), "GC=F")
	if err != nil {
		t.Fatalf("fail-soft: expected nil error, got %v", err)
	}
	if len(pts) != 0 {
		t.Fatalf("expected empty on failure, got %v", pts)
	}
}

func TestBenchmarkPeriod2_WarsawEndOfDayAsUTC(t *testing.T) {
	// Warsaw-today 23:59:59 read as UTC epoch — the exact Java formula
	// (WarsawTime.today().atTime(MAX).atZone(UTC)).
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if got, want := benchmarkPeriod2(now), int64(1786838399); got != want {
		t.Fatalf("expected period2 %d (2026-08-15T23:59:59Z), got %d", want, got)
	}
}
