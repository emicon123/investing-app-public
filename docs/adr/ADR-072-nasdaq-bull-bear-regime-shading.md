# ADR-072: NASDAQ Bull/Bear Regime Shading on the Blend-Factor History Chart

**Date:** 2026-08-06
**Status:** Accepted
**Extends:** ADR-070 (BTC/Nasdaq price overlays), ADR-069 (blend-factor history chart)

## Context

The Settings blend-factor history chart (ADR-069) plots the macro de-risk `blendFactor`
(0 = risk-off, 1 = risk-on) over time, with optional BTC/Nasdaq 100 price overlays on a
right-hand log axis (ADR-070, `showBtc` / `showNdx` toggles). The user wants a further, purely
visual cross-check: shade the chart background to show NASDAQ (`^NDX`) bull vs. bear market
periods, so the `blendFactor` line's regime can be eyeballed against actual NDX cycles rather
than just the raw overlay price line's shape.

**No new data is needed.** The `^NDX` daily close series is already fetched by the existing
`showNdx` overlay query (`getBenchmarkHistory({ symbol: "^NDX" })`, ADR-070, 12h `staleTime`).
Regime dating is a pure client-side derivation over that same series — no backend, API, or
schema change.

## Decision

### 1. Regime-dating algorithm — single-threshold zigzag peak/trough dating

Walk the `^NDX` daily close series once, left to right. Track a **pending extreme** (a running
high while scanning for a peak, a running low while scanning for a trough) and its index. When
price reverses by ≥ `thresholdPct` from that pending extreme, confirm a turning point (peak or
trough) at the pending extreme's index and flip direction. Segments alternate: **bull** =
trough→peak, **bear** = peak→trough.

```
computeZigzagRegimes(points: {ts, close}[], thresholdPct = 0.20): {type, startTs, endTs}[]

anchorIdx = 0                      // start of the segment currently being built
pendingIdx = 0, pendingClose = points[0].close
direction = null                   // 'up' | 'down', unset until the first confirmed break

for i in 1..points.length-1:
  close = points[i].close
  if direction == null:
    if close >= pendingClose * (1 + thresholdPct):     direction = 'up';   pendingIdx = i; pendingClose = close
    elif close <= pendingClose * (1 - thresholdPct):    direction = 'down'; pendingIdx = i; pendingClose = close
    // else: still undetermined, keep scanning from points[0]
  elif direction == 'up':
    if close > pendingClose:                            pendingIdx = i; pendingClose = close   // new running high
    elif close <= pendingClose * (1 - thresholdPct):     // 20% off the high -> confirmed peak
      emit { type: 'bull', startIdx: anchorIdx, endIdx: pendingIdx }
      anchorIdx = pendingIdx; direction = 'down'; pendingIdx = i; pendingClose = close
  elif direction == 'down':
    if close < pendingClose:                            pendingIdx = i; pendingClose = close   // new running low
    elif close >= pendingClose * (1 + thresholdPct):     // 20% off the low -> confirmed trough
      emit { type: 'bear', startIdx: anchorIdx, endIdx: pendingIdx }
      anchorIdx = pendingIdx; direction = 'up'; pendingIdx = i; pendingClose = close

// trailing, unconfirmed segment from the last confirmed turning point to the
// last available data point, typed by the current (in-progress) direction —
// otherwise the most recent months would render unshaded.
if direction != null and anchorIdx < points.length - 1:
  emit { type: direction == 'up' ? 'bull' : 'bear', startIdx: anchorIdx, endIdx: points.length - 1 }

return emitted.map(s => ({ type: s.type, startTs: points[s.startIdx].ts, endTs: points[s.endIdx].ts }))
```

Default **`thresholdPct = 0.20`** — the conventional bear-market definition — as a single
symmetric parameter. No separate entry/exit thresholds, no minimum-duration filter: any
confirmed turning point already required a full 20% move, which is what suppresses noise/
whipsaw from short-lived wiggles; a duration filter would be redundant on top of that and was
explicitly rejected in favor of this simpler rule.

