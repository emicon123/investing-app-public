package marketdatacore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
)

func newTestHandler(store *fakeStore) *Handler {
	yahoo := &fakeProvider{name: "YAHOO", prices: map[string]decimal.Decimal{"OK.WA": decimal.RequireFromString("10")}}
	binance := &fakeProvider{name: "BINANCE"}
	return NewHandler(NewService(store, yahoo, binance))
}

func storeWithPrices(tickers ...string) *fakeStore {
	s := newFakeStore()
	for _, t := range tickers {
		s.prices[t] = PriceEntry{Ticker: t, Price: decimal.RequireFromString("1"), Currency: "USD", PricePln: decimal.RequireFromString("4"), FetchedAt: time.Now()}
	}
	return s
}

func getPrices(t *testing.T, h *Handler, tickers *string) (*httptest.ResponseRecorder, []priceEntryJSON) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.GetPrices(rec, httptest.NewRequest(http.MethodGet, "/api/market-data/prices", nil), tickers)
	var body httpx.ListResponse[priceEntryJSON]
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
	}
	return rec, body.Data
}

func tickersOf(entries []priceEntryJSON) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Ticker] = true
	}
	return out
}

func TestHandler_GetPrices_Filtering(t *testing.T) {
	tests := []struct {
		name    string
		tickers *string
		want    []string
	}{
		{"no param: everything", nil, []string{"AAPL", "MSFT"}},
		{"filter", new("AAPL"), []string{"AAPL"}},
		{"segments are trimmed, unknown tickers ignored", new(" MSFT , NOPE"), []string{"MSFT"}},
		// MarketDataController's isBlank() edge case: present but empty means "no filter".
		{"blank param: everything", new(""), []string{"AAPL", "MSFT"}},
		{"whitespace-only param: everything", new("   "), []string{"AAPL", "MSFT"}},
		// ...while commas make it "not blank": it filters down to nothing (Java quirk, ported).
		{"comma-only param: nothing", new(" , "), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, data := getPrices(t, newTestHandler(storeWithPrices("AAPL", "MSFT")), tc.tickers)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			got := tickersOf(data)
			if len(got) != len(tc.want) {
				t.Fatalf("tickers = %v, want %v", got, tc.want)
			}
			for _, w := range tc.want {
				if !got[w] {
					t.Fatalf("tickers = %v, want %v", got, tc.want)
				}
			}
			if !strings.Contains(rec.Body.String(), `"data":[`) {
				t.Fatalf("data must be a JSON array, got %s", rec.Body.String())
			}
		})
	}
}

func TestHandler_GetPrices_Validation(t *testing.T) {
	tests := []struct {
		name, tickers, field, message string
	}{
		{"lower-case ticker", "AAPL,aapl", "", "Invalid ticker: aapl"},
		{"bad character", "AA$PL", "", "Invalid ticker: AA$PL"},
		{"segment over 50 chars", strings.Repeat("A", 51), "", "Invalid ticker: " + strings.Repeat("A", 51)},
		{"param over 500 chars", strings.Repeat("A,", 251), "tickers", "size must be between 0 and 500"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := getPrices(t, newTestHandler(storeWithPrices("AAPL")), &tc.tickers)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			var body httpx.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Errors) != 1 || body.Errors[0].Code != httpx.CodeValidationError ||
				body.Errors[0].Field != tc.field || body.Errors[0].Message != tc.message {
				t.Fatalf("errors = %+v", body.Errors)
			}
		})
	}
}

func TestHandler_GetPrices_ExactlyFiveHundredCharsIsAllowed(t *testing.T) {
	raw := strings.Repeat("A,", 249) + "AA" // 500 chars
	rec, _ := getPrices(t, newTestHandler(storeWithPrices("AA")), &raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_GetPrices_NumbersAreFloat64(t *testing.T) {
	s := newFakeStore()
	s.prices["BTC-USD"] = PriceEntry{Ticker: "BTC-USD", Price: decimal.RequireFromString("65432.1234"), Currency: "USD",
		PricePln: decimal.RequireFromString("261728.49361234"), FetchedAt: time.Now()}
	rec, _ := getPrices(t, newTestHandler(s), nil)
	if body := rec.Body.String(); !strings.Contains(body, `"price":65432.1234`) || !strings.Contains(body, `"pricePln":261728.49361234`) {
		t.Fatalf("expected full float64 precision, got %s", body)
	}
}

func TestHandler_RefreshPrices_RawRefreshResult(t *testing.T) {
	s := newFakeStore()
	s.tickers = []TrackedTicker{{YahooTicker: "OK.WA", PriceCurrency: "PLN", Exchange: "XTB"}, {YahooTicker: "GONE.WA", PriceCurrency: "PLN", Exchange: "XTB"}}
	rec := httptest.NewRecorder()
	newTestHandler(s).RefreshPrices(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got RefreshResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || got.Succeeded != 1 || len(got.Failures) != 1 || got.Failures[0].Ticker != "GONE.WA" {
		t.Fatalf("result = %+v", got)
	}
	if strings.Contains(rec.Body.String(), `"meta"`) {
		t.Fatalf("refresh result is written raw, not enveloped: %s", rec.Body.String())
	}
}

func TestHandler_GetFxRates_PLNFirst(t *testing.T) {
	s := newFakeStore()
	s.fxRates["EUR"] = FxRateEntry{FromCurrency: "EUR", RateToPln: decimal.RequireFromString("4.28171234"), Source: "YAHOO", FetchedAt: time.Now()}
	rec := httptest.NewRecorder()
	newTestHandler(s).GetFxRates(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var body httpx.ListResponse[fxRateEntryJSON]
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(body.Data) != 2 || body.Data[0].FromCurrency != "PLN" || body.Data[0].RateToPln != 1 ||
		body.Data[1].FromCurrency != "EUR" || body.Data[1].RateToPln != 4.28171234 {
		t.Fatalf("status %d, data = %+v", rec.Code, body.Data)
	}
}

func TestHandler_RefreshFxRates_Is204(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler(newFakeStore()).RefreshFxRates(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body %q; want 204 and no body", rec.Code, rec.Body.String())
	}
}
