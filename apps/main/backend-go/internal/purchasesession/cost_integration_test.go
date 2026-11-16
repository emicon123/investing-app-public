package purchasesession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/jackc/pgx/v5"
	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// subSpec describes one subasset row of a cost-bookkeeping fixture asset.
type subSpec struct {
	ticker  string
	units   string  // units_regular_pln
	ike     string  // units_ike (default "0")
	price   string  // price_cache PLN price (default "100")
	cost    *string // nil = NULL total_spent_pln
	primary bool
}

// seedAsset inserts one asset (no target) with the given subassets (XTB / PLN, yahoo ticker =
// ticker + ".YAHOO") priced at 100 PLN in price_cache, and returns the positions' ids by ticker.
func seedAsset(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key string, subs []subSpec) map[string]int64 {
	t.Helper()
	var assetID int64
	if err := pool.QueryRow(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class)
		VALUES ('MAIN', $1::text, $1::text, 'CASH', 'ETF') RETURNING id`, key).Scan(&assetID); err != nil {
		t.Fatalf("insert asset %s: %v", key, err)
	}
	ids := map[string]int64{}
	for _, s := range subs {
		yahoo := s.ticker + ".YAHOO"
		if s.ike == "" {
			s.ike = "0"
		}
		if s.price == "" {
			s.price = "100"
		}
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency, purchase_currency,
				units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
				total_spent_pln, asset_id, is_primary, yahoo_ticker, company_backed)
			VALUES ($1::text, $1::text, 'MAIN', 'XTB', 'ETF', 'CASH', 'PLN', 'PLN', $7::numeric, 0, 0, 0, $2::numeric, $3::numeric, $4, $5, $6::text, true)
			RETURNING id`, s.ticker, s.units, s.cost, assetID, s.primary, yahoo, s.ike).Scan(&id); err != nil {
			t.Fatalf("insert position %s: %v", s.ticker, err)
		}
		ids[s.ticker] = id
		if _, err := pool.Exec(ctx, `INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ($1::text, $2::numeric, 'PLN', $2::numeric)
			ON CONFLICT (ticker) DO UPDATE SET price_pln = $2::numeric`, yahoo, s.price); err != nil {
			t.Fatalf("seed price for %s: %v", yahoo, err)
		}
	}
	return ids
}

func costText(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64) string {
	t.Helper()
	var c *string
	if err := pool.QueryRow(ctx, `SELECT total_spent_pln::text FROM positions WHERE id = $1`, id).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c == nil {
		return "NULL"
	}
	return *c
}

func unitsText(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64) decimal.Decimal {
	t.Helper()
	var u string
	if err := pool.QueryRow(ctx, `SELECT units_regular_pln::text FROM positions WHERE id = $1`, id).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return decimal.RequireFromString(u)
}

// assetCost is the meaningful number: Σ recorded cost over an asset's rows; assetProfit is
// Σvalue / Σcost - 1 with every fixture priced at 100 PLN.
func assetCost(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key string) decimal.Decimal {
	t.Helper()
	var c string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(p.total_spent_pln), 0)::text FROM positions p JOIN assets a ON a.id = p.asset_id WHERE a.key = $1`, key).Scan(&c); err != nil {
		t.Fatal(err)
	}
	return decimal.RequireFromString(c)
}

func assetProfit(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key string) decimal.Decimal {
	t.Helper()
	var units string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(units_ike + units_ikze + units_regular_eur + units_regular_usd + units_regular_pln), 0)::text
		FROM positions p JOIN assets a ON a.id = p.asset_id WHERE a.key = $1`, key).Scan(&units); err != nil {
		t.Fatal(err)
	}
	value := decimal.RequireFromString(units).Mul(decimal.NewFromInt(100))
	return value.DivRound(assetCost(ctx, t, pool, key), 8).Sub(decimal.NewFromInt(1))
}

