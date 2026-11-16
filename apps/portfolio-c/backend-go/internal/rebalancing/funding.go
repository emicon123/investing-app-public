package rebalancing

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"portfolio-c/backend-go/db/sqlcgen"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// Funding is portfolioC's rebalancing funding policy (ADR-113, the one Strategy siblingcore leaves per
// app): the deposit amount is request-scoped — FIXED falls back to its stored default, VARIABLE
// has none and requires it — it is allocated with no account step, and Confirm records exactly
// that amount as the deposit.
type Funding struct{}

// NewFunding returns portfolioC's funding policy.
func NewFunding() Funding { return Funding{} }

var _ screbalancing.Funding[*store.Queries] = Funding{}

// Budget: the override, else FIXED's stored default, else a 400 for VARIABLE.
func (Funding) Budget(ctx context.Context, q *store.Queries, p siblingcore.Portfolio, override *decimal.Decimal) (screbalancing.Budget, error) {
	amount, err := depositAmount(ctx, q.SQL(), p, override)
	if err != nil {
		return screbalancing.Budget{}, err
	}
	return screbalancing.Budget{DeployablePln: amount, Allocate: identityAllocate}, nil
}

func depositAmount(ctx context.Context, q *sqlcgen.Queries, p siblingcore.Portfolio, override *decimal.Decimal) (decimal.Decimal, error) {
	if override != nil {
		return *override, nil
	}
	if p == book.Fixed {
		return fixedMonthlyDepositDefaultPln(ctx, q)
	}
	return decimal.Zero, httpx.NewValidationError("depositAmountPln",
		"depositAmountPln is required for the VARIABLE portfolio — it has no stored default")
}

// Check: a confirm must name a positive deposit amount.
func (Funding) Check(req screbalancing.ConfirmRequest) error {
	if req.DepositAmountPln == nil || req.DepositAmountPln.Sign() <= 0 {
		return httpx.NewValidationError("depositAmountPln", "depositAmountPln must be positive")
	}
	return nil
}

// Settle records the request's deposit amount (no account cash is touched).
func (Funding) Settle(_ context.Context, _ *store.Queries, req screbalancing.ConfirmRequest, _ map[siblingcore.Bucket]decimal.Decimal) (decimal.Decimal, error) {
	return *req.DepositAmountPln, nil
}

// identityAllocate: portfolioC has no sub-accounts, so the buy list is final as computed.
func identityAllocate(buyList []rebalancecalc.BuyListItem, _ map[string]decimal.Decimal) []rebalancecalc.BuyListItem {
	return buyList
}

const fixedMonthlyDepositDefaultKey = "fixed_monthly_deposit_default_pln"

// fixedMonthlyDepositDefaultFallback is used when the setting is absent or unparseable.
var fixedMonthlyDepositDefaultFallback = decimal.RequireFromString("400")

func fixedMonthlyDepositDefaultPln(ctx context.Context, q *sqlcgen.Queries) (decimal.Decimal, error) {
	row, err := q.GetSettingByKey(ctx, fixedMonthlyDepositDefaultKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fixedMonthlyDepositDefaultFallback, nil
		}
		return decimal.Zero, err
	}
	v, err := decimal.NewFromString(row.Value)
	if err != nil {
		slog.Warn("rebalancing: invalid fixed_monthly_deposit_default_pln value, using default", "value", row.Value)
		return fixedMonthlyDepositDefaultFallback, nil
	}
	return v, nil
}
