# ADR-070: Optional Price Overlays (BTC / Nasdaq 100) on the Blend-Factor History Chart

**Date:** 2026-08-04
**Status:** Accepted
**Extends:** ADR-069 (blend-factor history chart), ADR-044 (blend factor)

## Context

The Settings blend-factor history chart (ADR-069) now supports full-screen zoom (Brush) and a
hideable backtest segment. The user wants to optionally overlay two reference prices — Bitcoin
and the Nasdaq 100 — on the same chart, log-scaled, to visually compare market price action with
the macro de-risk `blendFactor` curve (0 = risk-on, 1 = risk-off). Both overlays are off by
default and toggle from the chart legend.

**Price source.** Yahoo Finance is already the app's primary market-data source and its v8 chart
API provides free, keyless daily history for both symbols (`BTC-USD`, `^NDX`) back before
2011-07. Nasdaq 100 is requested explicitly — `^NDX`, not the Composite `^IXIC`.

**The overlay must sit on the blend line's scale.** `blendFactor ∈ [0,1]`; raw BTC closes run
~1–100k+ USD and Nasdaq ~100–20k, so raw prices can't share the left axis. Log-scaling alone
still spans several decades — hence the mapping decision below.

## Decision

### 1. New read-only endpoint — `GET /market-data/benchmark-history?symbol=BTC-USD|^NDX`

- `BenchmarkHistoryService` calls `YahooFinanceClient.fetchOHLCByRange(symbol, "1d", p1, p2)`
  with an explicit `period1`/`period2` window from `2011-07-01` (the ADR-069 backtest window
  start) through today — **never** `fetchAllOHLC`'s `range=max` shorthand. Returns
  `BenchmarkHistoryPoint(symbol, date, close)` records.
  - *Amendment (2026-08-05):* the initial implementation used `fetchAllOHLC(symbol, "1mo")`
    (`range=max&interval=1mo`). That shorthand is the same one documented as broken elsewhere
    (`LinearRegressionService.fetchOHLC`, `TradeChartService.fetchOHLC`): for a long-history
    request Yahoo silently substitutes coarser, irregularly-spaced candles instead of true
    monthly ones. In practice this produced a gappy series — worse for `^NDX` than `BTC-USD`,
    since the degradation differs by ticker — that rendered as a visibly broken/dotted overlay
    line instead of the solid one `BTC-USD` mostly got. Switching to an explicit daily range
    avoids the degradation and gives the frontend enough resolution to carry the last close
    forward per row (see §2), so the overlay line is always solid regardless of ticker.
