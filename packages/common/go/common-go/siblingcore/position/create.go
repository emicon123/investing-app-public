package position

import (
	"context"
	"errors"
	"fmt"

	"investing-app/common-go/assetlifecycle"
	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgerr"
	"investing-app/common-go/positioncalc"
	"investing-app/common-go/siblingcore"
)

// Create adds one subasset: under an existing asset (AssetID) or under a brand-new asset
// (NewAsset, which carries the targets). Re-adding a soft-deleted ticker restores the old row
// (same id, units, cost, history) instead of inserting (ADR-109): the shared assetlifecycle
// kernel decides, this method looks up and executes. On restore the required request fields
// overwrite and the optional ones (targets, yahooTicker, sortOrder) keep their old value when
// omitted. The response is the asset with its subassets, nothing computed yet.
func (s *Service) Create(ctx context.Context, in CreateInput) (AssetDTO, error) {
	if err := s.cfg.CheckPortfolio("portfolio", in.Portfolio); err != nil {
		return AssetDTO{}, err
	}
	isPrimary, err := validateCreate(in)
	if err != nil {
		return AssetDTO{}, err
	}
	var createdAssetID int64
	err = s.tx(ctx, func(q Store) error {
		st, err := lifecycleState(ctx, q, in, isPrimary)
		if err != nil {
			return err
		}
		plan, err := assetlifecycle.PlanCreate(st)
		if err != nil {
			return createPlanError(in, err)
		}
		createdAssetID, err = executeCreate(ctx, q, in, plan)
		return err
	})
	if err != nil {
		return AssetDTO{}, err
	}
	data, err := load(ctx, s.store, in.Portfolio)
	if err != nil {
		return AssetDTO{}, err
	}
	a, ok := data.byID[createdAssetID]
	if !ok {
		return AssetDTO{}, fmt.Errorf("position: created subasset %s not found after create", in.Ticker)
	}
	return baseDTO(a, positioncalc.OrderSubassets(data.subs, a.ID)), nil
}

// validateCreate checks the request shape and returns whether the new subasset is primary
// (a new asset's first subasset trivially is).
func validateCreate(in CreateInput) (bool, error) {
	if in.AssetID == nil && in.NewAsset == nil {
		return false, httpx.NewValidationError("assetId", "either assetId or newAsset is required")
	}
	if in.AssetID != nil && in.NewAsset != nil {
		return false, httpx.NewValidationError("newAsset", "newAsset must be omitted when assetId is set")
	}
	if in.NewAsset != nil {
		if err := decx.ValidatePercentRange("targetAllocationPct", in.NewAsset.TargetAllocationPct); err != nil {
			return false, err
		}
		if in.NewAsset.Name == "" || in.NewAsset.Exposure == "" || in.NewAsset.AssetClass == "" {
			return false, httpx.NewValidationError("newAsset", "name, exposure and assetClass are required")
		}
	}
	isPrimary := in.AssetID == nil
	if in.IsPrimary != nil {
		if in.AssetID == nil && !*in.IsPrimary {
			return false, httpx.NewValidationError("isPrimary", "a new asset's first subasset is always primary")
		}
		isPrimary = *in.IsPrimary
	}
	return isPrimary, nil
}

// lifecycleState reads, inside the transaction, everything assetlifecycle.PlanCreate decides on.
func lifecycleState(ctx context.Context, q Store, in CreateInput, isPrimary bool) (assetlifecycle.CreateInput, error) {
	st := assetlifecycle.CreateInput{NewAsset: in.NewAsset != nil, WantPrimary: isPrimary}
	pos, err := q.FindSubassetByTickerAnyState(ctx, in.Portfolio, in.Ticker)
	switch {
	case err == nil:
		st.Position = &assetlifecycle.RowState{ID: pos.ID, AssetID: pos.AssetID, Deleted: pos.Deleted, IsPrimary: pos.IsPrimary}
		pa, err := q.GetAssetAnyState(ctx, pos.AssetID)
		if err != nil {
			return st, fmt.Errorf("position: read asset %d: %w", pos.AssetID, err)
		}
		st.PositionAsset = &assetlifecycle.AssetState{ID: pa.ID, Deleted: pa.Deleted}
	case !isNoRows(err):
		return st, fmt.Errorf("position: read %s: %w", in.Ticker, err)
	}

	if in.AssetID != nil {
		target, err := q.GetAsset(ctx, *in.AssetID)
		switch {
		case err == nil:
			if target.Portfolio != in.Portfolio {
				return st, httpx.NewValidationError("portfolio", "must match the asset's portfolio")
			}
			hasPrimary := false
			if isPrimary {
				data, err := load(ctx, q, in.Portfolio)
				if err != nil {
					return st, err
				}
				hasPrimary = positioncalc.PrimaryOf(data.subs, target.ID) != nil
			}
			st.TargetAsset = &assetlifecycle.AssetState{ID: target.ID, HasLivePrimary: hasPrimary}
		case !isNoRows(err):
			return st, fmt.Errorf("position: read asset %d: %w", *in.AssetID, err)
		}
		return st, nil
	}
	ka, err := q.FindAssetByKeyAnyState(ctx, in.Portfolio, in.Ticker)
	switch {
	case err == nil:
		st.KeyAsset = &assetlifecycle.AssetState{ID: ka.ID, Deleted: ka.Deleted}
	case !isNoRows(err):
		return st, fmt.Errorf("position: read asset key %s: %w", in.Ticker, err)
	}
	return st, nil
}

