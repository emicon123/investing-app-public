package accountcash

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/accountcash"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/db/sqlcgen"
)

// TestAccountCash_RealPostgres exercises the per-portfolio read/update/deduct path
// against portfolioC's own migrated database — never mock the database. Covers:
// getCurrent per portfolio, updateManual overwrite + MANUAL stamp, deduct
// floor-zero + POST_PURCHASE stamp, and the 404 on a missing portfolio row.
func TestAccountCash_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `UPDATE account_cash_snapshot SET balance_pln = 1500 WHERE portfolio = 'VARIABLE'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE account_cash_snapshot SET balance_pln = 400 WHERE portfolio = 'FIXED'`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	svc := NewService(NewStore(queries))

	t.Run("getCurrent reads one portfolio's row", func(t *testing.T) {
		snap, err := svc.GetCurrent(ctx, "VARIABLE")
		if err != nil {
			t.Fatal(err)
		}
		if got := snap.Balances[BalancePln].StringFixed(2); got != "1500.00" {
			t.Fatalf("expected 1500.00, got %s", got)
		}
	})

	t.Run("updateManual overwrites and stamps MANUAL", func(t *testing.T) {
		snap, err := svc.UpdateManual(ctx, "FIXED", map[string]decimal.Decimal{
			BalancePln: decimal.RequireFromString("450"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if snap.UpdateReason != accountcash.ReasonManual || snap.Balances[BalancePln].String() != "450" {
			t.Fatalf("unexpected snapshot: %+v", snap)
		}
		// The other portfolio is untouched.
		other, err := svc.GetCurrent(ctx, "VARIABLE")
		if err != nil {
			t.Fatal(err)
		}
		if other.Balances[BalancePln].String() != "1500" {
			t.Fatalf("VARIABLE must be untouched, got %+v", other.Balances)
		}
	})

	t.Run("deduct floors at zero and stamps POST_PURCHASE", func(t *testing.T) {
		snap, err := svc.Deduct(ctx, "VARIABLE", map[string]decimal.Decimal{
			BalancePln: decimal.RequireFromString("2000"), // over-deduct → 0
		})
		if err != nil {
			t.Fatal(err)
		}
		if snap.UpdateReason != accountcash.ReasonPostPurchase {
			t.Fatalf("expected POST_PURCHASE, got %s", snap.UpdateReason)
		}
		if !snap.Balances[BalancePln].IsZero() {
			t.Fatalf("expected floor 0, got %s", snap.Balances[BalancePln])
		}
	})

	t.Run("missing portfolio row is 404", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM account_cash_snapshot WHERE portfolio = 'FIXED'`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.GetCurrent(ctx, "FIXED"); err == nil {
			t.Fatal("expected 404")
		}
	})
}
