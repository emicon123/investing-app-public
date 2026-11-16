package refdata

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/inflationsync"
	"investing-app/common-go/marketdatacore"
	"investing-app/common-go/settingscore"
	"investing-app/common-go/testsupport"
)

// TestRefdata_ParityAcrossBothApps runs every Store method against portfolioB's AND portfolioC's migrated
// databases (their real Flyway migrations): one query set serves both, so a schema drift in
// either app's reference tables fails here (task 180 wave 5).
func TestRefdata_ParityAcrossBothApps(t *testing.T) {
	for app, seedPosition := range map[string]string{
		"portfolio-b": `INSERT INTO assets (key, name, exposure, asset_class) VALUES ('R', 'R', 'STOCKS', 'ETF');
			INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary)
			VALUES ('R', 'R', 'XTB', 'ETF', 'STOCKS', 'EUR', 'R.DE', (SELECT id FROM assets WHERE key = 'R'), true)`,
		"portfolio-c": `INSERT INTO assets (portfolio, key, name, exposure, asset_class) VALUES ('FIXED', 'R', 'R', 'STOCKS', 'ETF');
			INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker, asset_id, is_primary)
			VALUES ('FIXED', 'R', 'R', 'XTB', 'ETF', 'STOCKS', 'EUR', 'R.DE', (SELECT id FROM assets WHERE key = 'R'), true)`,
	} {
		t.Run(app, func(t *testing.T) {
			pg := testsupport.StartMigratedPostgres(t, "../../../../../../apps/"+app+"/db/migration")
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, pg.ConnString)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, price_cache, fx_rate_cache, inflation CASCADE;
				INSERT INTO settings (key, value) VALUES ('refdata_test', 'a') ON CONFLICT (key) DO NOTHING`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, seedPosition); err != nil {
				t.Fatal(err)
			}
			assertStore(t, ctx, New(pool))
		})
	}
}

func assertStore(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	d := decimal.RequireFromString

	// market data
	if err := s.UpsertPrice(ctx, marketdatacore.PriceEntry{Ticker: "R.DE", Price: d("10.5"), Currency: "EUR", PricePln: d("45.15"), FetchedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFxRate(ctx, marketdatacore.FxRateEntry{FromCurrency: "EUR", RateToPln: d("4.3"), Source: "TEST", FetchedAt: at}); err != nil {
		t.Fatal(err)
	}
	prices, err := s.ListPrices(ctx)
	if err != nil || len(prices) != 1 || !prices[0].PricePln.Equal(d("45.15")) || prices[0].Currency != "EUR" {
		t.Fatalf("ListPrices %+v %v", prices, err)
	}
	fx, err := s.GetFxRate(ctx, "EUR")
	if err != nil || !fx.RateToPln.Equal(d("4.3")) || fx.Source != "TEST" {
		t.Fatalf("GetFxRate %+v %v", fx, err)
	}
	if _, err := s.GetFxRate(ctx, "USD"); !errors.Is(err, marketdatacore.ErrNotFound) {
		t.Fatalf("GetFxRate(USD) %v", err)
	}
	if rates, err := s.ListFxRates(ctx); err != nil || len(rates) != 1 {
		t.Fatalf("ListFxRates %+v %v", rates, err)
	}
	tracked, err := s.ListTrackedTickers(ctx)
	if err != nil || len(tracked) != 1 || tracked[0].YahooTicker != "R.DE" || tracked[0].PriceCurrency != "EUR" || tracked[0].Exchange != "XTB" {
		t.Fatalf("ListTrackedTickers %+v %v", tracked, err)
	}

	// price source
	pp, err := s.PricePlnByYahooTicker(ctx)
	if err != nil || !pp["R.DE"].Equal(d("45.15")) {
		t.Fatalf("PricePlnByYahooTicker %v %v", pp, err)
	}
	rates, err := s.AllRatesToPln(ctx)
	if err != nil || !rates["PLN"].Equal(d("1")) || !rates["EUR"].Equal(d("4.3")) {
		t.Fatalf("AllRatesToPln %v %v", rates, err)
	}
	fetched, err := s.FetchedAtByYahooTicker(ctx)
	if err != nil || !fetched["R.DE"].Equal(at) {
		t.Fatalf("FetchedAtByYahooTicker %v %v", fetched, err)
	}

	// settings
	if row, err := s.Update(ctx, "refdata_test", "b"); err != nil || row.Value != "b" {
		t.Fatalf("Update %+v %v", row, err)
	}
	if row, err := s.Get(ctx, "refdata_test"); err != nil || row.Value != "b" {
		t.Fatalf("Get %+v %v", row, err)
	}
	if _, err := s.Get(ctx, "no_such_key"); !errors.Is(err, settingscore.ErrNotFound) {
		t.Fatalf("Get(missing) %v", err)
	}
	if _, err := s.Update(ctx, "no_such_key", "x"); !errors.Is(err, settingscore.ErrNotFound) {
		t.Fatalf("Update(missing) %v", err)
	}
	if rows, err := s.List(ctx); err != nil || len(rows) == 0 {
		t.Fatalf("List %+v %v", rows, err)
	}

	// inflation
	if _, err := s.GetCpi(ctx, 2026, 1); !errors.Is(err, inflationsync.ErrNotFound) {
		t.Fatalf("GetCpi(missing) %v", err)
	}
	if err := s.CreateEntry(ctx, 2026, 1, d("100.5")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEntry(ctx, 2025, 12, d("100.1")); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCpi(ctx, 2026, 1, d("100.7")); err != nil {
		t.Fatal(err)
	}
	if cpi, err := s.GetCpi(ctx, 2026, 1); err != nil || !cpi.Equal(d("100.7")) {
		t.Fatalf("GetCpi %v %v", cpi, err)
	}
	months, err := s.ListAsc(ctx)
	if err != nil || len(months) != 2 || months[0] != (inflationsync.YearMonth{Year: 2025, Month: 12}) {
		t.Fatalf("ListAsc %+v %v", months, err)
	}
	mom, err := s.MomAsc(ctx)
	if err != nil || len(mom) != 2 {
		t.Fatalf("MomAsc %v %v", mom, err)
	}

	assertListsSorted(t, ctx, s, at)
}

// assertListsSorted: the price and FX lists come back ordered by key whatever order the rows were
// written in (task 187 B1 / C1). Without ORDER BY Postgres returns heap order, which changes after
// every refresh upsert (each one writes a new tuple version).
func assertListsSorted(t *testing.T, ctx context.Context, s *Store, at time.Time) {
	t.Helper()
	d := decimal.RequireFromString
	for _, ticker := range []string{"Z.DE", "M.DE", "A.DE"} { // reverse key order
		if err := s.UpsertPrice(ctx, marketdatacore.PriceEntry{Ticker: ticker, Price: d("1"), Currency: "EUR", PricePln: d("4"), FetchedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	// Re-upserting the first-written row moves it to the end of the heap.
	if err := s.UpsertPrice(ctx, marketdatacore.PriceEntry{Ticker: "Z.DE", Price: d("2"), Currency: "EUR", PricePln: d("8"), FetchedAt: at}); err != nil {
		t.Fatal(err)
	}
	for _, cur := range []string{"USD", "CHF"} { // EUR already exists; USD and CHF sort around it
		if err := s.UpsertFxRate(ctx, marketdatacore.FxRateEntry{FromCurrency: cur, RateToPln: d("4"), Source: "TEST", FetchedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpsertFxRate(ctx, marketdatacore.FxRateEntry{FromCurrency: "EUR", RateToPln: d("4.4"), Source: "TEST", FetchedAt: at}); err != nil {
		t.Fatal(err)
	}

	prices, err := s.ListPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var tickers []string
	for _, p := range prices {
		tickers = append(tickers, p.Ticker)
	}
	if want := []string{"A.DE", "M.DE", "R.DE", "Z.DE"}; !slices.Equal(tickers, want) {
		t.Errorf("ListPrices order = %v, want %v", tickers, want)
	}
	rates, err := s.ListFxRates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var currencies []string
	for _, r := range rates {
		currencies = append(currencies, r.FromCurrency)
	}
	// ORDER BY a currency_enum column sorts by enum declaration order (PLN, EUR, USD, CHF, GBP),
	// not alphabetically; main's GetAllFxRateCache sorts the same way.
	if want := []string{"EUR", "USD", "CHF"}; !slices.Equal(currencies, want) {
		t.Errorf("ListFxRates order = %v, want %v", currencies, want)
	}
}
