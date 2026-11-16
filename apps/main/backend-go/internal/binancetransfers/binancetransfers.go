// Package binancetransfers serves the binance-transfers endpoints (ported from the Java
// BinanceTransferService, ADR-094): read/list/manual-entry/delete only.
//
// Write-ownership split (doc 13 §6.3): the purchase-completion-triggered insert into
// `binance_transfers` (the session-sourced rows) belongs to the purchasesession package. This package's
// only INSERT is InsertBinanceTransferManual, which hardcodes the manual origin AND session_id=NULL
// in SQL — there is deliberately no input field, parameter, or code path that could produce a
// completion-linked row (see service_test.go's negative test, which enforces this statically).
package binancetransfers

import (
	"time"

	"github.com/shopspring/decimal"
)

// TransferDTO is the service-layer shape of one binance_transfers row — free of sqlc
// row types and openapi response types (doc 13's layering rule). Amounts ride as
// decimal.Decimal end-to-end (never float64); NUMERIC(14,2)/NUMERIC(18,4) precision
// survives the round-trip exactly (asserted in integration_test.go).
type TransferDTO struct {
	ID             int64
	TransferDate   time.Time
	AmountPln      decimal.Decimal
	AmountNative   *decimal.Decimal
	NativeCurrency *string
	Comment        *string
	Source         string
	SessionID      *int64
	CreatedAt      time.Time
}

// YearlySum is one per-year SUM(amount_pln) row — the PIT-38 crypto
// acquisition-cost figure. Always computed over the WHOLE table, independent of
// the ?year filter (BinanceTransferService.list, ported as-is).
type YearlySum struct {
	Year     int
	TotalPln decimal.Decimal
}

// ListResult is one listBinanceTransfers result: the (optionally year-filtered)
// rows plus the whole-table yearly sums.
type ListResult struct {
	Data       []TransferDTO
	YearlySums []YearlySum
}

// CreateInput is a validated manual-entry request. There is deliberately no origin
// field of any kind: the server forces the manual origin with a NULL session link
// (BinanceTransferMapper ignores both fields, BinanceTransferService.create sets
// them explicitly, and the INSERT hardcodes the literals), so a client-supplied
// value has nowhere to go.
type CreateInput struct {
	TransferDate   time.Time
	AmountPln      decimal.Decimal
	AmountNative   *decimal.Decimal
	NativeCurrency *string
	Comment        *string
}
