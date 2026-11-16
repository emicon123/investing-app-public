package rebalancingcalc

import (
	"fmt"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

// decodePosition converts a sqlc-generated positions row into this package's own
// decimal.Decimal-typed PositionRow (doc 13's layering rule: sqlc row types never leak past this
// boundary). Task 156: group membership and targets come from the parent asset (resolved by
// asset_id FK in DecodeAllPositions, never by string matching) — see that function's doc
// comment for the derivation rules.
func decodePosition(r sqlcgen.Position, asset sqlcgen.Asset) (PositionRow, error) {
	unitsIke, err := pgnum.ToDecimal(r.UnitsIke)
	if err != nil {
		return PositionRow{}, fmt.Errorf("rebalancingcalc: decode units_ike for %s: %w", r.Ticker, err)
	}
	unitsIkze, err := pgnum.ToDecimal(r.UnitsIkze)
	if err != nil {
		return PositionRow{}, fmt.Errorf("rebalancingcalc: decode units_ikze for %s: %w", r.Ticker, err)
	}
	unitsEur, err := pgnum.ToDecimal(r.UnitsRegularEur)
	if err != nil {
		return PositionRow{}, fmt.Errorf("rebalancingcalc: decode units_regular_eur for %s: %w", r.Ticker, err)
	}
	unitsUsd, err := pgnum.ToDecimal(r.UnitsRegularUsd)
	if err != nil {
		return PositionRow{}, fmt.Errorf("rebalancingcalc: decode units_regular_usd for %s: %w", r.Ticker, err)
	}
	unitsPln, err := pgnum.ToDecimal(r.UnitsRegularPln)
	if err != nil {
		return PositionRow{}, fmt.Errorf("rebalancingcalc: decode units_regular_pln for %s: %w", r.Ticker, err)
	}

	// Targets live ONLY on the asset (V75 dropped the positions columns). The asset's targets
	// ride on its primary row; no downstream consumer reads a non-primary's target
	// (strategy/macroderisk iterate primaries — one entry per asset).
	var target *decimal.Decimal
	var targetRiskOff *decimal.Decimal
	if r.IsPrimary {
		target, err = pgnum.ToDecimalPtr(asset.TargetAllocationPct)
		if err != nil {
			return PositionRow{}, fmt.Errorf("rebalancingcalc: decode assets.target_allocation_pct for %s: %w", asset.Key, err)
		}
		targetRiskOff, err = pgnum.ToDecimalPtr(asset.TargetAllocationPctRiskOff)
		if err != nil {
			return PositionRow{}, fmt.Errorf("rebalancingcalc: decode assets.target_allocation_pct_risk_off for %s: %w", asset.Key, err)
		}
	}

	return PositionRow{
		Ticker:                     r.Ticker,
		Name:                       r.Name,
		Portfolio:                  string(r.Portfolio),
		Exchange:                   string(r.Exchange),
		AssetClass:                 string(r.AssetClass),
		PriceCurrency:              string(r.PriceCurrency),
		PurchaseCurrency:           string(r.PurchaseCurrency),
		TargetAllocationPct:        target,
		TargetAllocationPctRiskOff: targetRiskOff,
		UnitsIke:                   unitsIke,
		UnitsIkze:                  unitsIkze,
		UnitsRegularEur:            unitsEur,
		UnitsRegularUsd:            unitsUsd,
		UnitsRegularPln:            unitsPln,
		Asset:                      AssetRef{ID: asset.ID, Key: asset.Key, Class: string(asset.AssetClass)},
		IsPrimary:                  r.IsPrimary,
		YahooTicker:                r.YahooTicker,
	}, nil
}

// DecodeAllPositions decodes every row from ListAllPositions joined to its parent asset.
// Exported (unlike decodePosition) so internal/purchasesession's lifecycle.go can build the
// []PositionRow slice planning.ComputePlan and rebalancingcalc.Calculate both need, without a
// second, duplicate decode of the same table (the only other consumer that needs this exact
// decoded shape).
//
// Task 156: membership is the asset_id FK — a row whose asset_id matches no asset is a hard
// error (the FK + SET NOT NULL forbid it on committed data; this trips only on a
// partially-applied migration, never in production).
func DecodeAllPositions(rows []sqlcgen.Position, assets []sqlcgen.Asset) ([]PositionRow, error) {
	byID := make(map[int64]sqlcgen.Asset, len(assets))
	for _, a := range assets {
		byID[a.ID] = a
	}
	out := make([]PositionRow, 0, len(rows))
	for _, r := range rows {
		asset, ok := byID[r.AssetID]
		if !ok {
			return nil, fmt.Errorf("rebalancingcalc: subasset %s references missing asset %d", r.Ticker, r.AssetID)
		}
		p, err := decodePosition(r, asset)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
