package rebalancing

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

type subSpec struct {
	ticker  string
	units   string  // units_regular_pln
	cost    *string // nil = NULL total_spent_pln
	primary bool
}

// seedAsset inserts one MAIN asset (no target) with the given subassets (XTB / PLN, yahoo ticker =
// ticker + ".YAHOO") priced at 100 PLN in price_cache; returns the position ids by ticker.
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
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency, purchase_currency,
				units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
				total_spent_pln, asset_id, is_primary, yahoo_ticker, company_backed)
			VALUES ($1::text, $1::text, 'MAIN', 'XTB', 'ETF', 'CASH', 'PLN', 'PLN', 0, 0, 0, 0, $2::numeric, $3::numeric, $4, $5, $6::text, true)
			RETURNING id`, s.ticker, s.units, s.cost, assetID, s.primary, yahoo).Scan(&id); err != nil {
			t.Fatalf("insert position %s: %v", s.ticker, err)
		}
		ids[s.ticker] = id
		if _, err := pool.Exec(ctx, `INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ($1::text, 100, 'PLN', 100)
			ON CONFLICT (ticker) DO UPDATE SET price_pln = 100`, yahoo); err != nil {
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

func unitsOf(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64, col string) decimal.Decimal {
	t.Helper()
	var u string
	if err := pool.QueryRow(ctx, `SELECT `+col+`::text FROM positions WHERE id = $1`, id).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return decimal.RequireFromString(u)
}

// TestExecuteTransfer_AssetLevelCost_RealPostgres is design 161 §7 I2 (+ S9/S10 on real rows) for
// the main app's manual transfer (task 161a-2): swap detection by asset_id, cost carried on a
// same-asset swap, sold at the asset's own entry price and bought at market across assets - all
// written in the transfer's own transaction, real Postgres, no DB mocks.
func TestExecuteTransfer_AssetLevelCost_RealPostgres(t *testing.T) {
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
	transact := pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
	svc := NewService(queries, transact)
	reset := func(t *testing.T) {
		t.Helper()
		if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, asset_purchases, price_cache CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	leg := func(ticker, account string, units, price int64) Leg {
		return Leg{Ticker: ticker, AccountType: account, Units: decimal.NewFromInt(units), PricePln: decimal.NewFromInt(price)}
	}

	t.Run("same-asset swap (sell ETN, buy the primary of the SAME asset) carries the cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true}, // go-live group cost on the head
			{ticker: "ETN", units: "10"},                                    // NULL cost
		}) // V = 2 000, C = 1 000
		_, err := svc.ExecuteTransfer(ctx, TransferRequest{
			Portfolio: "MAIN",
			Sold:      leg("ETN", "REGULAR", 5, 100), // S = 500, f = 0.25
			Buys:      []Leg{leg("HEAD", "REGULAR", 5, 100)},
		})
		if err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		// head: 1000 x 0.75 = 750, plus the carried 250 = 1000 - the asset's cost is unchanged
		// (a market-priced buy would have produced 1500).
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("head cost = %s, want 1000.0000", got)
		}
		if got := costText(ctx, t, pool, ids["ETN"]); got != "NULL" {
			t.Fatalf("ETN cost = %s, want NULL", got)
		}
		if got := unitsOf(ctx, t, pool, ids["HEAD"], "units_regular_pln"); !got.Equal(decimal.NewFromInt(15)) {
			t.Fatalf("head units = %s, want 15", got)
		}
		if got := unitsOf(ctx, t, pool, ids["ETN"], "units_regular_pln"); !got.Equal(decimal.NewFromInt(5)) {
			t.Fatalf("ETN units = %s, want 5", got)
		}
	})

	t.Run("same-asset swap with cost on both rows moves the carried cost to the bought row", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "10", cost: new("400"), primary: true},
			{ticker: "ETN", units: "10", cost: new("400")},
		})
		if _, err := svc.ExecuteTransfer(ctx, TransferRequest{
			Portfolio: "MAIN", Sold: leg("ETN", "REGULAR", 5, 100), Buys: []Leg{leg("HEAD", "REGULAR", 5, 100)},
		}); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if h, e := costText(ctx, t, pool, ids["HEAD"]), costText(ctx, t, pool, ids["ETN"]); h != "500.0000" || e != "300.0000" {
			t.Fatalf("head/ETN cost = %s/%s, want 500.0000/300.0000 (sum 800 unchanged)", h, e)
		}
	})

	t.Run("same instrument REGULAR to IKE (same subasset, other bucket) leaves the cost untouched", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{{ticker: "HEAD", units: "10", cost: new("1000"), primary: true}})
		if _, err := svc.ExecuteTransfer(ctx, TransferRequest{
			Portfolio: "MAIN", Sold: leg("HEAD", "REGULAR", 4, 100), Buys: []Leg{leg("HEAD", "IKE", 4, 100)},
		}); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("cost = %s, want 1000.0000", got)
		}
		if got := unitsOf(ctx, t, pool, ids["HEAD"], "units_ike"); !got.Equal(decimal.NewFromInt(4)) {
			t.Fatalf("IKE units = %s, want 4", got)
		}
	})

	t.Run("cross-asset transfer sells at the asset's own entry price and buys at market", func(t *testing.T) {
		reset(t)
		btc := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "ETN", units: "10"},
		})
		eth := seedAsset(ctx, t, pool, "ETH", []subSpec{{ticker: "ETHHEAD", units: "10", cost: new("900"), primary: true}})
		if _, err := svc.ExecuteTransfer(ctx, TransferRequest{
			Portfolio: "MAIN", Sold: leg("ETN", "REGULAR", 5, 100), Buys: []Leg{leg("ETHHEAD", "REGULAR", 5, 100)},
		}); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if got := costText(ctx, t, pool, btc["HEAD"]); got != "750.0000" {
			t.Fatalf("sold asset's head cost = %s, want 750.0000 (x 0.75)", got)
		}
		if got := costText(ctx, t, pool, eth["ETHHEAD"]); got != "1400.0000" {
			t.Fatalf("bought asset cost = %s, want 1400.0000 (900 + 5 x 100 at market)", got)
		}
	})

	t.Run("a rejected transfer writes no cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "BTC", []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "ETN", units: "10"},
		})
		_, err := svc.ExecuteTransfer(ctx, TransferRequest{
			Portfolio: "MAIN", Sold: leg("ETN", "REGULAR", 999, 100), Buys: []Leg{leg("HEAD", "REGULAR", 1, 100)},
		})
		if err == nil {
			t.Fatal("expected an insufficient-units error")
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("head cost = %s, want the untouched 1000.0000", got)
		}
	})
}
