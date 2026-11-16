// Package refdata is the one adapter portfolioB and portfolioC share for the reference tables whose
// columns are identical in both apps (task 180 wave 5, ADR-113): price_cache, fx_rate_cache,
// settings and inflation, plus the tracked-ticker read of positions. It implements
// marketdatacore.Store, settingscore.Store, inflationsync.Queries, the CPI index of the historia
// read path (MomAsc) and siblingcore.PriceSource over one sqlc query set generated against
// portfolioB's schema; parity_integration_test.go runs it against both apps' databases.
package refdata

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/inflationsync"
	"investing-app/common-go/marketdatacore"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/settingscore"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/refdata/sqlcgen"
)

// Store is the reference-table adapter over one connection, pool or transaction.
type Store struct{ q *sqlcgen.Queries }

// New binds the store to db (a *pgxpool.Pool, *pgx.Conn or pgx.Tx).
func New(db sqlcgen.DBTX) *Store { return &Store{q: sqlcgen.New(db)} }

var (
	_ marketdatacore.Store    = (*Store)(nil)
	_ settingscore.Store      = (*Store)(nil)
	_ inflationsync.Queries   = (*Store)(nil)
	_ siblingcore.PriceSource = (*Store)(nil)
)

// ── market data (marketdatacore.Store) ──────────────────────────────────────

// ListPrices returns every price_cache row.
func (s *Store) ListPrices(ctx context.Context) ([]marketdatacore.PriceEntry, error) {
	rows, err := s.q.ListAllPriceCache(ctx)
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

// UpsertPrice writes one price_cache row.
func (s *Store) UpsertPrice(ctx context.Context, entry marketdatacore.PriceEntry) error {
	return s.q.UpsertPriceCache(ctx, sqlcgen.UpsertPriceCacheParams{
		Ticker:    entry.Ticker,
		Price:     pgnum.FromDecimal(entry.Price),
		Currency:  sqlcgen.CurrencyEnum(entry.Currency),
		PricePln:  pgnum.FromDecimal(entry.PricePln),
		FetchedAt: pgtype.Timestamptz{Time: entry.FetchedAt, Valid: true},
	})
}

// ListFxRates returns every fx_rate_cache row.
func (s *Store) ListFxRates(ctx context.Context) ([]marketdatacore.FxRateEntry, error) {
	rows, err := s.q.ListAllFxRateCache(ctx)
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

// GetFxRate returns one currency's rate (marketdatacore.ErrNotFound when absent).
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

// UpsertFxRate writes one fx_rate_cache row.
func (s *Store) UpsertFxRate(ctx context.Context, entry marketdatacore.FxRateEntry) error {
	return s.q.UpsertFxRateCache(ctx, sqlcgen.UpsertFxRateCacheParams{
		FromCurrency: sqlcgen.CurrencyEnum(entry.FromCurrency),
		RateToPln:    pgnum.FromDecimal(entry.RateToPln),
		Source:       entry.Source,
		FetchedAt:    pgtype.Timestamptz{Time: entry.FetchedAt, Valid: true},
	})
}

// ListTrackedTickers is every distinct yahoo ticker of the live positions (both portfolios in
// portfolioC) with the currency and exchange that route its refresh.
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

// ── cache-only prices (siblingcore.PriceSource) ─────────────────────────────

// PricePlnByYahooTicker is price_cache as yahoo ticker → PLN price (never a live fetch).
func (s *Store) PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error) {
	rows, err := s.q.ListAllPriceCache(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]decimal.Decimal, len(rows))
	for _, r := range rows {
		v, err := pgnum.ToDecimal(r.PricePln)
		if err != nil {
			return nil, err
		}
		out[r.Ticker] = v
	}
	return out, nil
}

// AllRatesToPln is fx_rate_cache as currency → PLN rate, with PLN = 1 always present.
func (s *Store) AllRatesToPln(ctx context.Context) (map[string]decimal.Decimal, error) {
	rows, err := s.q.ListAllFxRateCache(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}
	for _, r := range rows {
		v, err := pgnum.ToDecimal(r.RateToPln)
		if err != nil {
			return nil, err
		}
		out[string(r.FromCurrency)] = v
	}
	return out, nil
}

