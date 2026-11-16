package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/marketdatacore"
	"portfolio-c/backend-go/openapi"
)

// Golden wire test for the four market-data endpoints, driven through this app's own
// ServerInterface forwards (task 179 B9.1). The bytes in testdata/marketdata.golden were recorded
// against the per-app handler before it moved to common-go/marketdatacore.Handler; only the
// per-request meta (requestId, timestamp) and the synthetic PLN entry's fetchedAt (time.Now) are
// masked. Recorded once more after the move: the only bytes that changed were price, pricePln and
// rateToPln, from float32-rounded to the float64 of the decimal source (finding F3.8, which
// assertFloat64Numbers pins); the output is now identical to main's. Re-record with:
// go test ./cmd/server -run TestMarketDataGolden -update-golden
var updateGolden = flag.Bool("update-golden", false, "rewrite cmd/server/testdata/*.golden")

// goldenWarsaw pins a non-UTC offset like the one pgx hands back for timestamptz columns.
var goldenWarsaw = time.FixedZone("CEST", 2*60*60)

var goldenFetched = time.Date(2026, 9, 28, 11, 49, 52, 783035000, goldenWarsaw)

// goldenMarketStore is a deterministic marketdatacore.Store (fixed order, no DB).
type goldenMarketStore struct {
	prices  []marketdatacore.PriceEntry
	fx      []marketdatacore.FxRateEntry
	tickers []marketdatacore.TrackedTicker
}

func (s *goldenMarketStore) ListPrices(context.Context) ([]marketdatacore.PriceEntry, error) {
	return append([]marketdatacore.PriceEntry(nil), s.prices...), nil
}
func (s *goldenMarketStore) UpsertPrice(context.Context, marketdatacore.PriceEntry) error { return nil }
func (s *goldenMarketStore) ListFxRates(context.Context) ([]marketdatacore.FxRateEntry, error) {
	return append([]marketdatacore.FxRateEntry(nil), s.fx...), nil
}
func (s *goldenMarketStore) GetFxRate(_ context.Context, currency string) (marketdatacore.FxRateEntry, error) {
	for _, e := range s.fx {
		if e.FromCurrency == currency {
			return e, nil
		}
	}
	return marketdatacore.FxRateEntry{}, marketdatacore.ErrNotFound
}
func (s *goldenMarketStore) UpsertFxRate(context.Context, marketdatacore.FxRateEntry) error {
	return nil
}
func (s *goldenMarketStore) ListTrackedTickers(context.Context) ([]marketdatacore.TrackedTicker, error) {
	return append([]marketdatacore.TrackedTicker(nil), s.tickers...), nil
}

// goldenProvider knows a fixed set of prices; anything else is unavailable.
type goldenProvider struct {
	name   string
	prices map[string]decimal.Decimal
}

func (p goldenProvider) GetPrice(_ context.Context, ticker string) (decimal.Decimal, error) {
	if v, ok := p.prices[ticker]; ok {
		return v, nil
	}
	return decimal.Zero, errors.New(p.name + ": no price for " + ticker)
}
func (p goldenProvider) Name() string { return p.name }

func goldenDec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// The price and rate values have more than 7 significant digits on purpose: float32 rounding
// (finding F3.8) would show in the bytes.
func newGoldenMarketService() *marketdatacore.Service {
	store := &goldenMarketStore{
		prices: []marketdatacore.PriceEntry{
			{Ticker: "BTC-USD", Price: goldenDec("65432.1234"), Currency: "USD", PricePln: goldenDec("261728.49361234"), FetchedAt: goldenFetched},
			{Ticker: "AAPL", Price: goldenDec("200"), Currency: "USD", PricePln: goldenDec("800.5"), FetchedAt: goldenFetched.Add(-time.Hour)},
			{Ticker: "ETFBW20TR.WA", Price: goldenDec("123.456789"), Currency: "PLN", PricePln: goldenDec("123.456789"), FetchedAt: goldenFetched.UTC()},
		},
		fx: []marketdatacore.FxRateEntry{
			{FromCurrency: "USD", RateToPln: goldenDec("4.00012345"), Source: "YAHOO", FetchedAt: goldenFetched},
			{FromCurrency: "EUR", RateToPln: goldenDec("4.28171234"), Source: "YAHOO", FetchedAt: goldenFetched.UTC()},
		},
		tickers: []marketdatacore.TrackedTicker{
			{YahooTicker: "OK.WA", PriceCurrency: "PLN", Exchange: "XTB"},
			{YahooTicker: "GONE.WA", PriceCurrency: "PLN", Exchange: "XTB"},
		},
	}
	yahoo := goldenProvider{name: "YAHOO", prices: map[string]decimal.Decimal{"OK.WA": goldenDec("10.5")}}
	binance := goldenProvider{name: "BINANCE", prices: map[string]decimal.Decimal{}}
	return marketdatacore.NewService(store, yahoo, binance)
}

