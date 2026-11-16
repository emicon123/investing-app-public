package historia

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/historiaquery"
	scsnapshot "investing-app/common-go/siblingcore/snapshot"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestHistoria_RealPostgres exercises the historia read path plus the snapshot
// write path against a real postgres container, migrated by portfolioC's own Flyway
// history (this app's own database — never mock the database). Covers:
// portfolio-scoped list round-trip with returnPct/profitPln and the extra
// portfolio field, the inflation-adjusted deposits break-even line plus the 2026-07
// INFLATION_BASE annualized value, per-portfolio snapshot upserts (insert + same-day update-in-place), and
// the read-only boundary on price_cache/fx_rate_cache (untouched — this phase
// adds no cache write path, task 144).
func TestHistoria_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, assets, deposits, inflation, price_cache, fx_rate_cache, purchase_lots, position_transfers RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedReal(t, ctx, pool)

	svc := historiaquery.NewService(NewAdapter(pool), Config())
	variable := "VARIABLE"

	t.Run("list is scoped to the requested portfolio", func(t *testing.T) {
		page, err := svc.List(ctx, historiaquery.ListParams{Portfolio: &variable})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 {
			t.Fatalf("expected total 1, got %+v", page)
		}
		first := page.Items[0]
		if first.Portfolio != "VARIABLE" {
			t.Fatalf("expected portfolio VARIABLE, got %q", first.Portfolio)
		}
		if got := first.ReturnPct.InexactFloat64(); got != 0.25 {
			t.Fatalf("expected returnPct 0.25, got %v", got)
		}
		if got := first.ProfitPln.InexactFloat64(); got != 250 {
			t.Fatalf("expected profitPln 250, got %v", got)
		}
	})

	t.Run("enrichment inflates each deposit from its own month", func(t *testing.T) {
		page, err := svc.List(ctx, historiaquery.ListParams{Portfolio: &variable})
		if err != nil {
			t.Fatal(err)
		}
		e := page.Items[0]
		if e.InflationAdjustedDeposits == nil {
			t.Fatalf("expected inflation-adjusted deposits, got %+v", e)
		}
		// The VARIABLE deposit of 1000 PLN on 2026-07-10 compounds only 2026-08's CPI
		// (its own month is exclusive; 100.2): 1000 × 1.002 = 1002. FIXED's deposit
		// never leaks into the VARIABLE line.
		if got := e.InflationAdjustedDeposits.StringFixed(2); got != "1002.00" {
			t.Fatalf("expected break-even 1002.00, got %s", got)
		}
		if e.RealReturnPct == nil || e.AnnualizedInflationPct == nil {
			t.Fatalf("expected all three enrichments, got %+v", e)
		}
		// One month since the 2026-07 base (<12): the raw cumulative, 100.2/100-1 = 0.002
		// — this is what pins portfolioC's INFLATION_BASE now that cumulative is not served.
		if got := e.AnnualizedInflationPct.StringFixed(4); got != "0.0020" {
			t.Fatalf("expected annualized 0.0020 (base 2026-07), got %s", got)
		}
	})

	t.Run("snapshots upsert per portfolio", func(t *testing.T) {
		snap := scsnapshot.NewSnapshotter(book.Config, store.New(pool), func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) })
		if err := snap.SnapshotPortfolio(ctx, "VARIABLE"); err != nil {
			t.Fatal(err)
		}
		if err := snap.SnapshotPortfolio(ctx, "FIXED"); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2026-08-20'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("expected 2 snapshot rows (one per portfolio), got %d", n)
		}
		var variableValue, fixedValue float64
		if err := pool.QueryRow(ctx, `SELECT portfolio_value FROM historia WHERE snapshot_date = '2026-08-20' AND portfolio = 'VARIABLE'`).Scan(&variableValue); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT portfolio_value FROM historia WHERE snapshot_date = '2026-08-20' AND portfolio = 'FIXED'`).Scan(&fixedValue); err != nil {
			t.Fatal(err)
		}
		// VARIABLE: 10u × 100 PLN = 1000. FIXED: 5u × 10 EUR × 4.3 = 215.
		if variableValue != 1000 || fixedValue != 215 {
			t.Fatalf("expected 1000/215, got %v/%v", variableValue, fixedValue)
		}
		if err := snap.SnapshotPortfolio(ctx, "VARIABLE"); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2026-08-20' AND portfolio = 'VARIABLE'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("same-day re-snapshot must update in place, got %d rows", n)
		}
	})

	t.Run("BackfillSnapshot targets the explicit date, not today (ADR-097)", func(t *testing.T) {
		// "now" is 2026-09-19 but the backfill call targets 2026-09-18 — the row must
		// land on the requested date, never on warsawDate(s.now()).
		snap := scsnapshot.NewSnapshotter(book.Config, store.New(pool), func() time.Time { return time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC) })
		backfillDate := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
		if err := snap.BackfillSnapshot(ctx, "VARIABLE", backfillDate); err != nil {
			t.Fatal(err)
		}
		if err := snap.BackfillSnapshot(ctx, "FIXED", backfillDate); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2026-09-18'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("expected 2 backfilled rows (one per portfolio) on 2026-09-18, got %d", n)
		}
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2026-09-19'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("BackfillSnapshot must never write under today's date, got %d rows on 2026-09-19", n)
		}
		var variableValue, fixedValue float64
		if err := pool.QueryRow(ctx, `SELECT portfolio_value FROM historia WHERE snapshot_date = '2026-09-18' AND portfolio = 'VARIABLE'`).Scan(&variableValue); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT portfolio_value FROM historia WHERE snapshot_date = '2026-09-18' AND portfolio = 'FIXED'`).Scan(&fixedValue); err != nil {
			t.Fatal(err)
		}
		// Same computation as the live path: VARIABLE 10u × 100 PLN = 1000; FIXED 5u × 10 EUR × 4.3 = 215.
		if variableValue != 1000 || fixedValue != 215 {
			t.Fatalf("expected backfilled 1000/215 (same computation as the live path), got %v/%v", variableValue, fixedValue)
		}
	})

	t.Run("price and fx caches are read-only", func(t *testing.T) {
		var prices, fx int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM price_cache`).Scan(&prices); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM fx_rate_cache`).Scan(&fx); err != nil {
			t.Fatal(err)
		}
		if prices != 2 || fx != 1 {
			t.Fatalf("caches must be untouched by reads+snapshots, got prices=%d fx=%d", prices, fx)
		}
	})
}

