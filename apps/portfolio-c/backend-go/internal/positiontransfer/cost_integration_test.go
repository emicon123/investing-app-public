package positiontransfer

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgtx"
	"investing-app/common-go/siblingcore"
	sctransfer "investing-app/common-go/siblingcore/transfer"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

type subSpec struct {
	ticker  string
	units   string
	cost    *string // nil = NULL total_spent_pln
	primary bool
}

// seedAsset inserts one asset of a portfolio (no target) with the given subassets, yahoo ticker =
// ticker + ".Y", each priced at price PLN in price_cache; returns the position ids by ticker.
func seedAsset(ctx context.Context, t *testing.T, pool *pgxpool.Pool, portfolio, key string, price int, subs []subSpec) map[string]int64 {
	t.Helper()
	var assetID int64
	if err := pool.QueryRow(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class)
		VALUES ($1::portfolio_enum, $2::text, $2::text, 'STOCKS', 'ETF') RETURNING id`, portfolio, key).Scan(&assetID); err != nil {
		t.Fatalf("insert asset %s/%s: %v", portfolio, key, err)
	}
	ids := map[string]int64{}
	for _, s := range subs {
		yahoo := s.ticker + ".Y"
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker,
				units, total_spent_pln, asset_id, is_primary)
			VALUES ($1::portfolio_enum, $2::text, $2::text, 'XTB', 'ETF', 'STOCKS', 'PLN', $3::text, $4::numeric, $5::numeric, $6, $7)
			RETURNING id`, portfolio, s.ticker, yahoo, s.units, s.cost, assetID, s.primary).Scan(&id); err != nil {
			t.Fatalf("insert position %s/%s: %v", portfolio, s.ticker, err)
		}
		ids[s.ticker] = id
		if _, err := pool.Exec(ctx, `INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ($1::text, $2::numeric, 'PLN', $2::numeric)
			ON CONFLICT (ticker) DO UPDATE SET price_pln = EXCLUDED.price_pln`, yahoo, price); err != nil {
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
	if err := pool.QueryRow(ctx, `SELECT units::text FROM positions WHERE id = $1`, id).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return decimal.RequireFromString(u)
}

