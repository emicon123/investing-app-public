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
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

// TestPositionTransferHandler_Golden pins the wire bytes and the resulting database state of the
// manual "Sprzedaj" transfer (task 180 wave 0): handler built as cmd/server/main.go builds it,
// driven with httptest, compared with testdata/golden/transfer_<case>.json. Re-record with -update.
func TestPositionTransferHandler_Golden(t *testing.T) {
	pool := goldenPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, position_transfers CASCADE;
		INSERT INTO assets (key, name, exposure, asset_class, target_allocation_pct) VALUES
			('TA', 'Asset A', 'STOCKS', 'ETF', 60), ('TB', 'Asset B', 'METALS', 'ETF', 40);
		INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary, units_ike_tata, units_ikze_mama, total_spent_pln) VALUES
			('SOLD', 'Sold leg', 'XTB', 'ETF', 'STOCKS', 'PLN', 'S.WA', (SELECT id FROM assets WHERE key = 'TA'), true, 10, 1, 1100),
			('SIB', 'Same asset', 'XTB', 'ETF', 'STOCKS', 'PLN', 'SIB.WA', (SELECT id FROM assets WHERE key = 'TA'), false, 0, 0, NULL),
			('BUY', 'Other asset', 'XTB', 'ETF', 'METALS', 'PLN', 'B.WA', (SELECT id FROM assets WHERE key = 'TB'), true, 0, 2, 40);
		INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES
			('S.WA', 100, 'PLN', 100), ('SIB.WA', 50, 'PLN', 50), ('B.WA', 25, 'PLN', 25)`); err != nil {
		t.Fatal(err)
	}
	h := goldenTransferHandler(pool)

	st, b := golden.Call(h.Execute, http.MethodPost, "/", `{"sold":{"ticker":"SOLD","accountType":"IKE_TATA","units":4,"pricePln":100},
		"buys":[{"ticker":"SIB","accountType":"IKE_TATA","units":4,"pricePln":50},{"ticker":"BUY","accountType":"IKZE_MAMA","units":8,"pricePln":25}]}`)
	golden.Response(t, "transfer_success", st, b)
	golden.Value(t, "transfer_success_db", transferDBState(t, pool))

	st, b = golden.Call(h.Execute, http.MethodPost, "/", `{"sold":{"ticker":"SOLD","accountType":"IKE_MAMA","units":1,"pricePln":100},
		"buys":[{"ticker":"BUY","accountType":"IKE_MAMA","units":4,"pricePln":25}]}`)
	golden.Response(t, "transfer_insufficient_units_400", st, b)
	st, b = golden.Call(h.Execute, http.MethodPost, "/", `{"sold":{"ticker":"NOPE","accountType":"IKE_TATA","units":1,"pricePln":100},
		"buys":[{"ticker":"BUY","accountType":"IKE_TATA","units":4,"pricePln":25}]}`)
	golden.Response(t, "transfer_unknown_ticker_404", st, b)
	st, b = golden.Call(h.Execute, http.MethodPost, "/", `{"sold":{"ticker":"SOLD","accountType":"IKE_TATA","units":1,"pricePln":100},"buys":[]}`)
	golden.Response(t, "transfer_no_buys_400", st, b)
	// D1 (task 180): a leg without an account type is a 400 before any write (it used to be a 500
	// from the audit insert's enum cast, rolled back).
	st, b = golden.Call(h.Execute, http.MethodPost, "/", `{"sold":{"ticker":"SOLD","units":1,"pricePln":100},"buys":[{"ticker":"BUY","accountType":"IKE_TATA","units":4,"pricePln":25}]}`)
	golden.Response(t, "transfer_missing_account_type_400", st, b)
	golden.Value(t, "transfer_after_rejections_db", transferDBState(t, pool))
}

type transferPositionRow struct {
	Ticker, IkeTata, IkeMama, IkzeTata, IkzeMama, Inne string
	TotalSpentPln                                      *string
}

type transferLegRow struct {
	Ticker, AccountType, UnitsDelta, PricePln, TotalPln string
}

type transferState struct {
	Positions []transferPositionRow
	Legs      []transferLegRow
}

func transferDBState(t *testing.T, pool *pgxpool.Pool) transferState {
	t.Helper()
	ctx := context.Background()
	var s transferState
	rows, err := pool.Query(ctx, `SELECT ticker, units_ike_tata::text, units_ike_mama::text, units_ikze_tata::text, units_ikze_mama::text, units_inne::text, total_spent_pln::text FROM positions ORDER BY ticker`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r transferPositionRow
		if err := rows.Scan(&r.Ticker, &r.IkeTata, &r.IkeMama, &r.IkzeTata, &r.IkzeMama, &r.Inne, &r.TotalSpentPln); err != nil {
			t.Fatal(err)
		}
		s.Positions = append(s.Positions, r)
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT ticker, account_type::text, units_delta::text, price_pln::text, total_pln::text FROM position_transfers ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r transferLegRow
		if err := rows.Scan(&r.Ticker, &r.AccountType, &r.UnitsDelta, &r.PricePln, &r.TotalPln); err != nil {
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