// executeCreate carries out plan and returns the asset the subasset now belongs to.
func executeCreate(ctx context.Context, q Store, in CreateInput, plan assetlifecycle.CreatePlan) (int64, error) {
	assetID := plan.AssetID
	switch plan.Asset {
	case assetlifecycle.AssetInsert:
		created, err := q.InsertAsset(ctx, siblingcore.Asset{
			Portfolio: in.Portfolio, Key: in.Ticker, Name: in.NewAsset.Name,
			Exposure: in.NewAsset.Exposure, AssetClass: in.NewAsset.AssetClass,
			TargetAllocationPct: in.NewAsset.TargetAllocationPct, TargetAllocationPctRiskOff: in.NewAsset.TargetAllocationPctRiskOff,
		})
		if err != nil {
			if pgerr.IsUniqueViolation(err) {
				return 0, httpx.NewConflictError(httpx.CodeDuplicateEntry, "Resource already exists")
			}
			return 0, fmt.Errorf("position: create asset: %w", err)
		}
		assetID = created.ID
	case assetlifecycle.AssetRestore:
		if _, err := q.RestoreAsset(ctx, AssetRestore{
			ID: assetID, Name: in.NewAsset.Name, Exposure: in.NewAsset.Exposure, AssetClass: in.NewAsset.AssetClass,
			TargetAllocationPct: in.NewAsset.TargetAllocationPct, TargetAllocationPctRiskOff: in.NewAsset.TargetAllocationPctRiskOff,
		}); err != nil {
			return 0, fmt.Errorf("position: restore asset %d: %w", assetID, err)
		}
	}
	if plan.ClearPrimaries {
		if err := q.ClearAssetPrimary(ctx, assetID); err != nil {
			return 0, fmt.Errorf("position: clear primary of asset %d: %w", assetID, err)
		}
	}
	if plan.Subasset == assetlifecycle.SubassetRestore {
		if _, err := q.RestoreSubasset(ctx, restoreSubasset(in, plan, assetID)); err != nil {
			return 0, fmt.Errorf("position: restore %d: %w", plan.SubassetID, err)
		}
		return assetID, nil
	}
	return assetID, insertSubasset(ctx, q, in, assetID, plan.IsPrimary)
}

// createPlanError maps the kernel's sentinel errors onto the wire errors.
func createPlanError(in CreateInput, err error) error {
	switch {
	case errors.Is(err, assetlifecycle.ErrDuplicate):
		return httpx.NewConflictError(httpx.CodeDuplicateEntry, "Resource already exists")
	case errors.Is(err, assetlifecycle.ErrAssetNotFound):
		return httpx.NewNotFoundError(fmt.Sprintf("Asset with id %d not found", *in.AssetID))
	case errors.Is(err, assetlifecycle.ErrPrimaryTaken):
		return httpx.NewConflictError(httpx.CodeConflict, "asset already has a primary subasset")
	}
	return err
}

// insertSubasset is the fresh-row path (zero units in every bucket, sortOrder default 0).
func insertSubasset(ctx context.Context, q Store, in CreateInput, assetID int64, isPrimary bool) error {
	sortOrder := 0
	if in.SortOrder != nil {
		sortOrder = *in.SortOrder
	}
	if _, err := q.InsertSubasset(ctx, siblingcore.Subasset{
		Portfolio: in.Portfolio, AssetID: assetID, IsPrimary: isPrimary,
		Ticker: in.Ticker, Name: in.Name, Exchange: in.Exchange, AssetClass: in.AssetClass,
		Exposure: in.Exposure, PriceCurrency: in.PriceCurrency, YahooTicker: in.YahooTicker, SortOrder: sortOrder,
	}); err != nil {
		if pgerr.IsUniqueViolation(err) {
			return httpx.NewConflictError(httpx.CodeDuplicateEntry, "Resource already exists")
		}
		return fmt.Errorf("position: create: %w", err)
	}
	return nil
}

// restoreSubasset maps a create request onto a restore (design 173 §6.1): required fields
// overwrite (the subasset's exposure is a required request field, so it overwrites when sent),
// optional ones pass the raw request pointer (nil keeps the old setting).
func restoreSubasset(in CreateInput, plan assetlifecycle.CreatePlan, assetID int64) SubassetRestore {
	r := SubassetRestore{
		ID: plan.SubassetID, AssetID: assetID, IsPrimary: plan.IsPrimary,
		Name: in.Name, AssetClass: in.AssetClass, PriceCurrency: in.PriceCurrency,
		YahooTicker: in.YahooTicker, SortOrder: in.SortOrder,
	}
	if in.Exposure != "" {
		e := in.Exposure
		r.Exposure = &e
	}
	return r
}
