package rebalancing

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgtx"
	"investing-app/common-go/siblingcore"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestConfirm_RealPostgres_OneTransaction exercises Confirm's full write path (position delta,
// purchase_lots, and the request-scoped deposit) against portfolioC's own migrated database — never
// mock the database.
func TestConfirm_RealPostgres_OneTransaction(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets CASCADE`); err != nil {
		t.Fatalf("truncate seeded positions: %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct)
		VALUES ('FIXED', 'IT-CONFIRM', 'Integration Test ETF', 'STOCKS', 'ETF', 100)`); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary)
		VALUES ('FIXED', 'IT-CONFIRM', 'Integration Test ETF', 'XTB', 'ETF', 'STOCKS', 'PLN', 'ITCONFIRM.WA', (SELECT id FROM assets WHERE key='IT-CONFIRM' AND portfolio='FIXED'), true)`); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ('ITCONFIRM.WA', 20, 'PLN', 20)`); err != nil {
		t.Fatalf("seed price_cache: %v", err)
	}

	q := store.New(pool)
	svc := screbalancing.NewService(book.Config, q, pgtx.Runner(pool, store.Bind), q, NewFunding(), nil)

	deposit := decimal.RequireFromString("400")
	err = svc.Confirm(ctx, screbalancing.ConfirmRequest{Portfolio: book.Fixed, DepositAmountPln: &deposit, Items: []screbalancing.ConfirmItem{
		{Ticker: "IT-CONFIRM", Units: siblingcore.Units{}.Add(siblingcore.SingleBucket, decimal.RequireFromString("5"))},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var units string
	if err := pool.QueryRow(ctx, `SELECT units::text FROM positions WHERE ticker = 'IT-CONFIRM' AND portfolio = 'FIXED'`).Scan(&units); err != nil {
		t.Fatal(err)
	}
	if decimal.RequireFromString(units).Cmp(decimal.RequireFromString("5")) != 0 {
		t.Fatalf("expected units=5, got %s", units)
	}

	var lotCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM purchase_lots WHERE ticker = 'IT-CONFIRM' AND portfolio = 'FIXED'`).Scan(&lotCount); err != nil {
		t.Fatal(err)
	}
	if lotCount != 1 {
		t.Fatalf("expected exactly 1 purchase lot, got %d", lotCount)
	}

	var depositCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM deposits WHERE portfolio = 'FIXED' AND amount = 400`).Scan(&depositCount); err != nil {
		t.Fatal(err)
	}
	if depositCount == 0 {
		t.Fatal("expected the request-scoped 400 PLN deposit to be recorded")
	}
}

func TestCalculate_RealPostgres_SeededSchema(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	q := store.New(pool)
	svc := screbalancing.NewService(book.Config, q, pgtx.Runner(pool, store.Bind), q, NewFunding(), nil)

	result, err := svc.Calculate(ctx, "FIXED", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.BuyList {
		if item.AllocatedPln.Sign() < 0 {
			t.Fatalf("ticker %s: negative AllocatedPln %s — manual-sell-only invariant violated", item.Ticker, item.AllocatedPln)
		}
	}
}
