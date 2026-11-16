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
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

type subSpec struct {
	ticker  string
	units   string  // units_ike_tata
	cost    *string // nil = NULL total_spent_pln
	primary bool
}

// seedAsset inserts one asset (no target) with the given subassets, yahoo ticker = ticker + ".Y",
// each priced at price PLN in price_cache; returns the position ids by ticker.
func seedAsset(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key string, price int, subs []subSpec) map[string]int64 {
	t.Helper()
	var assetID int64
	if err := pool.QueryRow(ctx, `INSERT INTO assets (key, name, exposure, asset_class)
		VALUES ($1::text, $1::text, 'STOCKS', 'ETF') RETURNING id`, key).Scan(&assetID); err != nil {
		t.Fatalf("insert asset %s: %v", key, err)
	}
	ids := map[string]int64{}
	for _, s := range subs {
		yahoo := s.ticker + ".Y"
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker,
				units_ike_tata, units_ike_mama, units_ikze_tata, units_ikze_mama, units_inne,
				total_spent_pln, asset_id, is_primary)
			VALUES ($1::text, $1::text, 'XTB', 'ETF', 'STOCKS', 'PLN', $2::text, $3::numeric, 0, 0, 0, 0, $4::numeric, $5, $6)
			RETURNING id`, s.ticker, yahoo, s.units, s.cost, assetID, s.primary).Scan(&id); err != nil {
			t.Fatalf("insert position %s: %v", s.ticker, err)
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

func unitsOf(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64, col string) decimal.Decimal {
	t.Helper()
	var u string
	if err := pool.QueryRow(ctx, `SELECT `+col+`::text FROM positions WHERE id = $1`, id).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return decimal.RequireFromString(u)
}

// TestExecuteTransfer_AssetLevelCost_RealPostgres is design 161 §7 I1/I2 for portfolioB's manual
// transfer (task 161a-3): a sale scales the sold asset's cost (also when the sold row is a
// NULL-cost sibling), an asset with NO recorded cost is left untouched by sales and buys (rule 7 -
// portfolioB's live shape), a same-asset swap CARRIES cost (detected by asset_id), a cross-asset
// transfer is independent, a full sale zeroes the cost exactly and a rebuy starts fresh, and a
// failure after the writes rolls units AND cost back. Prices come from price_cache read inside
// the transfer's transaction. Real Postgres, no DB mocks.
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
	leg := func(ticker, account string, units, price int64) sctransfer.Leg {
		return sctransfer.Leg{Ticker: ticker, Bucket: siblingcore.Bucket(account), Units: decimal.NewFromInt(units), PricePln: decimal.NewFromInt(price)}
	}

	t.Run("live shape - sale and buy on NULL-cost assets change no cost", func(t *testing.T) {
		reset(t)
		nasdaq := seedAsset(ctx, t, pool, "nasdaq-100", 100, []subSpec{
			{ticker: "SXRV.DE", units: "1", primary: true},
			{ticker: "ETFBNDXPL", units: "50"},
			{ticker: "NQSE", units: "176"},
		})
		bitcoin := seedAsset(ctx, t, pool, "bitcoin", 100, []subSpec{
			{ticker: "ETNVIRBTCP", units: "1248", primary: true},
			{ticker: "BTC", units: "1"},
			{ticker: "ETFBTCPL", units: "166"},
		})
		// cross-asset: sell NQSE, buy the Bitcoin ETN
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("NQSE", "IKE_TATA", 10, 100), Buys: []sctransfer.Leg{leg("ETNVIRBTCP", "IKE_TATA", 10, 100)},
		}); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		// same-asset swap inside the all-NULL Bitcoin asset
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("ETFBTCPL", "IKE_TATA", 20, 100), Buys: []sctransfer.Leg{leg("BTC", "IKE_TATA", 20, 100)},
		}); err != nil {
			t.Fatalf("ExecuteTransfer (swap): %v", err)
		}
		for _, ids := range []map[string]int64{nasdaq, bitcoin} {
			for ticker, id := range ids {
				if got := costText(ctx, t, pool, id); got != "NULL" {
					t.Fatalf("%s cost = %s, want NULL (nothing to reduce, nothing invented)", ticker, got)
				}
			}
		}
		if got := unitsOf(ctx, t, pool, nasdaq["NQSE"], "units_ike_tata"); !got.Equal(decimal.NewFromInt(166)) {
			t.Fatalf("NQSE units_ike_tata = %s, want 166 (units still applied)", got)
		}
		if got := unitsOf(ctx, t, pool, bitcoin["BTC"], "units_ike_tata"); !got.Equal(decimal.NewFromInt(21)) {
			t.Fatalf("BTC units_ike_tata = %s, want 21 (units still applied)", got)
		}
	})

	t.Run("sale of a NULL-cost sibling scales the row carrying the cost; the bought asset gains the paid amount", func(t *testing.T) {
		reset(t)
		gold := seedAsset(ctx, t, pool, "gold", 100, []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "SIB", units: "10"}, // NULL cost
		}) // V = 2 000, C = 1 000
		eth := seedAsset(ctx, t, pool, "eth", 100, []subSpec{{ticker: "ETHHEAD", units: "10", cost: new("900"), primary: true}})
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("SIB", "IKE_TATA", 5, 100), Buys: []sctransfer.Leg{leg("ETHHEAD", "IKE_TATA", 5, 100)}, // S = 500, f = 0.25
		}); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if got := costText(ctx, t, pool, gold["HEAD"]); got != "750.0000" {
			t.Fatalf("gold head cost = %s, want 750.0000 (1000 x 0.75)", got)
		}
		if got := costText(ctx, t, pool, gold["SIB"]); got != "NULL" {
			t.Fatalf("gold sibling cost = %s, want NULL", got)
		}
		if got := costText(ctx, t, pool, eth["ETHHEAD"]); got != "1400.0000" {
			t.Fatalf("eth cost = %s, want 1400.0000 (900 + 5 x 100 at market)", got)
		}
	})

	t.Run("same-asset swap (sell a subasset, buy the primary of the SAME asset) carries the cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "bitcoin", 100, []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true}, // go-live group cost on the head
			{ticker: "ETN", units: "10"},                                    // NULL cost
		}) // V = 2 000, C = 1 000
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("ETN", "IKE_TATA", 5, 100), Buys: []sctransfer.Leg{leg("HEAD", "IKE_TATA", 5, 100)}, // S = 500, f = 0.25
		}); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		// head: 1000 x 0.75 = 750, plus the carried 250 = 1000 - the asset's cost is unchanged (a
		// market-priced buy would have produced 1500).
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("head cost = %s, want 1000.0000", got)
		}
		if got := costText(ctx, t, pool, ids["ETN"]); got != "NULL" {
			t.Fatalf("ETN cost = %s, want NULL", got)
		}
		if got := unitsOf(ctx, t, pool, ids["HEAD"], "units_ike_tata"); !got.Equal(decimal.NewFromInt(15)) {
			t.Fatalf("head units = %s, want 15", got)
		}
	})

	t.Run("same-asset swap with cost on both rows moves the carried cost to the bought row", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "bitcoin", 100, []subSpec{
			{ticker: "HEAD", units: "10", cost: new("400"), primary: true},
			{ticker: "ETN", units: "10", cost: new("400")},
		})
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("ETN", "IKE_TATA", 5, 100), Buys: []sctransfer.Leg{leg("HEAD", "IKE_TATA", 5, 100)},
		}); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if h, e := costText(ctx, t, pool, ids["HEAD"]), costText(ctx, t, pool, ids["ETN"]); h != "500.0000" || e != "300.0000" {
			t.Fatalf("head/ETN cost = %s/%s, want 500.0000/300.0000 (sum 800 unchanged)", h, e)
		}
	})

	t.Run("same instrument moved between account buckets leaves the cost untouched", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "bitcoin", 100, []subSpec{{ticker: "HEAD", units: "10", cost: new("1000"), primary: true}})
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("HEAD", "IKE_TATA", 4, 100), Buys: []sctransfer.Leg{leg("HEAD", "IKZE_TATA", 4, 100)},
		}); err != nil {
			t.Fatalf("ExecuteTransfer: %v", err)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("cost = %s, want 1000.0000", got)
		}
		if got := unitsOf(ctx, t, pool, ids["HEAD"], "units_ikze_tata"); !got.Equal(decimal.NewFromInt(4)) {
			t.Fatalf("IKZE_TATA units = %s, want 4", got)
		}
	})

	t.Run("full sale of an asset leaves exactly 0 and a later rebuy starts fresh", func(t *testing.T) {
		reset(t)
		x := seedAsset(ctx, t, pool, "x", 100, []subSpec{
			{ticker: "XHEAD", units: "10", cost: new("777.7777"), primary: true},
			{ticker: "XSIB", units: "10"},
		})
		y := seedAsset(ctx, t, pool, "y", 100, []subSpec{{ticker: "YHEAD", units: "10", cost: new("500"), primary: true}})
		// sell every unit of X (both rows) in two transfers into Y
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("XSIB", "IKE_TATA", 10, 100), Buys: []sctransfer.Leg{leg("YHEAD", "IKE_TATA", 10, 100)},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("XHEAD", "IKE_TATA", 10, 100), Buys: []sctransfer.Leg{leg("YHEAD", "IKE_TATA", 10, 100)},
		}); err != nil {
			t.Fatal(err)
		}
		if got := costText(ctx, t, pool, x["XHEAD"]); got != "0.0000" {
			t.Fatalf("XHEAD cost after selling the whole asset = %s, want exactly 0.0000", got)
		}
		if got := costText(ctx, t, pool, x["XSIB"]); got != "NULL" {
			t.Fatalf("XSIB cost = %s, want NULL (never tracked)", got)
		}
		if got := costText(ctx, t, pool, y["YHEAD"]); got != "2500.0000" {
			t.Fatalf("YHEAD cost = %s, want 2500.0000 (500 + 2 x 1000 at market)", got)
		}
		// rebuy X out of Y: X holds no units, so the buy starts a fresh cost = the paid amount
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("YHEAD", "IKE_TATA", 3, 100), Buys: []sctransfer.Leg{leg("XHEAD", "IKE_TATA", 3, 100)},
		}); err != nil {
			t.Fatal(err)
		}
		if got := costText(ctx, t, pool, x["XHEAD"]); got != "300.0000" {
			t.Fatalf("XHEAD cost after the rebuy = %s, want 300.0000 (fresh start)", got)
		}
	})

	t.Run("a failure after the writes rolls back units and cost", func(t *testing.T) {
		reset(t)
		btc := seedAsset(ctx, t, pool, "btc", 100, []subSpec{
			{ticker: "HEAD", units: "10", cost: new("1000"), primary: true},
			{ticker: "ETN", units: "10"},
		})
		eth := seedAsset(ctx, t, pool, "eth", 100, []subSpec{{ticker: "ETHHEAD", units: "10", cost: new("900"), primary: true}})
		// The audit rows are inserted after the unit/cost writes; with the table gone that insert fails.
		if _, err := pool.Exec(ctx, `DROP TABLE position_transfers`); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
			Sold: leg("ETN", "IKE_TATA", 5, 100), Buys: []sctransfer.Leg{leg("ETHHEAD", "IKE_TATA", 5, 100)},
		}); err == nil {
			t.Fatal("expected the missing position_transfers table to fail the transfer")
		}
		if got := costText(ctx, t, pool, btc["HEAD"]); got != "1000.0000" {
			t.Fatalf("btc head cost = %s, want the rolled-back 1000.0000", got)
		}
		if got := costText(ctx, t, pool, eth["ETHHEAD"]); got != "900.0000" {
			t.Fatalf("eth cost = %s, want the rolled-back 900.0000", got)
		}
		if got := unitsOf(ctx, t, pool, btc["ETN"], "units_ike_tata"); !got.Equal(decimal.NewFromInt(10)) {
			t.Fatalf("ETN units = %s, want the rolled-back 10", got)
		}
	})
}
