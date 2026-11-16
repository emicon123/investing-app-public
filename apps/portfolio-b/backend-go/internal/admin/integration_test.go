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

// TestBackfill_RealPostgresGolden runs the full portfolioB backfill against portfolioB's
// own migrated database with a fixed OHLC fixture — never mock the database, never
// hit Yahoo. Golden expectations (hand-computed, NOT derived from the code under
// test):
//
//   - AAA (PLN, 10u, lot +4u bought 2026-08-10 → 6u @100) + companion CMP (3u @20)
//     → raw 660.
//   - BBB (EUR, 5u, transfer +1u on 2026-08-05 → 4u @10 EUR × 4.3) → raw 172.
//   - CHF (CHF, no yahoo, 2u × CHFPLN 4.5) → raw 9.
//   - rawSum 841 scaled to portfolio_value 2000 (last-row plug on CHF):
//     AAA 1569.56, BBB 409.04, CHF 21.40 — sum exactly 2000 (invariant 0.02).
//   - The pre-cutoff NULL row (2026-01-01) stays NULL; the already-filled row is
//     untouched; warnings are empty.
func TestBackfill_RealPostgresGolden(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, assets, purchase_lots, position_transfers RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedBackfill(t, ctx, pool)

	svc := NewService(pool, stubOHLC{series: map[string][]priceprovider.OHLCPoint{
		"YT-AAA":   {pt("2026-07-31", 100)},
		"YT-CMP":   {pt("2026-07-31", 20)},
		"YT-BBB":   {pt("2026-07-31", 10)},
		"EURPLN=X": {pt("2026-07-31", 4.3)},
		"CHFPLN=X": {pt("2026-07-31", 4.5)},
	}})

	result, err := svc.Backfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected updated 1, got %+v", result)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}

	var rawJSON []byte
	if err := pool.QueryRow(ctx, `SELECT position_values FROM historia WHERE snapshot_date = '2026-08-01'`).Scan(&rawJSON); err != nil {
		t.Fatal(err)
	}
	var got map[string]float64
	if err := json.Unmarshal(rawJSON, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"AAA": 1569.56, "BBB": 409.04, "CHF": 21.40}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	sum := 0.0
	for k, w := range want {
		if got[k] != w {
			t.Fatalf("expected %s=%v, got %v (full map %v)", k, w, got[k], got)
		}
		sum += got[k]
	}
	if diff := sum - 2000; diff < -0.02 || diff > 0.02 {
		t.Fatalf("sum invariant 0.02 broken: sum=%v", sum)
	}

	// Pre-cutoff NULL row untouched.
	var preCutoff []byte
	if err := pool.QueryRow(ctx, `SELECT position_values FROM historia WHERE snapshot_date = '2026-01-01'`).Scan(&preCutoff); err != nil {
		t.Fatal(err)
	}
	if len(preCutoff) != 0 {
		t.Fatalf("pre-cutoff row must stay NULL, got %s", preCutoff)
	}
	// Already-filled row untouched.
	var filled []byte
	if err := pool.QueryRow(ctx, `SELECT position_values FROM historia WHERE snapshot_date = '2026-08-02'`).Scan(&filled); err != nil {
		t.Fatal(err)
	}
	var filledMap map[string]float64
	if err := json.Unmarshal(filled, &filledMap); err != nil || filledMap["AAA"] != 1 {
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

	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, assets, purchase_lots, position_transfers RESTART IDENTITY`); err != nil {
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
	exec(`INSERT INTO assets (key, name, exposure, asset_class)
	      VALUES ('AAA','AAA fund','STOCKS','ETF'),
	             ('BBB','BBB fund','STOCKS','ETF'),
	             ('CHF','CHF cash','CURRENCY','CURRENCY')`)
	exec(`INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, units_ike_tata, units_ike_mama, units_ikze_tata, yahoo_ticker, asset_id, is_primary, sort_order)
	      VALUES ('AAA','AAA fund','XTB','ETF','STOCKS','PLN',10,0,0,'YT-AAA',(SELECT id FROM assets WHERE key='AAA'),true,1),
	             ('CMP','CMP comp','XTB','ETF','STOCKS','PLN',0,3,0,'YT-CMP',(SELECT id FROM assets WHERE key='AAA'),false,2),
	             ('BBB','BBB fund','XTB','ETF','STOCKS','EUR',0,0,5,'YT-BBB',(SELECT id FROM assets WHERE key='BBB'),true,3),
	             ('CHF','CHF cash','OTHER','CURRENCY','CURRENCY','CHF',0,0,0,NULL,(SELECT id FROM assets WHERE key='CHF'),true,4)`)
	exec(`UPDATE positions SET units_inne = 2 WHERE ticker = 'CHF'`)
	exec(`INSERT INTO purchase_lots (session_id, purchased_at, ticker, units, price_pln, cost_pln)
	      VALUES (gen_random_uuid(),'2026-08-10','AAA',4,100,400)`)
	exec(`INSERT INTO position_transfers (transfer_id, ticker, account_type, units_delta, price_pln, total_pln, transferred_at)
	      VALUES (gen_random_uuid(),'BBB','IKE_TATA',1,43,43,'2026-08-05 12:00:00+02')`)
	exec(`INSERT INTO historia (snapshot_date, total_deposits, portfolio_value, position_values)
	      VALUES ('2026-01-01',100,900,NULL),
	             ('2026-08-01',100,2000,NULL),
	             ('2026-08-02',100,2100,'{"AAA":1}')`)
}
