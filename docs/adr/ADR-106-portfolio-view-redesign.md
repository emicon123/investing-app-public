# ADR-106: One Portfolio View for All Three Apps (Grouped Table, KPI Bento, Slate Tokens)

**Date:** 2026-10-24
**Status:** Accepted. Implemented under task 170 (commits `0086f8a`..`9678e8c`, 2026-10-24).
**Scope:** frontend only. The pages are main `/portfolio` and `/retirement`, portfolio-b `/portfolio`,
and portfolio-c `/portfolio` (both books). The palette and font apply app-wide. No backend or contract
change.
**Related:** task 170, the owner-approved design canvas (2026-10-24), ADR-105 (the
`packages/common` names used here), ADR-045 and ADR-048 (manual-sell-only, sell thresholds,
`docs/architecture/05-rebalancing-algorithm.md` §7.5), ADR-053 (zero-target sentinel), ADR-086 (root
design-system convergence), task 136 (shared transfer dialog).
**Design detail:** `docs/architecture/diagrams/170-portfolio-redesign.md`. That document is
normative; this ADR records the decisions and the alternatives.

## Context

The portfolio page in all three apps is a stretched spreadsheet: about 34 equal-weight rows, nine
columns spread across an ultrawide monitor, and loud status pills. The "+70%" in the status pill
was being read as a return. The code for it is split. main has its own 751-line `PositionsTable`
and its own summary bar. portfolio-b and portfolio-c share `packages/common/ui/src/PositionsTable.tsx`, a
`PortfolioSummaryBar` and the `PortfolioPage` container. The per-app icon maps are duplicated
(main's `STOCK_ICONS` / `ETF_ICONS` against `portfolio-b-portfolio-c-asset-icons.ts`), and three crypto icons
404 on CoinCap (FLIP, LOFI, and the XPL override).

The owner approved a new design and set two rules. The UI must be the same in all three apps, each
app keeping its accent colour. Any code that would be edited the same way in 2 or 3 apps goes to
`packages/common` (AGENTS.md).

## Decision

1. **One presentational `PortfolioView` in `packages/common/ui/src/portfolio/`.** It composes a
   header, a 4-card KPI bento with a sparkline, an allocation ribbon, filter chips with search, and
   a positions table grouped by `exposure` with merged two-line cells, centred at a 1400 px
   maximum. Each app keeps only a container:
   - main gets a new `MainPortfolioScreen`, shared by its two routes, which were near-identical.
   - portfolio-b and portfolio-c keep the existing common `PortfolioPage`, which now renders `PortfolioView`.
   - App-specific behaviour enters only through slots: the bot badge, `MacroRegimePanel`, the
     subasset account-split cell, the sell and edit callbacks, `updateAsset`, and the
     collapsed-by-default exposures.
2. **Every figure comes from existing endpoints.** The data comes from `GET /positions`,
   `GET /deposits/xirr`, `GET /history?from=` and the cache-only `GET /market-data/prices`. The
   effective target is `computed.effectiveTargetAllocationPct ?? targetAllocationPct`. It is the
   macro-blended target in main MAIN and the raw target everywhere else, because the siblings have no
   such field and no macro gate. The KPI queries come from a hook factory,
   `createUsePortfolioKpis`, following the `createUseRefreshPrices` precedent. **No backend gaps.**
   main needs a frontend `getPrices` client, and a `prices` query key is added.
3. **The TargetMeter and status rules are the owner's** (task 170 rules 6 and 7, copied verbatim
   into the design doc §5.1). They are implemented as pure functions with unit tests on the
   mockup's real rows.
   - The sell thresholds are unchanged.
   - The status column never shows a percentage. It shows the SELL amount, or "całość, N zł" for a
     zero target, or the deviation in p.p.
   - The SELL amount and the transfer dialog's pre-fill call one shared function,
     `computeTrimExcessPln`, so they cannot drift apart (ADR-048 §7.5: trim back to 1.0× the
     effective target).
   - There is still no "Rebalansuj" button and nothing auto-sells (ADR-045).