- **Symbol whitelist only:** `Set.of("BTC-USD", "^NDX")`; anything else is the existing
  `IllegalArgumentException → 400 VALIDATION_ERROR` path. This keeps the endpoint from becoming
  an arbitrary Yahoo history proxy.
  - *Amendment (2026-08-06):* whitelist gains a third symbol, **`GC=F`** (COMEX Gold futures
    continuous contract) — same free/keyless Yahoo v8 chart API as `BTC-USD`/`^NDX`, no new fetch
    mechanism. `Set.of("BTC-USD", "^NDX", "GC=F")`. Also, `fetchOHLCByRange`'s interval changes
    from daily (`"1d"`) to **weekly (`"1wk"`)** for all three symbols, reducing point count on a
    15-year chart (Raspberry Pi hosting, cost-free/lightweight hard constraint per CLAUDE.md)
    while staying far finer than the old broken monthly fetch (§1 2026-08-05 amendment above).
    Still uses the explicit `period1`/`period2` range path, not `range=max`, so the monthly
    degradation bug does not reoccur — verified empirically against the live Yahoo endpoint
    (evenly-spaced weekly candles, no gaps) before shipping. `EARLIEST_DATE`, the 12h cache TTL,
    and the `BenchmarkHistoryPoint` DTO shape are unchanged.
  - *Amendment (2026-08-06), Hang Seng added:* whitelist gains a fourth symbol, **`^HSI`** (Hang
    Seng Index) — the China/HK equity proxy, chosen over `MCHI` (Yahoo history only since 2011,
    barely predates this chart's own 2011-07-01 window, no historical value added), `000001.SS`
    (~3% null rows, real data gaps), and `FXI` (clean but 18 years shorter) for the longest clean
    history (1986-12-28 onward, zero nulls, uniform weekly spacing) — despite `FXI` already being
    used as a China proxy elsewhere in this repo's backtest scripts, for unrelated reasons.
    `Set.of("BTC-USD", "^NDX", "GC=F", "^HSI")`. Same weekly (`"1wk"`) interval as the other
    three.
- **Server-side cache:** in-memory `ConcurrentHashMap` per symbol, 12h TTL — unchanged by the
  daily-fetch amendment; the chart is a read-only comparison view, so the overlay should be
  cheap to open repeatedly (matching the 12h `staleTime` used client-side) even though the
  series itself is now ~15 years of daily points (a few thousand rows) instead of ~180 monthly
  ones.
- No DB changes; no Flyway migration. The series is not persisted — it is re-fetched from Yahoo
  when the cache expires.

### 2. Overlay mapping — actual prices on a dedicated right-hand `log10` axis

Each enabled series is plotted on its **own right-hand Y axis** (`yAxisId="price"`,
`orientation="right"`, recharts `scale="log"`), so the user reads the **actual price** —
BTC $…, Nasdaq ~$… — at a glance, while the log scale keeps the multi-decade range
(2011 BTC ~1 USD → 2026 ~100k USD, NDX ~200 → ~20k) on one screen:

- The left axis stays the blend `[0,1]` scale; the two axes are decoupled (log prices
  and blend values are not directly comparable numerically — the overlay is a shape
  comparison, and the axis makes the price explicit instead of a hidden normalization).
- The right axis `domain` is recomputed from the **currently visible window** (whole
  range, Brush-zoomed range in full-screen, or live-only range when the backtest
  segment is hidden), padded ±5% **multiplicatively** (`min / 1.05`, `max * 1.05`), so
  the overlay refits on zoom/hide without rescaling the blend line. *Amendment
  (2026-08-05):* the initial padding was linear (`± (max - min) * 0.05`), which on a
  log axis is dominated by whichever overlay has the widest raw-price range (BTC:
  ~$3 in 2011 to six figures today) and barely nudges the other (Nasdaq 100: low
  thousands to ~25k) — the smaller series hugged the top/bottom of the plot. A fixed
  multiplicative factor gives both series equal visual breathing room regardless of
  their absolute price range.
- Alignment to rows is an **as-of lookup**, not a calendar-month bucket: since the price
  series is now daily (see §1 amendment), each blend-history row (monthly backtest or
  daily live `macro_state.asOfDate`) is matched to the latest overlay close on or before
  its own timestamp — a two-pointer merge over the two ascending-sorted series
  (`buildChartRows` in `MacroBlendHistoryChart.tsx`). *Amendment (2026-08-05):* the
  original design bucketed by calendar month (`yyyy-MM`) to match monthly overlay data;
  once the source data is daily, the as-of lookup is both simpler and gapless — every
  row from the overlay's start date onward gets a value, which is what keeps the line
  solid instead of breaking into disconnected segments at any month the monthly bucket
  join happened to miss.
- Colors — BTC = restrained gold (matches the app's CRYPTO exposure color); Nasdaq =
  light blue near the Nasdaq logo shade. Lines are solid (no dash pattern), width 1.5.
  - *Amendment (2026-08-06):* Gold (`GC=F`) reuses `EXPOSURE_COLORS.METALS` from
    `frontend-web/src/lib/chartColors.ts` (`oklch(0.70 0.13 85)`, "muted warm gold-silver")
    rather than a new hue — the same color already used for Gold's asset-class exposure
    elsewhere in the app's portfolio charts, keeping the overlay visually consistent with the
    rest of the UI. Deliberately distinct from BTC's own "restrained gold" line color above —
    the two would otherwise collide in the legend/chart. Legend label: **"Złoto"** (Polish,
    matching existing usage, e.g. `MexcAssetRegistry.java`).
  - *Amendment (2026-08-06), Hang Seng added:* `^HSI` reuses `FALLBACK_PALETTE[5]` from
    `chartColors.ts` (`oklch(0.60 0.20 20)`, "deep red") — same reuse-a-house-token approach as
    Gold's `EXPOSURE_COLORS.METALS` above, and hue-distinct from the existing BTC (restrained
    gold), Nasdaq (light blue), and Gold (muted warm gold-silver) overlay colors. Legend label:
    **"Hang Seng"**.
- Tooltip shows the raw price in USD alongside the blend value; the right axis ticks
  use compact formatting (`42.1k`, `1M`).
- Both overlays share the same right axis when enabled together.

### 3. Frontend toggles are client state only

`showBtc` / `showNdx` are `useState` booleans lifted into the card, passed to both the card chart
and the full-screen dialog. Enabling a toggle fires its `useQuery` (react-query, 12h `staleTime`);
overlays only render once the series has loaded. No persisted preference (no `settings` row) —
consistent with the backtest toggle being session state.

## Contract Summary (for the implementing agents)

- Backend:
  - `BenchmarkHistoryService.getBenchmarkHistory(symbol)` + `supports(symbol)`; cache TTL 12h.
  - `BenchmarkHistoryPoint(String symbol, LocalDate date, double close)`.
  - `GET /api/market-data/benchmark-history?symbol=...` → `ListResponse<BenchmarkHistoryPoint>`;
    400 for non-whitelisted symbol.
- shared-ts: `benchmarkHistoryPointSchema`, `benchmarkHistoryListResponseSchema`,
  `getBenchmarkHistory({ symbol: "BTC-USD" | "^NDX" })`.
- frontend-web: legend toggles + overlay `<Line>`s bound to a right-hand log Y axis
  (`yAxisId="price"`, domain recomputed per visible window) in `MacroBlendHistoryChart.tsx`.

*Amendment (2026-08-06):* three-symbol, weekly-cadence target contract. Whitelist is
`Set.of("BTC-USD", "^NDX", "GC=F")`; `fetchOHLCByRange` interval is `"1wk"` (not `"1d"`) for all
three. `shared-ts`'s `getBenchmarkHistory({ symbol })` union gains `"GC=F"`. frontend-web adds a
third overlay `<Line>` + legend toggle ("Złoto") colored via `EXPOSURE_COLORS.METALS` (§2
amendment above). Endpoint response shape, cache TTL, and `EARLIEST_DATE` are unchanged.

*Amendment (2026-08-06), Hang Seng added:* four-symbol target contract. Whitelist is
`Set.of("BTC-USD", "^NDX", "GC=F", "^HSI")`, all weekly. `shared-ts`'s `getBenchmarkHistory({
symbol })` union gains `"^HSI"`. frontend-web adds a fourth overlay `<Line>` + legend toggle
("Hang Seng") colored via `FALLBACK_PALETTE[5]` (§2 amendment above). Endpoint response shape,
cache TTL, and `EARLIEST_DATE` are unchanged.

## Consequences

- Allowed comparing market price action with the macro de-risk blend on one chart, reading
  actual prices off the right log axis — including zoomed/live-only windows.
- Two extra Yahoo fetch surfaces (BTC daily, NDX daily), each bounded to one call per 12h per
  symbol, so negligible billing/rate impact; failures degrade to "no overlay shown" (empty list).
- A dedicated right axis means the price scale is explicit rather than a hidden normalization —
  the overlay is still a shape-comparison guide (two decoupled axes), not a formal
  correlation/pricing model.
- *Amendment (2026-08-06):* extended to three Yahoo fetch surfaces (BTC, NDX, Gold `GC=F`) at
  weekly instead of daily resolution — still one call per 12h per symbol (same cache bound, so
  still negligible billing/rate impact), plus materially fewer points per series, which is the
  direct Pi-hosting motivation (§1 amendment above).
- *Amendment (2026-08-06), Hang Seng added:* extended to four Yahoo fetch surfaces (BTC, NDX,
  Gold, Hang Seng `^HSI`), same weekly cadence and 12h-per-symbol cache bound as the other three
  — still negligible billing/rate impact.