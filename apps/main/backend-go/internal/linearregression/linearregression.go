// Package linearregression serves the linear-regression endpoint (ported from the Java
// LinearRegressionService, ADR-094) with zero database tables: it fits a log-linear OLS regression
// of ln(close) over the full OHLC history fetched live from Yahoo Finance (with the ADR-085
// Virtune-ETP reconstruction fallback for the four GPW-listed tickers Yahoo has no history for),
// then returns the filtered history rows plus `since` extrapolated future rows. The OHLC history
// clients are the Yahoo chart client, the Binance klines client and the historical price service;
// the Virtune ratio scrape is common-go/priceprovider's VirtuneClient (task 178).
package linearregression

import (
	"time"

	"investing-app/common-go/priceprovider"
)

// PriceSource tags an OHLC point's provenance (ADR-085 §3) — the service-layer mirror of
// the openapi PriceSource enum, kept separate per doc 13's layering rule (no openapi
// types in the service layer).
type PriceSource string

const (
	PriceSourceYahoo                    PriceSource = "YAHOO"
	PriceSourceReconstructedPrecise     PriceSource = "RECONSTRUCTED_PRECISE"
	PriceSourceReconstructedApproximate PriceSource = "RECONSTRUCTED_APPROXIMATE"
)

// OHLCPoint is one raw Yahoo/Binance OHLC candle — the direct port of
// YahooFinanceClient.OHLCPoint.
type OHLCPoint struct {
	Date                   time.Time // calendar date (midnight UTC)
	Open, Close, High, Low float64
}

// PricedOHLCPoint wraps an OHLCPoint with its provenance — the direct port of
// PricedOHLCPoint (HistoricalPriceService's boundary type).
type PricedOHLCPoint struct {
	Date                   time.Time
	Open, Close, High, Low float64
	Source                 PriceSource
}

// RatioSnapshot is a Virtune ETP's current coin-per-ETP entitlement ratio — the shared
// priceprovider shape (task 178), the direct port of VirtuneEtpClient.VirtuneRatioSnapshot.
type RatioSnapshot = priceprovider.VirtuneRatioSnapshot

// VirtuneProduct is one entry of the static 4-product registry of physically-backed
// Virtune crypto ETPs (ADR-085 §3/§6) — the direct port of VirtuneProduct. An explicit
// map, not a derived transform: a 5th ETP that doesn't fit an assumed pattern fails
// safe (unmapped tickers never trigger reconstruction).
type VirtuneProduct struct {
	YahooTicker        string
	VirtuneSlug        string
	BinanceSymbol      string
	CoinLabel          string
	FeeAnnual          float64
	StakingYieldAnnual float64
	GPWListingDate     time.Time
	ConfidenceTier     PriceSource
}

func utcDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// virtuneProducts mirrors VirtuneProduct.BY_YAHOO_TICKER 1:1 (fee/staking verified
// 2026-09-02 per ADR-085 §6 — re-verify against the live product pages if a
// reconstructed chart ever looks visibly wrong).
var virtuneProducts = map[string]VirtuneProduct{
	"ETNVIRBTCP.WA": {
		YahooTicker: "ETNVIRBTCP.WA", VirtuneSlug: "bitcoin-prime",
		BinanceSymbol: "BTCUSDT", CoinLabel: "BTC",
		FeeAnnual: 0.0025, StakingYieldAnnual: 0,
		GPWListingDate: utcDate(2026, time.February, 25),
		ConfidenceTier: PriceSourceReconstructedPrecise,
	},
	"ETNVIRXRP.WA": {
		YahooTicker: "ETNVIRXRP.WA", VirtuneSlug: "xrp",
		BinanceSymbol: "XRPUSDT", CoinLabel: "XRP",
		FeeAnnual: 0.0149, StakingYieldAnnual: 0,
		GPWListingDate: utcDate(2026, time.February, 25),
		ConfidenceTier: PriceSourceReconstructedPrecise,
	},
	"ETNVIRETH.WA": {
		YahooTicker: "ETNVIRETH.WA", VirtuneSlug: "ethereum",
		BinanceSymbol: "ETHUSDT", CoinLabel: "ETH",
		FeeAnnual: 0.014, StakingYieldAnnual: 0.032,
		GPWListingDate: utcDate(2026, time.February, 25),
		ConfidenceTier: PriceSourceReconstructedApproximate,
	},
	"ETNVIRSOL.WA": {
		YahooTicker: "ETNVIRSOL.WA", VirtuneSlug: "solana",
		BinanceSymbol: "SOLUSDT", CoinLabel: "SOL",
		FeeAnnual: 0.0095, StakingYieldAnnual: 0.03,
		GPWListingDate: utcDate(2026, time.February, 25),
		ConfidenceTier: PriceSourceReconstructedApproximate,
	},
}

// NetAnnualRate is the entitlement-growth rate per ADR-085 §4: staking yield minus fee,
// additive (not compounded).
func (p VirtuneProduct) NetAnnualRate() float64 {
	return p.StakingYieldAnnual - p.FeeAnnual
}

// LookupProduct returns the Virtune registry entry for a Yahoo ticker, if mapped.
func LookupProduct(yahooTicker string) (VirtuneProduct, bool) {
	p, ok := virtuneProducts[yahooTicker]
	return p, ok
}

// DataPoint is one row of log-linear regression output (RegressionPointDto): actual-price
// fields are nil for future extrapolated rows.
type DataPoint struct {
	Date                   time.Time
	Count                  int
	Close, High, Low       *float64
	LogN                   *float64
	Trendline, LnTrendline float64
	Residual               *float64
	Sd1Plus, Sd2Plus       float64
	Sd1Minus, Sd2Minus     float64
	Sd3Plus, Sd3Minus      float64
	Sd4Plus, Sd4Minus      float64
}

// Result is the fitted regression plus its output rows.
type Result struct {
	Ticker      string
	Interval    string
	Slope       float64
	Intercept   float64
	StdDev      float64
	DataPoints  []DataPoint
	PriceSource PriceSource
}
