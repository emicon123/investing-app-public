package marketdatacore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/priceprovider"
)

// yahooFXTickers mirrors FxRateCacheService.YAHOO_FX_TICKERS — the four PLN pairs both apps
// track via Yahoo Finance's "=X" forex-pair notation. PLN itself is never fetched (always 1.0).
var yahooFXTickers = map[string]string{
	"EUR": "EURPLN=X",
	"USD": "USDPLN=X",
	"CHF": "CHFPLN=X",
	"GBP": "GBPPLN=X",
}

// fxTTL is the FX-cache staleness bound (60 minutes) — the only place the price/FX cache actually
// gates on staleness (the Java PriceCacheService's price-ttl-minutes setting was read from config
// but never consulted; that latent gap was ported as-is, not fixed).
const fxTTL = 60 * time.Minute

const binanceExchange = "BINANCE"

// Service is the market-data service layer shared by portfolioB and portfolioC: handler -> Service ->
// Store. It holds two interface fields for the one thing that genuinely varies at the
// PriceProvider level — which provider a ticker is routed through — composed rather than
// inherited (doc 13 §5: no AbstractPriceService base class, one concrete struct, shared by both
// apps since neither has a per-app routing difference).
type Service struct {
	store Store

	// yahoo serves both stock/ETF prices and every FX rate — always the same
	// CachingDecorator-wrapped instance passed in by each app's main.go composition root.
	yahoo priceprovider.PriceProvider
	// binanceWithFallback serves BINANCE-exchange positions: Binance first, Yahoo on failure —
	// mirrors both apps' PriceCacheService.fetchPriceRouted's BINANCE branch.
	binanceWithFallback priceprovider.PriceProvider
}

// NewService wires the store adapter + the two provider values each app's main.go composition
// root already constructed (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(store Store, yahoo, binanceWithFallback priceprovider.PriceProvider) *Service {
	return &Service{store: store, yahoo: yahoo, binanceWithFallback: binanceWithFallback}
}

// GetAllPriceEntries returns every cached price. No external fetch — the direct port of
// PriceCacheService.getAllPriceEntries.
func (s *Service) GetAllPriceEntries(ctx context.Context) ([]PriceEntry, error) {
	entries, err := s.store.ListPrices(ctx)
	if err != nil {
		return nil, fmt.Errorf("marketdatacore: list price cache: %w", err)
	}
	return entries, nil
}

// GetAllFxRateEntries returns every cached FX rate, with a synthetic PLN=1.0 entry always first
// — the direct port of FxRateCacheService.getAllFxRateEntries. No external fetch.
func (s *Service) GetAllFxRateEntries(ctx context.Context) ([]FxRateEntry, error) {
	entries := []FxRateEntry{
		{FromCurrency: "PLN", RateToPln: decimal.NewFromInt(1), Source: "INTERNAL", FetchedAt: time.Now().UTC()},
	}
	rows, err := s.store.ListFxRates(ctx)
	if err != nil {
		return nil, fmt.Errorf("marketdatacore: list fx rate cache: %w", err)
	}
	entries = append(entries, rows...)
	return entries, nil
}

// fetchedPrice bundles one successful fetch's result, mirroring PriceCacheService's private
// FetchedPrice record.
type fetchedPrice struct {
	priceNative decimal.Decimal
	currency    string
	pricePln    decimal.Decimal
}

