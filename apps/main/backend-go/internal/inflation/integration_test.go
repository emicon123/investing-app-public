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
	"investing-app/common-go/httpx"
	"investing-app/common-go/inflationsync"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// TestInflation_RealPostgresRoundTrip exercises the inflation write path against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod —
// the "never mock the database" gate, asserting against literally the same schema Java's
// own tests do. Covers: create+list round-trip with NUMERIC(7,4) precision surviving
// exactly, the UNIQUE(year,month) → 409 contract against the real constraint, DESC
// ordering + year filter + pagination, the sync-no-changes → 0 contract through the REAL
// single-transaction runner, and the startup-stale check.
func TestInflation_RealPostgresRoundTrip(t *testing.T) {
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
	transact := pgtx.Runner(pool, func(tx pgx.Tx) inflationsync.Queries { return NewAdapter(queries.WithTx(tx)) })
	svc := NewService(queries)
	coreWith := func(entries []inflationsync.MomEntry) *inflationsync.Service {
		return inflationsync.NewService(NewAdapter(queries), transact, stubGus{entries})
	}

	t.Run("create then list round-trips NUMERIC(7,4) exactly", func(t *testing.T) {
		dto, err := svc.Create(ctx, 2024, 3, decimal.RequireFromString("2.5000"))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if dto.CreatedAt.IsZero() {
			t.Fatal("expected created_at set by DEFAULT NOW()")
		}
		row, err := queries.GetInflationByYearMonth(ctx, sqlcgen.GetInflationByYearMonthParams{Year: 2024, Month: 3})
		if err != nil {
			t.Fatal(err)
		}
		v, _ := pgnum.ToDecimal(row.CpiValue)
		if v.StringFixed(4) != "2.5000" {
			t.Fatalf("expected 4dp precision to survive (2.5000), got %s", v.StringFixed(4))
		}

		page, err := svc.List(ctx, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 || !page.Items[0].Cpi.Equal(decimal.RequireFromString("2.5")) {
			t.Fatalf("unexpected page: %+v", page)
		}
	})

	t.Run("duplicate (year,month) is 409 against the real constraint", func(t *testing.T) {
		_, err := svc.Create(ctx, 2024, 3, decimal.NewFromFloat(9.9))
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Code != httpx.CodeDuplicateEntry {
			t.Fatalf("expected 409 DUPLICATE_ENTRY, got %v", err)
		}
	})

	t.Run("list ordering, year filter and pagination", func(t *testing.T) {
		for _, ym := range [][2]int{
			{2023, 12}, {2024, 1}, {2025, 6},
		} {
			if _, err := svc.Create(ctx, ym[0], ym[1], decimal.NewFromInt(100)); err != nil {
				t.Fatal(err)
			}
		}
		page, err := svc.List(ctx, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 {
			t.Fatalf("expected total 4, got %+v", page)
		}
		if page.Items[0].Year != 2025 || page.Items[3].Year != 2023 || page.Items[3].Month != 12 {
			t.Fatalf("expected year/month DESC, got %+v", page.Items)
		}

		y2024 := 2024
		size := 1
		page, err = svc.List(ctx, &y2024, &pageZero, &size)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || len(page.Items) != 1 || page.Items[0].Month != 3 {
			t.Fatalf("expected 2024 filter newest-first (month 3), got %+v", page)
		}
	})

	t.Run("sync through the real transaction runner counts only real changes", func(t *testing.T) {
		syncSvc := coreWith([]inflationsync.MomEntry{
			{Year: 2024, Month: 3, Cpi: decimal.RequireFromString("2.5000")}, // unchanged
			{Year: 2025, Month: 6, Cpi: decimal.RequireFromString("100")},    // unchanged
		})
		upserts, err := syncSvc.Sync(ctx)
		if err != nil || upserts != 0 {
			t.Fatalf("expected sync-no-changes → 0, got %d,%v", upserts, err)
		}

		syncSvc2 := coreWith([]inflationsync.MomEntry{
			{Year: 2024, Month: 3, Cpi: decimal.RequireFromString("2.5000")}, // unchanged
			{Year: 2025, Month: 6, Cpi: decimal.RequireFromString("101.25")}, // changed
			{Year: 2025, Month: 7, Cpi: decimal.RequireFromString("99.5")},   // new
		})
		upserts, err = syncSvc2.Sync(ctx)
		if err != nil || upserts != 2 {
			t.Fatalf("expected 2 real changes, got %d,%v", upserts, err)
		}
		row, err := queries.GetInflationByYearMonth(ctx, sqlcgen.GetInflationByYearMonthParams{Year: 2025, Month: 6})
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := pgnum.ToDecimal(row.CpiValue); !v.Equal(decimal.RequireFromString("101.25")) {
			t.Fatalf("expected updated 101.25, got %s", v)
		}
	})

	t.Run("stale check against real rows", func(t *testing.T) {
		// Newest row is 2025-07; at 2025-09-15 that is stale, at 2025-08-10 it is fresh.
		stale, err := coreWith(nil).IsStale(ctx, time.Date(2025, 9, 15, 12, 0, 0, 0, time.UTC))
		if err != nil || !stale {
			t.Fatalf("expected stale, got %v,%v", stale, err)
		}
		stale, err = coreWith(nil).IsStale(ctx, time.Date(2025, 8, 10, 12, 0, 0, 0, time.UTC))
		if err != nil || stale {
			t.Fatalf("expected fresh, got %v,%v", stale, err)
		}
	})
}

var pageZero = 0
