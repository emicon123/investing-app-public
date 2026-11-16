package marketdata

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/marketdatacore"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore/refdata"
	refsql "investing-app/common-go/siblingcore/refdata/sqlcgen"
	"investing-app/common-go/testsupport"
)

// fakeProvider is a scripted priceprovider.PriceProvider stand-in — no real Yahoo/Binance HTTP
// calls in this test, matching root's own Phase 1 integration_test.go convention.
type fakeProvider struct {
	name   string
	prices map[string]decimal.Decimal
	err    error
	calls  atomic.Int64 // incremented from the refresh fan-out goroutines
}

func (p *fakeProvider) GetPrice(_ context.Context, ticker string) (decimal.Decimal, error) {
	p.calls.Add(1)
	if p.err != nil {
		return decimal.Zero, p.err
	}
	price, ok := p.prices[ticker]
	if !ok {
		return decimal.Zero, errors.New("fakeProvider: no price configured for " + ticker)
	}
	return price, nil
}

func (p *fakeProvider) Name() string { return p.name }

// TestMarketData_RealPostgres exercises price_cache/fx_rate_cache reads and writes, plus the
// read-only `positions` tracked-ticker lookup, against portfolioB's own migrated database — never
// mock the database. Asserts against literally the same schema Java's own Testcontainers tests
// do (common-go/testsupport, same Flyway CLI image used in prod).
func TestMarketData_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	// The baseline schema ships no portfolio rows: seed synthetic positions, two of which share a
	// Yahoo ticker so the DISTINCT ON de-duplication bites.
	if _, err := pool.Exec(ctx, `
		WITH a AS (
			INSERT INTO assets (key, name, exposure, asset_class) VALUES
				('BTC', 'Bitcoin', 'CRYPTO', 'CRYPTO'),
				('SPY', 'S&P 500', 'STOCKS', 'ETF')
			RETURNING id, key
		)
		INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary)
		SELECT 'BTC', 'Bitcoin', 'BINANCE'::exchange_enum, 'CRYPTO'::asset_class_enum, 'CRYPTO'::exposure_enum, 'USD'::currency_enum, 'BTC-USD', id, TRUE FROM a WHERE key = 'BTC'
		UNION ALL SELECT 'BTC-ETN', 'Bitcoin ETN', 'XTB', 'CRYPTO', 'CRYPTO', 'USD', 'BTC-USD', id, FALSE FROM a WHERE key = 'BTC'
		UNION ALL SELECT 'SPY', 'S&P 500', 'XTB', 'ETF', 'STOCKS', 'USD', 'SPY', id, TRUE FROM a WHERE key = 'SPY'`); err != nil {
		t.Fatalf("seed positions: %v", err)
	}

	queries := refsql.New(pool)
	yahoo := &fakeProvider{name: "YAHOO"}
	service := marketdatacore.NewService(refdata.New(pool), yahoo, yahoo)

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
		err := queries.UpsertPriceCache(ctx, refsql.UpsertPriceCacheParams{
			Ticker:    "BTC-USD",
			Price:     pgnum.FromDecimal(decimal.RequireFromString("65432.12345678")),
			Currency:  refsql.CurrencyEnumUSD,
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
		params := refsql.UpsertPriceCacheParams{
			Ticker: "AAPL", Currency: refsql.CurrencyEnumUSD, FetchedAt: nowTimestamptz(),
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

		var priceText string
		if err := pool.QueryRow(ctx, `SELECT price::text FROM price_cache WHERE ticker = 'AAPL'`).Scan(&priceText); err != nil {
			t.Fatalf("read price_cache: %v", err)
		}
		price := decimal.RequireFromString(priceText)
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
		if err := queries.UpsertFxRateCache(ctx, refsql.UpsertFxRateCacheParams{
			FromCurrency: refsql.CurrencyEnumEUR,
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

	t.Run("GetTrackedTickers reads positions read-only", func(t *testing.T) {
		// The test seeds synthetic rows into `positions` above. The query must succeed against the real
		// schema (reading another phase's table is fine, writing is not — this package never writes to
		// `positions`), and every row must have a non-blank yahoo_ticker and be de-duplicated by ticker.
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

	t.Run("ForceRefreshAllPrices persists through the real Store adapter", func(t *testing.T) {
		yahoo.prices = map[string]decimal.Decimal{}
		tickers, err := queries.GetTrackedTickers(ctx)
		if err != nil {
			t.Fatalf("GetTrackedTickers: %v", err)
		}
		for _, tk := range tickers {
			if tk.YahooTicker != nil {
				yahoo.prices[*tk.YahooTicker] = decimal.NewFromInt(1)
			}
		}
		yahoo.prices["USDPLN=X"] = decimal.NewFromFloat(4.0)
		yahoo.prices["EURPLN=X"] = decimal.NewFromFloat(4.3)
		yahoo.prices["CHFPLN=X"] = decimal.NewFromFloat(4.5)
		yahoo.prices["GBPPLN=X"] = decimal.NewFromFloat(5.0)

		result, err := service.ForceRefreshAllPrices(ctx)
		if err != nil {
			t.Fatalf("ForceRefreshAllPrices: %v", err)
		}
		if result.Total == 0 {
			t.Fatal("expected at least one tracked ticker to be refreshed")
		}
	})
}

func nowTimestamptz() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}
