package priceprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// YahooProvider fetches current prices from Yahoo Finance's unofficial HTTP endpoints (ported from
// the Java YahooFinanceClient, ADR-094; there is no official Go client for Yahoo Finance). Primary
// path is the authenticated v7 quote endpoint (cookie + crumb session, market-state-aware price
// selection); it falls back to the public v8 chart endpoint when the quote endpoint or its session
// is unavailable. Every failure is returned as an error (never a panic) so a caller can fail soft —
// see the CachingDecorator this is meant to be wrapped in for the "never crash on a stale price"
// behavior callers rely on.
type YahooProvider struct {
	chartClient *http.Client // v8 chart API on query2.finance.yahoo.com — no auth needed
	authClient  *http.Client // v7 quote + cookie/crumb flow on query1.finance.yahoo.com

	// Base URLs — overridable (unexported) so unit tests can point this provider at an
	// httptest.Server instead of the real Yahoo Finance hosts; production code always gets the
	// defaults set by NewYahooProvider.
	chartBaseURL  string
	quoteHost     string
	cookieSeedURL string

	mu           sync.Mutex
	cookieHeader string
	crumb        string
}

const (
	yahooChartBaseURL  = "https://query2.finance.yahoo.com"
	yahooQuoteHost     = "https://query1.finance.yahoo.com"
	yahooCookieSeedURL = "https://fc.yahoo.com/"
	yahooBrowserUA     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

// NewYahooProvider constructs a YahooProvider with a 10s overall http.Client timeout (the Java
// client's 5s connect + 10s read budget, collapsed — Go's http.Client.Timeout covers the whole
// round trip, dial included).
func NewYahooProvider() *YahooProvider {
	return &YahooProvider{
		chartClient:   &http.Client{Timeout: 10 * time.Second},
		authClient:    &http.Client{Timeout: 10 * time.Second},
		chartBaseURL:  yahooChartBaseURL,
		quoteHost:     yahooQuoteHost,
		cookieSeedURL: yahooCookieSeedURL,
	}
}

// Name identifies this provider in logs — "YAHOO", matching YahooPriceProvider.name().
func (*YahooProvider) Name() string { return "YAHOO" }

// GetPrice fetches ticker's current price: authenticated v7 quote first, then the public v8
// chart API as a fallback — the same two-tier strategy as YahooFinanceClient.fetchPrice.
func (p *YahooProvider) GetPrice(ctx context.Context, ticker string) (decimal.Decimal, error) {
	if price, err := p.fetchViaQuote(ctx, ticker); err == nil {
		return price, nil
	}
	return p.fetchViaChart(ctx, ticker)
}

// ── Primary path: authenticated v7 quote with marketState-aware selection ──────────────

type yahooQuoteResponse struct {
	QuoteResponse struct {
		Result []yahooQuoteItem `json:"result"`
	} `json:"quoteResponse"`
}

type yahooQuoteItem struct {
	MarketState        string   `json:"marketState"`
	RegularMarketPrice *float64 `json:"regularMarketPrice"`
	PreMarketPrice     *float64 `json:"preMarketPrice"`
	PostMarketPrice    *float64 `json:"postMarketPrice"`
}

func (p *YahooProvider) fetchViaQuote(ctx context.Context, ticker string) (decimal.Decimal, error) {
	if err := p.ensureSession(ctx); err != nil {
		return decimal.Zero, err
	}

	item, err := p.requestQuote(ctx, ticker)
	if err != nil {
		// Crumb/cookie may have expired — refresh once and retry.
		if refreshErr := p.refreshSession(ctx); refreshErr != nil {
			return decimal.Zero, err
		}
		item, err = p.requestQuote(ctx, ticker)
	}
	if err != nil {
		return decimal.Zero, err
	}

	return validPrice(selectByMarketState(item), ticker)
}

func (p *YahooProvider) requestQuote(ctx context.Context, ticker string) (*yahooQuoteItem, error) {
	p.mu.Lock()
	cookie, crumb := p.cookieHeader, p.crumb
	p.mu.Unlock()

	reqURL := p.quoteHost + "/v7/finance/quote?symbols=" + url.QueryEscape(ticker) +
		"&fields=marketState,regularMarketPrice,preMarketPrice,postMarketPrice" +
		"&crumb=" + url.QueryEscape(crumb)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("yahoo: build quote request: %w", err)
	}
	req.Header.Set("User-Agent", yahooBrowserUA)
	req.Header.Set("Cookie", cookie)

	resp, err := p.authClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yahoo: v7 quote request failed for %s: %w", ticker, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("yahoo: v7 quote returned status %d for %s", resp.StatusCode, ticker)
	}

	var body yahooQuoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("yahoo: decode v7 quote response for %s: %w", ticker, err)
	}
	if len(body.QuoteResponse.Result) == 0 {
		return nil, fmt.Errorf("yahoo: v7 quote returned no result for %s", ticker)
	}
	return &body.QuoteResponse.Result[0], nil
}

