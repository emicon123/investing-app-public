# ADR-069: Macro Blend-Factor History Chart — `GET /market-data/macro-state/history`

**Date:** 2026-08-04
**Status:** Accepted
**Extends:** ADR-044 (blend factor), ADR-048/049 (steepness divisor tightening)

## Context

The user wants a chart on the Settings page showing the macro de-risk `blendFactor` (ADR-044,
0 = full risk-on, 1 = full risk-off) over time — dashed for the pre-live/backtest period, solid
for the live period, split at the **2026-07-01** live-cutover date.

**Live data already exists and accumulates.** `macro_state` (entity:
`backend/src/main/java/pl/investing/entity/MacroState.java`) is one row per `asOfDate`, upserted
by `MacroDataService` on `MacroDataScheduler`'s weekly-Friday + monthly triggers. Queried against
the live DB before writing this ADR: 8 rows, 2026-07-01 through 2026-07-29, growing weekly. Only
`findTopByOrderByComputedAtDesc()` and `findByAsOfDate()` exist on `MacroStateRepository`
today — nothing reads the full history. `GET /market-data/macro-state`
(`MarketDataController` → `MacroDataService.currentStateDto()`) only ever returns the single
latest row.

**Pre-live data is a separate, already-solved problem, included here only for provenance.**
`backtest/results/macro_blend_series.csv` (produced by
`backtest/studies/portfolio_macro_blend_backtest.py`) has monthly `smoothed_z` values from 2011-07
through 2026-05, but its own `blend` column is **stale** — computed with the steepness divisor
that was live when the backtest last ran (`2.0`, pre-ADR-048/ADR-049), not the current live
formula. Using it as-is would create a formula-driven discontinuity at the 2026-07 seam (softer
historical swings meeting a much steeper live line) that would look like a regime change but
isn't one. This was already fixed, outside this ADR's scope: the CSV's raw `smoothed_z` column
was recomputed under the **current** live formula — `clip(0.5 - smoothedZ / 0.5, 0, 1)`, divisor
`0.5` per ADR-049, matching `MacroDataService.BLEND_STEEPNESS_DIVISOR` — and frozen as a static
TS module, `frontend-web/src/lib/macroBlendBacktest.ts` (179 monthly `{ date, blend }` points).
That file is done; `backtest/` isn't part of any Docker image and this data never changes, so
there is no backend involvement in the backtest side at all. It is documented here only so the
provenance of the chart's dashed segment is traceable from one place.

**What's missing is purely the live/solid side**: a read endpoint over the accumulating
`macro_state` history. This is a small, read-only addition — no new table, no new column, no
change to the blend computation itself.

## Decision

### 1. New endpoint — `GET /market-data/macro-state/history`

Returns **every** persisted `macro_state` row, ordered by `asOfDate` ascending, as
`ListResponse<MacroStateHistoryPoint>` (`{ data: [...], meta }`, unpaginated — same envelope
`GET /market-data/prices` and `GET /market-data/fx-rates` already use). Read-only and cheap,
same guarantee as the existing single-row `GET /market-data/macro-state`: it reads `macro_state`
via a new repository query and **never** calls FRED/ECB/BoE/BoJ/chinadata.live/
TradingEconomics/YCharts.

No pagination. `macro_state` accumulates at most a handful of rows per week (weekly net-liquidity
+ monthly Global-M2 triggers) — low hundreds of rows after years of operation, the same
"personal-app scale, unpaginated list" reasoning already applied to `/market-data/prices` and
`/market-data/fx-rates`.

### 2. New DTO — `MacroStateHistoryPoint`

Deliberately minimal — this backs one line on one chart, not the full macro panel
`GET /market-data/macro-state` already returns:

| Field | Type | Notes |
|---|---|---|
| `asOfDate` | `LocalDate` (`string`/`date`) | Never null — `macro_state.as_of_date` is `NOT NULL`. |
| `blendFactor` | `BigDecimal` (`number`), nullable | 0–1. Null only during the bootstrap period (insufficient history for a smoothed z-score) — same nullability `MacroState.blendFactor` already has. |
| `zone` | `MacroZone` (`string` enum), nullable | Derived via the existing pure function `MacroZone.fromBlendFactor(blendFactor)` — no new logic. Null iff `blendFactor` is null. |

`compositeZ`, `smoothedZ`, `riskOff`, `deriskEnabled`, the net-liq/Global-M2 breakdown fields, and
`components[]` are all **omitted** — they're per-request telemetry/debug fields on the current-
state panel, not something a 179+ point time-series chart needs repeated per point. If a future
need arises (e.g. a richer history view), extending this DTO is additive and cheap — every field
already exists on the entity. `zone` earns its place despite the chart's current design using
only `blendFactor` as the plotted value: it's a zero-cost derivation (pure function over a field
already being read) and gives the frontend a ready-made color/label axis (e.g. tooltip "MID_ON")
without a second round trip if the chart grows a hover state later.

### 3. New repository method

```java
// MacroStateRepository
List<MacroState> findAllByOrderByAsOfDateAsc();
```

