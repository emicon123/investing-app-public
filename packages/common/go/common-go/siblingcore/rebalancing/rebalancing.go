// Package rebalancing is the portfolioB + portfolioC buy-only DCA flow (ADR-113): Calculate (proportional
// allocation of the deployable cash over the portfolio's assets — never a sell), Confirm (apply
// the bought units, cost, purchase lots and the deposit in one serialised transaction) and the
// purchase history. The one genuinely per-app step is the funding policy (Funding, a Strategy):
// portfolioB deploys account cash and records the monthly contribution, portfolioC deploys the request's
// deposit amount.
package rebalancing

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
)

// Store is the rebalancing service's port.
type Store interface {
	ListAssets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Asset, error)
	ListSubassets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error)
	ListSubassetsIncludingDeleted(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error)
	ListPurchaseLots(ctx context.Context, p siblingcore.Portfolio) ([]PurchaseLot, error)
	PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error)
	WriteHolding(ctx context.Context, subassetID int64, h siblingcore.Holding) error
	WriteCost(ctx context.Context, subassetID int64, cost *decimal.Decimal) error
	InsertPurchaseLot(ctx context.Context, lot PurchaseLot) error
	InsertDeposit(ctx context.Context, d siblingcore.Deposit) (siblingcore.Deposit, error)
}

// PurchaseLot is one `purchase_lots` row (ID and CreatedAt are set on reads only).
type PurchaseLot struct {
	ID                       int64
	Portfolio                siblingcore.Portfolio
	SessionID                uuid.UUID
	PurchasedAt              time.Time
	Ticker                   string
	Units, PricePln, CostPln decimal.Decimal
	CreatedAt                time.Time
}

// AllocateFunc is rebalancecalc.AllocateProportional's per-app account step.
type AllocateFunc func(buyList []rebalancecalc.BuyListItem, balances map[string]decimal.Decimal) []rebalancecalc.BuyListItem

// Budget is what Funding.Budget resolves for one Calculate call.
type Budget struct {
	DeployablePln decimal.Decimal
	CashOnHandPln *decimal.Decimal // portfolioB: Σ IKE/IKZE cash; nil in portfolioC (the wire has no such field)
	Allocate      AllocateFunc     // portfolioB: account-cash waterfall; portfolioC: identity
	Balances      map[string]decimal.Decimal
}

// Funding is the one genuinely per-app policy (Strategy). S is the app's concrete adapter, so
// the policy can reach its own tables (account cash, settings) through the same transaction.
type Funding[S any] interface {
	// Budget resolves the deployable cash for Calculate. Called outside any transaction.
	Budget(ctx context.Context, q S, p siblingcore.Portfolio, override *decimal.Decimal) (Budget, error)
	// Check validates a confirm request before the transaction opens
	// (portfolioC: depositAmountPln > 0; portfolioB: nil).
	Check(req ConfirmRequest) error
	// Settle runs inside Confirm's serialised transaction after holdings, costs and lots are
	// written. paid is Σ round4(units × pricePln) per bucket. It returns the deposit amount to
	// record (portfolioB: deducts account cash per IKE/IKZE account, returns monthly_contribution_pln;
	// portfolioC: returns req.DepositAmountPln).
	Settle(ctx context.Context, q S, req ConfirmRequest, paid map[siblingcore.Bucket]decimal.Decimal) (depositPln decimal.Decimal, err error)
}

// ConfirmItem is the units actually bought for one ticker, per bucket.
type ConfirmItem struct {
	Ticker string
	Units  siblingcore.Units // per-bucket units bought (portfolioC: SingleBucket only)
}

// ConfirmRequest is one confirmed purchase session.
type ConfirmRequest struct {
	Portfolio        siblingcore.Portfolio
	DepositAmountPln *decimal.Decimal // portfolioC: required; portfolioB: nil
	Items            []ConfirmItem
}

// AccountAlloc is one bucket's share of a buy item (portfolioB's account waterfall).
type AccountAlloc struct {
	Bucket               siblingcore.Bucket
	AllocPln, UnitsToBuy decimal.Decimal
}

// BuyItem is one suggested buy (never a sell).
type BuyItem struct {
	Ticker, Name, Exchange, PurchaseCurrency  string
	AllocatedPln, AllocatedNative, UnitsToBuy decimal.Decimal
	CurrentPct, TargetPct, DeviationPct       decimal.Decimal
	AccountAllocations                        []AccountAlloc // nil in portfolioC
	AvailableTickers                          []string
}

// DonutSlice is one asset's current vs target share.
type DonutSlice struct {
	Ticker, Name                                  string
	CurrentPct, TargetPct, ValuePln, DeviationPct decimal.Decimal
	Status                                        string // "ON_TARGET" | "OVER" | "UNDER"
}

// Result is one Calculate answer.
type Result struct {
	Portfolio              siblingcore.Portfolio
	CashOnHandPln          *decimal.Decimal
	DeployableCashPln      decimal.Decimal
	TotalPortfolioValuePln decimal.Decimal
	PricesRefreshedAt      time.Time
	BuyList                []BuyItem
	DonutData              []DonutSlice
}
