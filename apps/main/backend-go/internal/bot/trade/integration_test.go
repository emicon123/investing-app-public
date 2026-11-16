package trade

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/control"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// TestTrade_RealPostgresRoundTrip exercises the trade lifecycle against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in
// prod — the "never mock the database" gate. Covers: create/close round-trip with
// NUMERIC(18,8)/(18,2) precision, the kill-switch 409 (paused → refused, resumed →
// allowed), the single-statement scalp claim/finish atomicity with SYSTEM audit
// rows, and action/tick persistence.
func TestTrade_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE bot_trade, bot_trade_action, bot_trade_tick RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE bot_control SET trading_enabled = TRUE WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	transact := func(ctx context.Context, fn func(Queries) error) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if err := fn(queries.WithTx(tx)); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return tx.Commit(ctx)
	}
	gate := control.NewService(queries)
	svc := NewService(queries, transact, gate, nil, DefaultExecutionConfig())
	lev := 3

	t.Run("create persists exact numerics", func(t *testing.T) {
		created, err := svc.Create(ctx, CreateInput{
			Side:     "LONG",
			Quantity: decimal.RequireFromString("0.01000000"), QuantitySet: true,
			Leverage:   &lev,
			EntryPrice: decimal.RequireFromString("60000.00"), EntryPriceSet: true,
			Notes: strPtr("cluster audit"),
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		var qty, price string
		if err := pool.QueryRow(ctx, `SELECT quantity::text, entry_price::text FROM bot_trade WHERE id = $1`,
			created.Id).Scan(&qty, &price); err != nil {
			t.Fatal(err)
		}
		if qty != "0.01000000" || price != "60000.00" {
			t.Fatalf("precision did not survive: %s/%s", qty, price)
		}
	})

	t.Run("kill switch 409 then resume", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE bot_control SET trading_enabled = FALSE, paused_reason = 'it'`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Create(ctx, createInput("SHORT", "0.01", "60000", nil)); err == nil {
			t.Fatal("expected 409 when paused, got nil")
		}
		if _, err := pool.Exec(ctx, `UPDATE bot_control SET trading_enabled = TRUE, paused_reason = NULL`); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("close then duplicate guard", func(t *testing.T) {
		if _, err := svc.Close(ctx, 1, CloseInput{
			ExitPrice: decimal.RequireFromString("61000"), ExitPriceSet: true,
		}); err != nil {
			t.Fatalf("Close: %v", err)
		}
		var pnl string
		if err := pool.QueryRow(ctx, `SELECT pnl_pln::text FROM bot_trade WHERE id = 1`).Scan(&pnl); err != nil {
			t.Fatal(err)
		}
		// NUMERIC(18,4): computed (61000−60000)×0.01×3 = 30 stored as 30.0000.
		if pnl != "30.0000" {
			t.Fatalf("expected computed pnl 30.0000, got %s", pnl)
		}
		if _, err := svc.Create(ctx, createInput("LONG", "0.01", "60000", nil)); err == nil {
			t.Fatal("expected 409 DUPLICATE_TRADE_WINDOW, got nil")
		}
	})

	t.Run("placed audit arms pending then claim finish audit", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE bot_trade SET opened_at = NOW() - INTERVAL '10 minutes' WHERE status = 'CLOSED'`); err != nil {
			t.Fatal(err)
		}
		created, err := svc.Create(ctx, createInput("SHORT", "0.02", "59000", nil))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := svc.AttachPlacedAudit(ctx, created.Id, "algo-9", nil, nil); err != nil {
			t.Fatalf("AttachPlacedAudit: %v", err)
		}
		pending, err := svc.PendingScalp(ctx)
		if err != nil {
			t.Fatalf("PendingScalp: %v", err)
		}
		if len(pending) != 1 || pending[0].Id != created.Id {
			t.Fatalf("expected 1 pending row, got %+v", pending)
		}
		claimed, err := svc.Claim(ctx, created.Id)
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if claimed.ScalpSessionStatus == nil || *claimed.ScalpSessionStatus != "RUNNING" {
			t.Fatalf("expected RUNNING, got %+v", claimed.ScalpSessionStatus)
		}
		if _, err := svc.Claim(ctx, created.Id); err == nil {
			t.Fatal("expected 409 on double claim, got nil")
		}
		code := 124
		finished, err := svc.Finish(ctx, created.Id, &code)
		if err != nil {
			t.Fatalf("Finish: %v", err)
		}
		if finished.ScalpSessionStatus == nil || *finished.ScalpSessionStatus != "DONE" {
			t.Fatalf("expected DONE, got %+v", finished.ScalpSessionStatus)
		}
		var started, done int
		if err := pool.QueryRow(ctx, `SELECT
			COUNT(*) FILTER (WHERE action_type = 'SCALP_SESSION_STARTED' AND source = 'SYSTEM'),
			COUNT(*) FILTER (WHERE action_type = 'SCALP_SESSION_FINISHED' AND source = 'SYSTEM')
			FROM bot_trade_action WHERE trade_id = $1`, created.Id).Scan(&started, &done); err != nil {
			t.Fatal(err)
		}
		if started != 1 || done != 1 {
			t.Fatalf("expected 1 STARTED + 1 FINISHED SYSTEM rows, got %d/%d", started, done)
		}
		var reasoning string
		if err := pool.QueryRow(ctx, `SELECT reasoning FROM bot_trade_action
			WHERE trade_id = $1 AND action_type = 'SCALP_SESSION_FINISHED'`, created.Id).Scan(&reasoning); err != nil {
			t.Fatal(err)
		}
		if reasoning == "" || !containsSub(reasoning, "exitCode=124") {
			t.Fatalf("expected exit code in finish audit, got %q", reasoning)
		}
	})

	t.Run("actions and ticks persist", func(t *testing.T) {
		price := decimal.RequireFromString("58500")
		if _, err := svc.AddAction(ctx, 2, ActionInput{ActionType: "SET_SL", Price: &price}); err != nil {
			t.Fatalf("AddAction: %v", err)
		}
		actions, err := svc.ListActions(ctx, 2)
		if err != nil {
			t.Fatalf("ListActions: %v", err)
		}
		if len(actions) != 3 { // STARTED + FINISHED + SET_SL
			t.Fatalf("expected 3 action rows, got %d", len(actions))
		}
		rows, err := svc.AddTicks(ctx, 2, []TickInput{{Price: &price}, {}})
		if err != nil || len(rows) != 2 {
			t.Fatalf("expected 2 ticks, got %+v/%v", rows, err)
		}
		listed, err := svc.ListTicks(ctx, 2, nil, nil)
		if err != nil || len(listed) != 2 {
			t.Fatalf("expected 2 listed ticks, got %+v/%v", listed, err)
		}
	})
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// failingPlacedAudit makes the LAST statement of the placed-entry transaction fail, to prove the
// rows written before it are rolled back.
type failingPlacedAudit struct{ Queries }

