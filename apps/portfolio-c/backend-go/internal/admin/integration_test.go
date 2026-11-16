package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/priceprovider"
	"investing-app/common-go/testsupport"
)

// stubOHLC is a fixed history fixture: every FetchOHLC call answers from the map,
// no network. The golden expectations below are hand-computed from these closes.
type stubOHLC struct {
	series map[string][]priceprovider.OHLCPoint
}

func (s stubOHLC) FetchOHLC(_ context.Context, ticker, _ string, _, _ int64) ([]priceprovider.OHLCPoint, error) {
	return s.series[ticker], nil
}

func pt(date string, close float64) priceprovider.OHLCPoint {
	d, _ := time.Parse("2006-01-02", date)
	return priceprovider.OHLCPoint{Date: d, Close: close}
}

// TestBackfill_RealPostgresGolden runs the full flat per-portfolio backfill against
// portfolioC's own migrated database with a fixed OHLC fixture — never mock the
// database, never hit Yahoo. Golden expectations (hand-computed, NOT derived from
// the code under test):
//
//   - VARIABLE SXRV (EUR, 10u, lot +2u bought 2026-08-10 → 8u @50 EUR × 4.3)
//     → raw 1720; 4GLD (EUR, 3u @20 × 4.3) → raw 258; rawSum 1978 scaled to 1000
//     (last-row plug on SXRV.DE): SXRV.DE 869.57, 4GLD.DE 130.43.
//   - FIXED ICGA (PLN, 5u @100) → raw 500 → scaled to 500 exactly.
//   - updated 2, warnings empty, sums exact (invariant 0.02).
func TestBackfill_RealPostgresGolden(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, assets, purchase_lots RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedBackfill(t, ctx, pool)

	svc := NewService(pool, stubOHLC{series: map[string][]priceprovider.OHLCPoint{
		"YT-SXRV":  {pt("2026-07-31", 50)},
		"YT-GLD":   {pt("2026-07-31", 20)},
		"YT-ICGA":  {pt("2026-07-31", 100)},
		"EURPLN=X": {pt("2026-07-31", 4.3)},
	}})

	result, err := svc.Backfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 2 {
		t.Fatalf("expected updated 2, got %+v", result)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}

	checkRow := func(portfolio, date string, want map[string]float64, total float64) {
		t.Helper()
		var rawJSON []byte
		if err := pool.QueryRow(ctx, `SELECT position_values FROM historia WHERE portfolio = $1 AND snapshot_date = $2`,
			portfolio, date).Scan(&rawJSON); err != nil {
			t.Fatal(err)
		}
		var got map[string]float64
		if err := json.Unmarshal(rawJSON, &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: expected %v, got %v", portfolio, want, got)
		}
		sum := 0.0
		for k, w := range want {
			if got[k] != w {
				t.Fatalf("%s: expected %s=%v, got %v (full map %v)", portfolio, k, w, got[k], got)
			}
			sum += got[k]
		}
		if diff := sum - total; diff < -0.02 || diff > 0.02 {
			t.Fatalf("%s: sum invariant 0.02 broken: sum=%v", portfolio, sum)
		}
	}
	checkRow("VARIABLE", "2026-08-01", map[string]float64{"SXRV.DE": 869.57, "4GLD.DE": 130.43}, 1000)
	checkRow("FIXED", "2026-08-01", map[string]float64{"ICGA.DE": 500}, 500)

	// Already-filled row untouched.
	var filled []byte
	if err := pool.QueryRow(ctx, `SELECT position_values FROM historia WHERE portfolio = 'VARIABLE' AND snapshot_date = '2026-08-02'`).Scan(&filled); err != nil {
		t.Fatal(err)
	}
	var filledMap map[string]float64
	if err := json.Unmarshal(filled, &filledMap); err != nil || filledMap["SXRV.DE"] != 1 {
		t.Fatalf("filled row must be untouched, got %s", filled)
	}

	// Second run: nothing missing → updated 0, skipped = total rows.
	again, err := svc.Backfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Updated != 0 || again.Skipped != 3 {
		t.Fatalf("expected updated 0 skipped 3, got %+v", again)
	}
}

func TestBackfillHandler_RawMap(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, assets, purchase_lots RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedBackfill(t, ctx, pool)

	h := NewHandler(NewService(pool, stubOHLC{}), nil) // BackfillSnapshot not exercised here
	rec := httptest.NewRecorder()
	h.Backfill(rec, httptest.NewRequest(http.MethodPost, "/api/admin/history/backfill-position-values", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"updated", "skipped", "warnings"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("expected key %s in %v", k, body)
		}
	}
	if _, ok := body["data"]; ok {
		t.Fatalf("admin must answer WITHOUT the envelope, got %s", rec.Body.String())
	}
}

func seedBackfill(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec(`INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct)
	      VALUES ('VARIABLE','SXRV.DE','SXRV fund','STOCKS','ETF',50),
	             ('VARIABLE','4GLD.DE','Gold ETC','METALS','ETF',50),
	             ('FIXED','ICGA.DE','ICGA fund','STOCKS','ETF',100)`)
	exec(`INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, units, yahoo_ticker, asset_id, is_primary, sort_order)
	      VALUES ('VARIABLE','SXRV.DE','SXRV fund','XTB','ETF','STOCKS','EUR',10,'YT-SXRV',(SELECT id FROM assets WHERE key='SXRV.DE' AND portfolio='VARIABLE'),true,1),
	             ('VARIABLE','4GLD.DE','Gold ETC','XTB','ETF','METALS','EUR',3,'YT-GLD',(SELECT id FROM assets WHERE key='4GLD.DE' AND portfolio='VARIABLE'),true,2),
	             ('FIXED','ICGA.DE','ICGA fund','XTB','ETF','STOCKS','PLN',5,'YT-ICGA',(SELECT id FROM assets WHERE key='ICGA.DE' AND portfolio='FIXED'),true,1)`)
	exec(`INSERT INTO purchase_lots (portfolio, session_id, purchased_at, ticker, units, price_pln, cost_pln)
	      VALUES ('VARIABLE',gen_random_uuid(),'2026-08-10','SXRV.DE',2,215,430)`)
	exec(`INSERT INTO historia (portfolio, snapshot_date, total_deposits, portfolio_value, position_values)
	      VALUES ('VARIABLE','2026-08-01',800,1000,NULL),
	             ('FIXED','2026-08-01',400,500,NULL),
	             ('VARIABLE','2026-08-02',800,1010,'{"SXRV.DE":1}')`)
}
