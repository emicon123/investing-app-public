package position

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
)

var (
	hundred = decimal.NewFromInt(100)
	// targetSumTolerance is the Σ targets == 100 tripwire's tolerance (rebalancing Step 6).
	targetSumTolerance = decimal.RequireFromString("0.01")
)

// SetPrimary flips which subasset of an asset is primary — a pure boolean flip in one
// transaction (ClearAssetPrimary + SetSubassetPrimary): no units move, no cost basis migrates.
// The portfolio's Σ targets == 100 guard runs first, inside the transaction, so a book that
// already violates the invariant is rejected before any write.
func (s *Service) SetPrimary(ctx context.Context, assetID, subassetID int64) (AssetDTO, error) {
	var portfolio siblingcore.Portfolio
	err := s.tx(ctx, func(q Store) error {
		a, err := q.GetAsset(ctx, assetID)
		if err != nil {
			return assetNotFoundOrErr(assetID, err)
		}
		portfolio = a.Portfolio
		if err := guardTargetSum(ctx, q, portfolio); err != nil {
			return err
		}
		if err := q.ClearAssetPrimary(ctx, assetID); err != nil {
			return fmt.Errorf("position: clear primary for asset %d: %w", assetID, err)
		}
		if err := q.SetSubassetPrimary(ctx, assetID, subassetID); err != nil {
			if isNoRows(err) {
				return httpx.NewValidationError("subassetId", fmt.Sprintf(
					"subasset %d does not belong to asset %d", subassetID, assetID))
			}
			return fmt.Errorf("position: set primary %d for asset %d: %w", subassetID, assetID, err)
		}
		return nil
	})
	if err != nil {
		return AssetDTO{}, err
	}
	data, err := load(ctx, s.store, portfolio)
	if err != nil {
		return AssetDTO{}, err
	}
	fresh, ok := data.byID[assetID]
	if !ok {
		return AssetDTO{}, assetNotFoundOrErr(assetID, pgx.ErrNoRows)
	}
	return s.withComputed(ctx, data, fresh)
}

// guardTargetSum is the per-portfolio Σ targets == 100 tripwire (the rebalancing Step-6
// precondition). The message names a named portfolio and omits WholeBook.
func guardTargetSum(ctx context.Context, q Store, p siblingcore.Portfolio) error {
	assets, err := q.ListAssets(ctx, p)
	if err != nil {
		return fmt.Errorf("position: list assets%s: %w", forPortfolio(p), err)
	}
	sum := decimal.Zero
	for _, a := range assets {
		if a.TargetAllocationPct != nil {
			sum = sum.Add(*a.TargetAllocationPct)
		}
	}
	if sum.Sub(hundred).Abs().Cmp(targetSumTolerance) <= 0 {
		return nil
	}
	if p == siblingcore.WholeBook {
		return httpx.NewValidationError("", fmt.Sprintf(
			"Invariant violated: asset targetAllocationPct would sum to %s (expected 100)", sum.StringFixed(4)))
	}
	return httpx.NewValidationError("", fmt.Sprintf(
		"Invariant violated: portfolio %s's asset targetAllocationPct would sum to %s (expected 100)", p, sum.StringFixed(4)))
}
