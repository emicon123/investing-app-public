package historia

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/siblingcore"
	scsnapshot "investing-app/common-go/siblingcore/snapshot"
	"investing-app/common-go/testsupport"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

// TestHistoria_RealPostgres exercises the historia read path plus the snapshot
// write path against a real postgres container, migrated by portfolioB's own Flyway
// history (this app's own database — never mock the database). Covers: list
// round-trip with returnPct/profitPln, the inflation-adjusted deposits break-even
// line with an exact value, snapshot upsert (insert + same-day update-in-place),
// and the read-only boundary on price_cache/fx_rate_cache (their row counts are
// unchanged by snapshots — this phase adds no cache write path, task 144).
func TestHistoria_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, deposits, inflation, price_cache, fx_rate_cache, purchase_lots, position_transfers RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedReal(t, ctx, pool)

	svc := historiaquery.NewService(NewAdapter(pool), Config())

	t.Run("list round-trips rows with computed fields", func(t *testing.T) {
		page, err := svc.List(ctx, historiaquery.ListParams{})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || len(page.Items) != 2 {
			t.Fatalf("expected total 2, got %+v", page)
		}
		first := page.Items[0]
		if got := first.ReturnPct.InexactFloat64(); got != 0.25 {
			t.Fatalf("expected returnPct 0.25, got %v", got)
		}
		if got := first.ProfitPln.InexactFloat64(); got != 250 {
			t.Fatalf("expected profitPln 250, got %v", got)
		}
		if first.Portfolio != "" {
			t.Fatalf("portfolioB rows carry no portfolio, got %q", first.Portfolio)
		}
		if first.PositionValues == nil || first.PositionValues["AAA"].InexactFloat64() != 1000 {
			t.Fatalf("expected position_values JSONB round-trip, got %+v", first.PositionValues)
		}
	})

	t.Run("enrichment inflates each deposit from its own month", func(t *testing.T) {
		page, err := svc.List(ctx, historiaquery.ListParams{})
		if err != nil {
			t.Fatal(err)
		}
		e := page.Items[0]
		if e.InflationAdjustedDeposits == nil {
			t.Fatalf("expected inflation-adjusted deposits, got %+v", e)
		}
		// The 1000 PLN deposit of 2024-01-10 compounds only 2024-02's CPI (its own month
		// is exclusive; 2024-02 100.3): 1000 × 1.003 = 1003 — the snapshot's value of
		// 1250 sits above it, a real gain.
		if got := e.InflationAdjustedDeposits.StringFixed(2); got != "1003.00" {
			t.Fatalf("expected break-even 1003.00, got %s", got)
		}
		if e.RealReturnPct == nil || e.AnnualizedInflationPct == nil {
			t.Fatalf("expected all three enrichments, got %+v", e)
		}
		if e.RealReturnPct.Sign() <= 0 {
			t.Fatalf("1250 above the 1003 break-even must be a positive real return, got %s", e.RealReturnPct)
		}
	})

	t.Run("snapshot upserts then updates in place", func(t *testing.T) {
		snap := scsnapshot.NewSnapshotter(book.Config, store.New(pool), func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) })
		if err := snap.SnapshotPortfolio(ctx, siblingcore.WholeBook); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2026-08-20'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("expected 1 snapshot row, got %d", n)
		}
		var value float64
		if err := pool.QueryRow(ctx, `SELECT portfolio_value FROM historia WHERE snapshot_date = '2026-08-20'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		// AAA 10u × 100 PLN + BBB 5u × 10 EUR × 4.3 = 1000 + 215 = 1215.
		if value != 1215 {
			t.Fatalf("expected snapshot value 1215, got %v", value)
		}
		if err := snap.SnapshotPortfolio(ctx, siblingcore.WholeBook); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2026-08-20'`).Scan(&n); err != nil {
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
		if err := snap.BackfillSnapshot(ctx, siblingcore.WholeBook, backfillDate); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2026-09-18'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("expected 1 backfilled row on 2026-09-18, got %d", n)
		}
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2026-09-19'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("BackfillSnapshot must never write under today's date, got %d rows on 2026-09-19", n)
		}
		var value float64
		if err := pool.QueryRow(ctx, `SELECT portfolio_value FROM historia WHERE snapshot_date = '2026-09-18'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		// Same computation as the live path: AAA 10u × 100 PLN + BBB 5u × 10 EUR × 4.3 = 1215.
		if value != 1215 {
			t.Fatalf("expected backfilled value 1215 (same computation as the live path), got %v", value)
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
	exec(`INSERT INTO assets (key, name, exposure, asset_class)
	      VALUES ('AAA','AAA fund','STOCKS','ETF'),
	             ('BBB','BBB fund','STOCKS','ETF')`)
	exec(`INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, units_ike_tata, yahoo_ticker, asset_id, is_primary, sort_order)
	      VALUES ('AAA','AAA fund','XTB','ETF','STOCKS','PLN',10,'YT-AAA',(SELECT id FROM assets WHERE key='AAA'),true,1),
	             ('BBB','BBB fund','XTB','ETF','STOCKS','EUR',5,'YT-BBB',(SELECT id FROM assets WHERE key='BBB'),true,2)`)
	exec(`INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ('YT-AAA',100,'PLN',100),('YT-BBB',10,'EUR',43)`)
	exec(`INSERT INTO fx_rate_cache (from_currency, rate_to_pln, source) VALUES ('EUR',4.3,'TEST')`)
	exec(`INSERT INTO deposits (deposit_date, amount) VALUES ('2024-01-10',1000)`)
	exec(`INSERT INTO inflation (year, month, cpi_value) VALUES (2024,1,100.4),(2024,2,100.3)`)
	exec(`INSERT INTO historia (snapshot_date, total_deposits, portfolio_value, position_values)
	      VALUES ('2024-02-15',1000,1250,'{"AAA":1000}'),('2024-03-15',2000,2400,NULL)`)
}
