package marketdatacore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// fakeStore is an in-memory stand-in for each app's own *sqlcgen.Queries adapter — business
// logic (ticker routing, PLN conversion, TTL gating, failure aggregation) is table-tested here;
// each app's own internal/marketdata/integration_test.go covers the real DB round-trip per the
// "never mock the database" gate. Guarded by a mutex because Service.ForceRefreshAllPrices/
// ForceRefreshAllFxRates write concurrently from multiple goroutines.
type fakeStore struct {
	mu      sync.Mutex
	prices  map[string]PriceEntry
	fxRates map[string]FxRateEntry
	tickers []TrackedTicker
}

func newFakeStore() *fakeStore {
	return &fakeStore{prices: map[string]PriceEntry{}, fxRates: map[string]FxRateEntry{}}
}

func (f *fakeStore) ListPrices(context.Context) ([]PriceEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]PriceEntry, 0, len(f.prices))
	for _, v := range f.prices {
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeStore) UpsertPrice(_ context.Context, entry PriceEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prices[entry.Ticker] = entry
	return nil
}

func (f *fakeStore) ListFxRates(context.Context) ([]FxRateEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FxRateEntry, 0, len(f.fxRates))
	for _, v := range f.fxRates {
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeStore) GetFxRate(_ context.Context, currency string) (FxRateEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.fxRates[currency]
	if !ok {
		return FxRateEntry{}, ErrNotFound
	}
	return row, nil
}

func (f *fakeStore) UpsertFxRate(_ context.Context, entry FxRateEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fxRates[entry.FromCurrency] = entry
	return nil
}

func (f *fakeStore) ListTrackedTickers(context.Context) ([]TrackedTicker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tickers, nil
}

// fakeProvider is a scripted priceprovider.PriceProvider for service-layer tests.
type fakeProvider struct {
	name   string
	prices map[string]decimal.Decimal
	err    error
	calls  atomic.Int64 // incremented from the refresh fan-out goroutines
}

func (p *fakeProvider) GetPrice(_ context.Context, ticker string) (decimal.Decimal, error) {
	p.calls.Add(1)
	if p.err != nil {
		return decimal.Zero, p.err
	}
	price, ok := p.prices[ticker]
	if !ok {
		return decimal.Zero, errors.New("fakeProvider: no price configured for " + ticker)
	}
	return price, nil
}

func (p *fakeProvider) Name() string { return p.name }