Spring Data derives this from the method name alone — no new query annotation needed, consistent
with the two existing methods on this repository.

### 4. Live/backtest merge is a frontend concern, noted here for traceability only

The frontend merges this endpoint's series with the frozen `macroBlendBacktest.ts` backtest
series client-side: backtest points (through 2026-05, dashed) followed by live points from this
endpoint (2026-07-01 onward, solid). June 2026 has no representation in either series (the
backtest CSV run ended in May; live accumulation started in July) — a one-month gap the frontend
renders as a break in the line rather than an interpolated bridge, since interpolating across a
formula-provenance seam would fabricate a data point neither source actually produced. This ADR
does not further specify chart rendering (axis, color, gap handling) — that's the frontend
agent's implementation call from this contract, not an architecture decision.

## Contract Summary (for the implementing agents)

**`GET /market-data/macro-state/history`**
- `operationId`: `getMacroStateHistory`
- Auth: `bearerAuth` (same as every other `market-data` endpoint)
- No parameters.
- `200`: `MacroStateHistoryListResponse` → `{ data: MacroStateHistoryPoint[], meta }`, `data`
  ordered by `asOfDate` ascending, one entry per persisted `macro_state` row.
- `401`/`403`/`500`: same shared responses every `market-data` endpoint already documents.
- No `4xx` specific to this endpoint — it's an unconditional full-table read, nothing to
  validate.

**`MacroStateHistoryPoint`**
```json
{ "asOfDate": "2026-07-29", "blendFactor": 0.31, "zone": "MID_ON" }
```

**Backend implementation notes (informational — not implemented by this ADR):**
- `MacroStateRepository.findAllByOrderByAsOfDateAsc(): List<MacroState>` (new).
- New record `pl.investing.dto.macro.MacroStateHistoryPoint(LocalDate asOfDate, BigDecimal
  blendFactor, MacroZone zone)`, built by mapping each `MacroState` row with `zone =
  MacroZone.fromBlendFactor(state.getBlendFactor())` — the same call `MacroDataService
  .currentStateDto()` already makes for the single-row endpoint, just applied per-row here. A
  one-line stream map in `MacroDataService` (or a MapStruct method on the existing
  `MacroStateMapper`, mirroring its `toDto`) both work; left to the backend agent's judgment
  since it's a trivial 1:1 projection with no new business logic.
- New `MarketDataController` method, `GET /macro-state/history` →
  `new ListResponse<>(macroDataService.historyDtos(), Meta.create())` (naming illustrative).
- No Flyway migration — `macro_state` is unchanged; this is a new query and a new read path
  only.

**Contract files updated by this ADR:**
- `docs/openapi/paths/market-data.yaml` — new `macroStateHistory` operation.
- `docs/openapi/openapi.yaml` — new path entry `/market-data/macro-state/history`.
- `docs/openapi/components/schemas/macro.yaml` — new schemas `MacroStateHistoryPoint`,
  `MacroStateHistoryListResponse`.
- `docs/architecture/03-api-contract.md` — new subsection under Macro Data, plus the
  "Endpoints Consumed by the Web Frontend" and "Response Wrapper Types" summary lists.

**Not touched by this ADR, left to the backend/frontend implementing agents:**
- `shared-ts/src/schemas/market-data.ts` / `shared-ts/src/client/market-data.ts` — need a hand-
  mirrored `macroStateHistoryPointSchema` / `getMacroStateHistory()`, same convention every other
  endpoint in that package already follows (mirrored by hand, not codegen'd, per the file's own
  existing header comment).
- Any frontend chart component — `frontend-web/src/lib/macroBlendBacktest.ts` (the frozen
  backtest series) already exists and already names this endpoint in its own doc comment; the
  chart component that fetches `GET /market-data/macro-state/history` and merges the two series
  is new frontend work, out of scope here.

## Consequences

- **One new read-only endpoint, one new DTO, one new repository method.** No schema change, no
  Flyway migration, no change to the blend computation, no change to any existing endpoint's
  response shape.
- **`macro_state` now has two read shapes**: the rich single-row current-state panel
  (`GET /market-data/macro-state`) and this minimal ordered-history list
  (`GET /market-data/macro-state/history`). Both derive `zone` the same way
  (`MacroZone.fromBlendFactor`), so the two are guaranteed to agree at their overlapping point
  (today's row) with no risk of drift between them — there's only one place the zone thresholds
  live.
- **Chart correctness depends on a fact outside this endpoint's control**: the backtest CSV's
  `blend` column being stale (pre-ADR-048/049 divisor) was already caught and corrected in the
  frozen `macroBlendBacktest.ts` module before this ADR was written. Anyone regenerating that
  file in the future must re-apply the same "recompute from `smoothed_z` under the current
  `BLEND_STEEPNESS_DIVISOR`, don't trust the CSV's own `blend` column" step, or the dashed segment
  will silently drift out of formula-alignment with the solid one again.
- **No pagination now; revisit if this table ever grows to real scale.** At weekly + monthly
  cadence this is a non-issue for years — flagged only so a future reader doesn't mistake the
  omission for an oversight.
