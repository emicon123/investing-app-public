package accountcash

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/accountcash"
	"investing-app/common-go/testsupport"
	"portfolio-b/backend-go/db/sqlcgen"
)

// TestAccountCash_RealPostgres exercises the singleton-row read/update/deduct path
// against portfolioB's own migrated database — never mock the database. Covers:
// getCurrent on the V10 seed row, updateManual overwrite + MANUAL stamp, deduct
// floor-zero + POST_PURCHASE stamp, and the 404 on a missing row.
func TestAccountCash_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	queries := sqlcgen.New(pool)
	svc := NewService(NewStore(queries))

	t.Run("getCurrent reads the seed row", func(t *testing.T) {
		snap, err := svc.GetCurrent(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		// V2__default_reference_data seeds the singleton row with zero balances.
		if got := snap.Balances[KeyIkeTata].StringFixed(2); got != "0.00" {
			t.Fatalf("expected the zero-balance seed row, got %s", got)
		}
	})

	t.Run("updateManual overwrites and stamps MANUAL", func(t *testing.T) {
		snap, err := svc.UpdateManual(ctx, "", map[string]decimal.Decimal{
			KeyIkeTata: decimal.RequireFromString("100"), KeyIkeMama: decimal.RequireFromString("200"),
			KeyIkzeTata: decimal.RequireFromString("300"), KeyIkzeMama: decimal.RequireFromString("400"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if snap.UpdateReason != accountcash.ReasonManual {
			t.Fatalf("expected MANUAL, got %s", snap.UpdateReason)
		}
		if snap.Balances[KeyIkzeMama].String() != "400" {
			t.Fatalf("expected 400, got %s", snap.Balances[KeyIkzeMama])
		}
	})

	t.Run("deduct floors at zero and stamps POST_PURCHASE", func(t *testing.T) {
		snap, err := svc.Deduct(ctx, "", map[string]decimal.Decimal{
			KeyIkeTata: decimal.RequireFromString("150"), // over-deduct → 0
			KeyIkeMama: decimal.RequireFromString("50"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if snap.UpdateReason != accountcash.ReasonPostPurchase {
			t.Fatalf("expected POST_PURCHASE, got %s", snap.UpdateReason)
		}
		if !snap.Balances[KeyIkeTata].IsZero() || snap.Balances[KeyIkeMama].String() != "150" {
			t.Fatalf("unexpected balances: %+v", snap.Balances)
		}
	})

	t.Run("missing row is 404", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM account_cash_snapshot WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
		_, err := svc.GetCurrent(ctx, "")
		if err == nil {
			t.Fatal("expected 404")
		}
	})
}
