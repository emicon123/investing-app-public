# ADR-073: Toggleable Legend + Macro Blend Overlay on the Portfolio History Chart

**Date:** 2026-08-06
**Status:** Accepted
**Related:** ADR-069 (macro blend history chart — origin of the toggle-legend and gradient-line
pattern being reused here), ADR-057 (trade-history chart — prior art for joining a second series
onto a portfolio-snapshot series by date)

## Context

`PortfolioValueLine.tsx` (`frontend-web/src/components/charts/`) is the "Historia portfela"
chart — a recharts `ComposedChart` rendered on the `/history` route and reused unchanged (same
component, `height="100%"`) inside `HistoriaDialog.tsx`'s full-screen view. It plots five series
against `HistoriaEntry[]` data received as a prop: `portfolioValue` and `totalDeposits` (PLN
axis, lines), `profitPln` (PLN axis, bar), and `xirr` / `returnPct` (percent axis, lines). All
five are always rendered, with a plain, non-interactive recharts `<Legend/>` — there is no way to
isolate one series or hide another.

Separately, the Settings-page macro chart (`MacroBlendHistoryChart.tsx`, ADR-069) already solved
two adjacent problems the user now wants applied here too:

1. **A custom clickable legend** (`MacroBlendLegend`) — plain `<button onClick aria-pressed>`
   elements with a checkbox-style square (`<Check/>` icon when enabled, dimmed inline SVG
   line-swatch when off) driving lifted `useState` booleans, instead of recharts' native
   (unused-in-this-repo) `<Legend onClick>` prop.
2. **A per-point gradient-stroke line** — `blendToColor(blend: number): string`
   (`frontend-web/src/lib/macroBlendColor.ts`, blue↔orange OKLab interpolation over `[0,1]`) plus
   a local `buildGradientStops(blends: number[])` helper that emits one SVG `<stop>` per data
   point into a `<linearGradient>`, with the `<Line stroke={\`url(#${gradientId})\`}>` pointing at
   it — so the blend line's own color encodes its value along its length.

The user's ask (verbatim): *"History charts - I would like to add a toggle button to those
charts that will trigger blend chart being applied into the historical chart. Also - XIRR, Zysk,
and all others should be also toggable, but all should be active by default, but blend should be
inactive by default."* Follow-up: *"So it's a bit like a copy paste of blend chart from
settings"* — confirming this is a direct reuse of the `MacroBlendLegend` / gradient-line pattern,
not a new design.

This is a pure frontend feature. It reuses the already-existing
`GET /market-data/macro-state/history` endpoint (ADR-069) and its already-registered
`shared-ts` client (`getMacroStateHistory`, `shared-ts/src/client/market-data.ts`) and query key
(`queryKeys.macroState.history`, `frontend-web/src/lib/queryKeys.ts`) exactly as-is. No backend,
database, or OpenAPI change is needed or made by this ADR.

## Decision

### 1. Custom clickable legend replaces the built-in `<Legend/>`, one toggle per series

