// Package cryptotrades serves the crypto-trades endpoints (ported from the Java CryptoTradeService,
// ADR-094): paginated crypto-trade listing with ticker/year/exchange/side filters, manual trade
// entry (server forces source=MANUAL), and the annual PIT-38 tax summary. Owns `crypto_trades`
// exclusively (doc 13 §6.3: no second writer exists for this table).
package cryptotrades

import (
	"time"

	"github.com/shopspring/decimal"
)

// TradeDTO is the service-layer shape of one crypto_trades row — free of sqlc row types
// and openapi response types (doc 13's layering rule). Amounts ride as decimal.Decimal
// end-to-end (never float64); NUMERIC(18,8)/(18,4) precision survives the round-trip
// exactly (asserted in integration_test.go).
type TradeDTO struct {
	ID            int64
	TradeDate     time.Time
	Ticker        string
	Exchange      string
	AccountType   string
	Side          string
	Quantity      decimal.Decimal
	UnitPrice     decimal.Decimal
	PriceCurrency string
	TotalAmount   decimal.Decimal
	AmountPln     decimal.Decimal
	Source        string
	Notes         *string
	CreatedAt     time.Time
}

// PageDTO is one paginated listTrades result.
type PageDTO struct {
	Items  []TradeDTO
	Total  int64
	Number int
	Size   int
}

// TickerSummary is one per-ticker PIT-38 breakdown row (CryptoTaxTickerSummary).
type TickerSummary struct {
	Ticker               string
	TotalBuysPln         decimal.Decimal
	TotalSellsPln        decimal.Decimal
	NetGainPln           decimal.Decimal
	EstimatedBelkaTaxPln decimal.Decimal
}

// Summary is the annual PIT-38 result (CryptoTaxSummaryResult): yearly totals plus the
// per-ticker breakdown.
type Summary struct {
	Year                 int
	TotalBuysPln         decimal.Decimal
	TotalSellsPln        decimal.Decimal
	NetGainPln           decimal.Decimal
	EstimatedBelkaTaxPln decimal.Decimal
	ByTicker             []TickerSummary
}

// TradeInput is the minimal trade shape the pure TaxSummary computation needs — deliberately
// decoupled from TradeDTO/sqlc rows so the math is unit-testable without a database at all.
type TradeInput struct {
	Ticker    string
	Side      string
	AmountPln decimal.Decimal
}

// CreateInput is a validated manual-trade request. There is deliberately no Source
// field: the server forces source=MANUAL (CryptoTradeMapper constant + the INSERT's
// hardcoded literal), so a client-supplied value has nowhere to go.
type CreateInput struct {
	TradeDate     time.Time
	Ticker        string
	Exchange      string
	AccountType   string
	Side          string
	Quantity      decimal.Decimal
	UnitPrice     decimal.Decimal
	PriceCurrency string
	TotalAmount   decimal.Decimal
	AmountPln     decimal.Decimal
	Notes         *string
}
