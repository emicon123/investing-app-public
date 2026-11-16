package historia

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// TestHistoria_RealPostgres exercises the historia read/write path against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in
// prod — the "never mock the database" gate. Covers: ALL-merge pagination against
// real rows, upsert ON CONFLICT (update-in-place, created_at preserved), JSONB
// round-trip plus legacy NULL, Warsaw-vs-UTC snapshot dating, and the read-only
// boundary on positions/deposits/inflation (their row counts are unchanged by
// snapshots — Phase 7 owns positions writes, and this phase must never add one).
//
// FX comes from a stub map, not the network: hermetic tests must not call Yahoo.
// The DB-interaction surface (the gate's actual subject) is fully real.
func TestHistoria_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, assets, deposits, inflation, price_cache RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedReal(t, ctx, pool)

	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
	fx := stubFx{rates: map[string]decimal.Decimal{"USD": decimal.RequireFromString("4")}}
	newSvc := func() *Service { return NewService(queries, transact, fx) }

	countOf := func(table string) int64 {
		var n int64
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	posBefore, depBefore, infBefore := countOf("positions"), countOf("deposits"), countOf("inflation")

	t.Run("ALL merge paginates in date order", func(t *testing.T) {
		page, err := newSvc().List(ctx, ListParams{})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 || len(page.Items) != 4 {
			t.Fatalf("expected merged total 4, got %+v", page)
		}
		for i := 1; i < len(page.Items); i++ {
			if page.Items[i].SnapshotDate.Before(page.Items[i-1].SnapshotDate) {
				t.Fatalf("expected date ASC, got %v before %v",
					page.Items[i-1].SnapshotDate, page.Items[i].SnapshotDate)
			}
		}
		sized := 2
		paged, err := newSvc().List(ctx, ListParams{Page: new(1), Size: &sized})
		if err != nil {
			t.Fatal(err)
		}
		if paged.Total != 4 || len(paged.Items) != 2 || paged.Number != 1 {
			t.Fatalf("expected page 1/2 total 4, got %+v", paged)
		}
		empty, err := newSvc().List(ctx, ListParams{Page: new(9), Size: &sized})
		if err != nil {
			t.Fatal(err)
		}
		if len(empty.Items) != 0 || empty.Total != 4 {
			t.Fatalf("expected empty data with total 4, got %+v", empty)
		}
	})

	t.Run("returnPct and profitPln computed on real rows", func(t *testing.T) {
		page, err := newSvc().List(ctx, ListParams{Portfolio: new("MAIN")})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 {
			t.Fatalf("expected 2 MAIN rows, got %+v", page)
		}
		first := page.Items[0] // 2024-01-10: 1000 → 1100
		if first.ReturnPct.String() != "0.1" || first.ProfitPln.String() != "100" {
			t.Fatalf("expected 0.1/100, got %s/%s", first.ReturnPct, first.ProfitPln)
		}
	})

	t.Run("JSONB round-trips, legacy NULL stays null", func(t *testing.T) {
		page, err := newSvc().List(ctx, ListParams{Portfolio: new("MAIN")})
		if err != nil {
			t.Fatal(err)
		}
		if pv := page.Items[0].PositionValues; pv != nil {
			t.Fatalf("expected nil breakdown for legacy row, got %v", pv)
		}
		pv := page.Items[1].PositionValues // 2024-01-11
		if pv == nil || pv["VUA"].String() != "4000" {
			t.Fatalf("expected {VUA:4000} round-trip, got %v", pv)
		}
	})

	t.Run("snapshot upserts ON CONFLICT, preserves created_at", func(t *testing.T) {
		svc := newSvc()
		svc.now = func() time.Time { return mustDate("2024-03-01") }

		if err := svc.SnapshotPortfolio(ctx, "MAIN"); err != nil {
			t.Fatal(err)
		}
		row, err := queries.GetHistoriaByDatePortfolio(ctx, sqlcgen.GetHistoriaByDatePortfolioParams{
			SnapshotDate: pgnum.Date(mustDate("2024-03-01")), Portfolio: sqlcgen.PortfolioEnumMAIN,
		})
		if err != nil {
			t.Fatalf("snapshot row missing: %v", err)
		}
		if val, _ := pgnum.ToDecimal(row.PortfolioValue); val.String() != "4500" {
			t.Fatalf("expected 10*400 + companion 5*100 = 4500, got %s", val)
		}
		if dep, _ := pgnum.ToDecimal(row.TotalDeposits); dep.String() != "1400" {
			t.Fatalf("expected EXTERNAL 1000 + 100*4 = 1400 (INTERNAL excluded), got %s", dep)
		}
		if !row.Xirr.Valid {
			t.Fatal("expected a computed xirr, got NULL")
		}
		created := row.CreatedAt.Time

		// Second run same day updates in place — no duplicate, created_at untouched.
		if _, err := pool.Exec(ctx, `UPDATE price_cache SET price_pln = 410 WHERE ticker = 'VUA.WA'`); err != nil {
			t.Fatal(err)
		}
		if err := svc.SnapshotPortfolio(ctx, "MAIN"); err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM historia WHERE snapshot_date = '2024-03-01' AND portfolio = 'MAIN'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("expected upsert-in-place (1 row), got %d", n)
		}
		row2, err := queries.GetHistoriaByDatePortfolio(ctx, sqlcgen.GetHistoriaByDatePortfolioParams{
			SnapshotDate: pgnum.Date(mustDate("2024-03-01")), Portfolio: sqlcgen.PortfolioEnumMAIN,
		})
		if err != nil {
			t.Fatal(err)
		}
		if val, _ := pgnum.ToDecimal(row2.PortfolioValue); val.String() != "4600" {
			t.Fatalf("expected updated 10*410+500=4600, got %s", val)
		}
		if !row2.CreatedAt.Time.Equal(created) {
			t.Fatalf("created_at must survive the upsert (got %v, was %v)", row2.CreatedAt.Time, created)
		}
	})

	t.Run("snapshot date follows Warsaw, not UTC", func(t *testing.T) {
		loc, err := time.LoadLocation("Europe/Warsaw")
		if err != nil {
			t.Skip("Europe/Warsaw zone data unavailable")
		}
		svc := newSvc()
		svc.now = func() time.Time { return time.Date(2026, 1, 1, 0, 30, 0, 0, loc) }
		if err := svc.SnapshotPortfolio(ctx, "RETIREMENT"); err != nil {
			t.Fatal(err)
		}
		var date string
		if err := pool.QueryRow(ctx, `SELECT snapshot_date::text FROM historia WHERE portfolio = 'RETIREMENT' ORDER BY snapshot_date DESC LIMIT 1`).Scan(&date); err != nil {
			t.Fatal(err)
		}
		if date != "2026-01-01" {
			t.Fatalf("expected Warsaw date 2026-01-01, got %s", date)
		}
	})

	t.Run("positions, deposits and inflation are read-only", func(t *testing.T) {
		if got := countOf("positions"); got != posBefore {
			t.Fatalf("positions changed by snapshots: %d → %d", posBefore, got)
		}
		if got := countOf("deposits"); got != depBefore {
			t.Fatalf("deposits changed by snapshots: %d → %d", depBefore, got)
		}
		if got := countOf("inflation"); got != infBefore {
			t.Fatalf("inflation changed by snapshots: %d → %d", infBefore, got)
		}
	})
}

