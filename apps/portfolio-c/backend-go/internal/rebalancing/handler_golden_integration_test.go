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
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
	"portfolio-c/backend-go/openapi"
)

// TestRebalancingHandler_Golden pins the wire bytes of calculate / confirm / history and the
// database state confirm leaves behind (task 180 wave 0): handler built as cmd/server/main.go
// builds it, driven with httptest, compared with testdata/golden/rebalancing_<case>.json.
// Re-record with -update.
func TestRebalancingHandler_Golden(t *testing.T) {
	pool := goldenPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, purchase_lots, deposits CASCADE;
		UPDATE settings SET value = '350' WHERE key = 'fixed_monthly_deposit_default_pln';
		INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct) VALUES
			('VARIABLE', 'RA', 'Asset A', 'STOCKS', 'ETF', 60), ('VARIABLE', 'RB', 'Asset B', 'METALS', 'ETF', 40),
			('FIXED', 'RA', 'Asset A fixed', 'STOCKS', 'ETF', 100);
		INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary, units, total_spent_pln) VALUES
			('VARIABLE', 'A1', 'A primary', 'XTB', 'ETF', 'STOCKS', 'PLN', 'A1.WA', (SELECT id FROM assets WHERE key = 'RA' AND portfolio = 'VARIABLE'), true, 5, 450),
			('VARIABLE', 'A2', 'A sibling', 'XTB', 'ETF', 'STOCKS', 'PLN', 'A2.WA', (SELECT id FROM assets WHERE key = 'RA' AND portfolio = 'VARIABLE'), false, 1, NULL),
			('VARIABLE', 'B1', 'B primary', 'XTB', 'ETF', 'METALS', 'PLN', 'B1.WA', (SELECT id FROM assets WHERE key = 'RB' AND portfolio = 'VARIABLE'), true, 10, 180),
			('FIXED', 'A1', 'A primary fixed', 'XTB', 'ETF', 'STOCKS', 'PLN', 'A1.WA', (SELECT id FROM assets WHERE key = 'RA' AND portfolio = 'FIXED'), true, 2, 200);
		INSERT INTO price_cache (ticker, price, currency, price_pln, fetched_at) VALUES
			('A1.WA', 100, 'PLN', 100, '2026-11-01T10:00:00Z'), ('A2.WA', 40, 'PLN', 40, '2026-11-01T11:00:00Z'), ('B1.WA', 20, 'PLN', 20, '2026-11-01T09:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	h := goldenRebalancingHandler(pool)

	st, b := golden.Call(h.Calculate, http.MethodPost, "/", `{"portfolio":"VARIABLE","depositAmountPln":1500}`)
	golden.Response(t, "rebalancing_calculate", st, b)
	st, b = golden.Call(h.Calculate, http.MethodPost, "/", `{"portfolio":"FIXED"}`)
	golden.Response(t, "rebalancing_calculate_fixed_default", st, b)
	st, b = golden.Call(h.Calculate, http.MethodPost, "/", `{"portfolio":"VARIABLE"}`)
	golden.Response(t, "rebalancing_calculate_variable_no_amount_400", st, b)
	// D2 (task 180): an unknown portfolio is a 400, no longer the enum cast's 500.
	st, b = golden.Call(h.Calculate, http.MethodPost, "/", `{"portfolio":"BOGUS","depositAmountPln":100}`)
	golden.Response(t, "rebalancing_calculate_unknown_portfolio_400", st, b)

	st, b = golden.Call(h.Confirm, http.MethodPost, "/", `{"portfolio":"VARIABLE","depositAmountPln":1500,"items":[
		{"ticker":"A1","units":4},{"ticker":"A2","units":0},{"ticker":"B1","units":7.5}]}`)
	golden.Response(t, "rebalancing_confirm", st, b)
	golden.Value(t, "rebalancing_confirm_db", rebalancingDBState(t, pool))

	st, b = golden.Call(h.Confirm, http.MethodPost, "/", `{"portfolio":"VARIABLE","depositAmountPln":100,"items":[]}`)
	golden.Response(t, "rebalancing_confirm_empty_400", st, b)
	st, b = golden.Call(h.Confirm, http.MethodPost, "/", `{"portfolio":"VARIABLE","depositAmountPln":0,"items":[{"ticker":"A1","units":1}]}`)
	golden.Response(t, "rebalancing_confirm_no_deposit_400", st, b)
	st, b = golden.Call(h.Confirm, http.MethodPost, "/", `{"portfolio":"FIXED","depositAmountPln":100,"items":[{"ticker":"B1","units":1}]}`)
	golden.Response(t, "rebalancing_confirm_unknown_404", st, b)
	golden.Value(t, "rebalancing_after_rejections_db", rebalancingDBState(t, pool))

	for _, p := range []string{"VARIABLE", "FIXED"} {
		st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) {
			h.History(w, r, openapi.HistoryParams{Portfolio: openapi.HistoryParamsPortfolio(p)})
		}, http.MethodGet, "/", "")
		golden.Response(t, "rebalancing_history_"+p, st, b)
	}
}

type rebalancingState struct {
	Positions []struct {
		Portfolio, Ticker, Units string
		TotalSpentPln            *string
	}
	Lots     []struct{ Portfolio, Ticker, Units, PricePln, CostPln string }
	Deposits []struct {
		Portfolio, Amount string
		Comment           *string
	}
	Cash []string
}

func rebalancingDBState(t *testing.T, pool *pgxpool.Pool) rebalancingState {
	t.Helper()
	ctx := context.Background()
	var s rebalancingState
	rows, err := pool.Query(ctx, `SELECT portfolio::text, ticker, units::text, total_spent_pln::text FROM positions ORDER BY portfolio, ticker`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r struct {
			Portfolio, Ticker, Units string
			TotalSpentPln            *string
		}
		if err := rows.Scan(&r.Portfolio, &r.Ticker, &r.Units, &r.TotalSpentPln); err != nil {
			t.Fatal(err)
		}
		s.Positions = append(s.Positions, r)
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT portfolio::text, ticker, units::text, price_pln::text, cost_pln::text FROM purchase_lots ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r struct{ Portfolio, Ticker, Units, PricePln, CostPln string }
		if err := rows.Scan(&r.Portfolio, &r.Ticker, &r.Units, &r.PricePln, &r.CostPln); err != nil {
			t.Fatal(err)
		}
		s.Lots = append(s.Lots, r)
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT portfolio::text, amount::text, comment FROM deposits ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r struct {
			Portfolio, Amount string
			Comment           *string
		}
		if err := rows.Scan(&r.Portfolio, &r.Amount, &r.Comment); err != nil {
			t.Fatal(err)
		}
		s.Deposits = append(s.Deposits, r)
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT portfolio::text || '=' || balance_pln::text FROM account_cash_snapshot ORDER BY portfolio`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		s.Cash = append(s.Cash, c)
	}
	rows.Close()
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
