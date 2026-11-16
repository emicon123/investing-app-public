package rebalancing

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgtx"
	"investing-app/common-go/siblingcore"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"investing-app/common-go/testsupport"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

type subSpec struct {
	ticker  string
	yahoo   string
	units   string  // units_inne (the historical INNE bucket, so the IKE/IKZE buckets a Confirm writes start at 0)
	cost    *string // nil = NULL total_spent_pln
	primary bool
}

// seedAsset inserts one asset (no target) with the given subassets; returns the position ids by
// ticker.
func seedAsset(ctx context.Context, t *testing.T, pool *pgxpool.Pool, key string, subs []subSpec) map[string]int64 {
	t.Helper()
	var assetID int64
	if err := pool.QueryRow(ctx, `INSERT INTO assets (key, name, exposure, asset_class)
		VALUES ($1::text, $1::text, 'STOCKS', 'ETF') RETURNING id`, key).Scan(&assetID); err != nil {
		t.Fatalf("insert asset %s: %v", key, err)
	}
	ids := map[string]int64{}
	for _, s := range subs {
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker,
				units_ike_tata, units_ike_mama, units_ikze_tata, units_ikze_mama, units_inne,
				total_spent_pln, asset_id, is_primary)
			VALUES ($1::text, $1::text, 'XTB', 'ETF', 'STOCKS', 'PLN', $2::text, 0, 0, 0, 0, $3::numeric, $4::numeric, $5, $6)
			RETURNING id`, s.ticker, s.yahoo, s.units, s.cost, assetID, s.primary).Scan(&id); err != nil {
			t.Fatalf("insert position %s: %v", s.ticker, err)
		}
		ids[s.ticker] = id
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

func unitsText(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64, col string) decimal.Decimal {
	t.Helper()
	var u string
	if err := pool.QueryRow(ctx, `SELECT `+col+`::text FROM positions WHERE id = $1`, id).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return decimal.RequireFromString(u)
}

// TestConfirm_AssetLevelCost_RealPostgres is design 161 §7 I1 (+ rule 7) for portfolioB's purchase
// confirm (task 161a-3): a buy on an asset with NO recorded cost that holds units leaves the cost
// NULL (portfolioB's live shape - every position is NULL today), a buy on a tracked asset adds exactly
// the PLN paid to the bought row, a buy on an empty asset initializes it, and a failure after the
// writes rolls units AND cost back. Real Postgres, no DB mocks.
func TestConfirm_AssetLevelCost_RealPostgres(t *testing.T) {
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

	// The confirm reads price_cache inside its serialised transaction (ADR-111), so the prices
	// are seeded there.
	if _, err := pool.Exec(ctx, `INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES
		('SXRV.DE', 1000, 'PLN', 1000), ('ETFBNDXPL.WA', 50, 'PLN', 50), ('NQSE.DE', 20, 'PLN', 20),
		('ETNVIRBTCP.WA', 30, 'PLN', 30), ('BTC-USD', 400000, 'PLN', 400000), ('ETFBTCPL.WA', 10, 'PLN', 10),
		('HEAD.Y', 50, 'PLN', 50), ('SIB.Y', 20, 'PLN', 20), ('EMPTY.Y', 20, 'PLN', 20)`); err != nil {
		t.Fatal(err)
	}
	q := store.New(pool)
	svc := screbalancing.NewService(book.Config, q, pgtx.Runner(pool, store.Bind), q, NewFunding(), nil)
	reset := func(t *testing.T) {
		t.Helper()
		if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, purchase_lots CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	type acct struct {
		bucket siblingcore.Bucket
		units  decimal.Decimal
	}
	item := func(ticker string, allocs ...acct) screbalancing.ConfirmItem {
		var units siblingcore.Units
		for _, a := range allocs {
			units = units.Add(a.bucket, a.units)
		}
		return screbalancing.ConfirmItem{Ticker: ticker, Units: units}
	}
	alloc := func(account siblingcore.Bucket, units int64) acct { return acct{account, decimal.NewFromInt(units)} }

	t.Run("live shape - buys on NULL-cost assets that hold units leave every cost NULL", func(t *testing.T) {
		reset(t)
		nasdaq := seedAsset(ctx, t, pool, "nasdaq-100", []subSpec{
			{ticker: "SXRV.DE", yahoo: "SXRV.DE", units: "0.1694", primary: true},
			{ticker: "ETFBNDXPL", yahoo: "ETFBNDXPL.WA", units: "4.931"},
			{ticker: "NQSE", yahoo: "NQSE.DE", units: "176"},
		})
		bitcoin := seedAsset(ctx, t, pool, "bitcoin", []subSpec{
			{ticker: "ETNVIRBTCP", yahoo: "ETNVIRBTCP.WA", units: "1248", primary: true},
			{ticker: "BTC", yahoo: "BTC-USD", units: "0.05209"},
			{ticker: "ETFBTCPL", yahoo: "ETFBTCPL.WA", units: "166"},
		})
		if err := svc.Confirm(ctx, screbalancing.ConfirmRequest{Items: []screbalancing.ConfirmItem{
			item("ETFBNDXPL", alloc(book.IkeTata, 10)),
			item("ETNVIRBTCP", alloc(book.IkeTata, 5), alloc(book.IkzeMama, 3)),
		}}); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		for _, ids := range []map[string]int64{nasdaq, bitcoin} {
			for ticker, id := range ids {
				if got := costText(ctx, t, pool, id); got != "NULL" {
					t.Fatalf("%s cost = %s, want NULL (rule 7: no cost invented for an asset with unknown baseline)", ticker, got)
				}
			}
		}
		if got := unitsText(ctx, t, pool, nasdaq["ETFBNDXPL"], "units_ike_tata"); !got.Equal(decimal.NewFromInt(10)) {
			t.Fatalf("ETFBNDXPL units_ike_tata = %s, want 10 (units still applied)", got)
		}
		if got := unitsText(ctx, t, pool, bitcoin["ETNVIRBTCP"], "units_ikze_mama"); !got.Equal(decimal.NewFromInt(3)) {
			t.Fatalf("ETNVIRBTCP units_ikze_mama = %s, want 3", got)
		}
	})

	t.Run("buy on a tracked asset adds exactly the paid amount to the bought row", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "gold", []subSpec{
			{ticker: "HEAD", yahoo: "HEAD.Y", units: "10", cost: new("1000"), primary: true},
			{ticker: "SIB", yahoo: "SIB.Y", units: "5"},
		})
		if err := svc.Confirm(ctx, screbalancing.ConfirmRequest{Items: []screbalancing.ConfirmItem{
			item("HEAD", alloc(book.IkeTata, 2), alloc(book.IkzeTata, 1)), // 3 x 50 = 150
			item("SIB", alloc(book.IkeMama, 4)),                           // 4 x 20 = 80
		}}); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1150.0000" {
			t.Fatalf("head cost = %s, want 1150.0000 (1000 + 3 x 50)", got)
		}
		if got := costText(ctx, t, pool, ids["SIB"]); got != "80.0000" {
			t.Fatalf("sibling cost = %s, want 80.0000 (4 x 20 on a NULL row of a tracked asset)", got)
		}
	})

	t.Run("buy on an empty asset initializes the cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "new-asset", []subSpec{{ticker: "EMPTY", yahoo: "EMPTY.Y", units: "0", primary: true}})
		if err := svc.Confirm(ctx, screbalancing.ConfirmRequest{Items: []screbalancing.ConfirmItem{item("EMPTY", alloc(book.IkeTata, 10))}}); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got := costText(ctx, t, pool, ids["EMPTY"]); got != "200.0000" {
			t.Fatalf("cost = %s, want 200.0000 (10 x 20)", got)
		}
	})

	t.Run("a buy with no price writes units but no cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "unpriced", []subSpec{{ticker: "NOPRICE", yahoo: "NOPRICE.Y", units: "5", cost: new("100"), primary: true}})
		if err := svc.Confirm(ctx, screbalancing.ConfirmRequest{Items: []screbalancing.ConfirmItem{item("NOPRICE", alloc(book.IkeTata, 10))}}); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got := costText(ctx, t, pool, ids["NOPRICE"]); got != "100.0000" {
			t.Fatalf("cost = %s, want the untouched 100.0000 (no derivable paid amount)", got)
		}
	})

	t.Run("a failure after the writes rolls back units and cost", func(t *testing.T) {
		reset(t)
		ids := seedAsset(ctx, t, pool, "gold", []subSpec{{ticker: "HEAD", yahoo: "HEAD.Y", units: "10", cost: new("1000"), primary: true}})
		// Confirm's last steps deduct account cash then record the deposit; with no cash row the
		// deduction fails AFTER the position delta (units + cost) and the lot were written.
		if _, err := pool.Exec(ctx, `DELETE FROM account_cash_snapshot`); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := pool.Exec(ctx, `INSERT INTO account_cash_snapshot (id) VALUES (1) ON CONFLICT DO NOTHING`); err != nil {
				t.Fatal(err)
			}
		}()
		if err := svc.Confirm(ctx, screbalancing.ConfirmRequest{Items: []screbalancing.ConfirmItem{item("HEAD", alloc(book.IkeTata, 2))}}); err == nil {
			t.Fatal("expected the missing cash snapshot to fail the confirm")
		}
		if got := costText(ctx, t, pool, ids["HEAD"]); got != "1000.0000" {
			t.Fatalf("cost = %s, want the rolled-back 1000.0000", got)
		}
		if got := unitsText(ctx, t, pool, ids["HEAD"], "units_ike_tata"); !got.IsZero() {
			t.Fatalf("units_ike_tata = %s, want the rolled-back 0", got)
		}
		var lots int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM purchase_lots`).Scan(&lots); err != nil {
			t.Fatal(err)
		}
		if lots != 0 {
			t.Fatalf("purchase_lots rows = %d, want 0 after the rollback", lots)
		}
	})
}
