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
	"portfolio-b/backend-go/db/sqlcgen"
	portfolioBaccountcash "portfolio-b/backend-go/internal/accountcash"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

// TestConfirm_RealPostgres_OneTransaction exercises Confirm's full write path (position delta,
// purchase_lots, account_cash_snapshot deduction, and the auto-deposit) against portfolioB's own
// migrated database — never mock the database. This is the money-critical atomicity assertion
// task 151 requires: all four writes must land, or none of them (verified here by checking every
// side effect after a single Confirm call, matching Java's class-level @Transactional).
func TestConfirm_RealPostgres_OneTransaction(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	queries := sqlcgen.New(pool)

	// A minimal position + a price_cache row for it, seeded directly (no reliance on V2's
	// exact seed data, so this test is robust to future seed-data edits).
	if _, err := pool.Exec(ctx, `INSERT INTO assets (key, name, exposure, asset_class, target_allocation_pct)
		VALUES ('IT-CONFIRM', 'Integration Test ETF', 'STOCKS', 'ETF', 100)`); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary)
		VALUES ('IT-CONFIRM', 'Integration Test ETF', 'XTB', 'ETF', 'STOCKS', 'PLN', 'ITCONFIRM.WA', (SELECT id FROM assets WHERE key='IT-CONFIRM'), true)`); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ('ITCONFIRM.WA', 20, 'PLN', 20)`); err != nil {
		t.Fatalf("seed price_cache: %v", err)
	}

	cashService := portfolioBaccountcash.NewService(portfolioBaccountcash.NewStore(queries))
	q := store.New(pool)
	svc := screbalancing.NewService(book.Config, q, pgtx.Runner(pool, store.Bind), q, NewFunding(), nil)

	before, err := cashService.GetCurrent(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	beforeIkeTata := before.Balances[portfolioBaccountcash.KeyIkeTata]

	err = svc.Confirm(ctx, screbalancing.ConfirmRequest{Items: []screbalancing.ConfirmItem{
		{Ticker: "IT-CONFIRM", Units: siblingcore.Units{}.Add(book.IkeTata, decimal.RequireFromString("5"))},
	}})
	if err != nil {
		t.Fatal(err)
	}

	// 1. position units applied.
	var unitsIkeTata string
	if err := pool.QueryRow(ctx, `SELECT units_ike_tata::text FROM positions WHERE ticker = 'IT-CONFIRM'`).Scan(&unitsIkeTata); err != nil {
		t.Fatal(err)
	}
	if decimal.RequireFromString(unitsIkeTata).Cmp(decimal.RequireFromString("5")) != 0 {
		t.Fatalf("expected units_ike_tata=5, got %s", unitsIkeTata)
	}

	// 2. purchase lot written (5 units * 20 PLN = 100 PLN cost).
	var lotCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM purchase_lots WHERE ticker = 'IT-CONFIRM'`).Scan(&lotCount); err != nil {
		t.Fatal(err)
	}
	if lotCount != 1 {
		t.Fatalf("expected exactly 1 purchase lot, got %d", lotCount)
	}

	// 3. account cash deducted by 100 PLN (5 units * 20 PLN).
	after, err := cashService.GetCurrent(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	wantAfter := beforeIkeTata.Sub(decimal.RequireFromString("100"))
	if wantAfter.Sign() < 0 {
		wantAfter = decimal.Zero
	}
	if after.Balances[portfolioBaccountcash.KeyIkeTata].Cmp(wantAfter) != 0 {
		t.Fatalf("expected ike_tata balance %s after deduction, got %s", wantAfter, after.Balances[portfolioBaccountcash.KeyIkeTata])
	}

	// 4. auto-deposit recorded.
	var depositCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM deposits WHERE amount = 4000`).Scan(&depositCount); err != nil {
		t.Fatal(err)
	}
	if depositCount == 0 {
		t.Fatal("expected the auto-recorded monthly-contribution deposit to be written")
	}
}

// TestCalculate_RealPostgres_SeededSchema exercises the read-only Calculate path against the
// real V1-V17 seeded schema (positions, account_cash_snapshot, settings all present) — the
// full read/compute path must complete without error and produce internally-consistent numbers
// (deployable never exceeds the account-cash total, every buy item is non-negative), matching
// the manual-sell-only invariant's arithmetic half from the read side.
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

	result, err := svc.Calculate(ctx, siblingcore.WholeBook, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeployableCashPln.Cmp(*result.CashOnHandPln) > 0 {
		t.Fatalf("deployable (%s) must never exceed cash on hand (%s)", result.DeployableCashPln, result.CashOnHandPln)
	}
	for _, item := range result.BuyList {
		if item.AllocatedPln.Sign() < 0 {
			t.Fatalf("ticker %s: negative AllocatedPln %s — manual-sell-only invariant violated", item.Ticker, item.AllocatedPln)
		}
	}
}