// ForceRefreshAllPrices fetches a fresh price for every ticker known to `positions` (read-only —
// doc 13 §6.3) in parallel, at most refreshConcurrency at a time (F4.1), then persists every
// successful fetch ("one failure never blocks another ticker"). A ticker not yet started when ctx
// is cancelled is reported as that ticker's failure. One deliberate divergence from the Java
// original: Java wrapped every persist in a single DB transaction (a write failure there would roll
// back the whole batch); this implementation persists each ticker independently, so a single
// ticker's write failure is reported as that ticker's own failure rather than possibly aborting an
// otherwise-successful batch — a strictly safer behavior for a read-mostly, low-risk domain, not a
// functional regression the frontend could observe.
func (s *Service) ForceRefreshAllPrices(ctx context.Context) (RefreshResult, error) {
	tickers, err := s.store.ListTrackedTickers(ctx)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("marketdatacore: list tracked tickers: %w", err)
	}
	if len(tickers) == 0 {
		return RefreshResult{Total: 0, Succeeded: 0, Failures: []TickerFailure{}}, nil
	}

	type outcome struct {
		ticker string
		price  fetchedPrice
		err    error
	}

	routable := make([]TrackedTicker, 0, len(tickers))
	for _, t := range tickers {
		if t.YahooTicker != "" { // defensive only — the Store's own query already excludes blanks
			routable = append(routable, t)
		}
	}

	// Buffered for every ticker, so no fan-out goroutine can block on send.
	results := make(chan outcome, len(tickers))
	fetch := func(t TrackedTicker) {
		priceNative, err := s.fetchPriceRouted(ctx, t.YahooTicker, t.Exchange)
		if err != nil {
			results <- outcome{ticker: t.YahooTicker, err: err}
			return
		}
		pricePln, err := s.convertToPln(ctx, priceNative, t.PriceCurrency)
		if err != nil {
			results <- outcome{ticker: t.YahooTicker, err: err}
			return
		}
		results <- outcome{ticker: t.YahooTicker, price: fetchedPrice{priceNative: priceNative, currency: t.PriceCurrency, pricePln: pricePln}}
	}
	notStarted := func(t TrackedTicker, err error) {
		results <- outcome{ticker: t.YahooTicker, err: fmt.Errorf("refresh cancelled before fetch: %w", err)}
	}
	go func() {
		fanOut(ctx, routable, fetch, notStarted)
		close(results)
	}()

	var failures []TickerFailure
	succeeded := 0
	for r := range results {
		if r.err != nil {
			failures = append(failures, TickerFailure{Ticker: r.ticker, Reason: r.err.Error()})
			continue
		}
		if err := s.upsertPriceCache(ctx, r.ticker, r.price); err != nil {
			failures = append(failures, TickerFailure{Ticker: r.ticker, Reason: err.Error()})
			continue
		}
		succeeded++
	}
	if failures == nil {
		failures = []TickerFailure{}
	}

	slog.Info("price cache: refresh complete", "total", len(tickers), "succeeded", succeeded, "failed", len(failures))
	return RefreshResult{Total: len(tickers), Succeeded: succeeded, Failures: failures}, nil
}

// fetchPriceRouted picks Yahoo or Binance-with-Yahoo-fallback by exchange, the direct port of
// both apps' PriceCacheService.fetchPriceRouted BINANCE branch (portfolioB's own
// PriceProviderRegistry-based branch resolves to the identical two providers at runtime — see
// this package's own doc comment).
func (s *Service) fetchPriceRouted(ctx context.Context, ticker, exchange string) (decimal.Decimal, error) {
	provider := s.yahoo
	if exchange == binanceExchange {
		provider = s.binanceWithFallback
	}
	price, err := provider.GetPrice(ctx, ticker)
	if err != nil {
		slog.Warn("price refresh: no price returned", "provider", provider.Name(), "ticker", ticker, "error", err)
		return decimal.Zero, err
	}
	slog.Info("price refresh", "provider", provider.Name(), "ticker", ticker, "price", price)
	return price, nil
}

// convertToPln mirrors both apps' PriceCacheService.convertToPln.
func (s *Service) convertToPln(ctx context.Context, priceNative decimal.Decimal, currency string) (decimal.Decimal, error) {
	if currency == "PLN" {
		return priceNative, nil
	}
	rate, err := s.GetRateToPln(ctx, currency)
	if err != nil {
		return decimal.Zero, fmt.Errorf("fx rate unavailable for currency %s: %w", currency, err)
	}
	return priceNative.Mul(rate), nil
}

func (s *Service) upsertPriceCache(ctx context.Context, ticker string, fp fetchedPrice) error {
	return s.store.UpsertPrice(ctx, PriceEntry{
		Ticker:    ticker,
		Price:     fp.priceNative,
		Currency:  fp.currency,
		PricePln:  fp.pricePln,
		FetchedAt: time.Now().UTC(),
	})
}

