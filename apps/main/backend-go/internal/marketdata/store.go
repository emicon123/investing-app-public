// Package marketdata is main's thin wiring over the shared common-go/marketdatacore kernel (task
// 178): the price/FX-rate endpoints (ported from the Java PriceCacheService and FxRateCacheService,
// ADR-094 / doc 13 §7). It owns exactly two tables: price_cache and fx_rate_cache.
//
// The business rules (Yahoo/Binance routing, the 60-minute FX TTL, PLN conversion, the
// parallel refresh and its 15-minute scheduler) are marketdatacore's — the same kernel
// portfolioB and portfolioC run. This package supplies only the Store adapter over main's sqlc
// queries (an unavoidable Go-module-boundary artifact) and the Handler mapping to main's
// generated openapi types.
//
// Scope note: the macro-liquidity endpoints (/market-data/macro-state* and friends) live in
// the macro sub-package, not here.
package marketdata

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/marketdatacore"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/priceprovider"
)

// Queries is the narrow slice of *sqlcgen.Queries the Store calls, declared as an interface
// so handler unit tests can run the real Store over an in-memory fake (store_test.go still
// exercises the real *sqlcgen.Queries against testcontainers-go, per the "never mock the
// database" gate).
type Queries interface {
	GetAllPriceCache(ctx context.Context) ([]sqlcgen.GetAllPriceCacheRow, error)
	UpsertPriceCache(ctx context.Context, arg sqlcgen.UpsertPriceCacheParams) error
	GetAllFxRateCache(ctx context.Context) ([]sqlcgen.GetAllFxRateCacheRow, error)
	GetFxRateCacheByCurrency(ctx context.Context, fromCurrency sqlcgen.CurrencyEnum) (sqlcgen.GetFxRateCacheByCurrencyRow, error)
	UpsertFxRateCache(ctx context.Context, arg sqlcgen.UpsertFxRateCacheParams) error
	GetTrackedTickers(ctx context.Context) ([]sqlcgen.GetTrackedTickersRow, error)
}

// Store satisfies marketdatacore.Store over main's sqlc queries (model: portfolioB's
// internal/marketdata/store.go; main's query names differ, the mapping does not).
type Store struct{ q Queries }

// NewStore wraps the queries.
func NewStore(q Queries) *Store { return &Store{q: q} }

// NewService wires the shared kernel over main's Store plus the two provider values main.go's
// composition root already constructed (explicit constructor wiring, ADR-094 §2).
func NewService(q Queries, yahoo, binanceWithFallback priceprovider.PriceProvider) *marketdatacore.Service {
	return marketdatacore.NewService(NewStore(q), yahoo, binanceWithFallback)
}

func (s *Store) ListPrices(ctx context.Context) ([]marketdatacore.PriceEntry, error) {
	rows, err := s.q.GetAllPriceCache(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]marketdatacore.PriceEntry, 0, len(rows))
	for _, r := range rows {
		price, err := pgnum.ToDecimal(r.Price)
		if err != nil {
			return nil, err
		}
		pricePln, err := pgnum.ToDecimal(r.PricePln)
		if err != nil {
			return nil, err
		}
		out = append(out, marketdatacore.PriceEntry{
			Ticker: r.Ticker, Price: price, Currency: string(r.Currency),
			PricePln: pricePln, FetchedAt: r.FetchedAt.Time,
		})
	}
	return out, nil
}

func (s *Store) UpsertPrice(ctx context.Context, entry marketdatacore.PriceEntry) error {
	return s.q.UpsertPriceCache(ctx, sqlcgen.UpsertPriceCacheParams{
		Ticker:    entry.Ticker,
		Price:     pgnum.FromDecimal(entry.Price),
		Currency:  sqlcgen.CurrencyEnum(entry.Currency),
		PricePln:  pgnum.FromDecimal(entry.PricePln),
		FetchedAt: pgtype.Timestamptz{Time: entry.FetchedAt, Valid: true},
	})
}

func (s *Store) ListFxRates(ctx context.Context) ([]marketdatacore.FxRateEntry, error) {
	rows, err := s.q.GetAllFxRateCache(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]marketdatacore.FxRateEntry, 0, len(rows))
	for _, r := range rows {
		rate, err := pgnum.ToDecimal(r.RateToPln)
		if err != nil {
			return nil, err
		}
		out = append(out, marketdatacore.FxRateEntry{
			FromCurrency: string(r.FromCurrency), RateToPln: rate,
			Source: r.Source, FetchedAt: r.FetchedAt.Time,
		})
	}
	return out, nil
}

// GetFxRate maps sqlc's pgx.ErrNoRows to the kernel's marketdatacore.ErrNotFound.
func (s *Store) GetFxRate(ctx context.Context, currency string) (marketdatacore.FxRateEntry, error) {
	row, err := s.q.GetFxRateCacheByCurrency(ctx, sqlcgen.CurrencyEnum(currency))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return marketdatacore.FxRateEntry{}, marketdatacore.ErrNotFound
		}
		return marketdatacore.FxRateEntry{}, err
	}
	rate, err := pgnum.ToDecimal(row.RateToPln)
	if err != nil {
		return marketdatacore.FxRateEntry{}, err
	}
	return marketdatacore.FxRateEntry{
		FromCurrency: string(row.FromCurrency), RateToPln: rate,
		Source: row.Source, FetchedAt: row.FetchedAt.Time,
	}, nil
}

func (s *Store) UpsertFxRate(ctx context.Context, entry marketdatacore.FxRateEntry) error {
	return s.q.UpsertFxRateCache(ctx, sqlcgen.UpsertFxRateCacheParams{
		FromCurrency: sqlcgen.CurrencyEnum(entry.FromCurrency),
		RateToPln:    pgnum.FromDecimal(entry.RateToPln),
		Source:       entry.Source,
		FetchedAt:    pgtype.Timestamptz{Time: entry.FetchedAt, Valid: true},
	})
}

// ListTrackedTickers reads `positions` read-only (doc 13 §6.3). main's row carries a nullable
// yahoo_ticker; nil/blank rows are skipped, as main's own refresh loop did before task 178.
func (s *Store) ListTrackedTickers(ctx context.Context) ([]marketdatacore.TrackedTicker, error) {
	rows, err := s.q.GetTrackedTickers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]marketdatacore.TrackedTicker, 0, len(rows))
	for _, r := range rows {
		if r.YahooTicker == nil || *r.YahooTicker == "" {
			continue // defensive only — the query's WHERE clause already excludes these
		}
		out = append(out, marketdatacore.TrackedTicker{
			YahooTicker: *r.YahooTicker, PriceCurrency: string(r.PriceCurrency), Exchange: string(r.Exchange),
		})
	}
	return out, nil
}