// TestExecuteTransfer_AssetLevelCost_RealPostgres is design 161 §7 I1/I2 for portfolioC's manual
// transfer (task 161a-3): a sale scales the sold asset's cost (also when the sold row is a
// NULL-cost sibling), an asset with no recorded cost is left untouched by sales and buys (rule 7),
// a same-asset swap CARRIES cost (detected by asset_id), a cross-asset transfer is independent and
// never crosses portfolios (QDV5.DE is held in both), a full sale zeroes the cost exactly and a
// rebuy starts fresh, and a failure after the writes rolls units AND cost back. Prices come from
// price_cache read inside the transfer's transaction. Real Postgres, no DB mocks.
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

	svc := sctransfer.NewService(book.Config, pgtx.Runner(pool, store.Bind))
	reset := func(t *testing.T) {
		t.Helper()
		if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, position_transfers, price_cache CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	leg := func(ticker string, units, price int64) sctransfer.Leg {
		return sctransfer.Leg{Ticker: ticker, Units: decimal.NewFromInt(units), PricePln: decimal.NewFromInt(price)}
	}
	transfer := func(portfolio string, sold sctransfer.Leg, buys ...sctransfer.Leg) error {
		_, err := svc.ExecuteTransfer(ctx, sctransfer.Request{Portfolio: siblingcore.Portfolio(portfolio), Sold: sold, Buys: buys})
		return err
	}

	t.Run("sale and buy on assets with no recorded cost change no cost", func(t *testing.T) {
		reset(t)
		a := seedAsset(ctx, t, pool, "VARIABLE", "NULLA", 100, []subSpec{{ticker: "NULLA", units: "10", primary: true}})
		b := seedAsset(ctx, t, pool, "VARIABLE", "NULLB", 100, []subSpec{{ticker: "NULLB", units: "10", primary: true}})
		if err := transfer("VARIABLE", leg("NULLA", 4, 100), leg("NULLB", 4, 100)); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		for name, id := range map[string]int64{"NULLA": a["NULLA"], "NULLB": b["NULLB"]} {
			if got := costText(ctx, t, pool, id); got != "NULL" {
				t.Fatalf("%s cost = %s, want NULL (nothing to reduce, nothing invented)", name, got)
			}
		}
		if got := unitsText(ctx, t, pool, b["NULLB"]); !got.Equal(decimal.NewFromInt(14)) {
			t.Fatalf("NULLB units = %s, want 14 (units still applied)", got)
		}
	})

	t.Run("cross-asset transfer: the sold asset scales by its own share, the bought asset gains the paid amount, the other portfolio is untouched", func(t *testing.T) {
		reset(t)
		gold := seedAsset(ctx, t, pool, "VARIABLE", "4GLD.DE", 100, []subSpec{{ticker: "4GLD.DE", units: "10", cost: new("1000"), primary: true}})
		variableQdv := seedAsset(ctx, t, pool, "VARIABLE", "QDV5.DE", 100, []subSpec{{ticker: "QDV5.DE", units: "10", cost: new("900"), primary: true}})
		fixedQdv := seedAsset(ctx, t, pool, "FIXED", "QDV5.DE", 100, []subSpec{{ticker: "QDV5.DE", units: "5", cost: new("174.0385"), primary: true}})
		if err := transfer("VARIABLE", leg("4GLD.DE", 4, 100), leg("QDV5.DE", 4, 100)); err != nil { // S = 400, f = 0.4
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if got := costText(ctx, t, pool, gold["4GLD.DE"]); got != "600.0000" {
			t.Fatalf("sold asset cost = %s, want 600.0000 (1000 x 0.6)", got)
		}
		if got := costText(ctx, t, pool, variableQdv["QDV5.DE"]); got != "1300.0000" {
			t.Fatalf("bought asset cost = %s, want 1300.0000 (900 + 4 x 100 at market)", got)
		}
		if got := costText(ctx, t, pool, fixedQdv["QDV5.DE"]); got != "174.0385" {
			t.Fatalf("FIXED QDV5.DE cost = %s, want the untouched 174.0385", got)
		}
		if got := unitsText(ctx, t, pool, fixedQdv["QDV5.DE"]); !got.Equal(decimal.NewFromInt(5)) {
			t.Fatalf("FIXED QDV5.DE units = %s, want the untouched 5", got)
		}
	})

	t.Run("sale of a NULL-cost sibling scales the row carrying the cost", func(t *testing.T) {
		reset(t)
		gold := seedAsset(ctx, t, pool, "VARIABLE", "gold", 100, []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "SIB", units: "10"},
		}) // V = 2 000, C = 1 000
		other := seedAsset(ctx, t, pool, "VARIABLE", "other", 100, []subSpec{{ticker: "OTHER", units: "10", cost: new("500"), primary: true}})
		if err := transfer("VARIABLE", leg("SIB", 5, 100), leg("OTHER", 5, 100)); err != nil { // S = 500, f = 0.25
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if got := costText(ctx, t, pool, gold["HEAD"]); got != "750.0000" {
			t.Fatalf("head cost = %s, want 750.0000 (1000 x 0.75)", got)
		}
		if got := costText(ctx, t, pool, gold["SIB"]); got != "NULL" {
			t.Fatalf("sibling cost = %s, want NULL", got)
		}
		if got := costText(ctx, t, pool, other["OTHER"]); got != "1000.0000" {
			t.Fatalf("other cost = %s, want 1000.0000 (500 + 5 x 100)", got)
		}
	})

	t.Run("same-asset swap (sell a subasset, buy the primary of the SAME asset) carries the cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "VARIABLE", "bitcoin", 100, []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true}, // baseline on the head
			{ticker: "ETN", units: "10"},                                    // NULL cost
		}) // V = 2 000, C = 1 000
		if err := transfer("VARIABLE", leg("ETN", 5, 100), leg("HEAD", 5, 100)); err != nil { // S = 500, f = 0.25
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		// head: 1000 x 0.75 = 750 plus the carried 250 = 1000 - the asset's cost is unchanged (a
		// market-priced buy would have produced 1500).
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("head cost = %s, want 1000.0000", got)
		}
		if got := costText(ctx, t, pool, ids["ETN"]); got != "NULL" {
			t.Fatalf("ETN cost = %s, want NULL", got)
		}
		if got := unitsText(ctx, t, pool, ids["HEAD"]); !got.Equal(decimal.NewFromInt(15)) {
			t.Fatalf("head units = %s, want 15", got)
		}
	})

	t.Run("same-asset swap with cost on both rows moves the carried cost to the bought row", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "FIXED", "bitcoin", 100, []subSpec{
			{ticker: "HEAD", units: "10", cost: new("400"), primary: true},
			{ticker: "ETN", units: "10", cost: new("400")},
		})
		if err := transfer("FIXED", leg("ETN", 5, 100), leg("HEAD", 5, 100)); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if h, e := costText(ctx, t, pool, ids["HEAD"]), costText(ctx, t, pool, ids["ETN"]); h != "500.0000" || e != "300.0000" {
			t.Fatalf("head/ETN cost = %s/%s, want 500.0000/300.0000 (sum 800 unchanged)", h, e)
		}
	})

	t.Run("full sale of an asset leaves exactly 0 and a later rebuy starts fresh", func(t *testing.T) {
		reset(t)
		x := seedAsset(ctx, t, pool, "VARIABLE", "x", 100, []subSpec{{ticker: "XHEAD", units: "10", cost: new("777.7777"), primary: true}})
		y := seedAsset(ctx, t, pool, "VARIABLE", "y", 100, []subSpec{{ticker: "YHEAD", units: "10", cost: new("500"), primary: true}})
		if err := transfer("VARIABLE", leg("XHEAD", 10, 100), leg("YHEAD", 10, 100)); err != nil {
			t.Fatal(err)
		}
		if got := costText(ctx, t, pool, x["XHEAD"]); got != "0.0000" {
			t.Fatalf("XHEAD cost after selling the whole asset = %s, want exactly 0.0000", got)
		}
		if got := costText(ctx, t, pool, y["YHEAD"]); got != "1500.0000" {
			t.Fatalf("YHEAD cost = %s, want 1500.0000 (500 + 10 x 100 at market)", got)
		}
		// rebuy X out of Y: X holds no units, so the buy starts a fresh cost = the paid amount
		if err := transfer("VARIABLE", leg("YHEAD", 3, 100), leg("XHEAD", 3, 100)); err != nil {
			t.Fatal(err)
		}
		if got := costText(ctx, t, pool, x["XHEAD"]); got != "300.0000" {
			t.Fatalf("XHEAD cost after the rebuy = %s, want 300.0000 (fresh start)", got)
		}
	})

	t.Run("a failure after the writes rolls back units and cost", func(t *testing.T) {
		reset(t)
		btc := seedAsset(ctx, t, pool, "VARIABLE", "btc", 100, []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "ETN", units: "10"},
		})
		eth := seedAsset(ctx, t, pool, "VARIABLE", "eth", 100, []subSpec{{ticker: "ETHHEAD", units: "10", cost: new("900"), primary: true}})
		// The audit rows are inserted after the unit/cost writes; with the table gone that insert fails.
		if _, err := pool.Exec(ctx, `DROP TABLE position_transfers`); err != nil {
			t.Fatal(err)
		}
		if err := transfer("VARIABLE", leg("ETN", 5, 100), leg("ETHHEAD", 5, 100)); err == nil {
			t.Fatal("expected the missing position_transfers table to fail the transfer")
		}
		if got := costText(ctx, t, pool, btc["HEAD"]); got != "1000.0000" {
			t.Fatalf("btc head cost = %s, want the rolled-back 1000.0000", got)
		}
		if got := costText(ctx, t, pool, eth["ETHHEAD"]); got != "900.0000" {
			t.Fatalf("eth cost = %s, want the rolled-back 900.0000", got)
		}
		if got := unitsText(ctx, t, pool, btc["ETN"]); !got.Equal(decimal.NewFromInt(10)) {
			t.Fatalf("ETN units = %s, want the rolled-back 10", got)
		}
	})
}
