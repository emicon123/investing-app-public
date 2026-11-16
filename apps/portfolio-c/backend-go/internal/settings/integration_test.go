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

// TestSettings_RealPostgres exercises the read/list/update path against portfolioC's own migrated
// database — never mock the database. Covers: listing the V2 seed row, updating the 1
// registered key (valid + invalid), the 404 on an unknown key, and the updated_at trigger
// firing on write.
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

	t.Run("list returns the V2 seed row", func(t *testing.T) {
		dtos, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(dtos) != 1 {
			t.Fatalf("expected 1 seeded setting, got %d: %+v", len(dtos), dtos)
		}
		if dtos[0].Key != KeyFixedMonthlyDepositDefault || dtos[0].Value != "400" {
			t.Fatalf("expected fixed_monthly_deposit_default_pln=400, got %+v", dtos[0])
		}
	})

	t.Run("valid update persists and advances updated_at", func(t *testing.T) {
		before, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		beforeUpdatedAt := before[0].UpdatedAt

		dto, err := svc.Update(ctx, KeyFixedMonthlyDepositDefault, "450")
		if err != nil {
			t.Fatal(err)
		}
		if dto.Value != "450" {
			t.Fatalf("expected 450, got %s", dto.Value)
		}
		if !dto.UpdatedAt.After(beforeUpdatedAt) {
			t.Fatalf("expected updated_at to advance, before=%v after=%v", beforeUpdatedAt, dto.UpdatedAt)
		}
	})

	t.Run("invalid value is 400 VALIDATION_ERROR", func(t *testing.T) {
		_, err := svc.Update(ctx, KeyFixedMonthlyDepositDefault, "not-a-number")
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