// FetchedAtByYahooTicker is when each cached price was fetched (the rebalancing result's
// pricesRefreshedAt).
func (s *Store) FetchedAtByYahooTicker(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.q.ListAllPriceCache(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		if r.FetchedAt.Valid {
			out[r.Ticker] = r.FetchedAt.Time
		}
	}
	return out, nil
}

// ── settings (settingscore.Store) ───────────────────────────────────────────

// Get returns one setting (settingscore.ErrNotFound when absent).
func (s *Store) Get(ctx context.Context, key string) (settingscore.Row, error) {
	row, err := s.q.GetSettingByKey(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settingscore.Row{}, settingscore.ErrNotFound
		}
		return settingscore.Row{}, err
	}
	return settingRow(row), nil
}

// List returns every setting.
func (s *Store) List(ctx context.Context) ([]settingscore.Row, error) {
	rows, err := s.q.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]settingscore.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, settingRow(r))
	}
	return out, nil
}

// Update sets one existing setting (settingscore.ErrNotFound when absent).
func (s *Store) Update(ctx context.Context, key, value string) (settingscore.Row, error) {
	row, err := s.q.UpdateSettingValue(ctx, sqlcgen.UpdateSettingValueParams{Key: key, Value: value})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return settingscore.Row{}, settingscore.ErrNotFound
		}
		return settingscore.Row{}, err
	}
	return settingRow(row), nil
}

func settingRow(s sqlcgen.Setting) settingscore.Row {
	return settingscore.Row{Key: s.Key, Value: s.Value, UpdatedAt: s.UpdatedAt.Time}
}

// ── inflation (inflationsync.Queries, historiaquery's MomAsc) ───────────────

// GetCpi returns one month's CPI (inflationsync.ErrNotFound when absent).
func (s *Store) GetCpi(ctx context.Context, year, month int) (decimal.Decimal, error) {
	row, err := s.q.GetInflationByYearMonth(ctx, sqlcgen.GetInflationByYearMonthParams{Year: int32(year), Month: int32(month)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, inflationsync.ErrNotFound
		}
		return decimal.Zero, err
	}
	return pgnum.ToDecimal(row.CpiValue)
}

// CreateEntry inserts one month's CPI.
func (s *Store) CreateEntry(ctx context.Context, year, month int, cpi decimal.Decimal) error {
	_, err := s.q.CreateInflationEntry(ctx, sqlcgen.CreateInflationEntryParams{Year: int32(year), Month: int32(month), CpiValue: pgnum.FromDecimal(cpi)})
	return err
}

// UpdateCpi overwrites one month's CPI.
func (s *Store) UpdateCpi(ctx context.Context, year, month int, cpi decimal.Decimal) error {
	_, err := s.q.UpdateInflationCpi(ctx, sqlcgen.UpdateInflationCpiParams{Year: int32(year), Month: int32(month), CpiValue: pgnum.FromDecimal(cpi)})
	return err
}

// ListAsc returns every stored month, oldest first.
func (s *Store) ListAsc(ctx context.Context) ([]inflationsync.YearMonth, error) {
	rows, err := s.q.ListInflationAsc(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]inflationsync.YearMonth, 0, len(rows))
	for _, r := range rows {
		out = append(out, inflationsync.YearMonth{Year: int(r.Year), Month: int(r.Month)})
	}
	return out, nil
}

// MomAsc returns the whole inflation table as a (year, month) → CPI map (historiaquery.Queries).
func (s *Store) MomAsc(ctx context.Context) (map[historiaquery.YearMonth]decimal.Decimal, error) {
	rows, err := s.q.ListHistoriaInflationAsc(ctx)
	if err != nil {
		return nil, fmt.Errorf("historia: read inflation index: %w", err)
	}
	mom := make(map[historiaquery.YearMonth]decimal.Decimal, len(rows))
	for _, r := range rows {
		cpi, err := pgnum.ToDecimal(r.CpiValue)
		if err != nil {
			return nil, fmt.Errorf("historia: decode cpi %d-%02d: %w", r.Year, r.Month, err)
		}
		mom[historiaquery.YearMonth{Year: int(r.Year), Month: int(r.Month)}] = cpi
	}
	return mom, nil
}
