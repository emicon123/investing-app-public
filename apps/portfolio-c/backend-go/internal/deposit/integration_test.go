package deposit

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/siblingcore"
	scdeposit "investing-app/common-go/siblingcore/deposit"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestDeposit_RealPostgres exercises create/list/xirr against portfolioC's own migrated database —
// never mock the database.
func TestDeposit_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	q := store.New(pool)
	svc := scdeposit.NewService(book.Config, q, q, nil)

	t.Run("create then list scoped to portfolio", func(t *testing.T) {
		comment := "integration test deposit"
		dto, err := svc.Create(ctx, siblingcore.Deposit{
			Portfolio: "FIXED", DepositDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Amount: decimal.RequireFromString("400"), Comment: &comment,
		})
		if err != nil {
			t.Fatal(err)
		}
		if dto.ID == 0 {
			t.Fatal("expected a generated id")
		}
		items, total, err := svc.List(ctx, "FIXED", nil, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if total == 0 || len(items) == 0 {
			t.Fatal("expected at least one FIXED deposit")
		}
		for _, item := range items {
			if item.Portfolio != "FIXED" {
				t.Fatalf("expected only FIXED deposits, got %s", item.Portfolio)
			}
		}
	})

	t.Run("xirr computes independently per portfolio", func(t *testing.T) {
		fixedResult, err := svc.ComputeXirr(ctx, "FIXED")
		if err != nil {
			t.Fatal(err)
		}
		variableResult, err := svc.ComputeXirr(ctx, "VARIABLE")
		if err != nil {
			t.Fatal(err)
		}
		if fixedResult.TotalInvestedPln.Sign() < 0 || variableResult.TotalInvestedPln.Sign() < 0 {
			t.Fatal("expected non-negative totalInvestedPln for both portfolios")
		}
	})
}
