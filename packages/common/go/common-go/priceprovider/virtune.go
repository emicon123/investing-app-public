package priceprovider

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
	"strings"
	"sync"
	"time"
)

// This file implements the ADR-085 Virtune ETP coin-entitlement reconstruction for the
// portfolioB/portfolioC historia backfills (task 138) — the shared-Go port of
// apps/main/backend-go/internal/linearregression's HistoryService, cut down to what the
// backfill seam needs: daily CLOSES in PLN (the backfills price off closes alone, via the
// OHLCProvider interface in history.go), not full O/H/L/C bars. VirtuneClient is the one
// Virtune scrape of all three apps: main's HistoryService uses it too (task 178).
//
// Chain per day t, for exactly the four registry tickers below (ADR-085 §4):
//
//	closePLN(t) = binanceSpotClose(underlying, t) × ratio(t) × usdPlnClose(t)
//	ratio(t)    = ratioAnchor × exp(−netAnnualRate × (asOfDate − t).days / 365)
//
// The output is already PLN-denominated (the USD→PLN leg is inside the reconstruction),
// matching what Yahoo would have returned natively for these GPW-listed tickers. Callers
// must NOT apply any further FX conversion on top (ADR-085 §4 hard constraint) — the
// siblings' PLN branch satisfies this with zero backfill-math changes.
//
// Fail-soft at every step: any scrape/parse/network failure degrades to Yahoo's original
// result, never a new error — never worse than pre-reconstruction behavior.

// ── Static product registry ──────────────────────────────────────────────────

// VirtuneProduct is one entry of the static 4-product registry of physically-backed
// Virtune crypto ETPs (ADR-085 §3/§6). An explicit map, not a derived transform: a 5th
// ETP that doesn't fit an assumed pattern fails safe (unmapped tickers never trigger
// reconstruction). Values verified 2026-09-02 per ADR-085 §6 — re-verify against the
// live product pages if a reconstructed series ever looks visibly wrong.
type VirtuneProduct struct {
	YahooTicker        string
	VirtuneSlug        string
	BinanceSymbol      string
	CoinLabel          string
	FeeAnnual          float64
	StakingYieldAnnual float64
	GPWListingDate     time.Time
}

func virtuneUTCDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

var virtuneProducts = map[string]VirtuneProduct{
	"ETNVIRBTCP.WA": {
		YahooTicker: "ETNVIRBTCP.WA", VirtuneSlug: "bitcoin-prime",
		BinanceSymbol: "BTCUSDT", CoinLabel: "BTC",
		FeeAnnual: 0.0025, StakingYieldAnnual: 0,
		GPWListingDate: virtuneUTCDate(2026, time.February, 25),
	},
	"ETNVIRXRP.WA": {
		YahooTicker: "ETNVIRXRP.WA", VirtuneSlug: "xrp",
		BinanceSymbol: "XRPUSDT", CoinLabel: "XRP",
		FeeAnnual: 0.0149, StakingYieldAnnual: 0,
		GPWListingDate: virtuneUTCDate(2026, time.February, 25),
	},
	"ETNVIRETH.WA": {
		YahooTicker: "ETNVIRETH.WA", VirtuneSlug: "ethereum",
		BinanceSymbol: "ETHUSDT", CoinLabel: "ETH",
		FeeAnnual: 0.014, StakingYieldAnnual: 0.032,
		GPWListingDate: virtuneUTCDate(2026, time.February, 25),
	},
	"ETNVIRSOL.WA": {
		YahooTicker: "ETNVIRSOL.WA", VirtuneSlug: "solana",
		BinanceSymbol: "SOLUSDT", CoinLabel: "SOL",
		FeeAnnual: 0.0095, StakingYieldAnnual: 0.03,
		GPWListingDate: virtuneUTCDate(2026, time.February, 25),
	},
}

// NetAnnualRate is the entitlement-growth rate per ADR-085 §4: staking yield minus fee,
// additive (not compounded) — confirmed by Virtune's own worked example.
func (p VirtuneProduct) NetAnnualRate() float64 {
	return p.StakingYieldAnnual - p.FeeAnnual
}

// LookupVirtuneProduct returns the registry entry for a Yahoo ticker, if mapped.
func LookupVirtuneProduct(yahooTicker string) (VirtuneProduct, bool) {
	p, ok := virtuneProducts[yahooTicker]
	return p, ok
}

// ── Virtune ratio scrape client ──────────────────────────────────────────────

// VirtuneRatioSnapshot is a Virtune ETP's current coin-per-ETP entitlement ratio.
type VirtuneRatioSnapshot struct {
	CoinPerEtp float64
	AsOfDate   time.Time
}

