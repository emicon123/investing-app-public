package priceprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// This file holds the two shared helpers of the admin/backfill split: the OHLC history seam and the
// nearest-close lookup. The per-app backfill math (portfolioB's cutoff + companions + transfers + CHF
// special vs portfolioC's flat per-portfolio loop) stays per-app — only these two pieces are shared.

// OHLCPoint is one daily bar's (date, close) — the Go equivalent of
// YahooFinanceClient.OHLCPoint. Only the close is carried: the backfill prices
// positions off closes alone.
type OHLCPoint struct {
	Date  time.Time
	Close float64
}

// OHLCProvider is the history-data seam the backfill builds its price/FX maps
// through — production passes the Yahoo implementation below, tests pass a stub.
// Mirrors YahooFinanceClient.fetchOHLCByRange(ticker, "1d", period1, period2)
// (periods are Unix seconds, interval is always "1d" here).
type OHLCProvider interface {
	FetchOHLC(ctx context.Context, ticker, interval string, period1, period2 int64) ([]OHLCPoint, error)
}

// NearestClose ports HistoriaPositionValuesBackfillService.nearestClose: the close
// at the greatest date <= target (floorEntry), falling back to the earliest point
// when the target predates the whole series. Reports false on an empty series.
func NearestClose(points []OHLCPoint, target time.Time) (float64, bool) {
	if len(points) == 0 {
		return 0, false
	}
	t := dateOnly(target)
	best := -1
	for i, p := range points {
		if !dateOnly(p.Date).After(t) {
			if best == -1 || dateOnly(points[best].Date).Before(dateOnly(p.Date)) {
				best = i
			}
		}
	}
	if best != -1 {
		return points[best].Close, true
	}
	earliest := points[0]
	for _, p := range points[1:] {
		if dateOnly(p.Date).Before(dateOnly(earliest.Date)) {
			earliest = p
		}
	}
	return earliest.Close, true
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// YahooOHLCProvider fetches daily bars from Yahoo Finance's public v8 chart API
// (no auth — the same endpoint family YahooProvider.fetchViaChart already uses).
// It is the production OHLCProvider for both siblings' backfills.
type YahooOHLCProvider struct {
	client  *http.Client
	baseURL string
}

// NewYahooOHLCProvider constructs a YahooOHLCProvider with the same 10s overall
// timeout budget as YahooProvider.
func NewYahooOHLCProvider() *YahooOHLCProvider {
	return &YahooOHLCProvider{
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: yahooChartBaseURL,
	}
}

type yahooHistoryResponse struct {
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

// FetchOHLC fetches daily closes for ticker in [period1, period2] (Unix seconds). A null close
// (market holiday, no trade) is skipped, not zero-filled — the backfill's NearestClose bridges
// those gaps (a holiday simply has no entry, so the floor lookup lands on the prior session).
func (p *YahooOHLCProvider) FetchOHLC(ctx context.Context, ticker, interval string, period1, period2 int64) ([]OHLCPoint, error) {
	if interval == "" {
		interval = "1d"
	}
	reqURL := p.baseURL + "/v8/finance/chart/" + url.PathEscape(ticker) +
		fmt.Sprintf("?period1=%d&period2=%d&interval=%s&events=history", period1, period2, url.QueryEscape(interval))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("yahoo-ohlc: build request: %w", err)
	}
	req.Header.Set("User-Agent", yahooBrowserUA)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yahoo-ohlc: request failed for %s: %w", ticker, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("yahoo-ohlc: returned status %d for %s", resp.StatusCode, ticker)
	}

	var body yahooHistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("yahoo-ohlc: decode response for %s: %w", ticker, err)
	}
	if len(body.Chart.Result) == 0 {
		return nil, fmt.Errorf("yahoo-ohlc: empty result for %s", ticker)
	}
	result := body.Chart.Result[0]
	if len(result.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("yahoo-ohlc: no quote data for %s", ticker)
	}
	closes := result.Indicators.Quote[0].Close
	out := make([]OHLCPoint, 0, len(result.Timestamp))
	n := min(len(closes), len(result.Timestamp))
	for i := range n {
		c := closes[i]
		if c == nil {
			continue
		}
		out = append(out, OHLCPoint{
			Date:  time.Unix(result.Timestamp[i], 0).UTC(),
			Close: *c,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}
