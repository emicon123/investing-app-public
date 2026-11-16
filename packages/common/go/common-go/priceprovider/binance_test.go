package priceprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
)

func newTestBinanceProvider(server *httptest.Server) *BinanceProvider {
	return &BinanceProvider{httpClient: server.Client(), baseURL: server.URL}
}

func TestToBinanceSymbol(t *testing.T) {
	tests := []struct {
		yahooTicker string
		want        string
		wantErr     bool
	}{
		{"DOGE-USD", "DOGEUSDT", false},
		{"SUI20947-USD", "SUIUSDT", false},
		{"TAO22974-USD", "TAOUSDT", false},
		{"BTC-USD", "BTCUSDT", false},
		{"", "", true},
	}
	for _, tc := range tests {
		got, err := ToBinanceSymbol(tc.yahooTicker)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ToBinanceSymbol(%q): expected error, got nil", tc.yahooTicker)
			}
			continue
		}
		if err != nil {
			t.Errorf("ToBinanceSymbol(%q): unexpected error: %v", tc.yahooTicker, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ToBinanceSymbol(%q) = %q, want %q", tc.yahooTicker, got, tc.want)
		}
	}
}

func TestBinanceProvider_GetPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbol") != "BTCUSDT" {
			http.Error(w, "unexpected symbol", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprint(w, `{"symbol":"BTCUSDT","price":"65432.10000000"}`)
	}))
	defer server.Close()

	p := newTestBinanceProvider(server)
	price, err := p.GetPrice(context.Background(), "BTC-USD")
	if err != nil {
		t.Fatalf("GetPrice: %v", err)
	}
	if !price.Equal(decimal.RequireFromString("65432.10000000")) {
		t.Fatalf("expected 65432.1, got %s", price)
	}
}

func TestBinanceProvider_GetPrice_FailsSoftOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestBinanceProvider(server)
	if _, err := p.GetPrice(context.Background(), "BTC-USD"); err == nil {
		t.Fatal("expected an error on HTTP 500")
	}
}

func TestBinanceProvider_GetPrice_FailsSoftOnEmptyPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"symbol":"BTCUSDT","price":""}`)
	}))
	defer server.Close()

	p := newTestBinanceProvider(server)
	if _, err := p.GetPrice(context.Background(), "BTC-USD"); err == nil {
		t.Fatal("expected an error on empty price field")
	}
}

func TestBinanceProvider_Name(t *testing.T) {
	if got := NewBinanceProvider().Name(); got != "BINANCE" {
		t.Fatalf("expected BINANCE, got %s", got)
	}
}