*Amendment (2026-08-06):* the single global `thresholdPct = 0.20` default is replaced by a
**per-symbol threshold** — a fixed threshold doesn't fit assets of very different volatility.
BTC's normal volatility means ≥20% pullbacks happen routinely within an ongoing bull run, so 20%
made BTC's regime toggle noisy/uninformative (routine scratches, not real regime changes); Gold
is comparatively low-volatility, so a 20% move is a rare/major event, and the fixed default left
Gold's toggle almost never firing. Final thresholds: **BTC-USD 30%**, **`^NDX` 20%** (unchanged —
the conventional equity bear-market definition), **`GC=F` 10%**. These live as a caller-side
`REGIME_THRESHOLD: Record<BenchmarkSymbol, number>` map in `MacroBlendHistoryChart.tsx`, passed
explicitly into each symbol's `computeZigzagRegimes(points, threshold)` call —
`computeZigzagRegimes`'s own signature/default (`thresholdPct = 0.20` in `priceRegimes.ts`) is
unchanged; this is purely a caller-side override, not an algorithm change. Also, regime segments
are now computed over the **weekly** (not daily) point series, following ADR-070's interval
change — a better fit for the zigzag algorithm, since 10–30% thresholds are far larger than
single-week noise, so weekly resolution cuts algorithmic noise from day-to-day whipsaws without
materially changing which turning points get confirmed.

*Amendment (2026-08-06), Hang Seng added:* a fourth threshold, **`^HSI` 25%**, positioned as a
middle value between `^NDX`'s 20% (developed-market convention) and BTC's 30% (crypto-tuned).
Unlike the other three thresholds — each directly confirmed by the user — this one is a starting
default pending user feedback, not yet validated against real regime behavior.

Two edge behaviors are intentional, not bugs:
- **Leading gap:** points before `direction` is first established (i.e. before the series has
  moved 20% in either direction from its first close) belong to no segment and render
  unshaded. Given `^NDX`'s multi-decade daily history this window is at most a few weeks.
- **Trailing provisional segment:** the most recent segment (anchor → last point) is always
  emitted even though its end is not yet a confirmed turning point. It can retroactively
  extend or its endpoint can shift as new data arrives — inherent to any real-time zigzag,
  not a defect.

### 2. Fully client-side — new pure function module, no backend/API/schema changes

New file `frontend-web/src/lib/priceRegimes.ts`:

```ts
export type PriceRegimeType = "bull" | "bear";
export interface PriceRegimeSegment { type: PriceRegimeType; startTs: number; endTs: number }
export function computeZigzagRegimes(
  points: { ts: number; close: number }[],
  thresholdPct = 0.20
): PriceRegimeSegment[]
```

Pure function, no React/recharts import — trivially unit-testable in isolation against a
synthetic close series. It operates on the same `{ ts, close }[]` shape `buildOverlayPoints`
already produces from `BenchmarkHistoryPoint[]` (`MacroBlendHistoryChart.tsx`), so the caller
reuses that existing conversion rather than re-parsing dates.

### 3. Visualization — translucent full-height background bands, not a line series

Rendered as recharts `ReferenceArea` per segment (`x1={startTs}`, `x2={endTs}`, no `y1`/`y2` so
each band spans the full plot height), placed in `MacroBlendHistoryChartInner`'s JSX **before**
the `<Line>` elements so they paint behind the blend line and any price overlay. Restrained,
very-low-opacity fills so the blend line's own per-point gradient (`blendToColor`,
`frontend-web/src/lib/macroBlendColor.ts`) stays legible on top:

- Bear: `oklch(0.55 0.16 25 / 0.08)` (restrained rose/red)
- Bull: `oklch(0.65 0.15 145 / 0.08)` (restrained green)

These follow the same convention ADR-070 established for overlay colors — literal `oklch()`
strings passed straight to recharts' `fill`, not hex-plus-OKLab-conversion like
`macroBlendColor.ts`'s interpolated scale (that machinery exists to *interpolate* along a
continuum; these are two fixed, non-interpolated tints, so a literal `oklch()` per ADR-070's
precedent is simpler and sufficient). The frontend agent may fine-tune lightness/chroma/alpha
for legibility against both themes — these are a starting point, not a pixel-exact spec.

