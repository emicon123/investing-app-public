package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/marketdatacore"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/testsupport"
)

// TestStore_RealPostgresRoundTrip exercises the Store adapter (and the shared kernel over it)
// against price_cache/fx_rate_cache on a real postgres:17.10-alpine container, migrated by the
// same Flyway CLI image used in prod (common-go/testsupport) — the "never mock the database"
// gate, asserting against literally the same schema prod runs.
func TestStore_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	// The baseline schema ships no portfolio rows: seed three synthetic positions, two of
	// which share a Yahoo ticker (one per portfolio) so the DISTINCT ON de-duplication bites.
	if _, err := pool.Exec(ctx, `
		WITH a AS (
			INSERT INTO assets (portfolio, key, name, exposure, asset_class) VALUES
				('MAIN', 'BTC', 'Bitcoin', 'CRYPTO', 'CRYPTO'),
				('RETIREMENT', 'BTC', 'Bitcoin', 'CRYPTO', 'CRYPTO'),
				('MAIN', 'SPY', 'S&P 500', 'EQUITY', 'ETF')
			RETURNING id, portfolio, key
		)
		INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, price_currency,
			yahoo_ticker, exposure, asset_id, is_primary)
		SELECT a.key, 'synthetic ' || a.key, a.portfolio,
			CASE a.key WHEN 'BTC' THEN 'BINANCE'::exchange_enum ELSE 'XTB'::exchange_enum END,
			CASE a.key WHEN 'BTC' THEN 'CRYPTO'::asset_class_enum ELSE 'ETF'::asset_class_enum END,
			'USD', CASE a.key WHEN 'BTC' THEN 'BTC-USD' ELSE 'SPY' END,
			CASE a.key WHEN 'BTC' THEN 'CRYPTO'::exposure_enum ELSE 'EQUITY'::exposure_enum END,
			a.id, TRUE
		FROM a`); err != nil {
		t.Fatalf("seed positions: %v", err)
	}

	queries := sqlcgen.New(pool)
	store := NewStore(queries)
	yahoo := &fakeProvider{name: "YAHOO"}
	service := NewService(queries, yahoo, yahoo)

	t.Run("a missing fx_rate_cache row is marketdatacore.ErrNotFound", func(t *testing.T) {
		_, err := store.GetFxRate(ctx, "GBP")
		if !errors.Is(err, marketdatacore.ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("want marketdatacore.ErrNotFound, got %v", err)
		}
	})

	t.Run("empty cache reads return empty, not an error", func(t *testing.T) {
		prices, err := service.GetAllPriceEntries(ctx)
		if err != nil {
			t.Fatalf("GetAllPriceEntries: %v", err)
		}
		if len(prices) != 0 {
			t.Fatalf("expected an empty price cache, got %d entries", len(prices))
		}

		rates, err := service.GetAllFxRateEntries(ctx)
		if err != nil {
			t.Fatalf("GetAllFxRateEntries: %v", err)
		}
		if len(rates) != 1 || rates[0].FromCurrency != "PLN" {
			t.Fatalf("expected only the synthetic PLN=1.0 entry, got %+v", rates)
		}
	})

	t.Run("upsert then read round-trips price_cache exactly, including NUMERIC(18,8) precision", func(t *testing.T) {
		err := queries.UpsertPriceCache(ctx, sqlcgen.UpsertPriceCacheParams{
			Ticker:    "BTC-USD",
			Price:     pgnum.FromDecimal(decimal.RequireFromString("65432.12345678")),
			Currency:  sqlcgen.CurrencyEnumUSD,
			PricePln:  pgnum.FromDecimal(decimal.RequireFromString("254432.99999999")),
			FetchedAt: nowTimestamptz(),
		})
		if err != nil {
			t.Fatalf("UpsertPriceCache: %v", err)
		}

		entries, err := service.GetAllPriceEntries(ctx)
		if err != nil {
			t.Fatalf("GetAllPriceEntries: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected exactly 1 entry, got %d", len(entries))
		}
		e := entries[0]
		if e.Ticker != "BTC-USD" || e.Currency != "USD" {
			t.Fatalf("unexpected entry: %+v", e)
		}
		if !e.Price.Equal(decimal.RequireFromString("65432.12345678")) {
			t.Errorf("expected price 65432.12345678, got %s", e.Price)
		}
		if !e.PricePln.Equal(decimal.RequireFromString("254432.99999999")) {
			t.Errorf("expected pricePln 254432.99999999, got %s", e.PricePln)
		}
	})

	t.Run("re-upserting the same ticker updates in place (ON CONFLICT), never duplicates", func(t *testing.T) {
		params := sqlcgen.UpsertPriceCacheParams{
			Ticker: "AAPL", Currency: sqlcgen.CurrencyEnumUSD, FetchedAt: nowTimestamptz(),
			Price: pgnum.FromDecimal(decimal.NewFromInt(100)), PricePln: pgnum.FromDecimal(decimal.NewFromInt(400)),
		}
		if err := queries.UpsertPriceCache(ctx, params); err != nil {
			t.Fatalf("first upsert: %v", err)
		}
		params.Price = pgnum.FromDecimal(decimal.NewFromInt(101))
		params.PricePln = pgnum.FromDecimal(decimal.NewFromInt(404))
		if err := queries.UpsertPriceCache(ctx, params); err != nil {
			t.Fatalf("second upsert: %v", err)
		}

		row, err := queries.GetPriceCacheByTicker(ctx, "AAPL")
		if err != nil {
			t.Fatalf("GetPriceCacheByTicker: %v", err)
		}
		price, _ := pgnum.ToDecimal(row.Price)
		if !price.Equal(decimal.NewFromInt(101)) {
			t.Errorf("expected the second upsert's price 101 to win, got %s", price)
		}

		all, err := service.GetAllPriceEntries(ctx)
		if err != nil {
			t.Fatalf("GetAllPriceEntries: %v", err)
		}
		count := 0
		for _, e := range all {
			if e.Ticker == "AAPL" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 AAPL row after re-upsert, found %d", count)
		}
	})

	t.Run("fx_rate_cache upsert then GetRateToPln honors the TTL check", func(t *testing.T) {
		if err := queries.UpsertFxRateCache(ctx, sqlcgen.UpsertFxRateCacheParams{
			FromCurrency: sqlcgen.CurrencyEnumEUR,
			RateToPln:    pgnum.FromDecimal(decimal.RequireFromString("4.28")),
			Source:       "YAHOO_FINANCE",
			FetchedAt:    nowTimestamptz(),
		}); err != nil {
			t.Fatalf("UpsertFxRateCache: %v", err)
		}

		rate, err := service.GetRateToPln(ctx, "EUR")
		if err != nil {
			t.Fatalf("GetRateToPln: %v", err)
		}
		if !rate.Equal(decimal.RequireFromString("4.28")) {
			t.Errorf("expected the fresh cached rate 4.28 with no external call, got %s", rate)
		}
		if yahoo.calls.Load() != 0 {
			t.Errorf("expected no external call for a fresh cached rate, got %d calls", yahoo.calls.Load())
		}

		entries, err := service.GetAllFxRateEntries(ctx)
		if err != nil {
			t.Fatalf("GetAllFxRateEntries: %v", err)
		}
		if len(entries) != 2 { // PLN (synthetic) + EUR
			t.Fatalf("expected 2 entries (PLN + EUR), got %+v", entries)
		}
	})

	t.Run("Store upserts round-trip through the kernel's entry types", func(t *testing.T) {
		at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
		if err := store.UpsertPrice(ctx, marketdatacore.PriceEntry{
			Ticker: "SOL-USD", Price: decimal.RequireFromString("150.12345678"), Currency: "USD",
			PricePln: decimal.RequireFromString("600.5"), FetchedAt: at,
		}); err != nil {
			t.Fatalf("UpsertPrice: %v", err)
		}
		prices, err := store.ListPrices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var sol *marketdatacore.PriceEntry
		for i := range prices {
			if prices[i].Ticker == "SOL-USD" {
				sol = &prices[i]
			}
		}
		if sol == nil || !sol.Price.Equal(decimal.RequireFromString("150.12345678")) || sol.Currency != "USD" || !sol.FetchedAt.Equal(at) {
			t.Fatalf("unexpected SOL-USD entry: %+v", sol)
		}
		if err := store.UpsertFxRate(ctx, marketdatacore.FxRateEntry{
			FromCurrency: "CHF", RateToPln: decimal.RequireFromString("4.5123"), Source: "YAHOO_FINANCE", FetchedAt: at,
		}); err != nil {
			t.Fatalf("UpsertFxRate: %v", err)
		}
		chf, err := store.GetFxRate(ctx, "CHF")
		if err != nil || !chf.RateToPln.Equal(decimal.RequireFromString("4.5123")) || chf.Source != "YAHOO_FINANCE" || !chf.FetchedAt.Equal(at) {
			t.Fatalf("unexpected CHF entry: %+v, %v", chf, err)
		}
	})

	t.Run("Store.ListTrackedTickers matches the query's rows", func(t *testing.T) {
		rows, err := queries.GetTrackedTickers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		tracked, err := store.ListTrackedTickers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(tracked) == 0 || len(tracked) != len(rows) {
			t.Fatalf("want one tracked ticker per query row (%d), got %d", len(rows), len(tracked))
		}
		for i, r := range rows {
			if tracked[i].YahooTicker != *r.YahooTicker || tracked[i].PriceCurrency != string(r.PriceCurrency) || tracked[i].Exchange != string(r.Exchange) {
				t.Fatalf("row %d: %+v vs %+v", i, tracked[i], r)
			}
		}
	})

	t.Run("GetTrackedTickers reads positions read-only", func(t *testing.T) {
		// The test seeds synthetic rows into `positions` above. The query must succeed against
		// the real schema (no write-ownership violation, doc 13 §6.3: reading another phase's
		// table is fine, writing is not — this package never writes to `positions`) and every
		// row must satisfy the WHERE clause (non-null, non-blank yahoo_ticker) and be
		// de-duplicated by ticker.
		tickers, err := queries.GetTrackedTickers(ctx)
		if err != nil {
			t.Fatalf("GetTrackedTickers: %v", err)
		}
		if len(tickers) == 0 {
			t.Fatal("expected the seeded positions data to yield at least one tracked ticker")
		}
		seen := map[string]bool{}
		for _, row := range tickers {
			if row.YahooTicker == nil || *row.YahooTicker == "" {
				t.Fatalf("expected every row to have a non-blank yahoo_ticker, got %+v", row)
			}
			if seen[*row.YahooTicker] {
				t.Fatalf("expected DISTINCT ON (yahoo_ticker) to de-duplicate, but %s appeared twice", *row.YahooTicker)
			}
			seen[*row.YahooTicker] = true
		}
	})
}

// TestStore_ListTrackedTickers_SkipsNilAndBlankYahooTickers: main's GetTrackedTickers row has a
// nullable yahoo_ticker; the adapter drops nil/blank rows before the kernel sees them.
func TestStore_ListTrackedTickers_SkipsNilAndBlankYahooTickers(t *testing.T) {
	q := newFakeQueries()
	q.tickers = []sqlcgen.GetTrackedTickersRow{
		{YahooTicker: nil, PriceCurrency: sqlcgen.CurrencyEnumPLN, Exchange: sqlcgen.ExchangeEnumXTB},
		{YahooTicker: new(""), PriceCurrency: sqlcgen.CurrencyEnumPLN, Exchange: sqlcgen.ExchangeEnumXTB},
		{YahooTicker: new("BTC-USD"), PriceCurrency: sqlcgen.CurrencyEnumUSD, Exchange: sqlcgen.ExchangeEnumBINANCE},
	}
	got, err := NewStore(q).ListTrackedTickers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []marketdatacore.TrackedTicker{{YahooTicker: "BTC-USD", PriceCurrency: "USD", Exchange: "BINANCE"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func nowTimestamptz() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}