func seedReal(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct)
	      VALUES ('VARIABLE','AAA','AAA fund','STOCKS','ETF',100),
	             ('FIXED','BBB','BBB fund','STOCKS','ETF',100)`)
	exec(`INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, units, yahoo_ticker, asset_id, is_primary, sort_order)
	      VALUES ('VARIABLE','AAA','AAA fund','XTB','ETF','STOCKS','PLN',10,'YT-AAA',(SELECT id FROM assets WHERE key='AAA' AND portfolio='VARIABLE'),true,1),
	             ('FIXED','BBB','BBB fund','XTB','ETF','STOCKS','EUR',5,'YT-BBB',(SELECT id FROM assets WHERE key='BBB' AND portfolio='FIXED'),true,1)`)
	exec(`INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ('YT-AAA',100,'PLN',100),('YT-BBB',10,'EUR',43)`)
	exec(`INSERT INTO fx_rate_cache (from_currency, rate_to_pln, source) VALUES ('EUR',4.3,'TEST')`)
	exec(`INSERT INTO deposits (portfolio, deposit_date, amount) VALUES ('VARIABLE','2026-07-10',1000),('FIXED','2026-07-10',400)`)
	exec(`INSERT INTO inflation (year, month, cpi_value) VALUES (2026,7,100.5),(2026,8,100.2)`)
	exec(`INSERT INTO historia (portfolio, snapshot_date, total_deposits, portfolio_value, position_values)
	      VALUES ('VARIABLE','2026-08-15',1000,1250,NULL)`)
}