func newPlannedSession(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_sessions DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestCompleteSession_AssetLevelCost_RealPostgres is design 161 §7 I1 for the main app's session
// completion (task 161a-2): the completion transaction re-scales the asset's recorded cost with
// the costbasis kernel and writes it atomically with the units - real Postgres, no DB mocks.
func TestCompleteSession_AssetLevelCost_RealPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	queries := sqlcgen.New(pool)
	prices := realPrices{prices: map[string]decimal.Decimal{}, rates: map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}}
	svc := NewService(queries, newTransactFunc(pool, queries), newTransactFunc(pool, queries), prices)

	reset := func(t *testing.T) {
		t.Helper()
		if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, asset_purchases, purchase_sessions, purchase_session_items, binance_transfers, price_cache CASCADE`); err != nil {
			t.Fatal(err)
		}
		mustZeroRetirementContribution(ctx, t, pool)
	}
	addItemIn := func(t *testing.T, sessionID int64, ticker, account string, migrationSell bool, plannedUnits, plannedValue string, actualUnits *string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO purchase_session_items
			(session_id, portfolio, ticker, account_type, planned_units, planned_value_pln, actual_units, price_pln_at_plan, is_migration_sell, is_migration_buy)
			VALUES ($1, 'MAIN', $2::text, $7::account_type_enum, $3::numeric, $4::numeric, $5::numeric, 100, $6, false)`,
			sessionID, ticker, plannedUnits, plannedValue, actualUnits, migrationSell, account); err != nil {
			t.Fatalf("insert item for %s: %v", ticker, err)
		}
	}
	addItem := func(t *testing.T, sessionID int64, ticker string, migrationSell bool, plannedUnits, plannedValue string, actualUnits *string) {
		t.Helper()
		addItemIn(t, sessionID, ticker, "REGULAR", migrationSell, plannedUnits, plannedValue, actualUnits)
	}
	complete := func(t *testing.T, sessionID int64) {
		t.Helper()
		if _, err := svc.CompleteSession(ctx, sessionID); err != nil {
			t.Fatalf("CompleteSession: %v", err)
		}
	}

	t.Run("sale of a NULL-cost sibling scales the head row's cost, atomically with the units", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true}, // go-live group cost on the head
			{ticker: "SIB", units: "10"},                                    // ETN sibling, NULL cost
		}) // V = 2 000, C = 1 000, profit +100 %
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "SIB", true, "5", "500", new("-5")) // migration sell of 5 ETN = 25 % of the asset

		if _, err := svc.CompleteSession(ctx, sid); err != nil {
			t.Fatalf("CompleteSession: %v", err)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "750.0000" {
			t.Fatalf("head cost = %s, want 750.0000 (1000 x (1 - 500/2000))", got)
		}
		if got := costText(ctx, t, pool, ids["SIB"]); got != "NULL" {
			t.Fatalf("sibling cost = %s, want NULL (never invented)", got)
		}
		if got := unitsText(ctx, t, pool, ids["SIB"]); !got.Equal(decimal.NewFromInt(5)) {
			t.Fatalf("sibling units = %s, want 5", got)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_purchases WHERE session_id = $1 AND trade_type = 'MIGRATION_SELL'`, sid).Scan(&n); err != nil || n != 1 {
			t.Fatalf("expected 1 MIGRATION_SELL ledger row, got %d (%v)", n, err)
		}
	})

	t.Run("buy on a tracked asset adds the paid amount to the bought row", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "SIB", units: "10"},
		})
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "SIB", false, "5", "500", nil)

		if _, err := svc.CompleteSession(ctx, sid); err != nil {
			t.Fatalf("CompleteSession: %v", err)
		}
		if got := costText(ctx, t, pool, ids["SIB"]); got != "500.0000" {
			t.Fatalf("bought row cost = %s, want 500.0000", got)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("head cost = %s, want unchanged 1000.0000", got)
		}
	})

	t.Run("buy into an untracked asset that holds units invents no cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "PORTFOLIO_BLIKE", []subSpec{
			{ticker: "HEAD", units: "10", primary: true},
			{ticker: "SIB", units: "10"},
		})
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "HEAD", false, "5", "500", nil)

		if _, err := svc.CompleteSession(ctx, sid); err != nil {
			t.Fatalf("CompleteSession: %v", err)
		}
		if got := unitsText(ctx, t, pool, ids["HEAD"]); !got.Equal(decimal.NewFromInt(15)) {
			t.Fatalf("head units = %s, want 15", got)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "NULL" {
			t.Fatalf("head cost = %s, want NULL (rule 7)", got)
		}
	})

	t.Run("buy into an empty asset starts tracking", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "NEW", []subSpec{{ticker: "HEAD", units: "0", primary: true}})
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "HEAD", false, "5", "500", nil)

		if _, err := svc.CompleteSession(ctx, sid); err != nil {
			t.Fatalf("CompleteSession: %v", err)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "500.0000" {
			t.Fatalf("cost = %s, want 500.0000", got)
		}
	})

	t.Run("selling every unit of the asset leaves exactly zero cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "3", cost: new("100"), primary: true},
			{ticker: "SIB", units: "0"},
		})
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "HEAD", true, "3", "300", new("-3"))

		if _, err := svc.CompleteSession(ctx, sid); err != nil {
			t.Fatalf("CompleteSession: %v", err)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "0.0000" {
			t.Fatalf("cost = %s, want exactly 0.0000", got)
		}
	})

	t.Run("session swap: selling the whole ETN and buying the physical asset in the same session carries the cost (BTC group shape)", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "600", cost: new("100000"), primary: true}, // go-live cost on the head: 60 000 PLN
			{ticker: "ETN", units: "200"},                                      // NULL cost: 20 000 PLN
		}) // V = 80 000, C = 100 000, profit -20 %
		profitBefore := assetProfit(ctx, t, pool, "BTC")
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "ETN", true, "200", "20000", new("-200")) // migration sell of the whole ETN
		addItem(t, sid, "HEAD", false, "200", "20000", nil)       // buy of the same value into BTC-USD
		complete(t, sid)

		if got := assetCost(ctx, t, pool, "BTC"); !got.Equal(decimal.NewFromInt(100000)) {
			t.Fatalf("asset cost = %s, want unchanged 100000 (a swap must not realize the loss / reset basis at market)", got)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "100000.0000" {
			t.Fatalf("bought row cost = %s, want 100000.0000 (the carry lands on the bought row)", got)
		}
		if got := costText(ctx, t, pool, ids["ETN"]); got != "NULL" {
			t.Fatalf("ETN cost = %s, want NULL", got)
		}
		if got := assetProfit(ctx, t, pool, "BTC"); !got.Equal(profitBefore) {
			t.Fatalf("profit = %s, want unchanged %s", got, profitBefore)
		}
		if profitBefore.Sub(decimal.RequireFromString("-0.20")).Abs().Cmp(decimal.RequireFromString("0.0001")) > 0 {
			t.Fatalf("fixture profit = %s, want about -20 %%", profitBefore)
		}
	})

	t.Run("session swap with a buy larger than the sale: only the excess enters at market", func(t *testing.T) {
		reset(t)
		seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "600", cost: new("100000"), primary: true},
			{ticker: "ETN", units: "200"},
		})
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "ETN", true, "100", "10000", new("-100"))
		addItem(t, sid, "HEAD", false, "150", "15000", nil)
		complete(t, sid)
		if got := assetCost(ctx, t, pool, "BTC"); !got.Equal(decimal.NewFromInt(105000)) {
			t.Fatalf("asset cost = %s, want 105000 (100000 + the 5 000 excess)", got)
		}
	})

	t.Run("session swap with a sale larger than the buy: cost scales by (S-B)/V_before", func(t *testing.T) {
		reset(t)
		seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "600", cost: new("100000"), primary: true},
			{ticker: "ETN", units: "200"},
		})
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "ETN", true, "150", "15000", new("-150"))
		addItem(t, sid, "HEAD", false, "100", "10000", nil)
		complete(t, sid)
		want := decimal.NewFromInt(100000).Mul(decimal.NewFromInt(80000 - 5000)).Div(decimal.NewFromInt(80000))
		if got := assetCost(ctx, t, pool, "BTC"); got.Sub(want).Abs().Cmp(decimal.RequireFromString("0.001")) > 0 {
			t.Fatalf("asset cost = %s, want %s (100000 x (80000-5000)/80000)", got, want)
		}
	})

	t.Run("donor sells its IKE wrapper, recipient buys IKE, donor gets the compensating REGULAR buy", func(t *testing.T) {
		reset(t)
		seedAsset(ctx, t, pool, "DONOR", []subSpec{
			{ticker: "DHEAD", units: "100", cost: new("8000"), primary: true},
			{ticker: "DETN", units: "0", ike: "100"}, // the IKE-held ETN wrapper, NULL cost; V = 20 000
		})
		recipient := seedAsset(ctx, t, pool, "RECIPIENT", []subSpec{{ticker: "RHEAD", units: "10", cost: new("600"), primary: true}})
		sid := newPlannedSession(ctx, t, pool)
		addItemIn(t, sid, "DETN", "IKE", true, "50", "5000", new("-50")) // donor sells IKE wrapper
		addItemIn(t, sid, "RHEAD", "IKE", false, "50", "5000", nil)      // recipient buys IKE
		addItemIn(t, sid, "DETN", "REGULAR", false, "50", "5000", nil)   // donor's compensating REGULAR buy
		complete(t, sid)

		if got := assetCost(ctx, t, pool, "DONOR"); !got.Equal(decimal.NewFromInt(8000)) {
			t.Fatalf("donor cost = %s, want unchanged 8000", got)
		}
		if got := assetProfit(ctx, t, pool, "DONOR"); !got.Equal(decimal.RequireFromString("1.5")) { // 20 000 / 8 000 - 1
			t.Fatalf("donor profit = %s, want unchanged 1.5", got)
		}
		if got := costText(ctx, t, pool, recipient["RHEAD"]); got != "5600.0000" {
			t.Fatalf("recipient cost = %s, want 5600.0000 (600 + 5 000 at market)", got)
		}
	})

	t.Run("migration sell + buy: the sold row holds the group cost, the bought row is NULL - Σ cost unchanged", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "SIB", units: "10"},
		})
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "HEAD", true, "5", "500", new("-5"))
		addItem(t, sid, "SIB", false, "5", "500", nil)
		complete(t, sid)
		if got := assetCost(ctx, t, pool, "BTC"); !got.Equal(decimal.NewFromInt(1000)) {
			t.Fatalf("asset cost = %s, want unchanged 1000", got)
		}
		if h, s := costText(ctx, t, pool, ids["HEAD"]), costText(ctx, t, pool, ids["SIB"]); h != "750.0000" || s != "250.0000" {
			t.Fatalf("head/sibling cost = %s/%s, want 750.0000/250.0000 (carry lands on the bought row)", h, s)
		}
	})

	t.Run("a failure after the cost writes rolls every row's cost back with the rest", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "SIB", units: "10"},
		})
		other := seedAsset(ctx, t, pool, "OTHER", []subSpec{{ticker: "ETF2", units: "10", primary: true}})
		sid := newPlannedSession(ctx, t, pool)
		addItem(t, sid, "SIB", true, "5", "500", new("-5")) // scales HEAD's cost
		addItem(t, sid, "ETF2", false, "8", "800", nil)     // net buys 300 > 0: the auto-deposit insert fails below
		failing := NewService(queries, newTransactFunc(pool, queries),
			pgtx.Runner(pool, func(tx pgx.Tx) Queries { return failingDeposits{queries.WithTx(tx)} }), prices)

		if _, err := failing.CompleteSession(ctx, sid); err == nil {
			t.Fatal("expected CompleteSession to fail on the injected deposit error")
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("head cost = %s, want the original 1000.0000 (rolled back)", got)
		}
		if got := unitsText(ctx, t, pool, ids["SIB"]); !got.Equal(decimal.NewFromInt(10)) {
			t.Fatalf("sibling units = %s, want the original 10 (rolled back)", got)
		}
		if got := unitsText(ctx, t, pool, other["ETF2"]); !got.Equal(decimal.NewFromInt(10)) {
			t.Fatalf("other units = %s, want the original 10 (rolled back)", got)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_purchases WHERE session_id = $1`, sid).Scan(&n); err != nil || n != 0 {
			t.Fatalf("expected no ledger rows after the rollback, got %d (%v)", n, err)
		}
	})
}

// failingDeposits injects a failure into the auto-deposit insert - the statement that runs AFTER
// every position and cost write of a completion - to prove the whole transaction rolls back.
type failingDeposits struct{ Queries }

func (failingDeposits) InsertDeposit(context.Context, sqlcgen.InsertDepositParams) (sqlcgen.Deposit, error) {
	return sqlcgen.Deposit{}, errors.New("injected deposit failure")
}
