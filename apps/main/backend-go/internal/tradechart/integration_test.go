package tradechart

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestTradeChart_RealPostgresQueries runs the trade-chart read queries against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in
// prod — the "never mock the database" gate. It asserts schema/query compatibility
// (the three SELECTs execute against the real tables) plus the seeded-picker
// invariants (seed-only tickers listed with tradeCount 0, never omitted); row-level
// logic is covered by handler_test.go's fakes.
func TestTradeChart_RealPostgresQueries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	// The baseline schema ships no portfolio rows, so seed one synthetic asset whose only
	// purchase is a SEED_IMPORT row: the picker must still list it (tradeCount 0).
	if _, err := pool.Exec(ctx, `
		WITH a AS (
			INSERT INTO assets (portfolio, key, name, exposure, asset_class)
			VALUES ('MAIN', 'BTC', 'Bitcoin', 'CRYPTO', 'CRYPTO') RETURNING id
		)
		INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, price_currency,
			yahoo_ticker, exposure, asset_id, is_primary)
		SELECT 'BTC', 'Bitcoin', 'MAIN', 'BINANCE', 'CRYPTO', 'USD', 'BTC-USD', 'CRYPTO', a.id, TRUE
		FROM a`); err != nil {
		t.Fatalf("seed asset/position: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO asset_purchases (portfolio, ticker, account_type, units_delta, price_pln, total_pln, trade_type)
		VALUES ('MAIN', 'BTC', 'REGULAR', 0.01, 100000, 1000, 'SEED_IMPORT')`); err != nil {
		t.Fatalf("seed purchase: %v", err)
	}

	queries := sqlcgen.New(pool)

	if _, err := queries.AggregateTradesByTicker(ctx); err != nil {
		t.Fatalf("AggregateTradesByTicker: %v", err)
	}
	if _, err := queries.ListPositionsForChart(ctx); err != nil {
		t.Fatalf("ListPositionsForChart: %v", err)
	}
	if _, err := queries.ListTradesByTicker(ctx, "BTC"); err != nil {
		t.Fatalf("ListTradesByTicker: %v", err)
	}

	svc := NewService(queries, &fakeHistory{})
	res, err := svc.ListTickers(ctx)
	if err != nil {
		t.Fatalf("ListTickers: %v", err)
	}
	// The seed-only ticker appears with tradeCount 0 (never omitted), and every listed
	// row resolves Yahoo metadata.
	if len(res.Data) == 0 {
		t.Fatal("expected seeded picker rows, got none")
	}
	if res.Data[0].Ticker != "BTC" || res.Data[0].TradeCount != 0 {
		t.Fatalf("expected seed-only BTC with tradeCount 0, got %+v", res.Data[0])
	}
	for _, e := range res.Data {
		if e.YahooTicker == "" {
			t.Fatalf("picker row %q listed without Yahoo metadata", e.Ticker)
		}
	}
	for _, x := range res.Excluded {
		if x.Reason != "NO_POSITION_MATCH" && x.Reason != "MISSING_YAHOO_TICKER" {
			t.Fatalf("unexpected exclusion reason %q for %q", x.Reason, x.Ticker)
		}
	}
}