// GetRateToPln returns the PLN rate for currency, the direct port of both apps'
// FxRateCacheService.getRateToPln: PLN is always 1.0 with no external call; otherwise a
// still-fresh (< fxTTL) cached rate is returned as-is; a stale-or-missing rate triggers a live
// Yahoo Finance fetch, persisted on success; on fetch failure, a stale cached value is returned
// rather than erroring (the exact "never crash on a stale price" gotcha); only a currency with
// no cache AND a failed fetch returns an error.
func (s *Service) GetRateToPln(ctx context.Context, currency string) (decimal.Decimal, error) {
	if currency == "PLN" {
		return decimal.NewFromInt(1), nil
	}

	cachedRow, cacheErr := s.store.GetFxRate(ctx, currency)
	hasCached := cacheErr == nil
	if cacheErr != nil && !errors.Is(cacheErr, ErrNotFound) {
		return decimal.Zero, fmt.Errorf("marketdatacore: read cached fx rate for %s: %w", currency, cacheErr)
	}
	if hasCached && time.Since(cachedRow.FetchedAt) < fxTTL {
		return cachedRow.RateToPln, nil
	}

	freshRate, fetchErr := s.fetchFxRateFromYahoo(ctx, currency)
	if fetchErr == nil {
		if err := s.saveFxRateCache(ctx, currency, freshRate); err != nil {
			return decimal.Zero, fmt.Errorf("marketdatacore: save fx rate for %s: %w", currency, err)
		}
		return freshRate, nil
	}

	if hasCached {
		slog.Warn("fx rate fetch failed; returning stale cached value",
			"currency", currency, "fetchedAt", cachedRow.FetchedAt, "error", fetchErr)
		return cachedRow.RateToPln, nil
	}

	return decimal.Zero, fmt.Errorf("marketdatacore: fx rate unavailable for currency %s: %w", currency, fetchErr)
}

func (s *Service) fetchFxRateFromYahoo(ctx context.Context, currency string) (decimal.Decimal, error) {
	yahooTicker, ok := yahooFXTickers[currency]
	if !ok {
		return decimal.Zero, fmt.Errorf("marketdatacore: no Yahoo FX ticker configured for currency %s", currency)
	}
	rate, err := s.yahoo.GetPrice(ctx, yahooTicker)
	if err != nil {
		slog.Warn("Yahoo Finance failed for FX rate", "currency", currency, "error", err)
		return decimal.Zero, err
	}
	return rate, nil
}

func (s *Service) saveFxRateCache(ctx context.Context, currency string, rate decimal.Decimal) error {
	return s.store.UpsertFxRate(ctx, FxRateEntry{
		FromCurrency: currency,
		RateToPln:    rate,
		Source:       "YAHOO_FINANCE",
		FetchedAt:    time.Now().UTC(),
	})
}

// ForceRefreshAllFxRates refreshes every tracked FX currency in parallel (bounded like the price
// refresh; the four currencies fit in one round), ignoring TTL. Blocks until every currency has
// been attempted, so callers can rely on a fully populated FX cache after this call completes.
// Per-currency failures are logged but never surfaced to the caller — POST
// /market-data/fx-rates/refresh always answers 204 regardless.
func (s *Service) ForceRefreshAllFxRates(ctx context.Context) {
	refresh := func(currency string) {
		rate, err := s.fetchFxRateFromYahoo(ctx, currency)
		if err != nil {
			slog.Warn("force refresh fx rate failed", "currency", currency, "error", err)
			return
		}
		if err := s.saveFxRateCache(ctx, currency, rate); err != nil {
			slog.Warn("force refresh fx rate: save failed", "currency", currency, "error", err)
		}
	}
	notStarted := func(currency string, err error) {
		slog.Warn("force refresh fx rate skipped", "currency", currency, "error", err)
	}
	fanOut(ctx, slices.Collect(maps.Keys(yahooFXTickers)), refresh, notStarted)
}

// refreshConcurrency bounds the refresh fan-out against Yahoo (F4.1): an unofficial API
// answers a burst of one request per tracked ticker with 429s.
const refreshConcurrency = 4

// fanOut calls run(item) for every item, each on its own goroutine but at most
// refreshConcurrency at a time, and returns when all have finished. An item that has not
// started when ctx is cancelled is handed to notStarted(item, ctx.Err()) instead of run,
// so every item is accounted for exactly once.
func fanOut[T any](ctx context.Context, items []T, run func(T), notStarted func(T, error)) {
	sem := make(chan struct{}, refreshConcurrency)
	var wg sync.WaitGroup
	for _, item := range items {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				notStarted(item, ctx.Err())
				return
			}
			defer func() { <-sem }()
			// select picks at random when both cases are ready; never start after a cancel.
			if err := ctx.Err(); err != nil {
				notStarted(item, err)
				return
			}
			run(item)
		})
	}
	wg.Wait()
}
