package taxreturns

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/testsupport"
)

// TestTaxReturns_RealPostgresRoundTrip exercises the tax_returns write path against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod —
// the "never mock the database" gate, asserting against literally the same schema Java's
// own tests do. Covers: create+list round-trip with NUMERIC(18,4)/NUMERIC(7,4) precision
// surviving exactly, the UNIQUE(year) → 409 contract against the real constraint, year
// DESC ordering, and the trigger-updated_at contract (Go never sends updated_at — the
// INSERT omits both audit columns; the trigger writes updated_at on UPDATE).
func TestTaxReturns_RealPostgresRoundTrip(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `TRUNCATE tax_returns`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	svc := NewService(queries)

	t.Run("create then list round-trips 4dp precision exactly", func(t *testing.T) {
		dto, err := svc.Create(ctx, 2024,
			decimal.RequireFromString("50000.0000"),
			decimal.RequireFromString("9500.0000"),
			decimal.RequireFromString("19.0000"))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if dto.CreatedAt.IsZero() || dto.UpdatedAt.IsZero() {
			t.Fatal("expected audit timestamps set")
		}
		// Fresh INSERT: both audit columns default to NOW().
		if dto.UpdatedAt.Before(dto.CreatedAt) {
			t.Fatalf("expected updated_at >= created_at, got %v < %v", dto.UpdatedAt, dto.CreatedAt)
		}

		row, err := queries.GetTaxReturnByYear(ctx, 2024)
		if err != nil {
			t.Fatal(err)
		}
		gross, _ := pgnum.ToDecimal(row.GrossGain)
		tax, _ := pgnum.ToDecimal(row.TaxPaid)
		rate, _ := pgnum.ToDecimal(row.EffectiveRatePct)
		if gross.StringFixed(4) != "50000.0000" || tax.StringFixed(4) != "9500.0000" || rate.StringFixed(4) != "19.0000" {
			t.Fatalf("expected 4dp precision to survive, got %s/%s/%s", gross, tax, rate)
		}
	})

	t.Run("duplicate year is 409 against the real constraint", func(t *testing.T) {
		_, err := svc.Create(ctx, 2024, decimal.NewFromInt(1), decimal.NewFromInt(1), decimal.NewFromInt(1))
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Code != httpx.CodeDuplicateEntry {
			t.Fatalf("expected 409 DUPLICATE_ENTRY, got %v", err)
		}
	})

	t.Run("list is year DESC with pagination", func(t *testing.T) {
		for _, y := range []int{2022, 2025} {
			if _, err := svc.Create(ctx, y, decimal.NewFromInt(100), decimal.Zero, decimal.Zero); err != nil {
				t.Fatal(err)
			}
		}
		page, err := svc.List(ctx, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 3 || page.Items[0].Year != 2025 || page.Items[2].Year != 2022 {
			t.Fatalf("expected year DESC total 3, got %+v", page)
		}
		one, two := 1, 2
		page, err = svc.List(ctx, &one, &two)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].Year != 2022 {
			t.Fatalf("expected second size-2 page with 2022, got %+v", page)
		}
	})

	t.Run("trigger writes updated_at on UPDATE, Go never sends it", func(t *testing.T) {
		// Size-1 page trick above is unrelated; fetch the 2024 row for the trigger check.
		created, err := queries.GetTaxReturnByYear(ctx, 2024)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond) // separate NOW() readings deterministically
		if _, err := pool.Exec(ctx, `UPDATE tax_returns SET tax_paid = 9501.00 WHERE year = 2024`); err != nil {
			t.Fatal(err)
		}
		updated, err := queries.GetTaxReturnByYear(ctx, 2024)
		if err != nil {
			t.Fatal(err)
		}
		if !updated.CreatedAt.Time.Equal(created.CreatedAt.Time) {
			t.Fatal("created_at must be immutable across updates")
		}
		if !updated.UpdatedAt.Time.After(created.UpdatedAt.Time) {
			t.Fatalf("expected trigger to move updated_at past %v, got %v",
				created.UpdatedAt.Time, updated.UpdatedAt.Time)
		}
	})
}
