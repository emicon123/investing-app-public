package marketdata

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries, so the handler tests run the real
// Store + marketdatacore.Service without Postgres (store_test.go covers the real DB round-trip
// per the "never mock the database" gate; the kernel's business rules are tested in
// marketdatacore). Guarded by a mutex because ForceRefreshAllPrices/ForceRefreshAllFxRates
// write concurrently from multiple goroutines, like the real *pgxpool.Pool this stands in for.
type fakeQueries struct {
	mu       sync.Mutex
	prices   map[string]sqlcgen.GetAllPriceCacheRow
	fxRates  map[sqlcgen.CurrencyEnum]sqlcgen.GetFxRateCacheByCurrencyRow
	tickers  []sqlcgen.GetTrackedTickersRow
	failNext bool
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{
		prices:  map[string]sqlcgen.GetAllPriceCacheRow{},
		fxRates: map[sqlcgen.CurrencyEnum]sqlcgen.GetFxRateCacheByCurrencyRow{},
	}
}

func (f *fakeQueries) GetAllPriceCache(context.Context) ([]sqlcgen.GetAllPriceCacheRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sqlcgen.GetAllPriceCacheRow, 0, len(f.prices))
	for _, v := range f.prices {
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeQueries) UpsertPriceCache(_ context.Context, arg sqlcgen.UpsertPriceCacheParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return errors.New("simulated upsert failure")
	}
	f.prices[arg.Ticker] = sqlcgen.GetAllPriceCacheRow(arg)
	return nil
}

func (f *fakeQueries) GetAllFxRateCache(context.Context) ([]sqlcgen.GetAllFxRateCacheRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sqlcgen.GetAllFxRateCacheRow, 0, len(f.fxRates))
	for _, v := range f.fxRates {
		out = append(out, sqlcgen.GetAllFxRateCacheRow(v))
	}
	return out, nil
}

func (f *fakeQueries) GetFxRateCacheByCurrency(_ context.Context, fromCurrency sqlcgen.CurrencyEnum) (sqlcgen.GetFxRateCacheByCurrencyRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.fxRates[fromCurrency]
	if !ok {
		return sqlcgen.GetFxRateCacheByCurrencyRow{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeQueries) UpsertFxRateCache(_ context.Context, arg sqlcgen.UpsertFxRateCacheParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fxRates[arg.FromCurrency] = sqlcgen.GetFxRateCacheByCurrencyRow(arg)
	return nil
}

func (f *fakeQueries) GetTrackedTickers(context.Context) ([]sqlcgen.GetTrackedTickersRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tickers, nil
}

// fakeProvider is a scripted priceprovider.PriceProvider.
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
