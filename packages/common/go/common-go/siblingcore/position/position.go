// Package position is the portfolioB + portfolioC asset/subasset service (ADR-113, task 156 model):
// list, get, create (insert or restore-on-create, ADR-109), update a subasset's own fields,
// update an asset's targets, set the primary subasset, and soft delete. Positions rows are
// subassets grouped by asset_id into assets; targets live only on the asset; exactly one
// subasset per asset is primary. Every write runs its reads and writes in one serialised
// transaction (ADR-111).
package position

import (
	"context"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/positioncalc"
	"investing-app/common-go/siblingcore"
)

// Store is the position service's port (consumer-side). Live reads exclude soft-deleted rows;
// a missing row is pgx.ErrNoRows (unwrapped or %w-wrapped), every other error is the raw pgx
// error.
type Store interface {
	ListAssets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Asset, error)
	ListSubassets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error)
	GetAsset(ctx context.Context, id int64) (siblingcore.Asset, error)
	GetSubasset(ctx context.Context, id int64) (siblingcore.Subasset, error)
	GetAssetAnyState(ctx context.Context, id int64) (siblingcore.Asset, error)
	FindAssetByKeyAnyState(ctx context.Context, p siblingcore.Portfolio, key string) (siblingcore.Asset, error)
	FindSubassetByTickerAnyState(ctx context.Context, p siblingcore.Portfolio, ticker string) (siblingcore.Subasset, error)
	InsertAsset(ctx context.Context, a siblingcore.Asset) (siblingcore.Asset, error)          // ID/timestamps ignored on input
	InsertSubasset(ctx context.Context, s siblingcore.Subasset) (siblingcore.Subasset, error) // units stored as zero in every bucket
	RestoreAsset(ctx context.Context, r AssetRestore) (siblingcore.Asset, error)
	RestoreSubasset(ctx context.Context, r SubassetRestore) (siblingcore.Subasset, error)
	UpdateSubassetFields(ctx context.Context, s siblingcore.Subasset) error // full-row overwrite of the editable fields
	UpdateAssetFields(ctx context.Context, a siblingcore.Asset) (siblingcore.Asset, error)
	ClearAssetPrimary(ctx context.Context, assetID int64) error
	SetSubassetPrimary(ctx context.Context, assetID, subassetID int64) error // ErrNoRows: not a live subasset of assetID
	SoftDeleteSubasset(ctx context.Context, id int64) error
	SoftDeleteAsset(ctx context.Context, assetID int64) error
}

// AssetRestore / SubassetRestore are the fields a restore-on-create overwrites (ADR-109);
// nil pointers keep the stored value.
type AssetRestore struct {
	ID                         int64
	Name, Exposure, AssetClass string
	TargetAllocationPct        *decimal.Decimal
	TargetAllocationPctRiskOff *decimal.Decimal
}

// SubassetRestore: see AssetRestore.
type SubassetRestore struct {
	ID, AssetID                     int64
	IsPrimary                       bool
	Name, AssetClass, PriceCurrency string
	Exposure, YahooTicker           *string
	SortOrder                       *int
}

// AssetDTO is one logical asset: its own fields (targets live only here), group-level computed
// fields (valuePln = Σ subassets) and its subassets, primary first.
type AssetDTO struct {
	siblingcore.Asset
	Computed  ComputedFields
	Subassets []SubassetDTO
}

// ComputedFields is the shared positioncalc result, field for field (task 161c-1 entry price
// included).
type ComputedFields = positioncalc.Computed

// SubassetDTO is one concrete holding within an asset, priced from the caches.
type SubassetDTO struct {
	ID, AssetID                           int64
	IsPrimary                             bool
	Ticker, Name, Exchange, PriceCurrency string
	Units                                 siblingcore.Units
	EffectiveUnits                        decimal.Decimal
	PricePln, PriceNative, ValuePln       *decimal.Decimal
	YahooTicker, GroupName                *string
	SortOrder                             int
}

// NewAssetInput is the inline asset of a create without assetId: the targets live here.
type NewAssetInput struct {
	Name, Exposure, AssetClass string
	TargetAllocationPct        *decimal.Decimal
	TargetAllocationPctRiskOff *decimal.Decimal
}

// CreateInput is one subasset to create, under AssetID or under the inline NewAsset.
type CreateInput struct {
	Portfolio                                                   siblingcore.Portfolio
	Ticker, Name, Exchange, AssetClass, Exposure, PriceCurrency string
	YahooTicker                                                 *string
	SortOrder                                                   *int
	AssetID                                                     *int64
	IsPrimary                                                   *bool
	NewAsset                                                    *NewAssetInput
}

// UpdateInput: nil / absent means "leave unchanged". Units is a per-bucket patch: only the
// buckets present are overwritten; each must be a configured bucket and >= 0
// (400 on that bucket's BucketSpec.Field, checked in Config.Buckets order). AssetID re-homes a
// non-primary subasset under another asset of the same portfolio.
type UpdateInput struct {
	Units                                    map[siblingcore.Bucket]decimal.Decimal
	TotalSpentPln, AvgBuyPrice               *decimal.Decimal
	Name, GroupName, AssetClass, YahooTicker *string
	SortOrder                                *int
	AssetID                                  *int64
}

// UpdateAssetInput edits targets (tri-state: absent keeps, explicit null clears, a value sets)
// and the name.
type UpdateAssetInput struct {
	TargetAllocationPct        httpx.Optional[decimal.Decimal]
	TargetAllocationPctRiskOff httpx.Optional[decimal.Decimal]
	Name                       *string
}
