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
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

// TestDeposit_RealPostgres exercises create/list/xirr against portfolioB's own migrated database —
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

	t.Run("create then list", func(t *testing.T) {
		comment := "integration test deposit"
		dto, err := svc.Create(ctx, siblingcore.Deposit{
			DepositDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Amount: decimal.RequireFromString("4000"), Comment: &comment,
		})
		if err != nil {
			t.Fatal(err)
		}
		if dto.ID == 0 {
			t.Fatal("expected a generated id")
		}
		items, total, err := svc.List(ctx, siblingcore.WholeBook, nil, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if total == 0 || len(items) == 0 {
			t.Fatal("expected at least one deposit")
		}
	})

	t.Run("xirr computes against the real seeded portfolio", func(t *testing.T) {
		result, err := svc.ComputeXirr(ctx, siblingcore.WholeBook)
		if err != nil {
			t.Fatal(err)
		}
		// V2/V3 seed real positions with real deposits already in this migration set, plus the
		// one we just inserted above — just assert this completes without error and produces a
		// non-negative totalInvestedPln (the money-critical invariant: XIRR never fabricates a
		// negative "invested" figure).
		if result.TotalInvestedPln.Sign() < 0 {
			t.Fatalf("expected non-negative totalInvestedPln, got %s", result.TotalInvestedPln)
		}
	})
}