*Amendment (2026-08-06):* the initial implementation set only the oklch alpha channel (`/ 0.08`
above) and left recharts' `ReferenceArea` `fillOpacity` prop unset. `ReferenceArea`'s own
`defaultProps` sets `fillOpacity: 0.5` (confirmed in
`node_modules/recharts/lib/cartesian/ReferenceArea.js`), which multiplies against the fill's own
alpha — so the effective on-screen opacity was ~0.08 × 0.5 ≈ 0.04, nearly invisible against both
themes' backgrounds, which is how the user caught it ("I can't see how the bear/bull works"). Fix:
every `<ReferenceArea>` now sets **`fillOpacity={1}`** explicitly, making the oklch alpha channel
the sole opacity source, and that alpha was raised to roughly **0.14–0.18** per the user's
explicit ask that "the background of the chart should be all colored red/green dependent on the
regime" — visibly colored, while still restrained enough for the blend line's own per-point
gradient to read on top. The `/ 0.08` figures above are superseded by this amendment; ~0.14–0.18
is the current starting point, still fine-tunable by the frontend agent for legibility in both
themes.

### 4. Gating — only meaningful, only shown, when the Nasdaq overlay is already on

The regime toggle depends on the same `^NDX` series `showNdx` already fetches and is visually
about the NDX line specifically (not BTC, not a generic "any overlay" feature), so it is only
enabled/shown when `showNdx` is true. Placed as a new legend item in `MacroBlendLegend`
(`MacroBlendHistoryChart.tsx`), next to the existing Nasdaq 100 overlay toggle — suggested
Polish label **"Cykle NASDAQ (byk/niedźwiedź)"**, consistent with the legend's existing Polish
labels ("Backtest (do 06.2026)", "Dane na żywo (od 07.2026)"). Whether it's disabled-but-visible
or hidden outright while `showNdx` is false is left to the frontend agent's judgment — either
satisfies the requirement, the only hard constraint is that it must not be interactable or
render bands while `showNdx` is off.

Applies in both the inline card chart and the full-screen zoom dialog — both already receive
`overlays` / `overlayToggles` props from the parent `MacroBlendHistoryChart`; the new
`showNdxRegimes` boolean and derived `PriceRegimeSegment[]` follow the same prop-drilling
pattern down to `MacroBlendHistoryChartInner`.

*Amendment (2026-08-06):* generalized to both overlays, not NDX-only. The user asked for the same
bull/bear shading on the BTC overlay too ("Same should happen for BTC"), so this section's rule
now applies **per overlay**, symmetrically: each price overlay (`showBtc`, `showNdx`, ADR-070 §3)
gets its own independent regime toggle in the legend, gated on — and reset to `false` when — its
own overlay toggle turns off, mirroring the existing per-overlay generalization ADR-070 §2/§3
already established for the two raw price lines. Suggested Polish labels: **"Cykle BTC (byk/
niedźwiedź)"** next to the Bitcoin overlay toggle, and the existing **"Cykle NASDAQ (byk/
niedźwiedź)"** next to the Nasdaq 100 overlay toggle. Both regime toggles may be enabled
simultaneously — the resulting bands can visually overlap, which is accepted rather than made
mutually exclusive, consistent with the app already allowing both raw price overlays on at once.

*Amendment (2026-08-06), Gold added:* extended to three overlays. Gold (`GC=F`, ADR-070's third
overlay symbol) gets its own regime toggle the same way, gated on its own Gold overlay toggle
(`showGold`) per this section's rule. Suggested Polish label: **"Cykle Złota (byk/niedźwiedź)"**
next to the Gold overlay toggle. All three regime toggles may be enabled simultaneously, same as
the existing BTC/NDX pair.

