package linearregression

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// ── Yahoo v8 chart client ──────────────────────────────────────────────────────

// YahooChartClient fetches OHLC history from Yahoo Finance's v8 chart API (priceprovider covers
// spot prices only, so this OHLC client lives here; ported from the Java YahooFinanceClient,
// ADR-094). Every failure returns an empty slice, never an error — fail-soft.
type YahooChartClient struct {
	http      *http.Client
	chartBase string
	userAgent string
}

// NewYahooChartClient constructs a client against the real Yahoo hosts with a 10s overall timeout
// (Go's http.Client.Timeout covers the whole round trip, dial included).
func NewYahooChartClient() *YahooChartClient {
	return &YahooChartClient{
		http:      &http.Client{Timeout: 10 * time.Second},
		chartBase: "https://query2.finance.yahoo.com",
		userAgent: "Mozilla/5.0",
	}
}

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open  []*float64 `json:"open"`
					Close []*float64 `json:"close"`
					High  []*float64 `json:"high"`
					Low   []*float64 `json:"low"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
	} `json:"chart"`
}

// FetchOHLCByRange fetches OHLC candles for an explicit [period1, period2] epoch-second
// window — never the range=max shorthand (see Service.fetchWindow's doc comment for why).
func (c *YahooChartClient) FetchOHLCByRange(ctx context.Context, yahooTicker, interval string, period1, period2 int64) []OHLCPoint {
	reqURL := fmt.Sprintf("%s/v8/finance/chart/%s?interval=%s&period1=%d&period2=%d",
		c.chartBase, url.PathEscape(yahooTicker), url.QueryEscape(interval), period1, period2)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		slog.Warn("linearregression: yahoo OHLC fetch failed", "ticker", yahooTicker, "error", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("linearregression: yahoo OHLC non-2xx", "ticker", yahooTicker, "status", resp.StatusCode)
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil
	}
	var parsed yahooChartResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	return parseYahooOHLC(parsed)
}

// parseYahooOHLC mirrors YahooFinanceClient.parseOHLC: skip candles with a missing/NaN/
// infinite close; open/high/low fall back to close. Dates are UTC calendar dates.
func parseYahooOHLC(resp yahooChartResponse) []OHLCPoint {
	if len(resp.Chart.Result) == 0 {
		return nil
	}
	res := resp.Chart.Result[0]
	if len(res.Indicators.Quote) == 0 {
		return nil
	}
	q := res.Indicators.Quote[0]
	var out []OHLCPoint
	for i, ts := range res.Timestamp {
		close := safeDouble(q.Close, i, math.NaN())
		if math.IsNaN(close) || math.IsInf(close, 0) {
			continue
		}
		date := time.Unix(ts, 0).UTC().Truncate(24 * time.Hour)
		out = append(out, OHLCPoint{
			Date:  date,
			Open:  safeDouble(q.Open, i, close),
			Close: close,
			High:  safeDouble(q.High, i, close),
			Low:   safeDouble(q.Low, i, close),
		})
	}
	return out
}

func safeDouble(list []*float64, i int, fallback float64) float64 {
	if list == nil || i >= len(list) || list[i] == nil {
		return fallback
	}
	v := *list[i]
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	return v
}

// ── Binance klines client ─────────────────────────────────────────────────────

// BinanceKlinesClient fetches daily spot klines — the direct port of
// BinanceClient.fetchHistoricalKlines (one request, limit 1000 — covers ~3 years of
// daily bars, ported as-is). Fail-soft: any failure yields an empty slice.
type BinanceKlinesClient struct {
	http    *http.Client
	baseURL string
}

// NewBinanceKlinesClient constructs a client against the real Binance spot API.
func NewBinanceKlinesClient() *BinanceKlinesClient {
	return &BinanceKlinesClient{
		http:    &http.Client{Timeout: 10 * time.Second},
		baseURL: "https://api.binance.com",
	}
}

// FetchHistoricalKlines fetches klines for [startMs, endMs] (epoch millis, inclusive).
func (c *BinanceKlinesClient) FetchHistoricalKlines(ctx context.Context, symbol, interval string, startMs, endMs int64) []OHLCPoint {
	reqURL := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&startTime=%d&endTime=%d&limit=1000",
		c.baseURL, url.QueryEscape(symbol), url.QueryEscape(interval), startMs, endMs)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil
	}
	resp, err := c.http.Do(req)
	if err != nil {
		slog.Warn("linearregression: binance klines fetch failed", "symbol", symbol, "error", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil
	}
	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	var out []OHLCPoint
	for _, row := range raw {
		if len(row) < 5 {
			continue
		}
		openMs, ok := row[0].(float64)
		if !ok {
			continue
		}
		nums := make([]float64, 4)
		valid := true
		for j := 1; j <= 4; j++ {
			v, err := strconv.ParseFloat(fmt.Sprint(row[j]), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				valid = false
				break
			}
			nums[j-1] = v
		}
		if !valid {
			continue
		}
		out = append(out, OHLCPoint{
			Date: time.UnixMilli(int64(openMs)).UTC().Truncate(24 * time.Hour),
			Open: nums[0], High: nums[1], Low: nums[2], Close: nums[3],
		})
	}
	return out
}

// ── History facade (HistoricalPriceService port) ──────────────────────────────

// HistoryService is the facade in front of the Yahoo chart client for historical OHLC
// series — the direct port of HistoricalPriceService (ADR-085): on Yahoo's confirmed
// empty/thin failure signature for exactly the four Virtune ETP tickers, reconstructs a
// daily PLN series from the ETP's disclosed coin-entitlement ratio, the underlying
// crypto's Binance spot history, and Yahoo's own USDPLN=X FX history. Every other
// ticker passes straight through tagged YAHOO. Fail-soft at every step: any failure in
// the chain degrades to Yahoo's original (possibly still-thin) result, never an error.
type HistoryService struct {
	yahoo   *YahooChartClient
	virtune RatioSource
	binance *BinanceKlinesClient
	now     func() time.Time // overridable clock (tests); defaults to time.Now
}

// RatioSource is the one call HistoryService makes on the Virtune ETP scrape: the
// current coin-per-ETP ratio for a product slug, or false. Production passes the shared
// *priceprovider.VirtuneClient (task 178: main's own copy of that client is deleted);
// tests pass a stub, since the scrape itself is tested in priceprovider.
type RatioSource interface {
	FetchCurrentRatio(ctx context.Context, slug, coinLabel string) (RatioSnapshot, bool)
}

// NewHistoryService wires the three HTTP clients (explicit constructor wiring,
// ADR-094 §2 — no DI framework).
func NewHistoryService(yahoo *YahooChartClient, virtune RatioSource, binance *BinanceKlinesClient) *HistoryService {
	return &HistoryService{yahoo: yahoo, virtune: virtune, binance: binance, now: time.Now}
}

// intervalPattern mirrors HistoricalPriceService.INTERVAL_PATTERN: optional leading
// count + d/wk/mo.
var intervalPattern = regexp.MustCompile(`^(\d+)?(d|wk|mo)$`)

// FetchOHLCByRange fetches OHLC history for an explicit epoch-second window —
// identical signature shape to YahooFinanceClient.fetchOHLCByRange.
func (s *HistoryService) FetchOHLCByRange(ctx context.Context, yahooTicker, interval string, period1, period2 int64) []PricedOHLCPoint {
	yahoo := s.yahoo.FetchOHLCByRange(ctx, yahooTicker, interval, period1, period2)
	if !isThin(yahoo, period2-period1 > intervalSeconds(interval)) {
		return tagYahoo(yahoo)
	}
	if points, ok := s.reconstruct(ctx, yahooTicker, interval); ok {
		return filterRange(points, period1, period2)
	}
	return tagYahoo(yahoo)
}

func isThin(yahoo []OHLCPoint, windowCouldHoldMoreThanOneBar bool) bool {
	if len(yahoo) == 0 {
		return true
	}
	return len(yahoo) <= 1 && windowCouldHoldMoreThanOneBar
}

func intervalSeconds(interval string) int64 {
	m := intervalPattern.FindStringSubmatch(interval)
	if m == nil {
		return 86_400 // unrecognized shape — assume a daily-sized bar, the narrowest case
	}
	count := int64(1)
	if m[1] != "" {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			count = n
		}
	}
	switch m[2] {
	case "d":
		return count * 86_400
	case "wk":
		return count * 604_800
	case "mo":
		return count * 2_592_000 // nominal 30-day month — plausibility check only
	}
	return 86_400
}

// reconstruct attempts the full ADR-085 §4 chain, returning false at the first point of
// failure so the caller falls back to Yahoo's original result — never panics.
func (s *HistoryService) reconstruct(ctx context.Context, yahooTicker, interval string) ([]PricedOHLCPoint, bool) {
	product, ok := LookupProduct(yahooTicker)
	if !ok {
		return nil, false
	}
	ratio, ok := s.virtune.FetchCurrentRatio(ctx, product.VirtuneSlug, product.CoinLabel)
	if !ok {
		slog.Warn("linearregression: virtune ratio unavailable — falling back to Yahoo", "ticker", yahooTicker)
		return nil, false
	}
	from, to := product.GPWListingDate, WarsawTodayAt(s.now())
	p1 := from.Unix()
	p2 := to.Add(23*time.Hour + 59*time.Minute + 59*time.Second).Unix()
	spot := s.binance.FetchHistoricalKlines(ctx, product.BinanceSymbol, "1d", p1*1000, p2*1000)
	if len(spot) == 0 {
		slog.Warn("linearregression: binance spot history unavailable — falling back to Yahoo", "ticker", yahooTicker)
		return nil, false
	}
	fx := s.yahoo.FetchOHLCByRange(ctx, "USDPLN=X", "1d", p1, p2)
	if len(fx) == 0 {
		slog.Warn("linearregression: USDPLN=X FX history unavailable — falling back to Yahoo", "ticker", yahooTicker)
		return nil, false
	}
	daily := reconstructDaily(product, ratio, spot, fx, from, to)
	if len(daily) == 0 {
		return nil, false
	}
	return resample(daily, interval), true
}

// reconstructDaily ports HistoricalPriceService.reconstructDaily 1:1:
// ratio(t) = anchor·exp(-netAnnualRate·daysBack/365),
// OHLC_PLN(t) = OHLC_USDT(t)·ratio(t)·usdPlnClose(t), per O/H/L/C field independently.
func reconstructDaily(product VirtuneProduct, ratio RatioSnapshot, spot, fx []OHLCPoint, from, to time.Time) []PricedOHLCPoint {
	netRate := product.NetAnnualRate()
	daily := make([]PricedOHLCPoint, 0, len(spot))
	for _, sp := range spot {
		t := sp.Date
		if t.Before(from) || t.After(to) {
			continue
		}
		fxPoint, ok := nearestOHLCByDate(fx, t)
		if !ok || fxPoint.Close <= 0 {
			continue // no usable FX near this date — skip the bar rather than guess
		}
		daysBack := t.Sub(ratio.AsOfDate).Hours() / -24 // (asOfDate - t).days, may be negative
		ratioAtT := ratio.CoinPerEtp * math.Exp(-netRate*daysBack/365.0)
		daily = append(daily, PricedOHLCPoint{
			Date:   t,
			Open:   sp.Open * ratioAtT * fxPoint.Close,
			Close:  sp.Close * ratioAtT * fxPoint.Close,
			High:   sp.High * ratioAtT * fxPoint.Close,
			Low:    sp.Low * ratioAtT * fxPoint.Close,
			Source: product.ConfidenceTier,
		})
	}
	sort.Slice(daily, func(i, j int) bool { return daily[i].Date.Before(daily[j].Date) })
	return daily
}

// nearestOHLCByDate is the nearest-calendar-date lookup, ties broken toward the earlier
// date — same convention as TradeChartService.nearestByDate (relies on ascending order:
// only a strictly smaller distance replaces the best).
func nearestOHLCByDate(series []OHLCPoint, target time.Time) (OHLCPoint, bool) {
	var best OHLCPoint
	bestDiff := int64(math.MaxInt64)
	found := false
	for _, p := range series {
		diff := absDays(p.Date, target)
		if diff < bestDiff {
			best, bestDiff, found = p, diff, true
		}
	}
	return best, found
}

// NearestPricedByDate is the PricedOHLCPoint variant, exported for the trade-chart
// package's FX markers (same tie-break rule).
func NearestPricedByDate(series []PricedOHLCPoint, target time.Time) (PricedOHLCPoint, bool) {
	var best PricedOHLCPoint
	bestDiff := int64(math.MaxInt64)
	found := false
	for _, p := range series {
		diff := absDays(p.Date, target)
		if diff < bestDiff {
			best, bestDiff, found = p, diff, true
		}
	}
	return best, found
}

func absDays(a, b time.Time) int64 {
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return int64(d.Hours() / 24)
}

// resample ports HistoricalPriceService.resample: daily passes through; weekly/monthly
// keep the last bar per ISO week / calendar month (input must be ascending).
func resample(daily []PricedOHLCPoint, interval string) []PricedOHLCPoint {
	m := intervalPattern.FindStringSubmatch(interval)
	unit := ""
	if m != nil {
		unit = m[2]
	}
	if unit == "" || unit == "d" {
		return daily
	}
	lastByBucket := map[string]PricedOHLCPoint{}
	order := []string{}
	for _, p := range daily {
		var bucket string
		if unit == "mo" {
			bucket = p.Date.Format("2006-01")
		} else {
			y, w := p.Date.ISOWeek()
			bucket = fmt.Sprintf("%d-W%d", y, w)
		}
		if _, seen := lastByBucket[bucket]; !seen {
			order = append(order, bucket)
		}
		lastByBucket[bucket] = p // ascending -> last write per bucket = latest date in it
	}
	out := make([]PricedOHLCPoint, 0, len(order))
	for _, b := range order {
		out = append(out, lastByBucket[b])
	}
	return out
}

func tagYahoo(points []OHLCPoint) []PricedOHLCPoint {
	out := make([]PricedOHLCPoint, 0, len(points))
	for _, p := range points {
		out = append(out, PricedOHLCPoint{
			Date: p.Date, Open: p.Open, Close: p.Close, High: p.High, Low: p.Low,
			Source: PriceSourceYahoo,
		})
	}
	return out
}

func filterRange(points []PricedOHLCPoint, p1, p2 int64) []PricedOHLCPoint {
	from := time.Unix(p1, 0).UTC().Truncate(24 * time.Hour)
	to := time.Unix(p2, 0).UTC().Truncate(24 * time.Hour)
	out := make([]PricedOHLCPoint, 0, len(points))
	for _, p := range points {
		if p.Date.Before(from) || p.Date.After(to) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// WarsawTodayAt is the Go equivalent of WarsawTime.today() for an explicit instant: the
// calendar date in Europe/Warsaw, regardless of the process/container timezone.
func WarsawTodayAt(now time.Time) time.Time {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return now.UTC().Truncate(24 * time.Hour)
	}
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
