package position

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
)

// SetPrimary flips which subasset of an asset is primary — the task-156 replacement
// for Promote (POST /positions/{id}/promote, retired with the companion columns).
// Pure boolean flip in one transaction (ClearAssetPrimary + SetSubassetPrimary): no
// units move, no asset_purchases row is written, no cost basis migrates (cost basis
// never leaves its row), no sibling repointing exists to do (there are no strings to
// repoint — membership is the asset_id FK). The book's Σ targets == 100 guard is kept
// (same tripwire promote had) and runs INSIDE the transaction, so a violation rolls the flip
// back; it passes by construction on a complete book since no target moves.
func (s *Service) SetPrimary(ctx context.Context, assetID, subassetID int64) (AssetDTO, error) {
	err := s.transact(ctx, func(q Queries) error {
		assetRow, err := q.GetAssetByID(ctx, assetID)
		if err != nil {
			return assetNotFoundOrErr(assetID, err)
		}
		if err := s.guardTargetSum(ctx, q, string(assetRow.Portfolio)); err != nil {
			return err
		}
		if err := q.ClearAssetPrimary(ctx, assetID); err != nil {
			return fmt.Errorf("position: clear primary for asset %d: %w", assetID, err)
		}
		if _, err := q.SetSubassetPrimary(ctx, sqlcgen.SetSubassetPrimaryParams{
			ID: subassetID, AssetID: assetID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
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

	data, err := s.loadAll(ctx, s.queries)
	if err != nil {
		return AssetDTO{}, err
	}
	fresh, ok := data.byID[assetID]
	if !ok {
		return AssetDTO{}, assetNotFoundOrErr(assetID, pgx.ErrNoRows)
	}
	return s.getWithComputed(ctx, data, fresh)
}