func seedReal(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}
	// Positions (task 156): MAIN asset = primary + a non-primary subasset folding into it, RETIREMENT single row.
	exec(`INSERT INTO assets (portfolio, key, name, exposure, asset_class) VALUES
		('MAIN', 'vua', 'VUA asset', 'EQUITY', 'STOCK'), ('RETIREMENT', 'ZBND', 'ZBND', 'BONDS', 'STOCK')`)
	exec(`INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency,
		units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
		asset_id, is_primary, yahoo_ticker)
		SELECT 'VUA', 'VUA', 'MAIN', 'XTB', 'STOCK', 'EQUITY', 'USD', 0, 0, 0, 10, 0, id, true, 'VUA.WA'
		FROM assets WHERE key = 'vua' AND portfolio = 'MAIN'`)
	exec(`INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency,
		units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
		asset_id, is_primary, yahoo_ticker)
		SELECT 'VUA-COMP', 'VUA-COMP', 'MAIN', 'XTB', 'STOCK', 'EQUITY', 'USD', 5, 0, 0, 0, 0, id, false, 'VUA2.WA'
		FROM assets WHERE key = 'vua' AND portfolio = 'MAIN'`)
	exec(`INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency,
		units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
		asset_id, is_primary, yahoo_ticker)
		SELECT 'ZBND', 'ZBND', 'RETIREMENT', 'XTB', 'STOCK', 'BONDS', 'PLN', 0, 0, 0, 0, 20, id, true, 'ZBND.WA'
		FROM assets WHERE key = 'ZBND' AND portfolio = 'RETIREMENT'`)
	exec(`INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES
		('VUA.WA', 100, 'USD', 400), ('VUA2.WA', 25, 'USD', 100), ('ZBND.WA', 50, 'PLN', 50)`)
	// Deposits: two EXTERNAL (one USD) + one INTERNAL_TRANSFER that must be ignored.
	exec(`INSERT INTO deposits (deposit_date, amount, currency, exchange, account_type, portfolio, capital_source) VALUES
		('2022-01-01', 1000, 'PLN', 'XTB', 'REGULAR', 'MAIN', 'EXTERNAL'),
		('2023-01-01', 100, 'USD', 'XTB', 'REGULAR', 'MAIN', 'EXTERNAL'),
		('2024-01-01', 999, 'PLN', 'XTB', 'REGULAR', 'MAIN', 'INTERNAL_TRANSFER'),
		('2024-01-01', 500, 'PLN', 'XTB', 'REGULAR', 'RETIREMENT', 'EXTERNAL')`)
	exec(`INSERT INTO inflation (year, month, cpi_value) VALUES (2022, 8, 100.0), (2022, 9, 101.6), (2023, 1, 100.2)`)
	// Historia rows: merge/pagination/JSONB fixtures (one legacy NULL breakdown).
	exec(`INSERT INTO historia (snapshot_date, portfolio, total_deposits, portfolio_value, xirr, position_values) VALUES
		('2024-01-10', 'MAIN', 1000, 1100, NULL, NULL),
		('2024-01-10', 'RETIREMENT', 500, 550, NULL, NULL),
		('2024-01-11', 'MAIN', 1000, 1200, 0.05, '{"VUA": 4000}'),
		('2024-01-12', 'RETIREMENT', 500, 560, NULL, NULL)`)
}
