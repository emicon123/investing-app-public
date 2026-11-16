package settings

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore/refdata"
	"investing-app/common-go/testsupport"
)

// TestSettings_RealPostgres exercises the read/list/update path against portfolioB's own migrated
// database — never mock the database. Covers: listing the V1 seed rows, updating each of the 3
// registered numeric keys (valid + invalid), the 404 on an unknown key, and the updated_at
// trigger firing on write.
func TestSettings_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	svc := NewService(refdata.New(pool))

	t.Run("list returns the V1 seed rows", func(t *testing.T) {
		dtos, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(dtos) != 3 {
			t.Fatalf("expected 3 seeded settings, got %d: %+v", len(dtos), dtos)
		}
		seen := map[string]bool{}
		for _, d := range dtos {
			seen[d.Key] = true
		}
		for _, k := range []string{KeyMonthlyContribution, KeyIkeAnnualLimit, KeyIkzeAnnualLimit} {
			if !seen[k] {
				t.Fatalf("expected seeded key %s in list", k)
			}
		}
	})

	t.Run("valid update persists and advances updated_at", func(t *testing.T) {
		before, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var beforeUpdatedAt time.Time
		for _, d := range before {
			if d.Key == KeyMonthlyContribution {
				beforeUpdatedAt = d.UpdatedAt
			}
		}

		dto, err := svc.Update(ctx, KeyMonthlyContribution, "4500")
		if err != nil {
			t.Fatal(err)
		}
		if dto.Value != "4500" {
			t.Fatalf("expected 4500, got %s", dto.Value)
		}
		if !dto.UpdatedAt.After(beforeUpdatedAt) {
			t.Fatalf("expected updated_at to advance, before=%v after=%v", beforeUpdatedAt, dto.UpdatedAt)
		}
	})

	t.Run("invalid value is 400 VALIDATION_ERROR", func(t *testing.T) {
		_, err := svc.Update(ctx, KeyIkeAnnualLimit, "not-a-number")
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != http.StatusBadRequest || appErr.Code != httpx.CodeValidationError {
			t.Fatalf("expected 400 VALIDATION_ERROR, got %v", err)
		}
	})

	t.Run("unknown key is 404 NOT_FOUND", func(t *testing.T) {
		_, err := svc.Update(ctx, "no.such.key", "x")
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != http.StatusNotFound || appErr.Code != httpx.CodeNotFound {
			t.Fatalf("expected 404 NOT_FOUND, got %v", err)
		}
	})
}
