package inflation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/inflationsync"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/siblingcore/refdata"
	refsql "investing-app/common-go/siblingcore/refdata/sqlcgen"
	"investing-app/common-go/testsupport"
)

// stubGus returns a fixed fetch list.
type stubGus struct{ entries []inflationsync.MomEntry }

func (s stubGus) FetchMomInflation(_ context.Context) []inflationsync.MomEntry { return s.entries }

// TestInflationSync_RealPostgresRoundTrip exercises the sync write path against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod —
// the "never mock the database" gate, asserting against literally the same schema Java's
// own tests do, and against this app's own database (portfolioC's migrations, not root's).
// Covers: sync inserts through the REAL single-transaction runner, NUMERIC(7,4) precision
// surviving exactly, sync-no-changes → 0 with nothing rewritten, changed+new counting,
// and the startup-stale check.
func TestInflationSync_RealPostgresRoundTrip(t *testing.T) {
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

	queries := refsql.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) inflationsync.Queries { return refdata.New(tx) })
	svc := inflationsync.NewService(refdata.New(pool), transact, stubGus{nil})

	t.Run("sync inserts through the real transaction runner", func(t *testing.T) {
		syncSvc := inflationsync.NewService(refdata.New(pool), transact, stubGus{[]inflationsync.MomEntry{
			{Year: 2024, Month: 3, Cpi: decimal.RequireFromString("2.5000")},
			{Year: 2024, Month: 4, Cpi: decimal.RequireFromString("101.25")},
		}})
		upserts, err := syncSvc.Sync(ctx)
		if err != nil || upserts != 2 {
			t.Fatalf("expected 2 inserts, got %d,%v", upserts, err)
		}
		row, err := queries.GetInflationByYearMonth(ctx, refsql.GetInflationByYearMonthParams{Year: 2024, Month: 3})
		if err != nil {
			t.Fatal(err)
		}
		v, _ := pgnum.ToDecimal(row.CpiValue)
		if v.StringFixed(4) != "2.5000" {
			t.Fatalf("expected 4dp precision to survive (2.5000), got %s", v.StringFixed(4))
		}
	})

	t.Run("sync-no-changes answers 0 and rewrites nothing", func(t *testing.T) {
		syncSvc := inflationsync.NewService(refdata.New(pool), transact, stubGus{[]inflationsync.MomEntry{
			{Year: 2024, Month: 3, Cpi: decimal.RequireFromString("2.5000")},
			{Year: 2024, Month: 4, Cpi: decimal.RequireFromString("101.25")},
		}})
		upserts, err := syncSvc.Sync(ctx)
		if err != nil || upserts != 0 {
			t.Fatalf("expected sync-no-changes → 0, got %d,%v", upserts, err)
		}
	})

	t.Run("changed value plus new month count as real changes", func(t *testing.T) {
		syncSvc := inflationsync.NewService(refdata.New(pool), transact, stubGus{[]inflationsync.MomEntry{
			{Year: 2024, Month: 3, Cpi: decimal.RequireFromString("2.5000")}, // unchanged
			{Year: 2024, Month: 4, Cpi: decimal.RequireFromString("102.00")}, // changed
			{Year: 2024, Month: 5, Cpi: decimal.RequireFromString("99.5")},   // new
		}})
		upserts, err := syncSvc.Sync(ctx)
		if err != nil || upserts != 2 {
			t.Fatalf("expected 2 real changes, got %d,%v", upserts, err)
		}
		row, err := queries.GetInflationByYearMonth(ctx, refsql.GetInflationByYearMonthParams{Year: 2024, Month: 4})
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := pgnum.ToDecimal(row.CpiValue); !v.Equal(decimal.RequireFromString("102.00")) {
			t.Fatalf("expected updated 102.00, got %s", v)
		}
	})

	t.Run("stale check against real rows", func(t *testing.T) {
		// Newest row is 2024-05; at 2024-07-15 that is stale, at 2024-06-10 it is fresh.
		stale, err := svc.IsStale(ctx, time.Date(2024, 7, 15, 12, 0, 0, 0, time.UTC))
		if err != nil || !stale {
			t.Fatalf("expected stale, got %v,%v", stale, err)
		}
		stale, err = svc.IsStale(ctx, time.Date(2024, 6, 10, 12, 0, 0, 0, time.UTC))
		if err != nil || stale {
			t.Fatalf("expected fresh, got %v,%v", stale, err)
		}
	})
}