func TestGetAllPriceEntries_EmptyCache(t *testing.T) {
	s := NewService(newFakeStore(), &fakeProvider{name: "YAHOO"}, &fakeProvider{name: "BINANCE"})

	entries, err := s.GetAllPriceEntries(context.Background())
	if err != nil {
		t.Fatalf("GetAllPriceEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetAllFxRateEntries_AlwaysIncludesPLNFirst(t *testing.T) {
	s := NewService(newFakeStore(), &fakeProvider{name: "YAHOO"}, &fakeProvider{name: "BINANCE"})

	entries, err := s.GetAllFxRateEntries(context.Background())
	if err != nil {
		t.Fatalf("GetAllFxRateEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].FromCurrency != "PLN" || !entries[0].RateToPln.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("expected exactly one synthetic PLN=1.0 entry, got %+v", entries)
	}
}

func TestForceRefreshAllPrices_RoutesByExchange(t *testing.T) {
	q := newFakeStore()
	q.tickers = []TrackedTicker{
		{YahooTicker: "AAPL", PriceCurrency: "USD", Exchange: "XTB"},
		{YahooTicker: "BTC-USD", PriceCurrency: "USD", Exchange: "BINANCE"},
	}

	yahoo := &fakeProvider{name: "YAHOO", prices: map[string]decimal.Decimal{
		"AAPL": decimal.NewFromInt(200), "USDPLN=X": decimal.NewFromFloat(4.0),
	}}
	binance := &fakeProvider{name: "BINANCE", prices: map[string]decimal.Decimal{
		"BTC-USD": decimal.NewFromInt(65000),
	}}

	s := NewService(q, yahoo, binance)
	result, err := s.ForceRefreshAllPrices(context.Background())
	if err != nil {
		t.Fatalf("ForceRefreshAllPrices: %v", err)
	}
	if result.Total != 2 || result.Succeeded != 2 || len(result.Failures) != 0 {
		t.Fatalf("expected 2/2 succeeded with no failures, got %+v", result)
	}
	if binance.calls.Load() != 1 {
		t.Errorf("expected BTC-USD routed to the BINANCE provider exactly once, got %d calls", binance.calls.Load())
	}

	aapl, ok := q.prices["AAPL"]
	if !ok {
		t.Fatal("expected AAPL to be persisted")
	}
	if !aapl.PricePln.Equal(decimal.NewFromInt(800)) { // 200 USD * 4.0 USDPLN
		t.Errorf("expected AAPL pricePln 800, got %s", aapl.PricePln)
	}
}

// fallbackForTest is a minimal local stand-in for priceprovider.FallbackProvider so this test
// doesn't need to depend on common-go/priceprovider's real HTTP-calling constructors.
type fallbackForTest struct {
	primary, fallback interface {
		GetPrice(context.Context, string) (decimal.Decimal, error)
		Name() string
	}
}

func (f fallbackForTest) GetPrice(ctx context.Context, ticker string) (decimal.Decimal, error) {
	if price, err := f.primary.GetPrice(ctx, ticker); err == nil {
		return price, nil
	}
	return f.fallback.GetPrice(ctx, ticker)
}
func (f fallbackForTest) Name() string { return f.primary.Name() + "→" + f.fallback.Name() }

func TestForceRefreshAllPrices_BinanceFailureFallsBackToYahoo(t *testing.T) {
	q := newFakeStore()
	q.tickers = []TrackedTicker{{YahooTicker: "BTC-USD", PriceCurrency: "PLN", Exchange: "BINANCE"}}
	yahoo := &fakeProvider{name: "YAHOO", prices: map[string]decimal.Decimal{"BTC-USD": decimal.NewFromInt(70000)}}
	binance := &fakeProvider{name: "BINANCE", err: errors.New("binance unreachable")}
	binanceWithFallback := fallbackForTest{primary: binance, fallback: yahoo}

	s := NewService(q, yahoo, binanceWithFallback)
	result, err := s.ForceRefreshAllPrices(context.Background())
	if err != nil {
		t.Fatalf("ForceRefreshAllPrices: %v", err)
	}
	if result.Succeeded != 1 || len(result.Failures) != 0 {
		t.Fatalf("expected the fallback to succeed, got %+v", result)
	}
}

func TestForceRefreshAllPrices_FetchFailureIsReportedNotFatal(t *testing.T) {
	q := newFakeStore()
	q.tickers = []TrackedTicker{
		{YahooTicker: "GOOD", PriceCurrency: "PLN", Exchange: "XTB"},
		{YahooTicker: "BAD", PriceCurrency: "PLN", Exchange: "XTB"},
	}
	yahoo := &fakeProvider{name: "YAHOO", prices: map[string]decimal.Decimal{"GOOD": decimal.NewFromInt(10)}}

	s := NewService(q, yahoo, yahoo)
	result, err := s.ForceRefreshAllPrices(context.Background())
	if err != nil {
		t.Fatalf("ForceRefreshAllPrices: %v", err)
	}
	if result.Total != 2 || result.Succeeded != 1 || len(result.Failures) != 1 {
		t.Fatalf("expected 1/2 succeeded with 1 failure, got %+v", result)
	}
	if result.Failures[0].Ticker != "BAD" {
		t.Errorf("expected the failure to name BAD, got %+v", result.Failures[0])
	}
}

func TestForceRefreshAllPrices_NoTrackedTickers(t *testing.T) {
	q := newFakeStore()
	s := NewService(q, &fakeProvider{name: "YAHOO"}, &fakeProvider{name: "BINANCE"})

	result, err := s.ForceRefreshAllPrices(context.Background())
	if err != nil {
		t.Fatalf("ForceRefreshAllPrices: %v", err)
	}
	if result.Total != 0 || result.Succeeded != 0 || len(result.Failures) != 0 {
		t.Fatalf("expected an all-zero result, got %+v", result)
	}
}

func TestGetRateToPln_PLNIsAlwaysOneWithNoFetch(t *testing.T) {
	q := newFakeStore()
	yahoo := &fakeProvider{name: "YAHOO"}
	s := NewService(q, yahoo, yahoo)

	rate, err := s.GetRateToPln(context.Background(), "PLN")
	if err != nil {
		t.Fatalf("GetRateToPln: %v", err)
	}
	if !rate.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("expected 1, got %s", rate)
	}
	if yahoo.calls.Load() != 0 {
		t.Errorf("expected no external call for PLN, got %d calls", yahoo.calls.Load())
	}
}

func TestGetRateToPln_FreshCacheIsUsedWithoutFetching(t *testing.T) {
	q := newFakeStore()
	q.fxRates["EUR"] = FxRateEntry{
		FromCurrency: "EUR", RateToPln: decimal.RequireFromString("4.28"),
		Source: "YAHOO_FINANCE", FetchedAt: time.Now().UTC(),
	}
	yahoo := &fakeProvider{name: "YAHOO"}
	s := NewService(q, yahoo, yahoo)

	rate, err := s.GetRateToPln(context.Background(), "EUR")
	if err != nil {
		t.Fatalf("GetRateToPln: %v", err)
	}
	if !rate.Equal(decimal.RequireFromString("4.28")) {
		t.Fatalf("expected 4.28, got %s", rate)
	}
	if yahoo.calls.Load() != 0 {
		t.Errorf("expected the fresh cache to be used with no external call, got %d calls", yahoo.calls.Load())
	}
}

func TestGetRateToPln_StaleCacheTriggersRefetch(t *testing.T) {
	q := newFakeStore()
	q.fxRates["EUR"] = FxRateEntry{
		FromCurrency: "EUR", RateToPln: decimal.RequireFromString("4.00"),
		Source: "YAHOO_FINANCE", FetchedAt: time.Now().UTC().Add(-2 * time.Hour),
	}
	yahoo := &fakeProvider{name: "YAHOO", prices: map[string]decimal.Decimal{"EURPLN=X": decimal.RequireFromString("4.30")}}
	s := NewService(q, yahoo, yahoo)

	rate, err := s.GetRateToPln(context.Background(), "EUR")
	if err != nil {
		t.Fatalf("GetRateToPln: %v", err)
	}
	if !rate.Equal(decimal.RequireFromString("4.30")) {
		t.Fatalf("expected the freshly-fetched 4.30, got %s", rate)
	}
	if yahoo.calls.Load() != 1 {
		t.Errorf("expected exactly 1 external call, got %d", yahoo.calls.Load())
	}
	if !q.fxRates["EUR"].RateToPln.Equal(decimal.RequireFromString("4.30")) {
		t.Errorf("expected the fresh rate to be persisted, got %s", q.fxRates["EUR"].RateToPln)
	}
}

func TestGetRateToPln_FetchFailureFallsBackToStaleCache(t *testing.T) {
	q := newFakeStore()
	staleFetchedAt := time.Now().UTC().Add(-2 * time.Hour)
	q.fxRates["EUR"] = FxRateEntry{
		FromCurrency: "EUR", RateToPln: decimal.RequireFromString("4.00"),
		Source: "YAHOO_FINANCE", FetchedAt: staleFetchedAt,
	}
	yahoo := &fakeProvider{name: "YAHOO", err: errors.New("yahoo down")}
	s := NewService(q, yahoo, yahoo)

	rate, err := s.GetRateToPln(context.Background(), "EUR")
	if err != nil {
		t.Fatalf("expected a stale-cache fallback, not an error: %v", err)
	}
	if !rate.Equal(decimal.RequireFromString("4.00")) {
		t.Fatalf("expected the stale cached 4.00, got %s", rate)
	}
}

func TestGetRateToPln_NoCacheAndFetchFailureReturnsError(t *testing.T) {
	q := newFakeStore()
	yahoo := &fakeProvider{name: "YAHOO", err: errors.New("yahoo down")}
	s := NewService(q, yahoo, yahoo)

	if _, err := s.GetRateToPln(context.Background(), "EUR"); err == nil {
		t.Fatal("expected an error when there is no cache and the fetch fails")
	}
}

func TestForceRefreshAllFxRates_RefreshesEveryConfiguredCurrency(t *testing.T) {
	q := newFakeStore()
	yahoo := &fakeProvider{name: "YAHOO", prices: map[string]decimal.Decimal{
		"EURPLN=X": decimal.RequireFromString("4.28"),
		"USDPLN=X": decimal.RequireFromString("3.90"),
		"CHFPLN=X": decimal.RequireFromString("4.50"),
		"GBPPLN=X": decimal.RequireFromString("5.00"),
	}}
	s := NewService(q, yahoo, yahoo)

	s.ForceRefreshAllFxRates(context.Background())

	if len(q.fxRates) != 4 {
		t.Fatalf("expected all 4 configured currencies to be refreshed, got %d", len(q.fxRates))
	}
}
