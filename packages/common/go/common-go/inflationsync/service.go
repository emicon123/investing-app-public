package inflationsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"
)

// ErrNotFound is returned by Queries.GetCpi when no row exists for (year, month) —
// the shared equivalent of pgx.ErrNoRows, so this kernel never imports a concrete
// sqlc package. Each sibling's thin adapter maps its own sqlc "no rows" error to
// this sentinel.
var ErrNotFound = errors.New("inflationsync: entry not found")

// YearMonth is one (year, month) pair from ListAsc — the only shape IsStale needs.
type YearMonth struct {
	Year  int
	Month int
}

// Queries is the narrow slice of per-app sqlc queries the sync needs, expressed in
// plain Go/decimal types so both siblings' generated sqlcgen packages satisfy it
// through a thin adapter (no per-app service code).
type Queries interface {
	GetCpi(ctx context.Context, year, month int) (decimal.Decimal, error)
	CreateEntry(ctx context.Context, year, month int, cpi decimal.Decimal) error
	UpdateCpi(ctx context.Context, year, month int, cpi decimal.Decimal) error
	ListAsc(ctx context.Context) ([]YearMonth, error)
}

// TransactFunc runs fn inside a single DB transaction — Sync's whole read-compare-write loop
// commits atomically. Production passes a pgx Begin/Commit closure; unit tests pass a direct
// call-through.
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// GusFetcher is the one varying step of Sync — the live GUS client in prod, a stub list in
// tests (composed, not inherited — doc 13 §5).
type GusFetcher interface {
	FetchMomInflation(ctx context.Context) []MomEntry
}

// Service is the inflation sync service shared by portfolioB and portfolioC: thin wiring hands it an adapter
// over its own *sqlcgen.Queries plus a transaction runner, and gets the shared behavior back
// (upsert loop + staleness rule), byte-for-byte identical per app.
type Service struct {
	queries  Queries
	transact TransactFunc
	gus      GusFetcher
}

// NewService wires queries + transaction runner + GUS fetcher (explicit constructor wiring,
// ADR-094 §2 — no DI framework).
func NewService(queries Queries, transact TransactFunc, gus GusFetcher) *Service {
	return &Service{queries: queries, transact: transact, gus: gus}
}

// Sync fetches the GUS month-over-month series and upserts it — the direct port of
// InflationSyncService.sync (identical in pl.portfolioB and pl.portfolioC after package-name
// normalization), in one transaction. The returned count covers ONLY real changes (a new
// row, or a row whose cpi_value actually differs — compareTo semantics,
// scale-insensitive): re-syncing unchanged data answers 0 and rewrites nothing.
func (s *Service) Sync(ctx context.Context) (int, error) {
	entries := s.gus.FetchMomInflation(ctx)
	if len(entries) == 0 {
		slog.Warn("GUS inflation sync: no entries fetched")
		return 0, nil
	}
	upserts := 0
	err := s.transact(ctx, func(q Queries) error {
		for _, e := range entries {
			stored, err := q.GetCpi(ctx, e.Year, e.Month)
			if err != nil {
				if !errors.Is(err, ErrNotFound) {
					return fmt.Errorf("inflation: sync read %d-%02d: %w", e.Year, e.Month, err)
				}
				if err := q.CreateEntry(ctx, e.Year, e.Month, e.Cpi); err != nil {
					return fmt.Errorf("inflation: sync insert %d-%02d: %w", e.Year, e.Month, err)
				}
				upserts++
				continue
			}
			if stored.Equal(e.Cpi) {
				continue
			}
			if err := q.UpdateCpi(ctx, e.Year, e.Month, e.Cpi); err != nil {
				return fmt.Errorf("inflation: sync update %d-%02d: %w", e.Year, e.Month, err)
			}
			upserts++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	slog.Info("GUS inflation sync: complete", "fetched", len(entries), "upserted", upserts)
	return upserts, nil
}

// IsStale ports InflationSyncService.isStale (identical in both apps): true when the table
// is empty or its newest (year, month) is older than YearMonth(now) minus one month. now
// is injected (the scheduler passes Warsaw time) so unit tests don't depend on the wall
// clock.
func (s *Service) IsStale(ctx context.Context, now time.Time) (bool, error) {
	rows, err := s.queries.ListAsc(ctx)
	if err != nil {
		return false, fmt.Errorf("inflation: staleness check: %w", err)
	}
	if len(rows) == 0 {
		return true, nil
	}
	last := rows[len(rows)-1]
	threshold := now.AddDate(0, -1, 0)
	return last.Year*12+last.Month < threshold.Year()*12+int(threshold.Month()), nil
}