// selectByMarketState picks the price field that matches the current trading session. During the
// pure overnight gap (PREPRE) Yahoo's website shows a 24h "overnightMarketPrice" no JSON endpoint
// returns, so it deliberately falls back to the last post-market close.
func selectByMarketState(item *yahooQuoteItem) *float64 {
	switch item.MarketState {
	case "PRE":
		return firstNonNilFinite(item.PreMarketPrice, item.RegularMarketPrice)
	case "POST", "POSTPOST", "PREPRE":
		return firstNonNilFinite(item.PostMarketPrice, item.RegularMarketPrice)
	default: // REGULAR, CLOSED, or unknown
		return item.RegularMarketPrice
	}
}

func firstNonNilFinite(primary, fallback *float64) *float64 {
	if primary != nil && !math.IsNaN(*primary) && !math.IsInf(*primary, 0) {
		return primary
	}
	return fallback
}

// ── Yahoo session (cookie + crumb) management ───────────────────────────────────────────

func (p *YahooProvider) ensureSession(ctx context.Context) error {
	p.mu.Lock()
	haveSession := p.cookieHeader != "" && p.crumb != ""
	p.mu.Unlock()
	if haveSession {
		return nil
	}
	return p.refreshSession(ctx)
}

func (p *YahooProvider) refreshSession(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. Seed cookies. fc.yahoo.com returns 404 but still sets the A1/A3 cookies.
	seedReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cookieSeedURL, nil)
	if err != nil {
		return fmt.Errorf("yahoo: build cookie-seed request: %w", err)
	}
	seedReq.Header.Set("User-Agent", yahooBrowserUA)
	seedResp, err := p.authClient.Do(seedReq)
	if err != nil {
		return fmt.Errorf("yahoo: cookie-seed request failed: %w", err)
	}
	cookies := extractCookies(seedResp.Header.Values("Set-Cookie"))
	_, _ = io.Copy(io.Discard, seedResp.Body)
	_ = seedResp.Body.Close() // read-side close; error not actionable
	if cookies == "" {
		return fmt.Errorf("yahoo: could not obtain session cookies")
	}

	// 2. Fetch a crumb tied to those cookies.
	crumbReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.quoteHost+"/v1/test/getcrumb", nil)
	if err != nil {
		return fmt.Errorf("yahoo: build crumb request: %w", err)
	}
	crumbReq.Header.Set("User-Agent", yahooBrowserUA)
	crumbReq.Header.Set("Cookie", cookies)
	crumbResp, err := p.authClient.Do(crumbReq)
	if err != nil {
		return fmt.Errorf("yahoo: crumb request failed: %w", err)
	}
	defer func() { _ = crumbResp.Body.Close() }() // read-side close; error not actionable
	if crumbResp.StatusCode < 200 || crumbResp.StatusCode >= 300 {
		return fmt.Errorf("yahoo: crumb request returned status %d", crumbResp.StatusCode)
	}
	crumbBytes, err := io.ReadAll(crumbResp.Body)
	if err != nil {
		return fmt.Errorf("yahoo: read crumb response: %w", err)
	}
	crumb := strings.TrimSpace(string(crumbBytes))
	// A valid crumb is a short token; an error response is JSON ("{...}").
	if crumb == "" || strings.Contains(crumb, "{") {
		return fmt.Errorf("yahoo: could not obtain crumb")
	}

	p.cookieHeader = cookies
	p.crumb = crumb
	return nil
}

func extractCookies(setCookieHeaders []string) string {
	var pairs []string
	for _, header := range setCookieHeaders {
		pair := header
		if before, _, ok := strings.Cut(header, ";"); ok {
			pair = before
		}
		pair = strings.TrimSpace(pair)
		if pair != "" {
			pairs = append(pairs, pair)
		}
	}
	return strings.Join(pairs, "; ")
}

// ── Fallback path: public v8 chart API (no auth) ────────────────────────────────────────

type yahooChartResponse struct {
	Chart struct {
		Result []yahooChartResult `json:"result"`
	} `json:"chart"`
}

type yahooChartResult struct {
	Meta       yahooChartMeta `json:"meta"`
	Timestamp  []int64        `json:"timestamp"`
	Indicators struct {
		Quote []yahooChartQuoteData `json:"quote"`
	} `json:"indicators"`
}

