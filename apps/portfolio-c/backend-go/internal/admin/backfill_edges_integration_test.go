package admin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/priceprovider"
	"investing-app/common-go/testsupport"
)

// errOHLC answers like stubOHLC but fails the fetch for the tickers in errs.
type errOHLC struct {
	stubOHLC
	errs map[string]bool
}

func (s errOHLC) FetchOHLC(ctx context.Context, ticker, interval string, p1, p2 int64) ([]priceprovider.OHLCPoint, error) {
	if s.errs[ticker] {
		return nil, errors.New("boom")
	}
	return s.stubOHLC.FetchOHLC(ctx, ticker, interval, p1, p2)
}

// TestBackfill_EdgeBranches_RealPostgres is the characterisation golden for the branches the
// hand-computed golden does not reach (task 185 B4, captured before the extract-step refactor):
// a row without a yahoo ticker, zero units, EUR via FX, USD without an FX series, an unsupported
// currency, missing / failing / sub-0.005 prices, a non-positive portfolio_value, a snapshot whose
// priced holdings were all bought later (empty raw), and a portfolio with nothing to fill.
func TestBackfill_EdgeBranches_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()
	exec := func(q string) {
		t.Helper()
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`TRUNCATE historia, positions, assets, purchase_lots RESTART IDENTITY CASCADE`)
	exec(`INSERT INTO assets (portfolio, key, name, exposure, asset_class) VALUES
	      ('VARIABLE','NOY','NOY','STOCKS','ETF'), ('VARIABLE','PLN1','PLN1','STOCKS','ETF'),
	      ('VARIABLE','EUR1','EUR1','STOCKS','ETF'), ('VARIABLE','USD1','USD1','STOCKS','ETF'),
	      ('VARIABLE','CHF1','CHF1','STOCKS','ETF'), ('VARIABLE','MISS','MISS','STOCKS','ETF'),
	      ('VARIABLE','TINY','TINY','STOCKS','ETF'), ('VARIABLE','ERR','ERR','STOCKS','ETF'),
	      ('VARIABLE','ZEROU','ZEROU','STOCKS','ETF'), ('FIXED','F1','F1','STOCKS','ETF')`)
	exec(`INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, units, yahoo_ticker, asset_id, is_primary, sort_order) VALUES
	      ('VARIABLE','NOY','NOY','XTB','ETF','STOCKS','PLN',5,NULL,(SELECT id FROM assets WHERE key='NOY' AND portfolio='VARIABLE'),true,1),
	      ('VARIABLE','PLN1','PLN1','XTB','ETF','STOCKS','PLN',5,'YT-P1',(SELECT id FROM assets WHERE key='PLN1' AND portfolio='VARIABLE'),true,2),
	      ('VARIABLE','EUR1','EUR1','XTB','ETF','STOCKS','EUR',10,'YT-E1',(SELECT id FROM assets WHERE key='EUR1' AND portfolio='VARIABLE'),true,3),
	      ('VARIABLE','USD1','USD1','XTB','ETF','STOCKS','USD',2,'YT-U1',(SELECT id FROM assets WHERE key='USD1' AND portfolio='VARIABLE'),true,4),
	      ('VARIABLE','CHF1','CHF1','XTB','ETF','STOCKS','CHF',1,'YT-C1',(SELECT id FROM assets WHERE key='CHF1' AND portfolio='VARIABLE'),true,5),
	      ('VARIABLE','MISS','MISS','XTB','ETF','STOCKS','PLN',1,'YT-MISS',(SELECT id FROM assets WHERE key='MISS' AND portfolio='VARIABLE'),true,6),
	      ('VARIABLE','TINY','TINY','XTB','ETF','STOCKS','PLN',1,'YT-T',(SELECT id FROM assets WHERE key='TINY' AND portfolio='VARIABLE'),true,7),
	      ('VARIABLE','ERR','ERR','XTB','ETF','STOCKS','PLN',1,'YT-ERR',(SELECT id FROM assets WHERE key='ERR' AND portfolio='VARIABLE'),true,8),
	      ('VARIABLE','ZEROU','ZEROU','XTB','ETF','STOCKS','PLN',0,'YT-Z',(SELECT id FROM assets WHERE key='ZEROU' AND portfolio='VARIABLE'),true,9),
	      ('FIXED','F1','F1','XTB','ETF','STOCKS','PLN',3,'YT-F1',(SELECT id FROM assets WHERE key='F1' AND portfolio='FIXED'),true,1)`)
	exec(`INSERT INTO purchase_lots (portfolio, session_id, purchased_at, ticker, units, price_pln, cost_pln) VALUES
	      ('VARIABLE',gen_random_uuid(),'2026-07-25','PLN1',5,10,50), ('VARIABLE',gen_random_uuid(),'2026-07-25','EUR1',10,8.6,86)`)
	exec(`INSERT INTO historia (portfolio, snapshot_date, total_deposits, portfolio_value, position_values) VALUES
	      ('VARIABLE','2026-07-20',100,500,NULL), ('VARIABLE','2026-08-01',100,1000,NULL), ('VARIABLE','2026-08-03',100,0,NULL),
	      ('FIXED','2026-08-01',100,21,'{"F1":21}')`)

	svc := NewService(pool, errOHLC{errs: map[string]bool{"YT-ERR": true}, stubOHLC: stubOHLC{series: map[string][]priceprovider.OHLCPoint{
		"YT-P1":    {pt("2026-07-31", 10)},
		"YT-E1":    {pt("2026-07-31", 2)},
		"YT-U1":    {pt("2026-07-31", 3)},
		"YT-C1":    {pt("2026-07-31", 1)},
		"YT-T":     {pt("2026-07-31", 0.001)},
		"YT-F1":    {pt("2026-07-31", 7)},
		"EURPLN=X": {pt("2026-07-31", 4.3)},
	}}})
	result, err := svc.Backfill(ctx)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `SELECT portfolio::text || ' ' || snapshot_date::text, COALESCE(position_values::text, 'NULL')
		FROM historia ORDER BY portfolio, snapshot_date`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		got = append(got, k+" "+v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	const wantRows = `VARIABLE 2026-07-20 NULL
VARIABLE 2026-08-01 {"EUR1": 632.35, "PLN1": 367.65}
VARIABLE 2026-08-03 NULL
FIXED 2026-08-01 {"F1": 21}`
	// Positions are read in ticker order (task 187 B5c), so the per-ticker warnings follow it.
	const wantWarnings = `no yahoo data for YT-ERR: boom
no yahoo data for YT-MISS
no yahoo data for YT-Z
no fx for CHF
VARIABLE 2026-07-20 no price for YT-ERR
VARIABLE 2026-07-20 no price for YT-MISS
VARIABLE 2026-07-20 no fx USDPLN=X
VARIABLE 2026-07-20 empty raw
no fx for CHF
VARIABLE 2026-08-01 no price for YT-ERR
VARIABLE 2026-08-01 no price for YT-MISS
VARIABLE 2026-08-01 no fx USDPLN=X`
	if result.Updated != 1 || result.Skipped != 3 {
		t.Errorf("updated/skipped = %d/%d", result.Updated, result.Skipped)
	}
	if g := strings.Join(got, "\n"); g != wantRows {
		t.Errorf("rows\n got:\n%s\nwant:\n%s", g, wantRows)
	}
	if g := strings.Join(result.Warnings, "\n"); g != wantWarnings {
		t.Errorf("warnings\n got:\n%s\nwant:\n%s", g, wantWarnings)
	}
}
