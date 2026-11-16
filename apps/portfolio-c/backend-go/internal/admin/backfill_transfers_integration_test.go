package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/priceprovider"
	"investing-app/common-go/siblingcore"
	scsnapshot "investing-app/common-go/siblingcore/snapshot"
	sctransfer "investing-app/common-go/siblingcore/transfer"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestBackfill_ReplaysTransfers_MatchesLiveSnapshot_RealPostgres is design 190 §A.7 (task 190 C2):
// a NULL row dated BEFORE a position transfer must be filled with the pre-transfer split, i.e. the
// very breakdown the live snapshot stored that day. Hand-computed fixture (PLN, price 100 / 50):
//
//   - day D (2026-08-01): VARIABLE holds A 10u and B 5u -> {A:1000, B:250}, portfolio_value 1250.
//     The live snapshotter writes it; the test copies it aside and NULLs the row.
//   - a real transfer then sells 4 A and buys 8 B (cash neutral): A 6u, B 13u today.
//   - FIXED holds an A as well, with its own transfer leg (-1): a query that forgot the portfolio
//     filter would rewind VARIABLE's A to 11u on D.
//   - a control row two days after today (D2) sees no change after it: {A:600, B:650}.
//
// Before the fix D came back {A:600, B:650} (the post-transfer split scaled to 1250).
func TestBackfill_ReplaysTransfers_MatchesLiveSnapshot_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec(`TRUNCATE historia, positions, assets, purchase_lots, position_transfers, price_cache, deposits RESTART IDENTITY CASCADE`)
	exec(`INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct)
	      VALUES ('VARIABLE','A','Asset A','STOCKS','ETF',50),
	             ('VARIABLE','B','Asset B','STOCKS','ETF',50),
	             ('FIXED','A','Asset A','STOCKS','ETF',100)`)
	exec(`INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, units, yahoo_ticker, asset_id, is_primary, sort_order)
	      VALUES ('VARIABLE','A','Asset A','XTB','ETF','STOCKS','PLN',10,'YT-A',(SELECT id FROM assets WHERE key='A' AND portfolio='VARIABLE'),true,1),
	             ('VARIABLE','B','Asset B','XTB','ETF','STOCKS','PLN',5,'YT-B',(SELECT id FROM assets WHERE key='B' AND portfolio='VARIABLE'),true,2),
	             ('FIXED','A','Asset A','XTB','ETF','STOCKS','PLN',4,'YT-A',(SELECT id FROM assets WHERE key='A' AND portfolio='FIXED'),true,1)`)
	exec(`INSERT INTO price_cache (ticker, price, currency, price_pln)
	      VALUES ('YT-A',100,'PLN',100), ('YT-B',50,'PLN',50)`)

	// The live value of day D: the snapshotter, at the pre-transfer book.
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	snap := scsnapshot.NewSnapshotter(book.Config, store.New(pool), nil)
	if err := snap.BackfillSnapshot(ctx, "VARIABLE", day); err != nil {
		t.Fatalf("live snapshot: %v", err)
	}
	readValues := func(date time.Time) (map[string]float64, string) {
		t.Helper()
		var raw []byte
		var pv string
		if err := pool.QueryRow(ctx, `SELECT position_values, portfolio_value::text FROM historia
			WHERE portfolio = 'VARIABLE' AND snapshot_date = $1`, date).Scan(&raw, &pv); err != nil {
			t.Fatalf("read historia %s: %v", date.Format("2006-01-02"), err)
		}
		var got map[string]float64
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode position_values %s: %v", raw, err)
		}
		return got, pv
	}
	live, pv := readValues(day)
	if len(live) != 2 || live["A"] != 1000 || live["B"] != 250 || !decimal.RequireFromString(pv).Equal(decimal.NewFromInt(1250)) {
		t.Fatalf("fixture must give the live split {A:1000,B:250} of 1250, got %v of %s", live, pv)
	}
	exec(`UPDATE historia SET position_values = NULL WHERE portfolio = 'VARIABLE' AND snapshot_date = $1`, day)

	// A real transfer after D: sell 4 A @100, buy 8 B @50.
	money := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, store.Bind)
	if _, err := sctransfer.NewService(book.Config, money).ExecuteTransfer(ctx, sctransfer.Request{
		Portfolio: "VARIABLE",
		Sold:      sctransfer.Leg{Ticker: "A", Bucket: siblingcore.SingleBucket, Units: decimal.NewFromInt(4), PricePln: decimal.NewFromInt(100)},
		Buys:      []sctransfer.Leg{{Ticker: "B", Bucket: siblingcore.SingleBucket, Units: decimal.NewFromInt(8), PricePln: decimal.NewFromInt(50)}},
	}); err != nil {
		t.Fatalf("ExecuteTransfer: %v", err)
	}

	// Portfolio scoping: FIXED's own leg on the same ticker must not leak into VARIABLE.
	exec(`INSERT INTO position_transfers (transfer_id, portfolio, ticker, units_delta, price_pln, total_pln, transferred_at)
	      VALUES (gen_random_uuid(),'FIXED','A',-1,100,-100,now())`)
	exec(`UPDATE positions SET units = 3 WHERE portfolio = 'FIXED' AND ticker = 'A'`)

	// Control row after every change.
	day2 := historiaquery.WarsawDate(time.Now()).AddDate(0, 0, 2)
	exec(`INSERT INTO historia (portfolio, snapshot_date, total_deposits, portfolio_value, position_values)
	      VALUES ('VARIABLE', $1, 0, 1250, NULL)`, day2)

	svc := NewService(pool, stubOHLC{series: map[string][]priceprovider.OHLCPoint{
		"YT-A": {pt(day.Format("2006-01-02"), 100), pt(day2.Format("2006-01-02"), 100)},
		"YT-B": {pt(day.Format("2006-01-02"), 50), pt(day2.Format("2006-01-02"), 50)},
	}})
	result, err := svc.Backfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 2 || len(result.Warnings) != 0 {
		t.Fatalf("expected updated 2 and no warnings, got %+v", result)
	}

	got, _ := readValues(day)
	if len(got) != len(live) || got["A"] != live["A"] || got["B"] != live["B"] {
		t.Fatalf("row before the transfer must equal the live snapshot %v, got %v", live, got)
	}
	got2, _ := readValues(day2)
	if len(got2) != 2 || got2["A"] != 600 || got2["B"] != 650 {
		t.Fatalf("row after the transfer must be {A:600,B:650}, got %v", got2)
	}
}