4. **Slate palette app-wide through `design-tokens.css`.**
   - Ground #0B0F17, cards #121926, hairlines at 8 % white, text #E7ECF3 / #A3AFC2 / #8592A6.
   - Semantic colours are the same in every app: gain #10B981 (text #34D399), loss/SELL #F43F5E
     (text #FB7185), OVER #E8B04B, UNDER #6AA8F0, OK #7D8BA1.
   - Per-app hue survives only in the accent (`--primary`, `--ring`, `--accent`), via the existing
     `--theme-hue-accent`.
   - Every text token is ≥ 4.5:1 on every surface (table in the design doc §8.3).
   - **IBM Plex Sans** is self-hosted through `@fontsource/ibm-plex-sans` (OFL, no Google request).
     `tabular-nums` applies app-wide.
   - The nebula background is turned off in the app shell.
5. **Group colours.** The owner's five are kept as given. The Architect adds COMMODITIES = bronze
   #956725 and CURRENCY = magenta #9B46B4; this pair passes the palette validator's CVD and
   normal-vision checks against the siblings' set. They live in one TypeScript table,
   `EXPOSURE_GROUPS`, because nivo's SVG attributes cannot read CSS variables. The treemap switches
   to the same colours, so a group has one colour everywhere.
6. **One icon map** (`ASSET_ICONS` in `packages/common/ts`), keyed by ticker, covers every live
   primary in all three databases.
   - The three 404 coins use CoinGecko image URLs.
   - Gold uses bundled bars, not the PAXG token logo. Silver gets the same shape in silver.
   - Flags cover China, India, US, PL and the new CH. An atom covers the quantum ETFs. The Nasdaq
     favicon covers every Nasdaq-100 tracker. Issuer favicons cover portfolio-b's thematic ETFs.
   - Nothing is guessed from a name. An unresolved or broken icon becomes a monogram instead of
     disappearing.
7. **Sticky headers work by removing the two `overflow` scroll containers in `AppLayout`.** The
   column becomes `overflow-x-clip` and `<main>` loses `overflow-y-auto`. The table section uses
   `overflow: clip`. The positions table scrolls horizontally only below about 1160 px.

## Alternatives considered

- **Restyle the two existing tables in place.** Rejected. It would mean editing main's 751-line
  table and the common table in the same way, twice: exactly the duplication the AGENTS.md rule
  forbids. They also cannot become a grouped two-line layout without a rewrite.
- **Build the grouped table on TanStack Table v9 (`groupedRowModel`).** Rejected. The design has no
  sorting, column visibility or resizing. Grouping is a reduce, filtering is two predicates, and the
  rows are CSS-grid `div`s with `role="table"`. A feature registry would only add weight. Column
  sorting and "Rozwiń/Zwiń wszystkie" are dropped deliberately (design doc §14 D3).
- **Move main's container onto common `PortfolioPage` now.** Deferred. Main's edit flows differ
  (`useAssetActions`: richer delete notes, a per-row subasset pencil, the transfer dialog's `open`
  prop), and the redesign does not edit them. A future dedupe task can merge them. This one swaps
  only the render body.
- **Group colours as CSS custom properties.** Rejected. The treemap writes SVG presentation
  attributes, where `var()` does not resolve. One TypeScript table is the single source instead.
- **Validator-compliant replacements for the owner's Akcje/Obligacje pair** (ΔE 9.3 normal vision).
  Rejected. Task rule 9 fixes those hexes. Identity never depends on colour alone (every swatch is
  labelled), and the ribbon uses 2 px gaps and `title`s.
- **Keep the bot widget card, or drop the bot from the page.** Rejected for a compact badge that
  links to `/bot`, with the next events and the plan summary in a tooltip (design doc §14 D1).

## Consequences

- **Removed or replaced:**
  - common: `PositionsTable.tsx`, `PortfolioSummaryBar.tsx`, `SubassetRows.tsx`,
    `EntryPriceCell.tsx`, `ProfitPctCell.tsx`, `PositionsTableStatus.tsx`;
    `deviationStatus` moves into `portfolio/model/rebalanceStatus.ts` and its `ON_TARGET` value
    becomes `OK`.
  - main: `components/tables/PositionsTable.tsx`, `components/PortfolioSummaryBar.tsx`,
    `components/portfolio/BotPortfolioWidget.tsx`, `PrismValue.tsx`.
  - siblings: `components/tables/PositionsTable.tsx`, `components/PortfolioSummaryBar.tsx`.
  - `portfolio-b-portfolio-c-asset-icons.ts` and `createAssetIconResolver`; `getAssetIconUrl` survives as a
    compatibility wrapper.
- **Visible app-wide side effects**, owner-approved through rule 9 or recorded in design doc §14:
  slate ground and cards, soft accent buttons, the Plex Sans font, no nebula background, and a top
  bar that now actually stays sticky (D12).
- **Behaviour kept exactly as before:**
  - Sell thresholds, the manual-sell-only rule, and when SELL is clickable (main: edit mode only;
    siblings: always).
  - Inline target edit, subasset expansion with account chips, set-primary with undo toast, the
    treemap, history and create dialogs, `MacroRegimePanel`, portfolio-c's book switcher, and 60 s
    polling.
- **Verification is by tests and built-bundle greps.** Headless Chromium is broken on this Pi. The
  owner does the visual check after the orchestrator deploys.
- **Documentation debt: cleared (2026-10-24).** The current state lives in
  `docs/architecture/12-portfolio-bpps-and-shared-packages.md` §5.1 (components, grouping, icons,
  tokens, font, shell) and `05-rebalancing-algorithm.md` §7.5 (`computeTrimExcessPln` as the one
  trim formula). Where the build differs from the design is recorded in the design doc's §15 "As
  built". This ADR keeps only the reasoning.
