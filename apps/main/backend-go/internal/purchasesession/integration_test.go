package purchasesession

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"

	"github.com/jackc/pgx/v5"
)

// realPrices is a fixed-price PriceSource backing the integration tests below — real Postgres
// for everything else ("never mock the database"), but prices/FX rates never touch the network
// in tests, matching the same split every prior phase's integration tests use for external HTTP
// dependencies.
type realPrices struct {
	prices map[string]decimal.Decimal
	rates  map[string]decimal.Decimal
}

func (p realPrices) PricePlnByYahooTicker(_ context.Context) (map[string]decimal.Decimal, error) {
	return p.prices, nil
}
func (p realPrices) AllRatesToPln(_ context.Context) (map[string]decimal.Decimal, error) {
	return p.rates, nil
}
func (p realPrices) RateToPln(_ context.Context, currency string) (decimal.Decimal, error) {
	if v, ok := p.rates[currency]; ok {
		return v, nil
	}
	return decimal.NewFromInt(1), nil
}

func newTransactFunc(pool *pgxpool.Pool, queries *sqlcgen.Queries) TransactFunc {
	return pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
}

// TestPurchaseSession_RealPostgresRoundTrip exercises the full write-ownership cluster
// (purchase_sessions/purchase_session_items/positions/deposits/asset_purchases/binance_transfers)
// against a real postgres:17.10-alpine container, migrated by the same Flyway CLI image used in
// prod — the "never mock the database" gate.
func TestPurchaseSession_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	// Clean slate — V6/V9/etc.'s real seed data would otherwise interfere with this test's
	// tightly-controlled fixture (specific tickers, specific targets summing to exactly 100%).
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, asset_purchases, purchase_sessions, purchase_session_items, binance_transfers, account_cap_adjustment CASCADE`); err != nil {
		t.Fatal(err)
	}
	// V1 seeds retirement.monthly_contribution_pln=1000 — zero it out so this test's own
	// cashOverridePln isn't silently netted to zero by the real seeded value (MAIN's
	// cashOnHandPln = grossCashPln - retirementContribution).
	mustZeroRetirementContribution(ctx, t, pool)

	queries := sqlcgen.New(pool)
	prices := realPrices{
		prices: map[string]decimal.Decimal{"ETF1.YAHOO": decimal.NewFromInt(100)},
		rates:  map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1), "EUR": decimal.RequireFromString("4.3"), "USD": decimal.RequireFromString("4.0")},
	}
	svc := NewService(queries, newTransactFunc(pool, queries), newTransactFunc(pool, queries), prices)

	yahoo := "ETF1.YAHOO"
	posID := mustInsertPosition(ctx, t, pool, "ETF1", "MAIN", "XTB", "PLN", decimal.NewFromInt(100), &yahoo)

	var sessionID int64

	t.Run("CreateSession produces a PLANNED session with one item filling the whole MAIN budget", func(t *testing.T) {
		detail, err := svc.CreateSession(ctx, CreateSessionInput{
			PortfoliosIncluded: PortfoliosIncludedMAIN, CashOverridePln: decPtrInt(1050),
		})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if detail.Status != StatusPLANNED {
			t.Fatalf("expected PLANNED, got %s", detail.Status)
		}
		if len(detail.Items) != 1 {
			t.Fatalf("expected exactly 1 item (single 100%%-target position), got %+v", detail.Items)
		}
		sessionID = detail.ID
	})

	t.Run("a second CreateSession is rejected with 409 while one is PLANNED", func(t *testing.T) {
		_, err := svc.CreateSession(ctx, CreateSessionInput{PortfoliosIncluded: PortfoliosIncludedMAIN})
		var appErr *httpx.AppError
		ok := errors.As(err, &appErr)
		if !ok || appErr.Status != 409 {
			t.Fatalf("expected 409 PLANNED_SESSION_EXISTS, got %v", err)
		}
	})

	t.Run("GetActiveSession returns the same session", func(t *testing.T) {
		detail, err := svc.GetActiveSession(ctx)
		if err != nil {
			t.Fatalf("GetActiveSession: %v", err)
		}
		if detail.ID != sessionID {
			t.Fatalf("expected session %d, got %d", sessionID, detail.ID)
		}
	})

	t.Run("CompleteSession applies the position delta, writes an asset_purchase, and auto-creates a deposit", func(t *testing.T) {
		result, err := svc.CompleteSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("CompleteSession: %v", err)
		}
		if result.Status != StatusDONE || result.PurchasesWritten != 1 {
			t.Fatalf("unexpected result: %+v", result)
		}

		row := pool.QueryRow(ctx, `SELECT units_regular_pln FROM positions WHERE id = $1`, posID)
		var raw string
		if err := row.Scan(&raw); err != nil {
			t.Fatalf("read updated position: %v", err)
		}
		if raw != "10.00000000" {
			t.Fatalf("expected units_regular_pln=10 (0+10 units bought), got %s", raw)
		}

		var purchaseCount int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_purchases WHERE session_id = $1`, sessionID).Scan(&purchaseCount); err != nil {
			t.Fatal(err)
		}
		if purchaseCount != 1 {
			t.Fatalf("expected 1 asset_purchases row, got %d", purchaseCount)
		}

		var depositCount int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM deposits WHERE comment LIKE $1`, "Auto from session%").Scan(&depositCount); err != nil {
			t.Fatal(err)
		}
		if depositCount != 1 {
			t.Fatalf("expected 1 auto-created deposit, got %d", depositCount)
		}

		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM purchase_sessions WHERE id = $1`, sessionID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "DONE" {
			t.Fatalf("expected session status DONE in the DB, got %s", status)
		}
	})

	t.Run("completing an already-DONE session is rejected with 409, no double-apply", func(t *testing.T) {
		_, err := svc.CompleteSession(ctx, sessionID)
		var appErr *httpx.AppError
		ok := errors.As(err, &appErr)
		if !ok || appErr.Status != 409 {
			t.Fatalf("expected 409 SESSION_ALREADY_DONE, got %v", err)
		}
		var raw string
		if err := pool.QueryRow(ctx, `SELECT units_regular_pln FROM positions WHERE id = $1`, posID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if raw != "10.00000000" {
			t.Fatalf("expected units unchanged at 10 (no double-apply), got %s", raw)
		}
	})
}