*Amendment (2026-08-06), Hang Seng added:* extended to four overlays. `^HSI` gets its own regime
toggle the same way, gated on its own Hang Seng overlay toggle (`showHsi`) per this section's
rule. Suggested Polish label: **"Cykle Hang Seng (byk/niedźwiedź)"** next to the Hang Seng
overlay toggle. All four regime toggles may be enabled simultaneously.

### 5. No persisted preference

Session-only `useState` in `MacroBlendHistoryChart` (e.g. `showNdxRegimes`, default `false`),
consistent with `showBacktest` / `showBtc` / `showNdx` (ADR-070 §3) — no `settings` row, no
localStorage.

*Amendment (2026-08-06):* generalized per §4's amendment — two independent booleans,
**`showBtcRegimes`** and **`showNdxRegimes`**, both session-only `useState` in
`MacroBlendHistoryChart`, both default `false`, both threaded to the inline card and the
full-screen dialog the same way.

*Amendment (2026-08-06), Gold added:* a third boolean, **`showGoldRegimes`**, joins the other
two — same session-only `useState`, default `false`, same threading.

*Amendment (2026-08-06), Hang Seng added:* a fourth boolean, **`showHsiRegimes`**, joins the
other three — same session-only `useState`, default `false`, same threading.

### 6. Recompute scope — full fetched series per symbol, independent of the visible window

Segments are computed via `useMemo` keyed on the raw `^NDX` query result (`ndxQuery.data`), over
the **entire** fetched series — the same date range the overlay itself covers (`^NDX` history
goes back to 2011-07 per ADR-070, matching the blend backtest window), not clipped to whatever
window is currently visible (whole range, Brush-zoomed range in full-screen, or live-only range
when the backtest segment is hidden). Recharts already clips `ReferenceArea`s whose `x1`/`x2`
fall outside the active axis domain, so no separate slicing logic is needed to keep bands from
rendering outside the chart's current view — segments computed once, rendering adapts for free.

*Amendment (2026-08-06):* generalized per §4 — two independent `useMemo`s, one per symbol, each
keyed on its own raw query result (`btcQuery.data` / `ndxQuery.data`, both already fetched by the
existing `showBtc`/`showNdx` overlay queries per ADR-070 §1, each already covering its full
history), each still computed over that symbol's entire fetched series per the unchanged rule
above. The two resulting `PriceRegimeSegment[]` arrays are concatenated into one combined
**`regimes: PriceRegimeSegment[]`** array passed down to the chart — no per-segment `source`/
`symbol` tag is needed, since rendering only ever needs each segment's own `type`/`startTs`/
`endTs` to place a `ReferenceArea`, regardless of which symbol produced it.

*Amendment (2026-08-06), Gold added:* a third `useMemo`, keyed on `goldQuery.data` (ADR-070 §1's
new Gold overlay query), joins the concatenation into `regimes` — same rule, same shape, no
`source` tag needed.