`PortfolioValueLine.tsx` gains a `MacroBlendLegend`-style legend (same button/checkbox/dimmed-
swatch interaction pattern, adapted for this component's own series set) with one toggle per
existing series, using the Polish labels already in place today:

| Toggle | dataKey | Default |
|---|---|---|
| Wartość portfela | `portfolioValue` | **ON** |
| Depozyty | `totalDeposits` | **ON** |
| Zysk (PLN) | `profitPln` | **ON** |
| XIRR | `xirr` | **ON** |
| Stopa zwrotu | `returnPct` | **ON** |
| Blend (makro) | `blendFactor` (new) | **OFF** |

Toggling a series off removes its `<Line>`/`<Bar>` from the chart (same lifted-`useState`-booleans
mechanism as `MacroBlendLegend`), not just hides it visually — consistent with how the Settings
chart's toggles already behave, and cheaper than keeping a rendered-but-invisible series around.

### 2. The component becomes a conditional self-fetcher, gated on the new toggle

`PortfolioValueLine` currently only receives data as a prop (`HistoriaEntry[]`) and fetches
nothing itself. This ADR changes that narrowly: when the "Blend (makro)" toggle is on, the
component fetches blend history itself —

```ts
useQuery({
  queryKey: queryKeys.macroState.history,
  queryFn: getMacroStateHistory,
  enabled: showBlend,
})
```

— the same query key and fetcher `MacroBlendHistoryChart.tsx` already uses, so both chart
instances share one cache entry once either has fetched it. No new query key, no new client
method. The fetch is gated (`enabled: showBlend`), not eager, since blend defaults off and most
chart views will never need it — the historical/investing use of this chart shouldn't pay a
network round trip it doesn't ask for.

Because `PortfolioValueLine` is one shared component rendered from both `/history` and
`HistoriaDialog`, this toggle/fetch behavior applies uniformly to both — there is no per-caller
opt-out and none is needed; both call sites just get the richer chart.

### 3. Join: as-of/carry-forward by date, not exact-date match

Portfolio snapshot dates (`HistoriaEntry.snapshotDate`) and `macro_state.asOfDate` rows don't
necessarily coincide (snapshots are taken on the app's own cadence; macro state updates on a
weekly/monthly trigger per ADR-069). The blend series is joined onto the existing chart rows using
the **same two-pointer carry-forward join** `buildChartRows` already implements in
`MacroBlendHistoryChart.tsx` (lines ~381–399) for its BTC/NDX overlays: for each snapshot row, walk
the (already date-sorted) blend series forward and attach the latest blend point with
`asOfDate <= snapshotDate`. Snapshot dates earlier than the first available blend point simply get
no blend value for that row (line renders from wherever data starts), matching how the existing
overlay join already treats its own no-data lead-in.

This is a straight lift of an existing, working algorithm — no new join logic needs to be
invented, only re-pointed at this chart's rows and this series.

### 4. Blend gets its own hidden Y-axis, not the existing `pct` axis

Blend is a `[0,1]` factor, not a percentage-formatted return figure, and plotting it on the
existing `pct` axis (which is scaled and tick-formatted for `xirr`/`returnPct`) would either
visually collide with those series or force a shared, misleading scale. This ADR adds a **third
`yAxisId="blend"`**, `domain={[0, 1]}`, rendered `hide` (no visible axis line or ticks) — same
approach as adding an axis purely to give a series its own coordinate space without exposing a
second, redundant reading surface. The Settings page already owns the labeled, authoritative view
of blend values over time (ADR-069); this overlay exists for **relative shape comparison**
against the portfolio's PLN/percent series, not as a second place to read exact blend numbers.

### 5. Blend line reuses the gradient-stroke technique verbatim

When rendered, the blend `<Line>` uses the same technique as `MacroBlendHistoryChart.tsx`: a
`buildGradientStops`-built `<linearGradient>` (one `<stop>` per joined blend value, colored via
`blendToColor`) in `<defs>`, with `stroke={\`url(#${gradientId})\`}`. This is copied into
`PortfolioValueLine.tsx` (or extracted to a small shared helper if the frontend agent judges that
cleaner — left to implementation judgment since it's a mechanical, low-risk extraction either
way), not reinvented — `blendToColor` itself (`frontend-web/src/lib/macroBlendColor.ts`) is
imported and reused unchanged from its existing location.

### 6. No backend, database, or contract changes

This is 100% frontend. `GET /market-data/macro-state/history`, `MacroStateHistoryPoint`,
`getMacroStateHistory`, and `queryKeys.macroState.history` all already exist (ADR-069) and are
consumed as-is. No new endpoint, no new DTO, no Flyway migration, no OpenAPI edit. Consistent with
ADR-072's precedent immediately before this one, none of the living `docs/architecture/` docs
document frontend chart-rendering internals — they cover the API contract, DB schema, and
rebalancing algorithm, none of which this feature touches — so no `docs/architecture/` update is
warranted either.

## Contract Summary (for the implementing agent)

- Modified: `frontend-web/src/components/charts/PortfolioValueLine.tsx`
  - Remove the built-in `<Legend/>` (currently line 133); add a `MacroBlendLegend`-style custom
    legend with six toggles per §1 (five existing series, all default `true`, plus
    `showBlend` default `false`).
  - Add `useQuery({ queryKey: queryKeys.macroState.history, queryFn: getMacroStateHistory, enabled: showBlend })`.
  - Add the carry-forward join (§3) producing a per-row `blendFactor` value, reusing the pattern
    from `MacroBlendHistoryChart.tsx`'s `buildChartRows`.
  - Add `<YAxis yAxisId="blend" domain={[0, 1]} hide />` and a gradient-stroked `<Line
    yAxisId="blend" dataKey="blendFactor" stroke={url(#...)} />`, conditionally rendered only
    when `showBlend` is true (and, naturally, only paints where a joined value exists).
  - Toggle state controls which of the six series render, mirroring `MacroBlendLegend`'s
    lifted-`useState` pattern — not recharts' `<Legend onClick>`.
- No changes anywhere else: `HistoriaDialog.tsx` needs no edit (it renders `PortfolioValueLine`
  as-is and inherits the new legend/toggle/fetch behavior automatically), `shared-ts` needs no
  edit, no backend/OpenAPI/Flyway edit, no `docs/architecture/` edit.

## Consequences

- **One shared component, two call sites, one behavior.** Because the toggle/fetch state lives
  inside `PortfolioValueLine` itself, `/history` and the `HistoriaDialog` full-screen view get
  the new legend and blend overlay for free, with no prop threading required from either caller.
- **A previously pure, prop-only chart component now has an internal, conditional data
  dependency.** `PortfolioValueLine` was fetch-free before this ADR; it now owns one gated
  `useQuery`. This is a narrow, precedented exception (mirrors what `MacroBlendHistoryChart`
  already does), not a general license for chart components to start self-fetching — future
  additions to this component should stay data-prop-driven unless they have the same
  toggle-gated, cache-shared shape this one does.
- **Blend's axis is intentionally unlabeled.** Anyone reading exact blend values off this chart
  is using the wrong tool — the Settings page (ADR-069/070/071/072) remains the sole labeled,
  authoritative source; this overlay is a relative-shape aid only, and should not be extended
  with visible ticks/legend value callouts without revisiting this trade-off.
- **Default-off blend keeps the common case cheap.** Most visits to `/history` or the dialog never
  fetch macro state at all; the network cost only appears once a user opts in, and is then shared
  via the existing query key with the Settings page if both are open in the same session.
- **No new join primitive was introduced.** The carry-forward join is a second call site for an
  algorithm that already existed for the BTC/NDX overlays (ADR-070) — if a third chart needs the
  same join in the future, that's a natural point to extract it into a shared utility rather than
  a third hand-copy.
