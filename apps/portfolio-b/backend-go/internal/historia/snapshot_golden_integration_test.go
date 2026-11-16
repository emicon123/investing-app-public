package historia

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/siblingcore"
	scsnapshot "investing-app/common-go/siblingcore/snapshot"
	"investing-app/common-go/testsupport"
	"investing-app/common-go/testsupport/golden"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

// TestSnapshotter_Golden pins the historia rows SnapshotPortfolio and BackfillSnapshot write (task
// 180 wave 0), before the snapshotter moves into common-go/siblingcore: a fixed clock, a seeded
// book (multi-subasset asset, a CHF holding, an unpriced holding) and the stored rows compared
// with testdata/golden/snapshot_rows.json. Re-record with -update.
func TestSnapshotter_Golden(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, historia CASCADE;
		INSERT INTO assets (key, name, exposure, asset_class, target_allocation_pct) VALUES
			('HA', 'Asset A', 'STOCKS', 'ETF', 50), ('HC', 'Cash CHF', 'CURRENCY', 'CURRENCY', 30), ('HN', 'No price', 'METALS', 'METALS', 20);
		INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary, units_ike_tata, units_inne) VALUES
			('HA1', 'A primary', 'XTB', 'ETF', 'STOCKS', 'PLN', 'HA1.WA', (SELECT id FROM assets WHERE key = 'HA'), true, 5, 1),
			('HA2', 'A sibling', 'XTB', 'ETF', 'STOCKS', 'EUR', 'HA2.DE', (SELECT id FROM assets WHERE key = 'HA'), false, 2, 0),
			('HC1', 'Swiss francs', 'OTHER', 'CURRENCY', 'CURRENCY', 'CHF', NULL, (SELECT id FROM assets WHERE key = 'HC'), true, 0, 100),
			('HN1', 'Unpriced', 'OTHER', 'METALS', 'METALS', 'PLN', NULL, (SELECT id FROM assets WHERE key = 'HN'), true, 0, 3);
		INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES ('HA1.WA', 10, 'PLN', 10), ('HA2.DE', 5, 'EUR', 20);
		INSERT INTO fx_rate_cache (from_currency, rate_to_pln, source) VALUES ('CHF', 4.5, 'TEST'), ('EUR', 4.0, 'TEST');
		INSERT INTO deposits (deposit_date, amount) VALUES ('2025-01-01', 300), ('2025-06-01', 200)`); err != nil {
		t.Fatal(err)
	}

	snap := goldenSnapshotter(pool, func() time.Time { return time.Date(2026, 11, 5, 22, 30, 0, 0, time.UTC) })
	if err := snap.SnapshotPortfolio(ctx, siblingcore.WholeBook); err != nil {
		t.Fatal(err)
	}
	if err := snap.BackfillSnapshot(ctx, siblingcore.WholeBook, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	golden.Value(t, "snapshot_rows", historiaRows(t, pool))
}

// goldenSnapshotter builds the snapshotter as cmd/server/main.go does, with a fixed clock.
func goldenSnapshotter(pool *pgxpool.Pool, now func() time.Time) *scsnapshot.Snapshotter {
	return scsnapshot.NewSnapshotter(book.Config, store.New(pool), now)
}

type historiaRow struct {
	SnapshotDate, TotalDeposits, PortfolioValue string
	Xirr, PositionValues                        *string
}

func historiaRows(t *testing.T, pool *pgxpool.Pool) []historiaRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT snapshot_date::text, total_deposits::text, portfolio_value::text, xirr::text, position_values::text FROM historia ORDER BY snapshot_date`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []historiaRow
	for rows.Next() {
		var r historiaRow
		if err := rows.Scan(&r.SnapshotDate, &r.TotalDeposits, &r.PortfolioValue, &r.Xirr, &r.PositionValues); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}
