package admin

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/testsupport"
	"portfolio-b/backend-go/db/sqlcgen"
)

// ListBackfillPositions answers in ticker order whatever order the rows were written and updated
// in (task 187 B5c): the backfill plugs rounding into the last priced row and lists warnings per
// position, so a heap-order read made both depend on row history.
func TestListBackfillPositions_OrderedByTicker(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, stmt := range []string{
		`TRUNCATE historia, positions, assets, purchase_lots, position_transfers RESTART IDENTITY`,
		`INSERT INTO assets (key, name, exposure, asset_class) VALUES ('ZZZ','ZZZ','STOCKS','ETF'), ('MMM','MMM','STOCKS','ETF'), ('AAA','AAA','STOCKS','ETF')`,
		`INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary, sort_order) VALUES
		 ('ZZZ','ZZZ','XTB','ETF','STOCKS','PLN','Z',(SELECT id FROM assets WHERE key='ZZZ'),true,1),
		 ('MMM','MMM','XTB','ETF','STOCKS','PLN','M',(SELECT id FROM assets WHERE key='MMM'),true,2),
		 ('AAA','AAA','XTB','ETF','STOCKS','PLN','A',(SELECT id FROM assets WHERE key='AAA'),true,3)`,
		`UPDATE positions SET name = 'AAA renamed' WHERE ticker = 'ZZZ'`, // moves ZZZ's tuple
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("%q: %v", stmt, err)
		}
	}

	rows, err := sqlcgen.New(pool).ListBackfillPositions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var tickers []string
	for _, r := range rows {
		tickers = append(tickers, r.Ticker)
	}
	if want := []string{"AAA", "MMM", "ZZZ"}; !slices.Equal(tickers, want) {
		t.Errorf("ListBackfillPositions order = %v, want %v", tickers, want)
	}
}
