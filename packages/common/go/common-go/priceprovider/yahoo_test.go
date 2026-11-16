package priceprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// newTestYahooProvider builds a YahooProvider wired against a single httptest.Server for all
// three Yahoo hosts (cookie seed, quote, chart) — the handler dispatches by path, same as the
// real Yahoo hosts would by hostname.
func newTestYahooProvider(server *httptest.Server) *YahooProvider {
	return &YahooProvider{
		chartClient:   server.Client(),
		authClient:    server.Client(),
		chartBaseURL:  server.URL,
		quoteHost:     server.URL,
		cookieSeedURL: server.URL + "/",
	}
}

func TestYahooProvider_GetPrice_ViaQuote_Regular(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "A1=seed; Path=/")
		w.WriteHeader(http.StatusNotFound) // fc.yahoo.com genuinely 404s but still sets cookies
	})
	mux.HandleFunc("/v1/test/getcrumb", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "abc123crumb")
	})
	mux.HandleFunc("/v7/finance/quote", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"quoteResponse":{"result":[{"marketState":"REGULAR","regularMarketPrice":123.45}]}}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newTestYahooProvider(server)
	price, err := p.GetPrice(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if !price.Equal(decimal.NewFromFloat(123.45)) {
		t.Fatalf("expected 123.45, got %s", price)
	}
}

func TestYahooProvider_GetPrice_ViaQuote_MarketStateSelection(t *testing.T) {
	tests := []struct {
		name          string
		marketState   string
		regularPrice  float64
		preMarket     *float64
		postMarket    *float64
		expectedPrice float64
	}{
		{"regular uses regular price", "REGULAR", 100, nil, nil, 100},
		{"closed uses regular price", "CLOSED", 100, nil, nil, 100},
		{"pre uses pre-market price", "PRE", 100, new(101.0), nil, 101},
		{"pre falls back to regular when pre-market nil", "PRE", 100, nil, nil, 100},
		{"post uses post-market price", "POST", 100, nil, new(99.0), 99},
		{"prepre uses post-market price (overnight fallback)", "PREPRE", 100, nil, new(98.0), 98},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Set-Cookie", "A1=seed; Path=/")
				w.WriteHeader(http.StatusNotFound)
			})
			mux.HandleFunc("/v1/test/getcrumb", func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, "abc123crumb")
			})
			mux.HandleFunc("/v7/finance/quote", func(w http.ResponseWriter, r *http.Request) {
				body := fmt.Sprintf(`{"quoteResponse":{"result":[{"marketState":%q,"regularMarketPrice":%v`,
					tc.marketState, tc.regularPrice)
				if tc.preMarket != nil {
					body += fmt.Sprintf(`,"preMarketPrice":%v`, *tc.preMarket)
				}
				if tc.postMarket != nil {
					body += fmt.Sprintf(`,"postMarketPrice":%v`, *tc.postMarket)
				}
				body += `}]}}`
				_, _ = fmt.Fprint(w, body)
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			p := newTestYahooProvider(server)
			price, err := p.GetPrice(context.Background(), "AAPL")
			if err != nil {
				t.Fatalf("GetPrice: %v", err)
			}
			if !price.Equal(decimal.NewFromFloat(tc.expectedPrice)) {
				t.Fatalf("expected %v, got %s", tc.expectedPrice, price)
			}
		})
	}
}

func TestYahooProvider_GetPrice_FallsBackToChart_WhenQuoteUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	// Cookie seed / crumb endpoints are unreachable (session can never be established), forcing
	// fetchViaQuote to fail and fetchViaChart to be used instead.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/v8/finance/chart/BTC-USD", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"chart":{"result":[{"meta":{"regularMarketPrice":65000.5,"hasPrePostMarketData":false}}]}}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newTestYahooProvider(server)
	price, err := p.GetPrice(context.Background(), "BTC-USD")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if !price.Equal(decimal.NewFromFloat(65000.5)) {
		t.Fatalf("expected 65000.5, got %s", price)
	}
}

func TestYahooProvider_GetPrice_ReturnsError_WhenBothPathsFail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	// No handler registered for /v8/finance/chart/* -> 404, which is also a failure.
	server := httptest.NewServer(mux)
	defer server.Close()

	p := newTestYahooProvider(server)
	if _, err := p.GetPrice(context.Background(), "NOPE"); err == nil {
		t.Fatal("expected an error when both the quote and chart paths fail")
	}
}

func TestYahooProvider_Name(t *testing.T) {
	if got := NewYahooProvider().Name(); got != "YAHOO" {
		t.Fatalf("expected YAHOO, got %s", got)
	}
}

func TestYahooProvider_RespectsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestYahooProvider(server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	if _, err := p.GetPrice(ctx, "AAPL"); err == nil {
		t.Fatal("expected an error when the context is cancelled before any request completes")
	}
}
