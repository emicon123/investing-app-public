// Package assetlifecycle is the shared pure decision kernel for soft-deleting and restoring
// assets/subassets (task 173, ADR-109, design docs/architecture/diagrams/173-asset-soft-delete.md
// §6.2/§6.3/§7).
//
// Functional core, imperative shell: each app's position.Service fetches the relevant rows
// (live-only or any-state, see the design's §5 tables), fills the plain state structs below,
// asks the kernel what to do, and executes the returned plan inside its own transaction. No DB
// types, no context, no I/O.
package assetlifecycle

import "errors"

// Sentinel errors; each app maps them onto its existing httpx errors (409 DUPLICATE_ENTRY,
// 404, 409 CONFLICT).
var (
	ErrDuplicate     = errors.New("assetlifecycle: live row with this natural key already exists")
	ErrAssetNotFound = errors.New("assetlifecycle: target asset missing or deleted")
	ErrPrimaryTaken  = errors.New("assetlifecycle: asset already has a live primary subasset")
)

// DeleteAction is what DELETE /positions/{id} does with one live subasset.
type DeleteAction int

const (
	// DeleteConflict: a primary with live siblings — 409, nothing written.
	DeleteConflict DeleteAction = iota
	// DeleteSoftDeleteAsset: the last live subasset — soft-delete the whole asset (cascade).
	DeleteSoftDeleteAsset
	// DeleteSoftDeleteSubasset: a non-primary with live siblings — soft-delete just the row.
	DeleteSoftDeleteSubasset
)

// PlanSubassetDelete decides a subasset delete from the row's primary flag and the number of
// LIVE subassets of its asset (the row itself included).
func PlanSubassetDelete(isPrimary bool, liveSubassets int) DeleteAction {
	switch {
	case liveSubassets <= 1:
		return DeleteSoftDeleteAsset
	case isPrimary:
		return DeleteConflict
	default:
		return DeleteSoftDeleteSubasset
	}
}

// AssetState is what the kernel needs to know about one assets row.
type AssetState struct {
	ID      int64
	Deleted bool
	// HasLivePrimary: the asset has a live subasset with is_primary = true.
	HasLivePrimary bool
}

// RowState is what the kernel needs to know about one positions row.
type RowState struct {
	ID        int64
	AssetID   int64
	Deleted   bool
	IsPrimary bool
}

// CreateInput is the looked-up state for one POST /positions. nil pointers mean "no row found".
type CreateInput struct {
	// NewAsset: the request carries newAsset (no assetId).
	NewAsset bool
	// TargetAsset: assetId mode only — the LIVE-ONLY lookup of the requested asset (nil when
	// missing or deleted).
	TargetAsset *AssetState
	// Position: the subasset found by the app's natural key, any state.
	Position *RowState
	// PositionAsset: Position's asset, any state (nil when Position is nil).
	PositionAsset *AssetState
	// KeyAsset: newAsset mode only — the asset found by key = ticker (+ portfolio), any state.
	KeyAsset *AssetState
	// WantPrimary: the subasset is to be the primary (always true in newAsset mode).
	WantPrimary bool
}

// AssetAction is how the plan obtains the asset.
type AssetAction int

const (
	AssetUseExisting AssetAction = iota // a live asset (assetId mode)
	AssetInsert                         // InsertAsset
	AssetRestore                        // RestoreAsset(AssetID)
)

// SubassetAction is how the plan obtains the subasset row.
type SubassetAction int

const (
	SubassetInsert  SubassetAction = iota // InsertPosition
	SubassetRestore                       // RestorePosition(SubassetID), re-homed into the plan's asset
)

// CreatePlan is the kernel's answer. AssetID is 0 for AssetInsert (the caller uses the
// inserted id); SubassetID is 0 for SubassetInsert.
type CreatePlan struct {
	Asset      AssetAction
	AssetID    int64
	Subasset   SubassetAction
	SubassetID int64
	IsPrimary  bool
	// ClearPrimaries: run ClearAssetPrimary(AssetID) before the subasset write (a restored
	// asset's stale primary flags sit on deleted rows only — invariant I3).
	ClearPrimaries bool
}

// PlanCreate decides a POST /positions (design §6.2).
func PlanCreate(in CreateInput) (CreatePlan, error) {
	if in.Position != nil && !in.Position.Deleted {
		return CreatePlan{}, ErrDuplicate
	}
	sub, subID := SubassetInsert, int64(0)
	if in.Position != nil {
		sub, subID = SubassetRestore, in.Position.ID
	}

	if !in.NewAsset {
		if in.TargetAsset == nil || in.TargetAsset.Deleted {
			return CreatePlan{}, ErrAssetNotFound
		}
		if in.WantPrimary && in.TargetAsset.HasLivePrimary {
			return CreatePlan{}, ErrPrimaryTaken
		}
		return CreatePlan{Asset: AssetUseExisting, AssetID: in.TargetAsset.ID,
			Subasset: sub, SubassetID: subID, IsPrimary: in.WantPrimary}, nil
	}

	plan := CreatePlan{Subasset: sub, SubassetID: subID, IsPrimary: true}
	switch {
	case in.Position != nil && in.PositionAsset != nil && in.PositionAsset.Deleted:
		plan.Asset, plan.AssetID, plan.ClearPrimaries = AssetRestore, in.PositionAsset.ID, true
	case in.KeyAsset != nil && !in.KeyAsset.Deleted:
		return CreatePlan{}, ErrDuplicate
	case in.KeyAsset != nil:
		plan.Asset, plan.AssetID, plan.ClearPrimaries = AssetRestore, in.KeyAsset.ID, true
	default:
		plan.Asset = AssetInsert
	}
	return plan, nil
}