type goldenCase struct {
	name string
	call func(w http.ResponseWriter, r *http.Request)
}

var (
	maskRequestID = regexp.MustCompile(`"requestId":"[^"]*"`)
	maskTimestamp = regexp.MustCompile(`"timestamp":"[^"]*"`)
	fetchedAtRe   = regexp.MustCompile(`"fetchedAt":"([^"]*)"`)
)

// maskVolatile hides the per-request meta and any fetchedAt within a minute of now (the
// synthetic PLN entry); every fixed fake timestamp stays verbatim.
func maskVolatile(body string) string {
	body = maskRequestID.ReplaceAllString(body, `"requestId":"<masked>"`)
	body = maskTimestamp.ReplaceAllString(body, `"timestamp":"<masked>"`)
	return fetchedAtRe.ReplaceAllStringFunc(body, func(m string) string {
		v := fetchedAtRe.FindStringSubmatch(m)[1]
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil && time.Since(t).Abs() < time.Minute {
			return `"fetchedAt":"<now>"`
		}
		return m
	})
}

func runGolden(t *testing.T, file string, cases []goldenCase) string {
	t.Helper()
	var out strings.Builder
	for _, c := range cases {
		rec := httptest.NewRecorder()
		c.call(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		fmt.Fprintf(&out, "== %s %d %s\n%s\n", c.name, rec.Code, rec.Header().Get("Content-Type"), maskVolatile(strings.TrimRight(rec.Body.String(), "\n")))
	}
	got := out.String()
	if *updateGolden {
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read golden (run with -update-golden to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("wire bytes differ from %s\n--- got ---\n%s\n--- want ---\n%s", file, got, want)
	}
	return got
}

// assertFloat64Numbers checks every price/pricePln/rateToPln on the wire is exactly the float64
// of its decimal source (no float32 rounding, finding F3.8).
func assertFloat64Numbers(t *testing.T, golden string) {
	t.Helper()
	want := map[string]float64{}
	for _, s := range []string{"65432.1234", "261728.49361234", "200", "800.5", "123.456789", "4.00012345", "4.28171234", "1"} {
		f, _ := goldenDec(s).Float64()
		want[s] = f
	}
	allowed := map[float64]bool{}
	for _, f := range want {
		allowed[f] = true
	}
	for _, block := range strings.Split(golden, "== ")[1:] {
		lines := strings.SplitN(block, "\n", 2)
		if !strings.HasPrefix(lines[0], "get-") || !strings.Contains(lines[0], " 200 ") {
			continue
		}
		var body struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(lines[1]), &body); err != nil {
			t.Fatalf("%s: decode: %v", lines[0], err)
		}
		for _, e := range body.Data {
			for _, k := range []string{"price", "pricePln", "rateToPln"} {
				if v, ok := e[k]; ok && !allowed[v.(float64)] {
					t.Errorf("%s: %s = %v is not the float64 of any source decimal", lines[0], k, v)
				}
			}
		}
	}
}

func TestMarketDataGolden(t *testing.T) {
	s := &apiServer{marketDataAPI: marketdatacore.NewHandler(newGoldenMarketService())}
	long := strings.Repeat("A,", 251)
	cases := []goldenCase{
		{"get-prices-no-filter", func(w http.ResponseWriter, r *http.Request) { s.GetPrices(w, r, openapi.GetPricesParams{}) }},
		{"get-prices-ticker-filter", func(w http.ResponseWriter, r *http.Request) {
			s.GetPrices(w, r, openapi.GetPricesParams{Tickers: new(" BTC-USD, ETFBW20TR.WA ,UNKNOWN")})
		}},
		{"get-prices-blank-filter", func(w http.ResponseWriter, r *http.Request) {
			s.GetPrices(w, r, openapi.GetPricesParams{Tickers: new("")})
		}},
		{"get-prices-comma-only-filter", func(w http.ResponseWriter, r *http.Request) {
			s.GetPrices(w, r, openapi.GetPricesParams{Tickers: new(" , ")})
		}},
		{"get-prices-invalid-ticker", func(w http.ResponseWriter, r *http.Request) {
			s.GetPrices(w, r, openapi.GetPricesParams{Tickers: new("AAPL,btc-usd")})
		}},
		{"get-prices-too-long", func(w http.ResponseWriter, r *http.Request) {
			s.GetPrices(w, r, openapi.GetPricesParams{Tickers: &long})
		}},
		{"refresh-prices", func(w http.ResponseWriter, r *http.Request) { s.RefreshPrices(w, r) }},
		{"get-fx-rates", func(w http.ResponseWriter, r *http.Request) { s.GetFxRates(w, r) }},
		{"refresh-fx-rates", func(w http.ResponseWriter, r *http.Request) { s.RefreshFxRates(w, r) }},
	}
	got := runGolden(t, "testdata/marketdata.golden", cases)
	assertFloat64Numbers(t, got)
}
