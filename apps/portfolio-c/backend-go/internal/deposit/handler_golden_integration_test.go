package deposit

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	scdeposit "investing-app/common-go/siblingcore/deposit"
	"investing-app/common-go/testsupport"
	"investing-app/common-go/testsupport/golden"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
	"portfolio-c/backend-go/openapi"
)

// TestDepositHandler_Golden pins the wire bytes of the deposit endpoints (task 180 wave 0) before
// the service moves into common-go/siblingcore: handler built as cmd/server/main.go builds it,
// driven with httptest, compared with testdata/golden/deposit_<case>.json. xirrPct and
// yearsElapsed depend on today's date and are masked. Re-record with -update.
func TestDepositHandler_Golden(t *testing.T) {
	pool := goldenPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE deposits, positions, assets CASCADE;
		INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct) VALUES
			('VARIABLE', 'DX', 'Deposit test', 'STOCKS', 'ETF', 100), ('FIXED', 'DX', 'Deposit test', 'STOCKS', 'ETF', 100);
		INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary, units)
			VALUES ('VARIABLE', 'DX', 'Deposit test', 'XTB', 'ETF', 'STOCKS', 'PLN', 'DX.WA', (SELECT id FROM assets WHERE key = 'DX' AND portfolio = 'VARIABLE'), true, 10),
			       ('FIXED', 'DX', 'Deposit test', 'XTB', 'ETF', 'STOCKS', 'PLN', 'DX.WA', (SELECT id FROM assets WHERE key = 'DX' AND portfolio = 'FIXED'), true, 3);
		INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ('DX.WA', 100, 'PLN', 100);
		INSERT INTO deposits (portfolio, deposit_date, amount, comment) VALUES
			('VARIABLE', '2024-01-15', 500, NULL), ('VARIABLE', '2025-03-01', 300, 'marzec'), ('VARIABLE', '2025-06-01', 200.5, NULL),
			('FIXED', '2025-02-01', 400, NULL), ('FIXED', '2025-03-01', 400, NULL)`); err != nil {
		t.Fatal(err)
	}
	h := goldenDepositHandler(pool)
	check := func(name string, status int, body []byte, volatile ...string) {
		t.Helper()
		golden.Response(t, "deposit_"+name, status, body, volatile...)
	}
	list := func(portfolio string, year *int32, page, size int32) (int, []byte) {
		return golden.Call(func(w http.ResponseWriter, r *http.Request) {
			h.ListDeposits(w, r, openapi.ListDepositsParams{Portfolio: openapi.ListDepositsParamsPortfolio(portfolio), Year: year, Pageable: openapi.Pageable{Page: &page, Size: &size}})
		}, http.MethodGet, "/", "")
	}

	st, b := list("VARIABLE", nil, 0, 50)
	check("list", st, b)
	st, b = list("FIXED", nil, 0, 50)
	check("fixed_list", st, b)
	y2025 := int32(2025)
	st, b = list("VARIABLE", &y2025, 0, 50)
	check("list_by_year", st, b)
	st, b = list("VARIABLE", nil, 1, 1)
	check("list_page", st, b)
	// D2 (task 180): an unknown portfolio is a 400, no longer the enum cast's 500.
	st, b = list("BOGUS", nil, 0, 50)
	check("list_unknown_portfolio_400", st, b)

	st, b = golden.Call(h.CreateDeposit, http.MethodPost, "/", `{"portfolio":"FIXED","depositDate":"2026-01-10","amount":150.25,"comment":"styczeń"}`)
	check("create", st, b)
	st, b = golden.Call(h.CreateDeposit, http.MethodPost, "/", `{"portfolio":"FIXED","depositDate":"2026-01-10","amount":-5}`)
	check("create_negative_400", st, b)
	st, b = golden.Call(h.CreateDeposit, http.MethodPost, "/", `{"portfolio":"FIXED","depositDate":"10.01.2026","amount":5}`)
	check("create_bad_date_400", st, b)

	for _, p := range []string{"VARIABLE", "FIXED"} {
		st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) {
			h.ComputeXirr(w, r, openapi.ComputeXirrParams{Portfolio: openapi.ComputeXirrParamsPortfolio(p)})
		}, http.MethodGet, "/", "")
		check("xirr_"+p, st, b, "xirrPct", "yearsElapsed")
	}
}

// goldenDepositHandler wires the handler exactly as cmd/server/main.go does.
func goldenDepositHandler(pool *pgxpool.Pool) *Handler {
	q := store.New(pool)
	return NewHandler(scdeposit.NewService(book.Config, q, q, nil))
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
