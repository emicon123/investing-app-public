package priceprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// BinanceProvider fetches current prices from Binance's public ticker-price endpoint (no API key
// required); ported from the Java BinanceClient (ADR-094).
//
// Endpoint: GET https://api.binance.com/api/v3/ticker/price?symbol=DOGEUSDT
// Response: {"symbol":"DOGEUSDT","price":"0.16320000"}
type BinanceProvider struct {
	httpClient *http.Client
	// baseURL — overridable (unexported) so unit tests can point this provider at an
	// httptest.Server instead of the real Binance host; production code always gets the default
	// set by NewBinanceProvider.
	baseURL string
}

const binanceBaseURL = "https://api.binance.com"

// trailingDigitsPattern strips CoinMarketCap numeric IDs Yahoo appends to some crypto tickers
// (e.g. "SUI20947" -> "SUI"), mirroring BinanceClient.toSymbol's regex.
var trailingDigitsPattern = regexp.MustCompile(`\d+$`)

// NewBinanceProvider constructs a BinanceProvider with the same timeout budget as YahooProvider.
func NewBinanceProvider() *BinanceProvider {
	return &BinanceProvider{httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: binanceBaseURL}
}

// Name identifies this provider in logs — "BINANCE", matching BinancePriceProvider.name().
func (*BinanceProvider) Name() string { return "BINANCE" }

// ToBinanceSymbol converts a Yahoo Finance ticker to a Binance trading symbol, the direct port of
// BinanceClient.toSymbol:
//  1. Strip the "-USD" suffix (Yahoo Finance forex-pair notation).
//  2. Strip any trailing digits (CoinMarketCap numeric IDs Yahoo appends, e.g. "SUI20947" -> "SUI").
//  3. Append "USDT".
//
// Examples: "DOGE-USD" -> "DOGEUSDT", "SUI20947-USD" -> "SUIUSDT", "BTC-USD" -> "BTCUSDT".
func ToBinanceSymbol(yahooTicker string) (string, error) {
	if yahooTicker == "" {
		return "", fmt.Errorf("binance: yahooTicker must not be empty")
	}
	symbol := strings.TrimSuffix(yahooTicker, "-USD")
	symbol = trailingDigitsPattern.ReplaceAllString(symbol, "")
	return symbol + "USDT", nil
}

type binanceTickerPrice struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

// GetPrice fetches yahooTicker's current USD price from Binance's public ticker endpoint, the
// direct port of BinanceClient.fetchPrice. Returns an error on any failure (bad ticker, HTTP
// error, missing field, parse error) — never propagates a panic.
func (b *BinanceProvider) GetPrice(ctx context.Context, yahooTicker string) (decimal.Decimal, error) {
	symbol, err := ToBinanceSymbol(yahooTicker)
	if err != nil {
		return decimal.Zero, err
	}

	reqURL := b.baseURL + "/api/v3/ticker/price?symbol=" + url.QueryEscape(symbol)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("binance: build request for symbol %s: %w", symbol, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return decimal.Zero, fmt.Errorf("binance: request failed for symbol %s (ticker %s): %w", symbol, yahooTicker, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decimal.Zero, fmt.Errorf("binance: unexpected status %d for symbol %s (ticker %s)", resp.StatusCode, symbol, yahooTicker)
	}

	var body binanceTickerPrice
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return decimal.Zero, fmt.Errorf("binance: decode response for symbol %s: %w", symbol, err)
	}
	if body.Price == "" {
		return decimal.Zero, fmt.Errorf("binance: empty response for symbol %s (ticker %s)", symbol, yahooTicker)
	}

	price, err := decimal.NewFromString(body.Price)
	if err != nil {
		return decimal.Zero, fmt.Errorf("binance: parse price %q for symbol %s: %w", body.Price, symbol, err)
	}
	return price, nil
}
