// Package position serves the positions/assets endpoints (ported from the Java PositionService,
// ADR-094 / doc 13 §7). It owns `positions` (the write-ownership cluster's canonical delta-write
// query, UpdatePositionDelta, lives here per .claude/tasks/150-architect-output.md §2) plus the
// direct-edit CRUD path, which NEVER shares a code path with the delta-apply write (see delta.go's
// own doc comment and architect-output §2's "genuinely different operations on the same table"
// analysis).
//
// Task 156 (Asset / Subasset model): positions rows are subassets grouped by a real asset_id
// FK into logical assets (assets.sql owns the group root). Targets live ONLY on the asset;
// exactly one subasset per asset is primary (partial UNIQUE(asset_id) WHERE is_primary) and
// is the default buy destination. POST /positions/{id}/promote is retired — replaced by
// POST /assets/{id}/primary (pure boolean flip, no cost-basis move).
package position

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/rebalancingcalc"
	"investing-app/common-go/httpx"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an interface
// so service-layer unit tests can fake the DB without a real Postgres connection
// (integration_test.go still exercises the real *sqlcgen.Queries against testcontainers-go, per
// the "never mock the database" gate). Embeds rebalancingcalc.Queries because
// effectiveTargetAllocationPct is computed by calling rebalancingcalc.LoadMainTargetOverrides,
// which needs its own read surface (settings, macro_state) — Go's structural typing means a
// single *sqlcgen.Queries value satisfies both interfaces at once.
type Queries interface {
	rebalancingcalc.Queries
	GetPositionByID(ctx context.Context, id int64) (sqlcgen.Position, error)
	GetAssetByID(ctx context.Context, id int64) (sqlcgen.Asset, error)
	InsertAsset(ctx context.Context, arg sqlcgen.InsertAssetParams) (sqlcgen.Asset, error)
	InsertPosition(ctx context.Context, arg sqlcgen.InsertPositionParams) (sqlcgen.Position, error)
	UpdatePositionFields(ctx context.Context, arg sqlcgen.UpdatePositionFieldsParams) (sqlcgen.Position, error)
	UpdatePositionDelta(ctx context.Context, arg sqlcgen.UpdatePositionDeltaParams) (sqlcgen.Position, error)
	UpdateAssetFields(ctx context.Context, arg sqlcgen.UpdateAssetFieldsParams) (sqlcgen.Asset, error)
	// Task 173 soft delete + restore-on-create (ADR-109).
	SoftDeletePosition(ctx context.Context, id int64) error
	SoftDeleteAsset(ctx context.Context, assetID int64) error
	GetPositionByNaturalKeyAnyState(ctx context.Context, arg sqlcgen.GetPositionByNaturalKeyAnyStateParams) (sqlcgen.Position, error)
	GetAssetByIDAnyState(ctx context.Context, id int64) (sqlcgen.Asset, error)
	GetAssetByKeyAnyState(ctx context.Context, arg sqlcgen.GetAssetByKeyAnyStateParams) (sqlcgen.Asset, error)
	RestoreAsset(ctx context.Context, arg sqlcgen.RestoreAssetParams) (sqlcgen.Asset, error)
	RestorePosition(ctx context.Context, arg sqlcgen.RestorePositionParams) (sqlcgen.Position, error)
	ClearAssetPrimary(ctx context.Context, assetID int64) error
	SetSubassetPrimary(ctx context.Context, arg sqlcgen.SetSubassetPrimaryParams) (sqlcgen.Position, error)
}

// TransactFunc runs fn inside a single DB transaction. Every write method (Create, Update,
// UpdateAsset, Delete, DeleteAsset, SetPrimary) performs its reads and writes inside one call,
// so the set-primary flip and asset+first-subasset creation commit atomically and no edit
// overwrites a concurrently committed money write. Production passes
// pgtx.SerializedRunner (ADR-111: the money-write advisory lock); unit tests pass nil (direct
// call-through).
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// PriceSource is the read-only cross-package dependency into internal/marketdata — implemented
// by an adapter in main.go (same convention as botPriceAdapter/historiaFxAdapter/
// rebalancingcalc.PriceSource; a single concrete adapter satisfies all three interfaces).
type PriceSource interface {
	rebalancingcalc.PriceSource
	// RateToPln mirrors FxRateCacheService.getRateToPln — MAY trigger a live fetch on a
	// stale/missing cache entry (unlike AllRatesToPln, which is cache-only). Used for
	// priceNative (pricePln converted back to native currency).
	RateToPln(ctx context.Context, currency string) (decimal.Decimal, error)
}

