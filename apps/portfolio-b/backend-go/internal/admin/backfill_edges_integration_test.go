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
// hand-computed golden above does not reach (task 185 B4, captured before the extract-step
// refactor): an unpriced primary whose subasset is priced, a zero-unit asset, a USD row with no
// FX series, a sub-0.005 value, an empty and a failing Yahoo series, a non-positive
// portfolio_value, and a snapshot whose every holding was bought later (empty raw).
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
	exec(`TRUNCATE historia, positions, assets, purchase_lots, position_transfers RESTART IDENTITY`)
	exec(`INSERT INTO assets (key, name, exposure, asset_class) VALUES
	      ('PAR','PAR','STOCKS','ETF'), ('ZERO','ZERO','STOCKS','ETF'), ('USDX','USDX','STOCKS','ETF'),
	      ('PLNA','PLNA','STOCKS','ETF'), ('TINY','TINY','STOCKS','ETF'), ('EMPTY','EMPTY','STOCKS','ETF'),
	      ('ERR','ERR','STOCKS','ETF')`)
	exec(`INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, units_ike_tata, units_ike_mama, units_ikze_tata, yahoo_ticker, asset_id, is_primary, sort_order) VALUES
	      ('PAR','PAR','XTB','ETF','STOCKS','PLN',5,0,0,'YT-PAR',(SELECT id FROM assets WHERE key='PAR'),true,1),
	      ('SUB','SUB','XTB','ETF','STOCKS','PLN',0,2,0,'YT-SUB',(SELECT id FROM assets WHERE key='PAR'),false,2),
	      ('ZERO','ZERO','XTB','ETF','STOCKS','PLN',0,0,0,'YT-ZERO',(SELECT id FROM assets WHERE key='ZERO'),true,3),
	      ('USDX','USDX','XTB','ETF','STOCKS','USD',3,0,0,'YT-USD',(SELECT id FROM assets WHERE key='USDX'),true,4),
	      ('PLNA','PLNA','XTB','ETF','STOCKS','PLN',0,4,0,'YT-PLNA',(SELECT id FROM assets WHERE key='PLNA'),true,5),
	      ('TINY','TINY','XTB','ETF','STOCKS','PLN',1,0,0,'YT-TINY',(SELECT id FROM assets WHERE key='TINY'),true,6),
	      ('EMPTY','EMPTY','XTB','ETF','STOCKS','PLN',1,0,0,'YT-EMPTY',(SELECT id FROM assets WHERE key='EMPTY'),true,7),
	      ('ERR','ERR','XTB','ETF','STOCKS','PLN',0,0,1,'YT-ERR',(SELECT id FROM assets WHERE key='ERR'),true,8)`)
	exec(`INSERT INTO purchase_lots (session_id, purchased_at, ticker, units, price_pln, cost_pln) VALUES
	      (gen_random_uuid(),'2026-07-25','PLNA',4,30,120), (gen_random_uuid(),'2026-07-25','SUB',2,10,20)`)
	exec(`INSERT INTO historia (snapshot_date, total_deposits, portfolio_value, position_values) VALUES
	      ('2026-07-20',100,500,NULL), ('2026-08-01',100,1000,NULL), ('2026-08-03',100,0,NULL)`)

	svc := NewService(pool, errOHLC{errs: map[string]bool{"YT-ERR": true}, stubOHLC: stubOHLC{series: map[string][]priceprovider.OHLCPoint{
		"YT-SUB":   {pt("2026-07-31", 10)},
		"YT-USD":   {pt("2026-07-31", 5)},
		"YT-PLNA":  {pt("2026-07-31", 30)},
		"YT-TINY":  {pt("2026-07-31", 0.001)},
		"EURPLN=X": {pt("2026-07-31", 4.3)},
	}}})
	result, err := svc.Backfill(ctx)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, `SELECT snapshot_date::text, COALESCE(position_values::text, 'NULL') FROM historia ORDER BY snapshot_date`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var d, v string
		if err := rows.Scan(&d, &v); err != nil {
			t.Fatal(err)
		}
		got = append(got, d+" "+v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	const wantRows = `2026-07-20 NULL
2026-08-01 {"PAR": 142.86, "PLNA": 857.14}
2026-08-03 NULL`
	// Positions are read in ticker order (task 187 B5c), so the per-ticker warnings follow it.
	const wantWarnings = `no yahoo YT-EMPTY
no yahoo YT-ERR: boom
no yahoo YT-PAR
no yahoo YT-ZERO
2026-07-20 no fx USDPLN=X for YT-USD
2026-07-20 empty raw
2026-08-01 no fx USDPLN=X for YT-USD`
	if result.Updated != 1 || result.Skipped != 2 {
		t.Errorf("updated/skipped = %d/%d", result.Updated, result.Skipped)
	}
	if g := strings.Join(got, "\n"); g != wantRows {
		t.Errorf("rows\n got:\n%s\nwant:\n%s", g, wantRows)
	}
	if g := strings.Join(result.Warnings, "\n"); g != wantWarnings {
		t.Errorf("warnings\n got:\n%s\nwant:\n%s", g, wantWarnings)
	}
}
