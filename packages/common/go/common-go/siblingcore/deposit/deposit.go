// Package deposit is the portfolioB + portfolioC deposits service (ADR-113): paginated list, create, and
// the deposits-page XIRR summary over one portfolio's flows and its current cache-priced value.
package deposit

import (
	"context"

	"investing-app/common-go/siblingcore"
)

// Store is the deposit service's port (consumer-side). Deposits are listed newest first
// (deposit_date DESC, id DESC) by ListDeposits and oldest first by ListDepositsAsc.
type Store interface {
	ListDeposits(ctx context.Context, p siblingcore.Portfolio, year *int, limit, offset int) ([]siblingcore.Deposit, error)
	CountDeposits(ctx context.Context, p siblingcore.Portfolio, year *int) (int64, error)
	ListDepositsAsc(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Deposit, error)
	InsertDeposit(ctx context.Context, d siblingcore.Deposit) (siblingcore.Deposit, error)
	ListAssets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Asset, error)
	ListSubassets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error)
}