type yahooChartMeta struct {
	RegularMarketPrice   *float64             `json:"regularMarketPrice"`
	PreMarketPrice       *float64             `json:"preMarketPrice"`
	PostMarketPrice      *float64             `json:"postMarketPrice"`
	HasPrePostMarketData *bool                `json:"hasPrePostMarketData"`
	CurrentTradingPeriod *yahooTradingPeriods `json:"currentTradingPeriod"`
}

type yahooTradingPeriods struct {
	Pre     *yahooTradingPeriod `json:"pre"`
	Regular *yahooTradingPeriod `json:"regular"`
	Post    *yahooTradingPeriod `json:"post"`
}

type yahooTradingPeriod struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type yahooChartQuoteData struct {
	Close []*float64 `json:"close"`
}

// fetchViaChart mirrors YahooFinanceClient.fetchPriceViaChart: during pre/post-market hours
// returns the latest extended-hours candle price; during regular hours or when extended-hours
// data is unavailable, returns regularMarketPrice.
func (p *YahooProvider) fetchViaChart(ctx context.Context, ticker string) (decimal.Decimal, error) {
	reqURL := p.chartBaseURL + "/v8/finance/chart/" + url.PathEscape(ticker) +
		"?interval=1m&range=1d&prePost=true&includePrePost=true"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("yahoo: build chart request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := p.chartClient.Do(req)
	if err != nil {
		return decimal.Zero, fmt.Errorf("yahoo: v8 chart request failed for %s: %w", ticker, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decimal.Zero, fmt.Errorf("yahoo: v8 chart returned status %d for %s", resp.StatusCode, ticker)
	}

	var body yahooChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return decimal.Zero, fmt.Errorf("yahoo: decode v8 chart response for %s: %w", ticker, err)
	}
	if len(body.Chart.Result) == 0 {
		return decimal.Zero, fmt.Errorf("yahoo: v8 chart returned empty result for %s", ticker)
	}

	result := body.Chart.Result[0]
	meta := result.Meta
	if meta.RegularMarketPrice == nil {
		return decimal.Zero, fmt.Errorf("yahoo: v8 chart missing meta/price for %s", ticker)
	}

	// Assets without extended-hours data (European stocks, crypto, forex) -> regular price.
	if meta.HasPrePostMarketData == nil || !*meta.HasPrePostMarketData || meta.CurrentTradingPeriod == nil {
		return validPrice(meta.RegularMarketPrice, ticker)
	}

	now := time.Now().Unix()
	tp := meta.CurrentTradingPeriod

	if tp.Regular != nil && now >= tp.Regular.Start && now <= tp.Regular.End {
		return validPrice(meta.RegularMarketPrice, ticker)
	}

	// Pre-market: prefer meta.preMarketPrice, then fall back to a candle scan.
	if tp.Pre != nil && tp.Regular != nil && now >= tp.Pre.Start && now < tp.Regular.Start {
		if price := firstNonNilFinite(meta.PreMarketPrice, nil); price != nil {
			return decimal.NewFromFloat(*price), nil
		}
		if candle := lastCloseInWindow(result, tp.Pre.Start, tp.Regular.Start); candle != nil {
			return decimal.NewFromFloat(*candle), nil
		}
	}

	// Post-market: prefer meta.postMarketPrice, then fall back to a candle scan.
	if tp.Post != nil && tp.Regular != nil && now > tp.Regular.End && now <= tp.Post.End {
		if price := firstNonNilFinite(meta.PostMarketPrice, nil); price != nil {
			return decimal.NewFromFloat(*price), nil
		}
		if candle := lastCloseInWindow(result, tp.Regular.End, tp.Post.End); candle != nil {
			return decimal.NewFromFloat(*candle), nil
		}
	}

	return validPrice(meta.RegularMarketPrice, ticker)
}

func lastCloseInWindow(result yahooChartResult, windowStart, windowEnd int64) *float64 {
	if len(result.Indicators.Quote) == 0 {
		return nil
	}
	closes := result.Indicators.Quote[0].Close
	var last *float64
	n := min(len(closes), len(result.Timestamp))
	for i := range n {
		c := closes[i]
		if c == nil || math.IsNaN(*c) || math.IsInf(*c, 0) {
			continue
		}
		t := result.Timestamp[i]
		if t >= windowStart && t < windowEnd {
			last = c
		}
	}
	return last
}

func validPrice(price *float64, ticker string) (decimal.Decimal, error) {
	if price == nil || math.IsNaN(*price) || math.IsInf(*price, 0) {
		return decimal.Zero, fmt.Errorf("yahoo: no valid price for %s", ticker)
	}
	return decimal.NewFromFloat(*price), nil
}
