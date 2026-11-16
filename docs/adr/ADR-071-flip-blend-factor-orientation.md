# ADR-071: Flip `blend_factor` Orientation (0 = Risk-Off, 1 = Risk-On)

**Date:** 2026-08-05
**Status:** Accepted
**Extends:** ADR-044 (blend factor), ADR-048/ADR-049 (steepness divisor)

## Context

Since ADR-044, the macro de-risk gate's continuous scalar `blend_factor ∈ [0,1]` has used the
orientation `0 = full risk-on, 1 = full risk-off`. The user finds this backwards to reason
about — "higher number = more risk-on" reads more intuitively than "higher number = more
defensive." This is a pure UX/readability preference: it does not reflect any change to which
macro conditions count as risk-off, nor to any of the mechanism's actual tuning.

## Decision

Flip the scalar's polarity so `blend_factor`: **0 = full risk-off, 1 = full risk-on**. This is a
pure orientation flip — `new_blend = 1 − old_blend` everywhere the value is computed, stored, or
consumed. Nothing else about the mechanism changes:

- The two stored per-position regime tables — `positions.target_allocation_pct` (risk-on) and
  `positions.target_allocation_pct_risk_off` (risk-off) — keep their names and their values
  untouched. Only which end of the `[0,1]` scale each one is multiplied by changes.
- The 0.7/0.3 net-liq/Global-M2 z-score weights, the 3-month smoothing window, and the
  `BLEND_STEEPNESS_DIVISOR = 0.5` (ADR-049) are unchanged.
- The 5-band `MacroZone` threshold values (`0.2/0.4/0.6/0.8`) are unchanged — only which label
  sits at which end swaps.

### 1. Blend formula

```
old: blend = clip(0.5 − smoothed_z / steepness_divisor, 0, 1)     [0 = risk-on, 1 = risk-off]
new: blend = clip(0.5 + smoothed_z / steepness_divisor, 0, 1)     [0 = risk-off, 1 = risk-on]
```

Only the sign in front of the `smoothed_z / steepness_divisor` term flips (`−` → `+`). `smoothed_z`
itself, and everything feeding it (`composite_z`, the 0.7/0.3 weights, the 3-month rolling
average), is untouched — a rising `smoothed_z` (improving net-liq/Global-M2 momentum) now pushes
`blend` up toward `1` (risk-on) instead of down toward `0` (risk-on under the old orientation);
the direction of the underlying signal is identical, only which numeral represents "risk-on" has
changed.

### 2. Per-position interpolation (`MacroDeriskGate`)

```
old: effective = riskOn × (1 − blend) + riskOff × blend
new: effective = riskOff × (1 − blend) + riskOn × blend
```

`riskOn` and `riskOff` still refer to the same two stored columns as before
(`target_allocation_pct` / `target_allocation_pct_risk_off`) — only which one is weighted by
`blend` vs. `(1 − blend)` swaps, so that `blend = 1` (now "full risk-on") still resolves every
position to its risk-on target, and `blend = 0` (now "full risk-off") still resolves every
position to its risk-off target.

### 3. `MacroZone` band mapping

Thresholds are unchanged; the label at each end swaps:

| Band | Old label | New label |
|---|---|---|
| `blend < 0.2` | `RISK_ON` | `RISK_OFF` |
| `[0.2, 0.4)` | `MID_ON` | `MID_OFF` |
| `[0.4, 0.6)` | `NEUTRAL` | `NEUTRAL` |
| `[0.6, 0.8)` | `MID_OFF` | `MID_ON` |
| `blend ≥ 0.8` | `RISK_OFF` | `RISK_ON` |

(`MID_ON`/`MID_OFF` swap which band they occupy, not their names — a value in `[0.6,0.8)` used
to be labeled `MID_OFF` and is now labeled `MID_ON`, matching the new "higher = more risk-on"
convention.)

### 4. Null-safe default (bootstrap period)

While there is insufficient history to compute a smoothed z-score, `blend_factor` stays `NULL`
in `macro_state`, and every consumer that null-coalesces it for the "not enough data yet, behave
as pure risk-on" fallback changes its numeral from `0` to `1` — the meaning of the fallback
("collapse to the risk-on target for every position") does not change, only which numeral now
represents that state.

### 5. Data migration for existing persisted rows

New Flyway migration `V53__flip_blend_factor_orientation.sql`:

```sql
UPDATE macro_state SET blend_factor = 1 - blend_factor WHERE blend_factor IS NOT NULL;
```

plus updated `COMMENT ON COLUMN` statements for `macro_state.blend_factor`,
`positions.target_allocation_pct`, and `positions.target_allocation_pct_risk_off` (their
`(blend=0)` / `(blend=1)` parentheticals swap to match). Every historical `blend_factor` value
must be inverted, not just newly computed ones — the column would otherwise mix two incompatible
orientations across rows written before vs. after this change, corrupting the Settings
blend-history chart and any other reader that trusts a single consistent scale.

`composite_z` and `smoothed_z` are **not** touched by this migration or this decision — they are
z-scores of the underlying macro momentum series, independent of which end of `blend_factor`
means what. Only `blend_factor` itself carries the orientation.

## Rationale

Purely a readability/UX preference the user expressed directly, not a change to the underlying
signal, its inputs, its calibration, or its thresholds. No backtest evidence bears on this
decision because no backtest result changes — every quantity the mechanism was tuned against
(z-scores, smoothing, steepness, per-position targets) is bit-for-bit identical before and after;
only the scalar's cosmetic polarity and its persisted historical values move.

## Consequences

- **Backend:** `MacroDataService.clipBlend`, `MacroZone.fromBlendFactor`,
  `MacroDeriskGate.computeOverrides` (including its null-safe default), and every Javadoc
  describing "0 = full risk-on, 1 = full risk-off" must be updated to the new orientation. Tests
  with hardcoded blend/zone expectations tied to the old orientation (`MacroDataIT`,
  `PositionsComputedIT`, `MarketDataControllerTest`) need rewriting by intent, not blind
  find-replace (e.g. "blend=0 must equal risk-on" becomes "blend=1 must equal risk-on").
- **Frontend:** `macroBlendColor.ts`'s color interpolation must swap its `t=0`/`t=1` color
  anchors so blend=0 (now risk-off) still renders blue and blend=1 (now risk-on) still renders
  orange. `macroBlendBacktest.ts`'s ~180 frozen historical points were computed under the old
  formula orientation and must be inverted at the export site (`blend: 1 - blend`), not
  hand-edited row by row. `MacroBlendHistoryChart.tsx`'s "(0 = risk-on, 1 = risk-off)" caption
  flips. Any other "0 = risk-on" comment in `frontend-web/src` or `shared-ts/src` must be
  updated to match.
- **No schema shape change** — `blend_factor` stays `NUMERIC(7,6) NULL`, same CHECK `0–1`, same
  column name. This is a values-and-meaning migration, not a structural one.
- **One-time historical-data inversion is irreversible in place** — `V53` overwrites
  `macro_state.blend_factor` values directly (as every prior data-shape migration in this schema
  does); recovering the pre-flip values requires a DB restore, same caveat as every other
  irreversible Flyway data migration in this project (e.g. V31's regression-column drop).
- Does not touch the buy-only invariant (ADR-045) or which macro conditions are treated as
  risk-off — a decelerating net-liq/Global-M2 environment still pushes the gate toward the
  defensive regime table; only the numeral naming that end of the scale changes.
