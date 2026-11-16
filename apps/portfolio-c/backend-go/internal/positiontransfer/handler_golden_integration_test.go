package positiontransfer

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/pgtx"
	sctransfer "investing-app/common-go/siblingcore/transfer"
	"investing-app/common-go/testsupport"
	"investing-app/common-go/testsupport/golden"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestPositionTransferHandler_Golden pins the wire bytes and the resulting database state of the
// manual "Sprzedaj" transfer (task 180 wave 0): handler built as cmd/server/main.go builds it,
// driven with httptest, compared with testdata/golden/transfer_<case>.json. Re-record with -update.
func TestPositionTransferHandler_Golden(t *testing.T) {
	pool := goldenPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, position_transfers CASCADE;
		INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct) VALUES
			('VARIABLE', 'TA', 'Asset A', 'STOCKS', 'ETF', 60), ('VARIABLE', 'TB', 'Asset B', 'METALS', 'ETF', 40),
			('FIXED', 'TA', 'Asset A fixed', 'STOCKS', 'ETF', 100);
		INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary, units, total_spent_pln) VALUES
			('VARIABLE', 'SOLD', 'Sold leg', 'XTB', 'ETF', 'STOCKS', 'PLN', 'S.WA', (SELECT id FROM assets WHERE key = 'TA' AND portfolio = 'VARIABLE'), true, 11, 1100),
			('VARIABLE', 'SIB', 'Same asset', 'XTB', 'ETF', 'STOCKS', 'PLN', 'SIB.WA', (SELECT id FROM assets WHERE key = 'TA' AND portfolio = 'VARIABLE'), false, 0, NULL),
			('VARIABLE', 'BUY', 'Other asset', 'XTB', 'ETF', 'METALS', 'PLN', 'B.WA', (SELECT id FROM assets WHERE key = 'TB' AND portfolio = 'VARIABLE'), true, 2, 40),
			('FIXED', 'SOLD', 'Sold leg fixed', 'XTB', 'ETF', 'STOCKS', 'PLN', 'S.WA', (SELECT id FROM assets WHERE key = 'TA' AND portfolio = 'FIXED'), true, 1, 100);
		INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES
			('S.WA', 100, 'PLN', 100), ('SIB.WA', 50, 'PLN', 50), ('B.WA', 25, 'PLN', 25)`); err != nil {
		t.Fatal(err)
	}
	h := goldenTransferHandler(pool)

	st, b := golden.Call(h.Execute, http.MethodPost, "/", `{"portfolio":"VARIABLE","sold":{"ticker":"SOLD","units":4,"pricePln":100},
		"buys":[{"ticker":"SIB","units":4,"pricePln":50},{"ticker":"BUY","units":8,"pricePln":25}]}`)
	golden.Response(t, "transfer_success", st, b)
	golden.Value(t, "transfer_success_db", transferDBState(t, pool))

	st, b = golden.Call(h.Execute, http.MethodPost, "/", `{"portfolio":"FIXED","sold":{"ticker":"SOLD","units":2,"pricePln":100},
		"buys":[{"ticker":"SOLD","units":2,"pricePln":100}]}`)
	golden.Response(t, "transfer_insufficient_units_400", st, b)
	st, b = golden.Call(h.Execute, http.MethodPost, "/", `{"portfolio":"FIXED","sold":{"ticker":"SOLD","units":1,"pricePln":100},
		"buys":[{"ticker":"BUY","units":4,"pricePln":25}]}`)
	golden.Response(t, "transfer_unknown_ticker_404", st, b)
	st, b = golden.Call(h.Execute, http.MethodPost, "/", `{"portfolio":"VARIABLE","sold":{"ticker":"SOLD","units":1,"pricePln":100},"buys":[]}`)
	golden.Response(t, "transfer_no_buys_400", st, b)
	// D2 (task 180): an unknown portfolio is a 400 before any write.
	st, b = golden.Call(h.Execute, http.MethodPost, "/", `{"portfolio":"BOGUS","sold":{"ticker":"SOLD","units":1,"pricePln":100},"buys":[{"ticker":"BUY","units":4,"pricePln":25}]}`)
	golden.Response(t, "transfer_unknown_portfolio_400", st, b)
	golden.Value(t, "transfer_after_rejections_db", transferDBState(t, pool))
}

type transferPositionRow struct {
	Portfolio, Ticker, Units string
	TotalSpentPln            *string
}

type transferLegRow struct {
	Portfolio, Ticker, UnitsDelta, PricePln, TotalPln string
}

type transferState struct {
	Positions []transferPositionRow
	Legs      []transferLegRow
}

func transferDBState(t *testing.T, pool *pgxpool.Pool) transferState {
	t.Helper()
	ctx := context.Background()
	var s transferState
	rows, err := pool.Query(ctx, `SELECT portfolio::text, ticker, units::text, total_spent_pln::text FROM positions ORDER BY portfolio, ticker`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r transferPositionRow
		if err := rows.Scan(&r.Portfolio, &r.Ticker, &r.Units, &r.TotalSpentPln); err != nil {
			t.Fatal(err)
		}
		s.Positions = append(s.Positions, r)
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT portfolio::text, ticker, units_delta::text, price_pln::text, total_pln::text FROM position_transfers ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r transferLegRow
		if err := rows.Scan(&r.Portfolio, &r.Ticker, &r.UnitsDelta, &r.PricePln, &r.TotalPln); err != nil {
			t.Fatal(err)
		}
		s.Legs = append(s.Legs, r)
	}
	rows.Close()
	return s
}

// goldenTransferHandler wires the handler exactly as cmd/server/main.go does.
func goldenTransferHandler(pool *pgxpool.Pool) *Handler {
	money := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, store.Bind)
	return NewHandler(sctransfer.NewService(book.Config, money))
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
