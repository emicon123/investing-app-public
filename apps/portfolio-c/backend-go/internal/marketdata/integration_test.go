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
// read-only `positions` tracked-ticker lookup (across BOTH portfolios), against portfolioC's own
// migrated database — never mock the database. Asserts against literally the same schema
// Java's own Testcontainers tests do (common-go/testsupport, same Flyway CLI image used in
// prod).
func TestMarketData_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	// The baseline schema ships no portfolio rows: seed synthetic positions, two of which (one per
	// portfolio) share a Yahoo ticker so the DISTINCT ON de-duplication bites.
	if _, err := pool.Exec(ctx, `
		WITH a AS (
			INSERT INTO assets (portfolio, key, name, exposure, asset_class) VALUES
				('VARIABLE', 'BTC', 'Bitcoin', 'CRYPTO', 'CRYPTO'),
				('FIXED', 'BTC', 'Bitcoin', 'CRYPTO', 'CRYPTO'),
				('VARIABLE', 'SPY', 'S&P 500', 'STOCKS', 'ETF')
			RETURNING id, key, portfolio
		)
		INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary)
		SELECT portfolio, key, CASE key WHEN 'BTC' THEN 'Bitcoin' ELSE 'S&P 500' END, CASE key WHEN 'BTC' THEN 'BINANCE'::exchange_enum ELSE 'XTB'::exchange_enum END,
			CASE key WHEN 'BTC' THEN 'CRYPTO'::asset_class_enum ELSE 'ETF'::asset_class_enum END,
			CASE key WHEN 'BTC' THEN 'CRYPTO'::exposure_enum ELSE 'STOCKS'::exposure_enum END,
			'USD', CASE key WHEN 'BTC' THEN 'BTC-USD' ELSE 'SPY' END, id, TRUE
		FROM a`); err != nil {
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
			Ticker:    "ETNVIRBTCP.WA",
			Price:     pgnum.FromDecimal(decimal.RequireFromString("65432.12345678")),
			Currency:  refsql.CurrencyEnumPLN,
			PricePln:  pgnum.FromDecimal(decimal.RequireFromString("65432.12345678")),
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
		if e.Ticker != "ETNVIRBTCP.WA" || e.Currency != "PLN" {
			t.Fatalf("unexpected entry: %+v", e)
		}
		if !e.Price.Equal(decimal.RequireFromString("65432.12345678")) {
			t.Errorf("expected price 65432.12345678, got %s", e.Price)
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

	t.Run("GetTrackedTickers reads positions read-only across both portfolios", func(t *testing.T) {
		// The test seeds synthetic rows into `positions` above for both VARIABLE and FIXED. The
		// query must succeed against the real schema (reading another phase's table is fine,
		// writing is not — this package never writes to `positions`) and every row must satisfy
		// the WHERE clause and be de-duplicated by ticker EVEN across portfolios (a ticker held
		// by both books must appear exactly once).
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
				t.Fatalf("expected DISTINCT ON (yahoo_ticker) to de-duplicate across portfolios, but %s appeared twice", *row.YahooTicker)
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
