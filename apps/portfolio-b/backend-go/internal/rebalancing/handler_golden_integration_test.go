package rebalancing

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/pgtx"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"investing-app/common-go/testsupport"
	"investing-app/common-go/testsupport/golden"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

// TestRebalancingHandler_Golden pins the wire bytes of calculate / confirm / history and the
// database state confirm leaves behind (task 180 wave 0): handler built as cmd/server/main.go
// builds it, driven with httptest, compared with testdata/golden/rebalancing_<case>.json.
// Re-record with -update.
func TestRebalancingHandler_Golden(t *testing.T) {
	pool := goldenPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, purchase_lots, deposits CASCADE;
		UPDATE account_cash_snapshot SET ike_tata = 1000, ike_mama = 500, ikze_tata = 300, ikze_mama = 200;
		UPDATE settings SET value = '1500' WHERE key = 'monthly_contribution_pln';
		INSERT INTO assets (key, name, exposure, asset_class, target_allocation_pct) VALUES
			('RA', 'Asset A', 'STOCKS', 'ETF', 60), ('RB', 'Asset B', 'METALS', 'ETF', 40);
		INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary, units_ike_tata, units_ikze_tata, units_inne, total_spent_pln) VALUES
			('A1', 'A primary', 'XTB', 'ETF', 'STOCKS', 'PLN', 'A1.WA', (SELECT id FROM assets WHERE key = 'RA'), true, 5, 0, 0, 450),
			('A2', 'A sibling', 'XTB', 'ETF', 'STOCKS', 'PLN', 'A2.WA', (SELECT id FROM assets WHERE key = 'RA'), false, 0, 0, 1, NULL),
			('B1', 'B primary', 'XTB', 'ETF', 'METALS', 'PLN', 'B1.WA', (SELECT id FROM assets WHERE key = 'RB'), true, 0, 10, 0, 180);
		INSERT INTO price_cache (ticker, price, currency, price_pln, fetched_at) VALUES
			('A1.WA', 100, 'PLN', 100, '2026-11-01T10:00:00Z'), ('A2.WA', 40, 'PLN', 40, '2026-11-01T11:00:00Z'), ('B1.WA', 20, 'PLN', 20, '2026-11-01T09:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	h := goldenRebalancingHandler(pool)

	st, b := golden.Call(h.Calculate, http.MethodPost, "/", "")
	golden.Response(t, "rebalancing_calculate", st, b)

	st, b = golden.Call(h.Confirm, http.MethodPost, "/", `{"items":[
		{"ticker":"A1","accountAllocations":[{"accountType":"IKE_TATA","units":3},{"accountType":"INNE","units":1},{"accountType":"IKZE_MAMA","units":0}]},
		{"ticker":"B1","accountAllocations":[{"accountType":"IKE_MAMA","units":5.5},{"accountType":"IKZE_TATA","units":2}]}]}`)
	golden.Response(t, "rebalancing_confirm", st, b)
	golden.Value(t, "rebalancing_confirm_db", rebalancingDBState(t, pool))

	st, b = golden.Call(h.Confirm, http.MethodPost, "/", `{"items":[]}`)
	golden.Response(t, "rebalancing_confirm_empty_400", st, b)
	st, b = golden.Call(h.Confirm, http.MethodPost, "/", `{"items":[{"ticker":"NOPE","accountAllocations":[{"accountType":"IKE_TATA","units":1}]}]}`)
	golden.Response(t, "rebalancing_confirm_unknown_404", st, b)
	// D1 (task 180): an unknown account type is a 400 before the transaction opens.
	st, b = golden.Call(h.Confirm, http.MethodPost, "/", `{"items":[{"ticker":"A1","accountAllocations":[{"accountType":"IKE_BABCIA","units":1}]}]}`)
	golden.Response(t, "rebalancing_confirm_unknown_account_type_400", st, b)
	golden.Value(t, "rebalancing_after_rejections_db", rebalancingDBState(t, pool))

	st, b = golden.Call(h.History, http.MethodGet, "/", "")
	golden.Response(t, "rebalancing_history", st, b)
}

type rebalancingState struct {
	Positions []struct {
		Ticker, IkeTata, IkeMama, IkzeTata, IkzeMama, Inne string
		TotalSpentPln                                      *string
	}
	Lots     []struct{ Ticker, Units, PricePln, CostPln string }
	Deposits []struct {
		Amount  string
		Comment *string
	}
	Cash []string
}

func rebalancingDBState(t *testing.T, pool *pgxpool.Pool) rebalancingState {
	t.Helper()
	ctx := context.Background()
	var s rebalancingState
	rows, err := pool.Query(ctx, `SELECT ticker, units_ike_tata::text, units_ike_mama::text, units_ikze_tata::text, units_ikze_mama::text, units_inne::text, total_spent_pln::text FROM positions ORDER BY ticker`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r struct {
			Ticker, IkeTata, IkeMama, IkzeTata, IkzeMama, Inne string
			TotalSpentPln                                      *string
		}
		if err := rows.Scan(&r.Ticker, &r.IkeTata, &r.IkeMama, &r.IkzeTata, &r.IkzeMama, &r.Inne, &r.TotalSpentPln); err != nil {
			t.Fatal(err)
		}
		s.Positions = append(s.Positions, r)
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT ticker, units::text, price_pln::text, cost_pln::text FROM purchase_lots ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r struct{ Ticker, Units, PricePln, CostPln string }
		if err := rows.Scan(&r.Ticker, &r.Units, &r.PricePln, &r.CostPln); err != nil {
			t.Fatal(err)
		}
		s.Lots = append(s.Lots, r)
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT amount::text, comment FROM deposits ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r struct {
			Amount  string
			Comment *string
		}
		if err := rows.Scan(&r.Amount, &r.Comment); err != nil {
			t.Fatal(err)
		}
		s.Deposits = append(s.Deposits, r)
	}
	rows.Close()
	var a, b2, c, d string
	if err := pool.QueryRow(ctx, `SELECT ike_tata::text, ike_mama::text, ikze_tata::text, ikze_mama::text FROM account_cash_snapshot`).Scan(&a, &b2, &c, &d); err != nil {
		t.Fatal(err)
	}
	s.Cash = []string{a, b2, c, d}
	return s
}

// goldenRebalancingHandler wires the handler exactly as cmd/server/main.go does.
func goldenRebalancingHandler(pool *pgxpool.Pool) *Handler {
	q := store.New(pool)
	money := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, store.Bind)
	return NewHandler(screbalancing.NewService(book.Config, q, money, q, NewFunding(), nil))
}

func goldenPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
