package inflation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/inflationsync"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// TestAdapter_SatisfiesInflationsyncQueries proves main's Adapter gives the shared
// inflationsync kernel the semantics it relies on, against the real Flyway-migrated
// schema: a missing month is inflationsync.ErrNotFound (never pgx.ErrNoRows), NUMERIC(7,4)
// round-trips exactly, ListAsc is oldest first, and the core Sync/IsStale run through the
// real single-transaction runner exactly as main's own copies did before task 178.
func TestAdapter_SatisfiesInflationsyncQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE inflation`); err != nil {
		t.Fatal(err)
	}
	queries := sqlcgen.New(pool)
	var a inflationsync.Queries = NewAdapter(queries)

	t.Run("missing month is ErrNotFound", func(t *testing.T) {
		_, err := a.GetCpi(ctx, 2031, 1)
		if !errors.Is(err, inflationsync.ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("want inflationsync.ErrNotFound, got %v", err)
		}
	})

	t.Run("create, read, update round-trip NUMERIC(7,4)", func(t *testing.T) {
		if err := a.CreateEntry(ctx, 2025, 2, decimal.RequireFromString("100.3500")); err != nil {
			t.Fatal(err)
		}
		if got, err := a.GetCpi(ctx, 2025, 2); err != nil || got.StringFixed(4) != "100.3500" {
			t.Fatalf("want 100.3500, got %v, %v", got, err)
		}
		if err := a.UpdateCpi(ctx, 2025, 2, decimal.RequireFromString("100.1234")); err != nil {
			t.Fatal(err)
		}
		if got, err := a.GetCpi(ctx, 2025, 2); err != nil || got.StringFixed(4) != "100.1234" {
			t.Fatalf("want 100.1234, got %v, %v", got, err)
		}
	})

	t.Run("ListAsc is oldest first", func(t *testing.T) {
		for _, ym := range [][2]int{{2025, 1}, {2024, 12}} {
			if err := a.CreateEntry(ctx, ym[0], ym[1], decimal.NewFromInt(100)); err != nil {
				t.Fatal(err)
			}
		}
		got, err := a.ListAsc(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := []inflationsync.YearMonth{{Year: 2024, Month: 12}, {Year: 2025, Month: 1}, {Year: 2025, Month: 2}}
		if len(got) != len(want) {
			t.Fatalf("want %v, got %v", want, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("want %v, got %v", want, got)
			}
		}
	})

	t.Run("core Sync and IsStale through the real transaction runner", func(t *testing.T) {
		transact := pgtx.Runner(pool, func(tx pgx.Tx) inflationsync.Queries { return NewAdapter(queries.WithTx(tx)) })
		core := inflationsync.NewService(a, transact, stubGus{[]inflationsync.MomEntry{
			{Year: 2025, Month: 2, Cpi: decimal.RequireFromString("100.1234")}, // unchanged
			{Year: 2025, Month: 1, Cpi: decimal.RequireFromString("100.5")},    // changed
			{Year: 2025, Month: 3, Cpi: decimal.RequireFromString("99.9")},     // new
		}})
		upserts, err := core.Sync(ctx)
		if err != nil || upserts != 2 {
			t.Fatalf("want 2 real changes, got %d, %v", upserts, err)
		}
		if upserts, err := core.Sync(ctx); err != nil || upserts != 0 {
			t.Fatalf("re-sync of unchanged data: want 0, got %d, %v", upserts, err)
		}
		// Newest row is 2025-03: stale on 2025-05-15, fresh on 2025-04-10.
		if stale, err := core.IsStale(ctx, time.Date(2025, 5, 15, 12, 0, 0, 0, time.UTC)); err != nil || !stale {
			t.Fatalf("want stale, got %v, %v", stale, err)
		}
		if stale, err := core.IsStale(ctx, time.Date(2025, 4, 10, 12, 0, 0, 0, time.UTC)); err != nil || stale {
			t.Fatalf("want fresh, got %v, %v", stale, err)
		}
	})
}
