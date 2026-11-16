package macro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// This file ports BenchmarkHistoryService: weekly closes of the 4 whitelisted reference
// symbols (BTC-USD, ^NDX, GC=F, ^HSI) via Yahoo's v8 chart API with an explicit
// period1/period2 window (never range=max — that silently degrades long history to coarser,
// irregular candles), cached server-side for 12h. None of the four are Virtune ETP tickers,
// so ADR-085 reconstruction never triggers here — a direct Yahoo fetch is the whole path.

// SupportedBenchmarks is the whitelist — anything else is a 400 (controller rejects before Yahoo).
var SupportedBenchmarks = map[string]bool{
	"BTC-USD": true,
	"^NDX":    true,
	"GC=F":    true,
	"^HSI":    true,
}

// benchmarkEarliest is the ADR-069 backtest/chart window start.
var benchmarkEarliest = time.Date(2011, 7, 1, 0, 0, 0, 0, time.UTC)

const benchmarkCacheTTL = 12 * time.Hour

// BenchmarkPoint is one weekly close. Source is always YAHOO for the four whitelisted symbols
// (carried explicitly because the wire always serializes priceSource:"YAHOO").
type BenchmarkPoint struct {
	Symbol string
	Date   time.Time
	Close  float64
	Source string
}

// BenchmarkService serves the blend-chart overlay with a 12h in-memory cache per symbol. Cold start
// is fine (empty cache → live fetch).
type BenchmarkService struct {
	mu      sync.Mutex
	cache   map[string]cachedBenchmark
	http    *http.Client
	baseURL string
	nowFunc func() time.Time // stubbed in tests for a deterministic period2
}

type cachedBenchmark struct {
	points    []BenchmarkPoint
	fetchedAt time.Time
}

func NewBenchmarkService() *BenchmarkService {
	return &BenchmarkService{
		cache:   map[string]cachedBenchmark{},
		http:    &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://query2.finance.yahoo.com",
		nowFunc: time.Now,
	}
}

// Supports ports BenchmarkHistoryService.supports.
func (s *BenchmarkService) Supports(symbol string) bool { return SupportedBenchmarks[symbol] }

// Get ports BenchmarkHistoryService.getBenchmarkHistory: fresh cache hit wins, else an
// explicit 1wk period1/period2 fetch from the 2011-07 chart start through Warsaw-today end
// of day. Fail-soft like every other client here: a failed fetch yields an empty slice, and
// a failed fetch does NOT poison the cache (a stale entry is still served while fresh).
func (s *BenchmarkService) Get(ctx context.Context, symbol string) ([]BenchmarkPoint, error) {
	if !s.Supports(symbol) {
		return nil, fmt.Errorf("macro: unsupported benchmark symbol: %s", symbol)
	}
	now := s.nowFunc()
	s.mu.Lock()
	if c, ok := s.cache[symbol]; ok && c.fetchedAt.Add(benchmarkCacheTTL).After(now) {
		points := c.points
		s.mu.Unlock()
		return points, nil
	}
	stale := s.cache[symbol].points
	s.mu.Unlock()

	points := s.fetchWeekly(ctx, symbol, now)
	if points == nil {
		// Fetch failed entirely — serve stale rather than nothing (chart overlay must not
		// break on a Yahoo hiccup); empty only when nothing was ever cached.
		if stale != nil {
			return stale, nil
		}
		return []BenchmarkPoint{}, nil
	}
	s.mu.Lock()
	s.cache[symbol] = cachedBenchmark{points: points, fetchedAt: now}
	s.mu.Unlock()
	return points, nil
}

// benchmarkPeriod2 is the window end: Warsaw-today at 23:59:59, read as UTC epoch seconds.
func benchmarkPeriod2(now time.Time) int64 {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		loc = time.UTC
	}
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 23, 59, 59, 0, time.UTC).Unix()
}

func (s *BenchmarkService) fetchWeekly(ctx context.Context, symbol string, now time.Time) []BenchmarkPoint {
	p1 := benchmarkEarliest.Unix()
	p2 := benchmarkPeriod2(now)
	u := s.baseURL + "/v8/finance/chart/" + url.PathEscape(symbol) +
		fmt.Sprintf("?interval=1wk&period1=%d&period2=%d", p1, p2)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := s.http.Do(req)
	if err != nil {
		slog.Warn("macro: benchmark fetch failed", "symbol", symbol, "error", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("macro: benchmark fetch status", "symbol", symbol, "status", resp.StatusCode)
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	return parseBenchmarkChart(symbol, body)
}

// parseBenchmarkChart ports YahooFinanceClient.parseOHLC for this service's needs: date (UTC
// calendar day) + close only; null/NaN/Inf closes are skipped, open/high/low are irrelevant
// to the overlay.
func parseBenchmarkChart(symbol string, body []byte) []BenchmarkPoint {
	var resp struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	if len(resp.Chart.Result) == 0 {
		return []BenchmarkPoint{}
	}
	r := resp.Chart.Result[0]
	if len(r.Indicators.Quote) == 0 {
		return []BenchmarkPoint{}
	}
	closes := r.Indicators.Quote[0].Close
	out := []BenchmarkPoint{}
	for i, ts := range r.Timestamp {
		var close *float64
		if i < len(closes) {
			close = closes[i]
		}
		if close == nil {
			continue
		}
		out = append(out, BenchmarkPoint{
			Symbol: symbol,
			Date:   time.Unix(ts, 0).UTC().Truncate(24 * time.Hour),
			Close:  *close,
			Source: "YAHOO",
		})
	}
	return out
}