*Amendment (2026-08-06), Hang Seng added:* a fourth `useMemo`, keyed on `hsiQuery.data` (ADR-070
§1's new Hang Seng overlay query), joins the concatenation into `regimes` — same rule, same
shape.

## Contract Summary (for the implementing agent)

*Amendment (2026-08-06): the bullets below are rewritten to the current, generalized target
contract (§3–§6 amendments above) — the original NDX-only `ndxRegime`/`ndxRegimes` shape has been
fully superseded, not left as a parallel option.*

- New file: `frontend-web/src/lib/priceRegimes.ts` — `computeZigzagRegimes`, no dependencies,
  exported `PriceRegimeType` / `PriceRegimeSegment` types (see §1–2 above for exact signature).
  Unchanged by both amendments — one pure algorithm, now simply called once per symbol instead
  of once.
- Modified: `frontend-web/src/routes/settings/_components/MacroBlendHistoryChart.tsx`
  - New `ReferenceArea` import from `recharts`.
  - Two independent regime toggles, one per overlay — **`showBtcRegimes`** and
    **`showNdxRegimes`**, session `useState` booleans in `MacroBlendHistoryChart` (§5), each
    threaded to both the inline card and `MacroBlendHistoryDialog`, and each reset to `false`
    when its own `showBtc`/`showNdx` overlay toggle turns off (§4).
  - Two `useMemo`s (§6), each keyed on its own overlay query result (`btcQuery.data` /
    `ndxQuery.data`), each reusing `buildOverlayPoints` for the `{ts, close}[]` input, computed
    only when that symbol's overlay + regime toggle are both on. The two resulting arrays are
    concatenated into one combined **`regimes: PriceRegimeSegment[]`** prop passed down to
    `MacroBlendHistoryChartInner` — no per-segment `source`/`symbol` field.
  - A **`regimeToggles: RegimeToggle[]`** prop (each entry carrying its own `id`/`name`, e.g.
    `"btc"` / `"Cykle BTC (byk/niedźwiedź)"` and `"ndx"` / `"Cykle NASDAQ (byk/niedźwiedź)"`)
    replaces the original singular `ndxRegime: RegimeToggle` prop.
  - `MacroBlendHistoryChartInner` renders one `ReferenceArea` per segment in the combined
    `regimes` array, before the `<Line>`s, with **`fillOpacity={1}`** set explicitly on every one
    (Amendment 1, §3 — this is the visibility fix, not optional) and the raised ~0.14–0.18 oklch
    fill alpha as the sole opacity source.
  - `MacroBlendLegend` gets two new toggle entries, sourced from `regimeToggles`, each placed next
    to its respective BTC / Nasdaq 100 overlay toggle, each enabled only while its own overlay
    toggle (`showBtc` / `showNdx`) is true.
- **No backend changes. No `shared-ts` changes. No OpenAPI changes. No Flyway migration. No
  `docs/architecture/` updates** — none of the living architecture docs document frontend
  chart-rendering internals (they document the API contract, DB schema, and rebalancing
  algorithm, none of which this ADR touches); `03-api-contract.md`'s existing ADR-069/ADR-070
  entries already fully describe the two endpoints this feature reuses unchanged. Re-checked
  against both 2026-08-06 amendments (opacity fix, BTC generalization) — still true, neither
  touches the API/schema surface.

*Amendment (2026-08-06), Gold added:* further extended to three symbols, with per-symbol
thresholds (§1 amendment above) — supersedes the two-symbol bullets above where they conflict:

- **`REGIME_THRESHOLD: Record<BenchmarkSymbol, number>`** — `{ "BTC-USD": 0.30, "^NDX": 0.20,
  "GC=F": 0.10 }` — caller-side map in `MacroBlendHistoryChart.tsx`, passed per-symbol into each
  `computeZigzagRegimes` call.
- A third boolean **`showGoldRegimes`** (§5) and a third `useMemo` keyed on `goldQuery.data` (§6)
  join the existing BTC/NDX pair; all three concatenate into the same
  `regimes: PriceRegimeSegment[]` prop.
- `regimeToggles` gains a third entry, `"gold"` / **"Cykle Złota (byk/niedźwiedź)"**, next to the
  Gold overlay toggle in `MacroBlendLegend` — six toggles total on the chart (3 overlay + 3
  regime).
- Regime points are now sourced from the weekly series (§1 amendment), matching ADR-070's
  interval change — no change to the algorithm itself.
- Still no backend / `shared-ts` / OpenAPI / Flyway / `docs/architecture/` changes — same
  reasoning as the bullet above.

A fourth benchmark symbol (a China equity index; ticker not yet chosen — candidates `^HSI`,
`000001.SS`, `MCHI`, `FXI`) is being researched in parallel and is expected as a near-term
follow-up amendment to both this ADR and ADR-070 once confirmed — its threshold and color are
out of scope here.

*Amendment (2026-08-06), Hang Seng added — resolves the "fourth benchmark symbol" flag above:*
further extended to four symbols. `REGIME_THRESHOLD` is now `{ "BTC-USD": 0.30, "^NDX": 0.20,
"GC=F": 0.10, "^HSI": 0.25 }` — the `^HSI` value is a starting default, not user-confirmed like
the other three (§1 amendment). A fourth boolean **`showHsiRegimes`** (§5) and a fourth `useMemo`
keyed on `hsiQuery.data` (§6) join the other three; all four concatenate into `regimes`.
`regimeToggles` gains a fourth entry, `"hsi"` / **"Cykle Hang Seng (byk/niedźwiedź)"**, next to
the Hang Seng overlay toggle — eight toggles total on the chart (4 overlay + 4 regime). Regime
points sourced from the weekly series, same as the other three. Still no backend / `shared-ts` /
OpenAPI / Flyway / `docs/architecture/` changes.

## Consequences

- The chart gains a visual cross-check between the macro `blendFactor` regime and actual price
  bull/bear history, at zero marginal network/backend cost — it reuses each overlay's
  already-fetched series (originally NDX-only; BTC added by the 2026-08-06 amendment below).
- Regime dating is a deliberate simplification (single symmetric 20% threshold, no
  minimum-duration filter) — it will occasionally date a turning point differently than a more
  elaborate NBER/Pagan-Sossounov-style algorithm (which typically also enforces a minimum phase
  length), but that's the explicitly chosen trade-off for simplicity over precision.
- The most recent shaded band is always provisional (§1) — its end can shift as new data arrives.
  This is inherent to any real-time zigzag and expected, not a defect to fix later.
- The 20% threshold is currently a hardcoded default at the call site with no UI control. If the
  user later wants it tunable, that's a small additive follow-up — `computeZigzagRegimes`
  already takes `thresholdPct` as a parameter.
  - *Amendment (2026-08-06):* superseded — the threshold is no longer a single hardcoded 20%
    default; see the per-symbol `REGIME_THRESHOLD` map amendment (§1, Contract Summary) above.
    Still no UI control; still a small additive follow-up if ever wanted.
- *Amendment (2026-08-06):* two changes shipped after initial user testing, neither touching the
  core algorithm (§1, unchanged):
  1. **Visibility bug fix (§3).** The initial fill relied on the oklch alpha channel alone
     (`/ 0.08`) without setting recharts' `ReferenceArea` `fillOpacity` prop, which defaults to
     `0.5` and silently halved the intended opacity again (~0.04 effective) — the bands were
     nearly invisible, which is how the user caught it. Fixed by setting `fillOpacity={1}`
     explicitly and raising the oklch alpha to ~0.14–0.18, now visibly colored per the user's
     ask.
  2. **Generalized to BTC (§4–§6).** The feature is no longer NDX-only — BTC gets its own
     independent regime toggle (`showBtcRegimes`), gated on its own `showBtc` overlay toggle the
     same way NDX's is. Naming generalized throughout: `showBtcRegimes`/`showNdxRegimes`, a
     combined `regimes: PriceRegimeSegment[]` array, and `regimeToggles: RegimeToggle[]` replace
     the original singular `ndxRegime`/`ndxRegimes` shape (see Contract Summary above for the
     full rewritten prop set).
  3. **Generalized to Gold, with per-symbol thresholds (§1, §4–§6).** A third overlay, Gold
     (`GC=F`), gets its own regime toggle (`showGoldRegimes`) the same way BTC's was added. The
     single global 20% threshold is replaced by a per-symbol `REGIME_THRESHOLD` map — BTC 30%,
     NDX 20% (unchanged), Gold 10% — since a fixed threshold suited neither BTC's routine ≥20%
     pullbacks (too noisy) nor Gold's comparatively rare big moves (too quiet). Regime points are
     now sourced from the weekly (not daily) series, matching ADR-070's interval change — a
     better fit for the algorithm's noise floor, not a compromise.
  4. **Generalized to Hang Seng, resolving the flagged 4th-symbol follow-up (§1, §4–§6).** A
     fourth overlay, `^HSI` (chosen over `MCHI`/`000001.SS`/`FXI` — see ADR-070 §1 for the
     comparison), gets its own regime toggle (`showHsiRegimes`) the same way. Its threshold, 25%,
     sits between NDX's 20% and BTC's 30% as a starting default — unlike the other three
     thresholds, not yet user-confirmed.