func (failingPlacedAudit) AttachPlacedAudit(context.Context, sqlcgen.AttachPlacedAuditParams) error {
	return errInjected
}

// TestRecordEntry_RealPostgresAtomic runs RecordEntry through the production transaction runner
// (pgtx.Runner) against a real postgres:17.10-alpine migrated by Flyway (task 191 S1): the trade
// row, its algo id, the margin audit and the PENDING outbox flag appear together, and a failure on
// the last statement leaves no row at all.
func TestRecordEntry_RealPostgresAtomic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}
	pg := testsupport.StartMigratedPostgres(t, "../../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE bot_trade, bot_trade_action, bot_trade_tick RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	entry := FilledEntry{
		Side: "LONG", QuantityBtc: decimal.RequireFromString("0.00200000"),
		EntryPrice: decimal.RequireFromString("60000.00"), Leverage: 5,
		OkxOrderID: "ord-1", OkxAlgoID: "algo-1", Protection: ProtectedBySL,
		MarginPln: ptrDecimal("480.0000"), Confidence: ptrDecimal("0.6000"),
		Notes: "cluster audit line",
	}

	t.Run("placed entry commits as one unit", func(t *testing.T) {
		svc := NewService(queries, pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) }),
			nil, nil, DefaultExecutionConfig())
		id, err := svc.RecordEntry(ctx, entry)
		if err != nil {
			t.Fatalf("RecordEntry: %v", err)
		}
		var algo, status, outbox, margin, orderID string
		if err := pool.QueryRow(ctx, `SELECT okx_algo_id, status, scalp_session_status,
			margin_allocated_pln::text, okx_order_id FROM bot_trade WHERE id = $1`, id).
			Scan(&algo, &status, &outbox, &margin, &orderID); err != nil {
			t.Fatal(err)
		}
		if algo != "algo-1" || status != "OPEN" || outbox != "PENDING" || orderID != "ord-1" || margin != "480.0000" {
			t.Fatalf("unexpected row: algo=%q status=%q outbox=%q order=%q margin=%q",
				algo, status, outbox, orderID, margin)
		}
	})

	t.Run("failure on the last statement rolls the trade row back", func(t *testing.T) {
		before := countTrades(t, ctx, pool)
		svc := NewService(queries, pgtx.Runner(pool, func(tx pgx.Tx) Queries {
			return failingPlacedAudit{queries.WithTx(tx)}
		}), nil, nil, DefaultExecutionConfig())
		if _, err := svc.RecordEntry(ctx, entry); err == nil {
			t.Fatal("expected the injected failure")
		}
		if after := countTrades(t, ctx, pool); after != before {
			t.Fatalf("expected a rollback, rows before=%d after=%d", before, after)
		}
	})

	t.Run("emergency close commits CLOSED with the marker and NONE outbox", func(t *testing.T) {
		svc := NewService(queries, pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) }),
			nil, nil, DefaultExecutionConfig())
		e := entry
		e.Protection, e.OkxAlgoID = EmergencyClosed, ""
		e.Notes = withMarker(entry.Notes, "SL_PLACE_FAILED_EMERGENCY_CLOSED orderId=ord-1")
		id, err := svc.RecordEntry(ctx, e)
		if err != nil {
			t.Fatalf("RecordEntry: %v", err)
		}
		var status, outbox, notes, pnl string
		if err := pool.QueryRow(ctx, `SELECT status, scalp_session_status, notes, pnl_pln::text
			FROM bot_trade WHERE id = $1`, id).Scan(&status, &outbox, &notes, &pnl); err != nil {
			t.Fatal(err)
		}
		if status != "CLOSED" || outbox != "NONE" || !containsSub(notes, "SL_PLACE_FAILED_EMERGENCY_CLOSED") ||
			!containsSub(notes, "cluster audit line") {
			t.Fatalf("unexpected row: status=%q outbox=%q notes=%q pnl=%q", status, outbox, notes, pnl)
		}
	})
}

func countTrades(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM bot_trade`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func ptrDecimal(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}