// TestCompleteSession_ConcurrentDoubleSubmit_ExactlyOneSucceeds is the MANDATORY concurrency
// test the phase task and architect-output §4 both demand: "the single highest-risk line in the
// entire phase to get wrong." Fires N concurrent CompleteSession calls against the SAME session
// id and asserts exactly one succeeds and the position delta is applied EXACTLY once — proving
// GetSessionForUpdate's SQL FOR UPDATE lock (the literal first statement inside the transaction)
// actually serializes concurrent completions against a real Postgres, the same class of test as
// BotScalpClaimConcurrencyIT/BotHardCloseClaimConcurrencyIT in the Java suite.
func TestCompleteSession_ConcurrentDoubleSubmit_ExactlyOneSucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, asset_purchases, purchase_sessions, purchase_session_items, binance_transfers, account_cap_adjustment CASCADE`); err != nil {
		t.Fatal(err)
	}
	mustZeroRetirementContribution(ctx, t, pool)

	queries := sqlcgen.New(pool)
	prices := realPrices{
		prices: map[string]decimal.Decimal{"ETF1.YAHOO": decimal.NewFromInt(100)},
		rates:  map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)},
	}
	svc := NewService(queries, newTransactFunc(pool, queries), newTransactFunc(pool, queries), prices)

	yahoo := "ETF1.YAHOO"
	posID := mustInsertPosition(ctx, t, pool, "ETF1", "MAIN", "XTB", "PLN", decimal.NewFromInt(100), &yahoo)

	detail, err := svc.CreateSession(ctx, CreateSessionInput{PortfoliosIncluded: PortfoliosIncludedMAIN, CashOverridePln: decPtrInt(1050)})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionID := detail.ID

	const concurrency = 12
	var (
		wg          sync.WaitGroup
		successes   atomic.Int64
		alreadyDone atomic.Int64
		otherErrs   atomic.Int64
	)
	start := make(chan struct{})
	for range concurrency {
		wg.Go(func() {
			<-start
			_, err := svc.CompleteSession(ctx, sessionID)
			switch {
			case err == nil:
				successes.Add(1)
			case isSessionAlreadyDone(err):
				alreadyDone.Add(1)
			default:
				otherErrs.Add(1)
				t.Logf("unexpected CompleteSession error: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()

	if otherErrs.Load() != 0 {
		t.Fatalf("expected only success or SESSION_ALREADY_DONE, got %d other errors", otherErrs.Load())
	}
	if successes.Load() != 1 {
		t.Fatalf("expected EXACTLY ONE successful CompleteSession out of %d concurrent calls, got %d successes and %d already-done",
			concurrency, successes.Load(), alreadyDone.Load())
	}
	if alreadyDone.Load() != concurrency-1 {
		t.Fatalf("expected the other %d calls to see SESSION_ALREADY_DONE, got %d", concurrency-1, alreadyDone.Load())
	}

	// The position delta must have been applied EXACTLY ONCE — 10 units bought (1000 PLN / 100
	// PLN price), never 2x, 3x, or any multiple of that.
	var raw string
	if err := pool.QueryRow(ctx, `SELECT units_regular_pln FROM positions WHERE id = $1`, posID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "10.00000000" {
		t.Fatalf("expected units_regular_pln=10 (delta applied exactly once), got %s — a double-apply would show 20, 30, etc.", raw)
	}

	var purchaseCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_purchases WHERE session_id = $1`, sessionID).Scan(&purchaseCount); err != nil {
		t.Fatal(err)
	}
	if purchaseCount != 1 {
		t.Fatalf("expected exactly 1 asset_purchases row (not %d) — proves the write-ownership cluster committed exactly once", purchaseCount)
	}

	var depositCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM deposits`).Scan(&depositCount); err != nil {
		t.Fatal(err)
	}
	if depositCount != 1 {
		t.Fatalf("expected exactly 1 auto-created deposit, got %d", depositCount)
	}
}

func isSessionAlreadyDone(err error) bool {
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	return ok && appErr.Code == httpx.CodeSessionAlreadyDone
}

// TestCompleteSession_MidTransactionFailure_RollsBackAllWrites is the transactional-atomicity
// test the phase task demands ("a failed write to one table rolls back the others in the same
// request, matching Java's @Transactional on completeSession"). Two items: a valid
// migration-sell processed FIRST (ListSessionItemsOrderedForCompletion's is_migration_sell DESC
// ordering) whose asset_purchase + position delta SUCCEED, then a buy whose price_pln_at_plan
// is SQL NaN — pgnum.ToDecimal rejects NaN mid-loop, AFTER the first item's writes already
// landed inside the same executionTx. The whole transaction must roll back: zero purchases,
// position untouched, zero deposits/transfers, session still PLANNED.
func TestCompleteSession_MidTransactionFailure_RollsBackAllWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, asset_purchases, purchase_sessions, purchase_session_items, binance_transfers, account_cap_adjustment CASCADE`); err != nil {
		t.Fatal(err)
	}
	mustZeroRetirementContribution(ctx, t, pool)

	queries := sqlcgen.New(pool)
	prices := realPrices{
		prices: map[string]decimal.Decimal{"ETF1.YAHOO": decimal.NewFromInt(100)},
		rates:  map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)},
	}
	svc := NewService(queries, newTransactFunc(pool, queries), newTransactFunc(pool, queries), prices)

	yahoo := "ETF1.YAHOO"
	posID := mustInsertPosition(ctx, t, pool, "ETF1", "MAIN", "XTB", "PLN", decimal.NewFromInt(100), &yahoo)
	// Second position for the NaN-price item — purchase_session_items carries a
	// (session, portfolio, ticker, account) uniqueness constraint, so item B needs its own
	// ticker. Fixture targets are irrelevant on the completion path under test.
	yahoo2 := "ETF2.YAHOO"
	mustInsertPosition(ctx, t, pool, "ETF2", "MAIN", "XTB", "PLN", decimal.NewFromInt(100), &yahoo2)

	var sessionID int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_sessions DEFAULT VALUES RETURNING id`).Scan(&sessionID); err != nil {
		t.Fatalf("insert PLANNED session: %v", err)
	}
	// Item A (migration sell, valid): sorts first, its purchase + delta succeed inside the tx.
	// planned_units stays nonneg per psi_planned_units_nonneg — the sell direction rides on
	// actual_units (negative allowed for migration sells, cf. UpdateSessionItems' guard).
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_session_items
		(session_id, portfolio, ticker, account_type, planned_units, planned_value_pln, actual_units, price_pln_at_plan, is_migration_sell, is_migration_buy)
		VALUES ($1, 'MAIN', 'ETF1', 'REGULAR', 3, 300, -3, 100, true, false)`, sessionID); err != nil {
		t.Fatalf("insert migration-sell item: %v", err)
	}
	// Item B (regular buy, NaN price, no value): sorts second; price decode fails mid-loop.
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_session_items
		(session_id, portfolio, ticker, account_type, planned_units, price_pln_at_plan, is_migration_sell, is_migration_buy)
		VALUES ($1, 'MAIN', 'ETF2', 'REGULAR', 5, 'NaN', false, false)`, sessionID); err != nil {
		t.Fatalf("insert NaN-price item: %v", err)
	}

	if _, err := svc.CompleteSession(ctx, sessionID); err == nil {
		t.Fatal("expected CompleteSession to fail on the NaN price, got nil error")
	}

	var purchaseCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_purchases WHERE session_id = $1`, sessionID).Scan(&purchaseCount); err != nil {
		t.Fatal(err)
	}
	if purchaseCount != 0 {
		t.Fatalf("expected item A's asset_purchase rolled back (0 rows), got %d", purchaseCount)
	}

	var raw string
	if err := pool.QueryRow(ctx, `SELECT units_regular_pln FROM positions WHERE id = $1`, posID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if got, err := decimal.NewFromString(raw); err != nil || !got.IsZero() {
		t.Fatalf("expected item A's position delta rolled back (units 0), got %s", raw)
	}

	var depositCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM deposits`).Scan(&depositCount); err != nil {
		t.Fatal(err)
	}
	if depositCount != 0 {
		t.Fatalf("expected zero deposits after rollback, got %d", depositCount)
	}

	var transferCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM binance_transfers`).Scan(&transferCount); err != nil {
		t.Fatal(err)
	}
	if transferCount != 0 {
		t.Fatalf("expected zero binance_transfers after rollback, got %d", transferCount)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM purchase_sessions WHERE id = $1`, sessionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "PLANNED" {
		t.Fatalf("expected session still PLANNED after rollback, got %s", status)
	}
}

func mustInsertPosition(ctx context.Context, t *testing.T, pool *pgxpool.Pool, ticker, portfolio, exchange, priceCurrency string, targetPct decimal.Decimal, yahooTicker *string) int64 {
	t.Helper()
	var id int64
	// Task 156: a position row is a subasset — every fixture is its own single-subasset asset
	// (key = ticker) carrying the target, with the subasset as its primary.
	err := pool.QueryRow(ctx, `
		WITH a AS (
			INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct)
			VALUES ($2::portfolio_enum, $1::text, $1::text, 'CASH', 'ETF', $5) RETURNING id)
		INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency,
			purchase_currency, units_ike, units_ikze, units_regular_eur,
			units_regular_usd, units_regular_pln, asset_id, is_primary, yahoo_ticker, company_backed)
		SELECT $1::text, $1::text, $2::portfolio_enum, $3::exchange_enum, 'ETF', 'CASH', $4::currency_enum, $4::currency_enum,
			0, 0, 0, 0, 0, a.id, true, $6, true FROM a
		RETURNING id`,
		ticker, portfolio, exchange, priceCurrency, pgnum.FromDecimal(targetPct), yahooTicker).Scan(&id)
	if err != nil {
		t.Fatalf("insert fixture position %s: %v", ticker, err)
	}
	return id
}

func decPtrInt(v int64) *decimal.Decimal {
	d := decimal.NewFromInt(v)
	return &d
}

// mustZeroRetirementContribution zeroes out the real Flyway-seeded budget/cap settings that
// would otherwise interact with this test's own fixture values: retirement.monthly_contribution_pln
// (would net MAIN's cashOverridePln down, since cashOnHandPln = grossCashPln - retirementContribution)
// and the IKE/IKZE annual limits (zeroing them forces every session item into the REGULAR
// account bucket, matching this test's simple single-position round-trip intent — the IKE/IKZE
// waterfall itself is covered by internal/purchasesession's own planning_test.go unit tests).
func mustZeroRetirementContribution(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE settings SET value = '0' WHERE key IN
		('retirement.monthly_contribution_pln', 'ike_annual_limit_pln', 'ikze_annual_limit_pln')`); err != nil {
		t.Fatalf("zero seeded budget/cap settings: %v", err)
	}
}