// VirtuneClient scrapes Virtune ETP product pages for the current coin-per-ETP
// entitlement ratio (base host www.virtune.com explicitly, avoiding the bare domain's
// 308). 6-hour in-memory TTL per slug, stale fallback on failure. Never throws —
// every failure degrades to "no ratio", and the caller falls back to Yahoo.
type VirtuneClient struct {
	mu      sync.Mutex
	http    *http.Client
	baseURL string
	cache   map[string]virtuneCachedRatio
}

type virtuneCachedRatio struct {
	snapshot VirtuneRatioSnapshot
	fetched  time.Time
}

const virtuneRatioTTL = 6 * time.Hour

var (
	virtuneCoinPerETPLabel = regexp.MustCompile(`^[A-Z]+ per ETP$`)
	virtuneLastUpdated     = regexp.MustCompile(`Last updated\s*:\s*(\d{4}-\d{2}-\d{2})`)
	virtuneDTTag           = regexp.MustCompile(`(?s)<dt[^>]*>(.*?)</dt>`)
	virtuneDDValueText     = regexp.MustCompile(`(?s)<dd[^>]*class="[^"]*value-text[^"]*"[^>]*>(.*?)</dd>`)
	virtuneHTMLTag         = regexp.MustCompile(`<[^>]*>`)
)

// NewVirtuneClient constructs a client against the real Virtune site.
func NewVirtuneClient() *VirtuneClient {
	return &VirtuneClient{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://www.virtune.com",
		cache:   map[string]virtuneCachedRatio{},
	}
}

// FetchCurrentRatio returns the live ratio snapshot for a product slug, or false.
func (c *VirtuneClient) FetchCurrentRatio(ctx context.Context, slug, coinLabel string) (VirtuneRatioSnapshot, bool) {
	c.mu.Lock()
	cached, hasCached := c.cache[slug]
	c.mu.Unlock()
	if hasCached && time.Since(cached.fetched) < virtuneRatioTTL {
		return cached.snapshot, true
	}
	if snap, ok := c.fetchAndParse(ctx, slug, coinLabel); ok {
		c.mu.Lock()
		c.cache[slug] = virtuneCachedRatio{snapshot: snap, fetched: time.Now()}
		c.mu.Unlock()
		return snap, true
	}
	if hasCached {
		slog.Warn("priceprovider: virtune falling back to stale cached ratio", "slug", slug)
		return cached.snapshot, true
	}
	return VirtuneRatioSnapshot{}, false
}

func (c *VirtuneClient) fetchAndParse(ctx context.Context, slug, coinLabel string) (VirtuneRatioSnapshot, bool) {
	reqURL := c.baseURL + "/en/product/" + url.PathEscape(slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return VirtuneRatioSnapshot{}, false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := c.http.Do(req)
	if err != nil {
		slog.Warn("priceprovider: virtune fetch failed", "slug", slug, "error", err)
		return VirtuneRatioSnapshot{}, false
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return VirtuneRatioSnapshot{}, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return VirtuneRatioSnapshot{}, false
	}
	return parseVirtuneHTML(string(body), slug, coinLabel, virtuneWarsawToday())
}

// parseVirtuneHTML extracts the "<COIN> per ETP" ratio without a DOM library (stdlib
// only — no goquery dependency for one narrow scrape): find the <dt> whose stripped text
// matches ^[A-Z]+ per ETP$, then the next sibling dd.value-text as the ratio. The
// "Last updated: YYYY-MM-DD" line is extracted page-wide, falling back to today's date.
// Fail-soft throughout: ("", false) on any shape mismatch (ADR-085 §6).
func parseVirtuneHTML(html, slug, coinLabel string, today time.Time) (VirtuneRatioSnapshot, bool) {
	dtMatches := virtuneDTTag.FindAllStringSubmatchIndex(html, -1)
	for _, m := range dtMatches {
		label := strings.TrimSpace(virtuneHTMLTag.ReplaceAllString(html[m[2]:m[3]], ""))
		label = strings.ReplaceAll(label, "\u00a0", " ")
		if !virtuneCoinPerETPLabel.MatchString(label) {
			continue
		}
		rest := html[m[1]:]
		dd := virtuneDDValueText.FindStringSubmatch(rest)
		if dd == nil {
			slog.Warn("priceprovider: virtune no dd.value-text", "slug", slug)
			return VirtuneRatioSnapshot{}, false
		}
		if coinLabel != "" {
			labelCoin := strings.TrimSuffix(label, " per ETP")
			if !strings.EqualFold(labelCoin, coinLabel) {
				slog.Warn("priceprovider: virtune coin label mismatch", "slug", slug, "label", labelCoin)
			}
		}
		raw := strings.TrimSpace(virtuneHTMLTag.ReplaceAllString(dd[1], ""))
		raw = strings.ReplaceAll(raw, "\u00a0", "")
		raw = strings.ReplaceAll(raw, ",", "")
		ratio, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			slog.Warn("priceprovider: virtune unparseable ratio", "slug", slug)
			return VirtuneRatioSnapshot{}, false
		}
		asOf := today
		if lm := virtuneLastUpdated.FindStringSubmatch(html); lm != nil {
			if parsed, err := time.Parse("2006-01-02", lm[1]); err == nil {
				asOf = parsed.UTC()
			}
		}
		return VirtuneRatioSnapshot{CoinPerEtp: ratio, AsOfDate: asOf}, true
	}
	slog.Warn("priceprovider: virtune no '<COIN> per ETP' dt — layout changed?", "slug", slug)
	return VirtuneRatioSnapshot{}, false
}

