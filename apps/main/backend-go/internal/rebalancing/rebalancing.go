// Package rebalancing serves the rebalancing-transfer endpoints (ported from the Java
// RebalancingTransferService, ADR-094 / doc 13 §7). This IS the sanctioned manual "Sprzedaj" sell
// path (docs/architecture/05-rebalancing-algorithm.md §7.5) — the sold leg comes entirely from the
// request DTO, never computed from internal/rebalancingcalc. This package does not import
// internal/rebalancingcalc at all (the Java service likewise never referenced the rebalancing
// strategies).
package rebalancing

import (
	"context"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls.
type Queries interface {
	ListAllPositions(ctx context.Context) ([]sqlcgen.Position, error)
	UpdatePositionDelta(ctx context.Context, arg sqlcgen.UpdatePositionDeltaParams) (sqlcgen.Position, error)
	// Task 161a-2 asset-level cost bookkeeping: sibling rows' cost writes and the tx-scoped
	// price_cache read (DB only) the costbasis kernel values the assets with.
	UpdatePositionCost(ctx context.Context, arg sqlcgen.UpdatePositionCostParams) error
	GetAllPriceCache(ctx context.Context) ([]sqlcgen.GetAllPriceCacheRow, error)
	InsertAssetPurchase(ctx context.Context, arg sqlcgen.InsertAssetPurchaseParams) (sqlcgen.AssetPurchase, error)
	InsertDeposit(ctx context.Context, arg sqlcgen.InsertDepositParams) (sqlcgen.Deposit, error)
}

// TransactFunc runs fn inside a single DB transaction spanning the ENTIRE ExecuteTransfer call
// (architect-output §4).
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// Leg is one side of a transfer — either the sold leg or one entry in buys (the sold and buy legs
// share one shape).
type Leg struct {
	Ticker          string
	AccountType     string // "IKE" | "IKZE" | "REGULAR"
	RegularCurrency *string
	Units           decimal.Decimal
	PricePln        decimal.Decimal
}

// TransferRequest is the direct port of RebalancingTransferRequest.
type TransferRequest struct {
	Portfolio string
	Sold      Leg
	Buys      []Leg
}

// TransferResult is the direct port of RebalancingTransferResult.
type TransferResult struct {
	AffectedTickers []string
	Message         string
}
