package rebalancingcalc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/settings"
	"investing-app/common-go/pgnum"
)

// LoadMainTargetOverrides is the direct port of MacroDeriskService.loadMainTargetOverrides.
// Returns an empty map when the gate is disabled (rebalancing.macro_derisk_enabled = false) —
// equivalent to every MAIN position sitting at its risk-on (DB) target, since
// target_allocation_pct already IS the risk-on value.
func LoadMainTargetOverrides(ctx context.Context, q Queries) (map[string]decimal.Decimal, error) {
	enabled, err := settings.Get(ctx, q, settings.DefMacroDeriskEnabled, settings.MacroDeriskEnabledFallback, false)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return map[string]decimal.Decimal{}, nil
	}

	var blend *decimal.Decimal
	state, err := q.MacroStateLatestByComputedAt(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("rebalancingcalc: read latest macro_state: %w", err)
		}
		// No row at all -> blend stays nil, treated as full risk-on by computeOverrides
		// (MacroDeriskGate's own null-safety, mirrored 1:1).
	} else if state.BlendFactor.Valid {
		b, err := pgnum.ToDecimal(state.BlendFactor)
		if err != nil {
			return nil, fmt.Errorf("rebalancingcalc: decode blend_factor: %w", err)
		}
		blend = &b
	}

	rows, err := q.ListAllPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebalancingcalc: list positions: %w", err)
	}
	assetRows, err := q.ListAllAssets(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebalancingcalc: list assets: %w", err)
	}
	all, err := DecodeAllPositions(rows, assetRows)
	if err != nil {
		return nil, err
	}
	return computeOverrides(mainPrimaries(all), blend), nil
}

// mainPrimaries selects the rows the macro gate interpolates: one per MAIN asset — its
// primary subasset, which carries the asset's targets (task 156; the old MAIN non-companion
// rows, the same set under the primary = old main row migration rule).
func mainPrimaries(all []PositionRow) []PositionRow {
	var out []PositionRow
	for _, p := range all {
		if p.Portfolio == PortfolioMAIN && p.IsPrimary {
			out = append(out, p)
		}
	}
	return out
}

// computeOverrides computes primary ticker -> effectiveTarget (0-100 scale) for every MAIN asset
// (ported from the Java MacroDeriskGate; callers pass mainPrimaries: one primary row per asset,
// carrying the asset's targets — the interpolation itself is unchanged), per ADR-044 §7.3. blend
// nil is treated as 1 (full risk-on), the null-safety for the macro-data bootstrap period.
//
// Sum-conserving by construction: since Sum(riskOn) == Sum(riskOff) == 100 across the input set
// (guaranteed by the DB data, not re-verified here — nor was it in the original) and the same blend
// scalar interpolates every ticker, Sum(effective) = (1-b)*100 + b*100 = 100 for any b in [0,1].
func computeOverrides(mainPositions []PositionRow, blend *decimal.Decimal) map[string]decimal.Decimal {
	if len(mainPositions) == 0 {
		return map[string]decimal.Decimal{}
	}
	b := decimal.NewFromInt(1)
	if blend != nil {
		b = *blend
	}
	oneMinusB := decimal.NewFromInt(1).Sub(b)

	overrides := make(map[string]decimal.Decimal, len(mainPositions))
	for _, p := range mainPositions {
		riskOn := decimal.Zero
		if p.TargetAllocationPct != nil {
			riskOn = *p.TargetAllocationPct
		}
		riskOff := riskOn
		if p.TargetAllocationPctRiskOff != nil {
			riskOff = *p.TargetAllocationPctRiskOff
		}
		effective := riskOff.Mul(oneMinusB).Add(riskOn.Mul(b))
		overrides[p.Ticker] = effective
	}
	return overrides
}