// ── Binance klines client (closes-only) ──────────────────────────────────────

// BinanceKlinesClient fetches daily spot klines — the closes-only analogue of main's
// BinanceKlinesClient (one request, limit 1000 — covers ~3 years of daily bars).
// Failures are returned as errors so the caller can fail soft to Yahoo.
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

// FetchCloses fetches daily closes for [startMs, endMs] (epoch millis, inclusive) from
// Binance's public klines endpoint (no key required). Only the close leg is carried —
// the backfill prices off closes alone.
func (c *BinanceKlinesClient) FetchCloses(ctx context.Context, symbol string, startMs, endMs int64) ([]OHLCPoint, error) {
	reqURL := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=1d&startTime=%d&endTime=%d&limit=1000",
		c.baseURL, url.QueryEscape(symbol), startMs, endMs)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("virtune-klines: build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("virtune-klines: request failed for %s: %w", symbol, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("virtune-klines: returned status %d for %s", resp.StatusCode, symbol)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("virtune-klines: read response for %s: %w", symbol, err)
	}
	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("virtune-klines: decode response for %s: %w", symbol, err)
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
		close, err := strconv.ParseFloat(fmt.Sprint(row[4]), 64)
		if err != nil || math.IsNaN(close) || math.IsInf(close, 0) || close <= 0 {
			continue
		}
		out = append(out, OHLCPoint{
			Date:  time.UnixMilli(int64(openMs)).UTC().Truncate(24 * time.Hour),
			Close: close,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}

// ── Reconstructing decorator (the siblings' integration point) ───────────────

// ReconstructingOHLCProvider is an OHLCProvider decorator: on Yahoo's confirmed
// empty/thin failure signature for exactly the four Virtune ETP tickers, it
// reconstructs a daily PLN close series from the ETP's disclosed coin-entitlement
// ratio, the underlying crypto's Binance spot history, and Yahoo's own USDPLN=X FX
// history (ADR-085 §7). Every other ticker passes straight through untouched —
// errors included. Fail-soft at every step: any failure in the chain degrades to
// Yahoo's original result, never a new error.
type ReconstructingOHLCProvider struct {
	inner   OHLCProvider
	virtune *VirtuneClient
	klines  *BinanceKlinesClient
	now     func() time.Time // overridable clock (tests); defaults to time.Now
}

// ReconstructingOHLCProvider implements OHLCProvider — compile-time seam guarantee
// for the portfolioB/portfolioC backfill wiring.
var _ OHLCProvider = (*ReconstructingOHLCProvider)(nil)

// NewReconstructingOHLCProvider wraps inner (production: NewYahooOHLCProvider()) with
// default live sub-clients.
func NewReconstructingOHLCProvider(inner OHLCProvider) *ReconstructingOHLCProvider {
	return &ReconstructingOHLCProvider{
		inner:   inner,
		virtune: NewVirtuneClient(),
		klines:  NewBinanceKlinesClient(),
		now:     time.Now,
	}
}

// FetchOHLC implements OHLCProvider. The thin trigger is adapted to this seam's
// (result, error) shape — YahooOHLCProvider returns an error on empty, unlike main's
// fail-soft chart client: a registered ticker triggers reconstruction when the inner
// call errored OR returned ≤1 point over a window that could plausibly hold more than
// one daily bar (interval at this seam is always "1d").
func (p *ReconstructingOHLCProvider) FetchOHLC(ctx context.Context, ticker, interval string, period1, period2 int64) ([]OHLCPoint, error) {
	yahooPts, yahooErr := p.inner.FetchOHLC(ctx, ticker, interval, period1, period2)
	product, registered := LookupVirtuneProduct(ticker)
	if !registered {
		return yahooPts, yahooErr
	}
	thin := yahooErr != nil || (len(yahooPts) <= 1 && period2-period1 > 86_400)
	if !thin {
		return yahooPts, yahooErr
	}
	if points, ok := p.reconstruct(ctx, product); ok {
		return filterVirtuneRange(points, period1, period2), nil
	}
	return yahooPts, yahooErr
}

// reconstruct attempts the full ADR-085 §4 chain, returning false at the first point
// of failure so the caller falls back to Yahoo's original result.
func (p *ReconstructingOHLCProvider) reconstruct(ctx context.Context, product VirtuneProduct) ([]OHLCPoint, bool) {
	ratio, ok := p.virtune.FetchCurrentRatio(ctx, product.VirtuneSlug, product.CoinLabel)
	if !ok {
		slog.Warn("priceprovider: virtune ratio unavailable — falling back to Yahoo", "ticker", product.YahooTicker)
		return nil, false
	}
	from, to := product.GPWListingDate, virtuneWarsawTodayAt(p.now())
	p1 := from.Unix()
	p2 := to.Add(23*time.Hour + 59*time.Minute + 59*time.Second).Unix()
	spot, err := p.klines.FetchCloses(ctx, product.BinanceSymbol, p1*1000, p2*1000)
	if err != nil || len(spot) == 0 {
		slog.Warn("priceprovider: binance spot history unavailable — falling back to Yahoo", "ticker", product.YahooTicker)
		return nil, false
	}
	fx, err := p.inner.FetchOHLC(ctx, "USDPLN=X", "1d", p1, p2)
	if err != nil || len(fx) == 0 {
		slog.Warn("priceprovider: USDPLN=X FX history unavailable — falling back to Yahoo", "ticker", product.YahooTicker)
		return nil, false
	}
	daily := reconstructVirtuneCloses(product, ratio, spot, fx, from, to)
	if len(daily) == 0 {
		return nil, false
	}
	return daily, true
}

// reconstructVirtuneCloses ports main's reconstructDaily, closes-only:
// ratio(t) = anchor·exp(-netAnnualRate·daysBack/365),
// closePLN(t) = binanceClose(t)·ratio(t)·fxClose(t).
// FX joins by floor-or-earlier lookup (NearestClose): a Saturday Binance bar joins
// Friday's FX close, and a holiday uses the prior session — never a future leak. Bars
// with no usable FX (or non-positive FX) are skipped rather than guessed.
func reconstructVirtuneCloses(product VirtuneProduct, ratio VirtuneRatioSnapshot, spot, fx []OHLCPoint, from, to time.Time) []OHLCPoint {
	netRate := product.NetAnnualRate()
	daily := make([]OHLCPoint, 0, len(spot))
	for _, sp := range spot {
		t := sp.Date
		if t.Before(from) || t.After(to) {
			continue
		}
		fxClose, ok := NearestClose(fx, t)
		if !ok || fxClose <= 0 || math.IsNaN(fxClose) || math.IsInf(fxClose, 0) {
			continue
		}
		daysBack := t.Sub(ratio.AsOfDate).Hours() / -24 // (asOfDate - t).days, may be negative
		ratioAtT := ratio.CoinPerEtp * math.Exp(-netRate*daysBack/365.0)
		daily = append(daily, OHLCPoint{
			Date:  t,
			Close: sp.Close * ratioAtT * fxClose,
		})
	}
	sort.Slice(daily, func(i, j int) bool { return daily[i].Date.Before(daily[j].Date) })
	return daily
}

func filterVirtuneRange(points []OHLCPoint, p1, p2 int64) []OHLCPoint {
	from := time.Unix(p1, 0).UTC().Truncate(24 * time.Hour)
	to := time.Unix(p2, 0).UTC().Truncate(24 * time.Hour)
	out := make([]OHLCPoint, 0, len(points))
	for _, p := range points {
		if p.Date.Before(from) || p.Date.After(to) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// virtuneWarsawToday is the calendar date in Europe/Warsaw for now, regardless of the
// process/container timezone.
func virtuneWarsawToday() time.Time {
	return virtuneWarsawTodayAt(time.Now())
}

// virtuneWarsawTodayAt is virtuneWarsawToday for an explicit instant (tests).
func virtuneWarsawTodayAt(now time.Time) time.Time {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return now.UTC().Truncate(24 * time.Hour)
	}
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
