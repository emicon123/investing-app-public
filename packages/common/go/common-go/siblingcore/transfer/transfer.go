// Package transfer is the portfolioB + portfolioC manual "Sprzedaj" transfer (ADR-113): one sold leg and
// one or more buy legs recorded as one serialised transaction — units moved per bucket, the cost
// basis planned by the shared kernel, one position_transfers audit row per leg. The backend never
// computes a sell: the sold leg is exactly what the user submitted (manual-sell-only rule).
package transfer

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"investing-app/common-go/siblingcore"
)

// Store is the transfer service's port; every call runs inside the serialised transaction.
type Store interface {
	GetSubassetByTicker(ctx context.Context, p siblingcore.Portfolio, ticker string) (siblingcore.Subasset, error) // live row
	ListSubassets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error)
	PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error)   // price_cache, read through the tx
	WriteHolding(ctx context.Context, subassetID int64, h siblingcore.Holding) error // UpdatePositionDelta
	WriteCost(ctx context.Context, subassetID int64, cost *decimal.Decimal) error    // UpdatePositionCost
	InsertTransferLeg(ctx context.Context, r LegRecord) error
}

// Leg is one side of a manual transfer. Bucket must be one of the app's buckets
// (Config.CheckBucket): an account type in portfolioB, SingleBucket in portfolioC.
type Leg struct {
	Ticker          string
	Bucket          siblingcore.Bucket
	Units, PricePln decimal.Decimal
}

// Request is one transfer within one portfolio.
type Request struct {
	Portfolio siblingcore.Portfolio
	Sold      Leg
	Buys      []Leg
}

// Result echoes the request with the generated transfer id.
type Result struct {
	TransferID uuid.UUID
	Sold       Leg
	Buys       []Leg
}

// LegRecord is one `position_transfers` audit row (the adapter stores Bucket as account_type in
// portfolioB and Portfolio in portfolioC).
type LegRecord struct {
	TransferID                     uuid.UUID
	Portfolio                      siblingcore.Portfolio
	Ticker                         string
	Bucket                         siblingcore.Bucket
	UnitsDelta, PricePln, TotalPln decimal.Decimal
	TransferredAt                  time.Time
}
