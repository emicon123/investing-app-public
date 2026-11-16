// Package tradechart serves the trade-chart endpoints (ported from the Java TradeChartService,
// ADR-094): the ticker picker (built from DISTINCT asset_purchases.ticker, zero Yahoo calls) and
// the per-ticker raw price series plus trade markers. READ-ONLY — sources markers entirely from
// existing asset_purchases rows, no new write path (ADR-057). `positions` and `asset_purchases` are
// written by other packages (position, purchasesession); these SELECTs only read them.
package tradechart

import (
	"time"

	"github.com/shopspring/decimal"
)

// TickerEntry is one chartable ticker for the picker (TradeChartTickerEntry).
type TickerEntry struct {
	Ticker        string
	Name          string
	YahooTicker   string
	PriceCurrency string
	TradeCount    int64
	FirstTradeAt  time.Time
	LastTradeAt   time.Time
}

// ExcludedTicker is a traded ticker that cannot be charted, with its reason
// (NO_POSITION_MATCH / MISSING_YAHOO_TICKER — never a silent drop).
type ExcludedTicker struct {
	Ticker string
	Reason string
}

// TickersResult is the picker payload (TradeChartTickersResponse without the meta,
// which the handler adds).
type TickersResult struct {
	Data     []TickerEntry
	Excluded []ExcludedTicker
}

// PricePoint is one raw OHLC point of the price series (TradeChartPricePoint).
type PricePoint struct {
	Date        time.Time
	Open        float64
	Close       float64
	High        float64
	Low         float64
	PriceSource string
}

// Marker is one asset_purchases row rendered as a chart dot (TradeChartMarker).
type Marker struct {
	ID          int64
	PurchasedAt time.Time
	TradeType   string
	Portfolio   string
	AccountType string
	UnitsDelta  decimal.Decimal
	PricePln    decimal.Decimal
	PriceNative decimal.Decimal
	TotalPln    decimal.Decimal
	SessionID   *int64
}

// ChartData is the per-ticker payload (TradeChartData without the meta envelope).
type ChartData struct {
	Ticker        string
	YahooTicker   string
	PriceCurrency string
	Interval      string
	PriceSeries   []PricePoint
	Trades        []Marker
}