// AssetDTO is the service-layer shape of one logical asset: the group root's own fields
// (targets live ONLY here) plus group-level computed fields (valuePln = Σ subassets,
// deviationPct on the asset) and the member subassets, primary first. Kept free of sqlc
// row types and openapi response types (doc 13's layering rule).
type AssetDTO struct {
	ID                         int64
	Portfolio                  string
	Key                        string
	Name                       string
	Exposure                   string
	AssetClass                 string
	TargetAllocationPct        *decimal.Decimal
	TargetAllocationPctRiskOff *decimal.Decimal
	CreatedAt                  time.Time
	UpdatedAt                  time.Time

	Computed  ComputedFields
	Subassets []SubassetDTO
}

// ComputedFields is the direct port of ComputedPositionFields — now group-level (valuePln =
// Σ subassets, currentPct/deviationPct/profitPct on the asset; effectiveTarget resolved from
// the asset's own targets via the macro gate).
type ComputedFields struct {
	UnitsTotal                   *decimal.Decimal
	PricePln                     *decimal.Decimal
	PriceNative                  *decimal.Decimal
	ValuePln                     *decimal.Decimal
	CurrentPct                   *decimal.Decimal
	EffectiveTargetAllocationPct *decimal.Decimal
	DeviationPct                 *decimal.Decimal
	ProfitPct                    *decimal.Decimal
	AvgBuyPricePln               *decimal.Decimal
	AvgBuyPriceNative            *decimal.Decimal
	EquivalentUnits              *decimal.Decimal
	CostlessSubassets            []string
}

// SubassetDTO is one concrete holding within an asset (one positions row).
type SubassetDTO struct {
	ID               int64
	AssetID          int64
	IsPrimary        bool
	Ticker           string
	Name             string
	Portfolio        string
	Exchange         string
	AssetClass       string
	PriceCurrency    string
	PurchaseCurrency string
	UnitsIke         decimal.Decimal
	UnitsIkze        decimal.Decimal
	UnitsRegularEur  decimal.Decimal
	UnitsRegularUsd  decimal.Decimal
	UnitsRegularPln  decimal.Decimal
	EffectiveUnits   decimal.Decimal
	PricePln         *decimal.Decimal
	PriceNative      *decimal.Decimal
	ValuePln         *decimal.Decimal
	YahooTicker      *string
	CompanyBacked    bool
	// Cost basis stays on the subasset row that incurred it (task 156 — set-primary moves
	// nothing; entry price in the primary's terms is the follow-up task 161).
	TotalSpentPln *decimal.Decimal
	AvgBuyPrice   *decimal.Decimal
}

// NewAssetInput is the inline asset payload for creating a subasset under a brand-new asset
// (assetId omitted on create): targets live here, never on the subasset row.
type NewAssetInput struct {
	Name                       string
	Exposure                   string
	AssetClass                 string
	TargetAllocationPct        *decimal.Decimal
	TargetAllocationPctRiskOff *decimal.Decimal
}

// CreateInput is the service-layer shape of CreatePositionRequest: a concrete subasset
// holding plus either its existing asset (assetId) or an inline new-asset payload
// (assetId omitted).
type CreateInput struct {
	Ticker           string
	Name             string
	Portfolio        string
	Exchange         string
	AssetClass       string
	Exposure         *string
	PriceCurrency    string
	PurchaseCurrency *string
	YahooTicker      *string
	CompanyBacked    *bool
	AssetID          *int64
	IsPrimary        *bool
	NewAsset         *NewAssetInput
}

// UpdateInput is the service-layer shape of UpdatePositionRequest — every field optional, a nil
// field means "leave unchanged" (mirrors PositionService.updatePosition's null-check chain).
// Targets are NOT editable here (they live on the parent asset — PUT /assets/{id}); the
// handler rejects them with a 400 before this is built.
type UpdateInput struct {
	UnitsIke        *decimal.Decimal
	UnitsIkze       *decimal.Decimal
	UnitsRegularEur *decimal.Decimal
	UnitsRegularUsd *decimal.Decimal
	UnitsRegularPln *decimal.Decimal
	TotalSpentPln   *decimal.Decimal
	AvgBuyPrice     *decimal.Decimal
	Name            *string
	AssetClass      *string
	YahooTicker     *string
	CompanyBacked   *bool
	// AssetID re-homes a NON-primary subasset under another asset of the same portfolio
	// (design §13): nil or equal to the current asset = ignored.
	AssetID *int64
}

// UpdateAssetInput is the service-layer shape of UpdateAssetRequest — targets (+name), the
// only mutable asset-level fields. Targets are tri-state (httpx.Optional): absent = leave
// unchanged, explicit JSON null = CLEAR the target (NULL "no target" is distinct from 0
// "explicit zero target", ADR-053), a value = set it.
type UpdateAssetInput struct {
	TargetAllocationPct        httpx.Optional[decimal.Decimal]
	TargetAllocationPctRiskOff httpx.Optional[decimal.Decimal]
	Name                       *string
}
