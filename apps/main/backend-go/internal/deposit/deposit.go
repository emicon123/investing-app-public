package deposit

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/rebalancingcalc"
	"investing-app/common-go/xirr"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls. No TransactFunc exists in
// this package (architect-output §4: manual create is a single-statement write; XIRR/
// account-cap-usage are read-only) — the same shape as the other single-statement-write domains.
type Queries interface {
	CapUsageQueries
	rebalancingcalc.Queries // XIRR needs computeValues' equivalent (current portfolio value)
	ListDeposits(ctx context.Context, arg sqlcgen.ListDepositsParams) ([]sqlcgen.Deposit, error)
	CountDeposits(ctx context.Context, arg sqlcgen.CountDepositsParams) (int64, error)
	InsertDeposit(ctx context.Context, arg sqlcgen.InsertDepositParams) (sqlcgen.Deposit, error)
	UpsertAccountCapAdjustment(ctx context.Context, arg sqlcgen.UpsertAccountCapAdjustmentParams) (sqlcgen.AccountCapAdjustment, error)
	// ListHistoriaExternalDeposits is defined once in db/queries/historia.sql (owned by
	// internal/historia) and reused here read-only per doc 13 §6.3 — the same cross-package
	// sqlc-method-name reuse convention as GetSettingByKey. Exactly the (deposit_date, amount,
	// currency) shape XIRR needs for EXTERNAL deposits of one portfolio.
	ListHistoriaExternalDeposits(ctx context.Context, arg sqlcgen.ListHistoriaExternalDepositsParams) ([]sqlcgen.ListHistoriaExternalDepositsRow, error)
}

// DepositDTO is the service-layer shape of one deposits row — the direct port of DepositDto.
type DepositDTO struct {
	ID          int64
	DepositDate time.Time
	Amount      decimal.Decimal
	Currency    string
	Exchange    string
	AccountType string
	Portfolio   string
	Comment     *string
	CreatedAt   time.Time
}

// CreateInput is the service-layer shape of CreateDepositRequest.
type CreateInput struct {
	DepositDate time.Time
	Amount      decimal.Decimal
	Currency    string
	Exchange    string
	AccountType string
	Portfolio   string
	Comment     *string
}

// ListFilter mirrors DepositsController.listDeposits' query params.
type ListFilter struct {
	Portfolio   *string
	AccountType *string
	Exchange    *string
	Year        *int
}

// AccountCapUsageItem is the direct port of AccountCapUsageItem.
type AccountCapUsageItem struct {
	AccountType       string
	Year              int
	TotalDepositedPln decimal.Decimal
	AnnualCapPln      decimal.Decimal
	RemainingPln      decimal.Decimal
	UtilizationPct    decimal.Decimal
}

// CashFlow is one dated PLN cash flow of the XIRR series (common-go/xirr, ADR-112).
type CashFlow = xirr.Flow

// XirrResult is the direct port of XirrService.XirrResult.
type XirrResult struct {
	XirrPct          *decimal.Decimal
	TotalInvestedPln decimal.Decimal
	CurrentValuePln  decimal.Decimal
	SimpleReturnPct  *decimal.Decimal
	YearsElapsed     *decimal.Decimal
}

// PriceSource is this package's read-only dependency into internal/marketdata — embeds
// rebalancingcalc.PriceSource (for XIRR's current-portfolio-value computation) plus RateToPln
// (mirrors FxRateCacheService.getRateToPln, used to convert each deposit's own currency to PLN;
// MAY trigger a live fetch, unlike AllRatesToPln).
type PriceSource interface {
	rebalancingcalc.PriceSource
	RateToPln(ctx context.Context, currency string) (decimal.Decimal, error)
}
