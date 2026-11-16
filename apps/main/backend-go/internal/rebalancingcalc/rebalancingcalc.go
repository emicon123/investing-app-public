// Package rebalancingcalc holds the pure rebalancing computation (ported from the Java
// RebalancingService, the Main/Retirement strategies, MacroDeriskGate and ValueComputationService —
// a ~1,126-LOC cluster, task 150 Finding 1, .claude/tasks/150-architect-output.md §1), read-only
// over positions/macro_state/settings/cash_positions, imported by both internal/position (for
// PositionDto.computed.effectiveTargetAllocationPct) and internal/purchasesession (for the buy-list
// generation that seeds a new session's plan).
//
// Per doc 13 §5 point 2 and diagram doc 150 §0/§5: the Java Template Method hierarchy (a strategy
// factory selecting the Main vs. Retirement strategy, both extending AbstractProportionalStrategy)
// collapses to ONE function, calculateBuyList, parameterized by a small portfolioConfig value
// (which budget formula, whether the macro de-risk override applies) — never a RebalancingStrategy
// interface selected by a factory. Do not reintroduce that shape; see calculate.go's own top
// comment for the exact collapse.
//
// No controller of its own — Calculate has no HTTP entry point (architect-output §1: only
// purchase-session creation and the position/macro-derisk code call into this cluster). Every
// public function here must accept the caller's own tx-scoped Queries value rather than opening a
// transaction (the original read-only transaction joined the caller's — see architect-output §4).
package rebalancingcalc

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/settings"
)

// Portfolio values (the former Java Portfolio enum). Plain strings (not the sqlcgen enum type) so
// this package's public API stays framework-agnostic for its callers, matching internal/position's
// own convention for the equivalent constants.
const (
	PortfolioMAIN       = "MAIN"
	PortfolioRETIREMENT = "RETIREMENT"
)

// Queries is the narrow, read-only slice of *sqlcgen.Queries this package needs — no writes, no
// TransactFunc of its own (architect-output §1 Finding 1's explicit recommendation). Callers
// pass their own tx-scoped Queries value when already inside a transaction.
type Queries interface {
	settings.SettingReader
	ListAllPositions(ctx context.Context) ([]sqlcgen.Position, error)
	ListAllAssets(ctx context.Context) ([]sqlcgen.Asset, error)
	MacroStateLatestByComputedAt(ctx context.Context) (sqlcgen.MacroState, error)
	LatestCashRecordedAt(ctx context.Context) (pgtype.Timestamptz, error)
	CashPositionsByRecordedAt(ctx context.Context, recordedAt pgtype.Timestamptz) ([]sqlcgen.CashPosition, error)
}

// PriceSource is the read-only cross-package dependency into internal/marketdata (doc 13 §6.3:
// "reading another phase's owned data is fine, writing is not") — implemented by an adapter in
// main.go, the same composition-root-glue convention as botPriceAdapter/historiaFxAdapter.
type PriceSource interface {
	// PricePlnByYahooTicker mirrors PriceCacheService.getAllPricesPln — cache-only, no fetch.
	PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error)
	// AllRatesToPln mirrors FxRateCacheService.getAllRates — cache-only, no fetch, PLN=1 always
	// present.
	AllRatesToPln(ctx context.Context) (map[string]decimal.Decimal, error)
}

// AssetRef is the asset-level identity every subasset row carries (task 156): joined by the
// asset_id FK at decode time, never string-matched. Class is the ASSET's own asset_class — the
// crypto-donor / wrapper-substitution predicate (design §5/§7), distinct from the row-level
// PositionRow.AssetClass.
type AssetRef struct {
	ID    int64
	Key   string
	Class string
}

// PositionRow is this package's own decoded, decimal.Decimal-typed view of a `positions`
// (subasset) row joined to its asset — kept free of sqlc/pgtype types (doc 13's layering rule),
// independent of internal/position's own equivalent decode (every package decodes sqlc rows into
// its own service-layer shape; this is the established convention, not duplicated business
// logic).
//
// Task 156 (Asset / Subasset model): group membership is Asset.ID (the asset_id FK); exactly one
// subasset per asset IsPrimary (the default buy destination). Targets live ONLY on the asset;
// the asset's targets ride on its PRIMARY row (TargetAllocationPct/RiskOff below, nil on
// non-primaries) because every strategy/macro-derisk consumer iterates primaries only — one
// entry per asset, keyed by the primary's ticker exactly as the old parent ticker was keyed.
type PositionRow struct {
	Ticker           string
	Name             string
	Portfolio        string
	Exchange         string
	AssetClass       string // the row's own class (a subasset can differ from its asset's)
	PriceCurrency    string
	PurchaseCurrency string
	// The owning asset's targets, populated on the primary row only.
	TargetAllocationPct        *decimal.Decimal
	TargetAllocationPctRiskOff *decimal.Decimal
	UnitsIke                   decimal.Decimal
	UnitsIkze                  decimal.Decimal
	UnitsRegularEur            decimal.Decimal
	UnitsRegularUsd            decimal.Decimal
	UnitsRegularPln            decimal.Decimal
	Asset                      AssetRef
	IsPrimary                  bool
	YahooTicker                *string
}

// UnitsTotal sums all five sub-account buckets — the direct port of Position.computeUnitsTotal.
func (p PositionRow) UnitsTotal() decimal.Decimal {
	return p.UnitsIke.Add(p.UnitsIkze).Add(p.UnitsRegularEur).Add(p.UnitsRegularUsd).Add(p.UnitsRegularPln)
}

// EffectivePurchaseCurrency falls back to PLN when PurchaseCurrency is empty — the direct port
// of Position.effectivePurchaseCurrency.
func (p PositionRow) EffectivePurchaseCurrency() string {
	if p.PurchaseCurrency == "" {
		return "PLN"
	}
	return p.PurchaseCurrency
}

// BuyListItem is one entry in the rebalancing buy list — the direct port of BuyListItemDto.
type BuyListItem struct {
	Ticker           string
	Name             string
	Exchange         string
	PurchaseCurrency string
	AllocatedPln     decimal.Decimal
	AllocatedNative  decimal.Decimal
	UnitsToBuy       decimal.Decimal
	CurrentPct       decimal.Decimal
	TargetPct        decimal.Decimal
	DeviationPct     decimal.Decimal
}

// Result is Calculate's return value — only the fields purchase-session creation actually reads
// (buyList, deployableCashPln) plus cashOnHandPln/totalPortfolioValuePln for
// completeness/testability. grossCashPln/donutData/exchangeSummary from the Java
// RebalancingResultDto are deliberately omitted: Calculate has no controller of its own
// (architect-output §1) and nothing reads those fields — computing them here would be dead code
// (YAGNI).
type Result struct {
	Portfolio              string
	CashOnHandPln          decimal.Decimal
	DeployableCashPln      decimal.Decimal
	TotalPortfolioValuePln decimal.Decimal
	BuyList                []BuyListItem
}
