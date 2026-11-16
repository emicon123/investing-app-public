# 10 — Refactoring Plan

> **Audience:** Backend-Go and Frontend-web agents.
> Actionable list of what's still open in the backend and frontend. Not an architecture
> description — this is a status/task tracker, re-verified against the actual codebase.

---

## Status (re-audited 2026-07-06)

A full line-by-line re-audit against the current code and `git log`/`git status` found that
**the overwhelming majority of this plan's items (backend Phases 1-4, all Database Quality
Issues, and most of the Frontend Refactoring Plan) are already implemented.** Several items
that were "Open" as of the 2026-06-04 audit were fixed as ordinary follow-on work in the
weeks since (regression-DCA removal, ADR-042 macro de-risk gate, the monorepo dead-code
sweep, and a frontend pass that is not separately documented anywhere else). A handful of
items are now moot because the feature they described (the ISM gate, `monthly_entries`,
`AsyncConfig`) was deleted outright by a later ADR.

This file previously carried ~500 lines of resolved-item write-ups (full code samples,
before/after diffs) for changes that are now just... how the code already looks. Per the
project's ADR-consolidation policy, that history has been pruned to a compact reference
table. **What remains below is the still-open list** — keep this section long-form and
actionable; the resolved table is intentionally terse.

> **Update 2026-09-19 (Phase 9, ADR-098):** the Java backend was deleted and replaced by Go. The three
> backend items that were open here were Java-specific and are closed by the rewrite (see "Currently
> Open — Backend"); the backend "resolved"/"moot" rows below describe the Java code as audited on
> 2026-07-06 and are kept as history only (restore anchor `2be75ba`).

**Tally (as of the 2026-07-06 audit):** ~55 checklist items examined (15 original issues, 8 new-issue categories with
~20 sub-items, 12 DB issues, 30 frontend items). **3 backend items and 3 frontend items were
open** (the 3 backend items are now closed by the Go rewrite; the 3 frontend items remain below). ~6 items are moot/superseded (feature deleted). Everything
else is confirmed resolved in the working tree.

---

## Currently Open — Backend

**None.** The three items that were open at the 2026-07-06 audit were all about Java classes that no
longer exist; the Go rewrite (ADR-094, completed by ADR-098 on 2026-09-19) closed them:

- **A3 — `PurchaseSessionService` mixing four responsibilities: closed.** There is no service class any
  more. `backend-go/internal/purchasesession` splits the domain by concern — `handler.go` (HTTP ↔ DTOs),
  `service.go` (use-case wiring), `lifecycle.go` (create/get/update/complete/delete), `planning.go`
  (purchase planning), `execution.go` — and `computeLimits`/`computeBreakdown`/`requireRate` are
  package-level functions in `lifecycle.go`, not methods of a god class. Optional cosmetic follow-up
  only: move those three helpers to their own file if `lifecycle.go` (~580 lines) keeps growing.
- **P4-2 — `UpdateSessionItemsRequest` mutable `@Data` class: moot.** Request bodies are now
  oapi-codegen-generated plain structs in `backend-go/openapi/server.gen.go`, regenerated from
  `docs/openapi.yaml`.
- **DB11 — `OffsetDateTime` vs `Instant` inconsistency: moot.** It was a JPA-entity nit; every column is
  `TIMESTAMPTZ` and sqlc maps them uniformly (`pgtype.Timestamptz`).

Open backend work is now tracked as tasks, not here: BLS direct actuals (ADR-093/task 147), TradingEconomics
PPI enrichment stub, and porting the ADR-095/096 bot designs to Go before
`db/unapplied/V72`/`V73` are un-parked.

---

## Currently Open — Frontend (frontend-web)

> Note on structure: the frontend has moved from a `pages/` directory to a `routes/`
> directory since this plan was written (confirmed independently by the monorepo-layout
> doc). `pages/` now contains only `LoginPage.tsx`; everything else lives under
> `frontend-web/src/routes/<name>/index.tsx` with route-local `_components/` and
> `schemas.ts` files. All item locations below use the current `routes/` paths.

### F26 (open): currency enum arrays still duplicated

**Files:** `frontend-web/src/routes/deposits/schemas.ts`, `frontend-web/src/routes/crypto/schemas.ts`

Both independently declare `z.enum(["PLN", "EUR", "USD", "CHF", "GBP"])`. The duplication
survived the schema-file consolidation (F13, resolved — inline schemas were moved out of
route components into these two `schemas.ts` files) because it moved the duplicate rather
than removing it. Extract a shared `CURRENCY_VALUES = ["PLN", "EUR", "USD", "CHF", "GBP"] as const`
(e.g. in `shared-ts` next to the existing `Currency` zod schema, or in
`frontend-web/src/lib/formConstants.ts`, which already exists and holds similar constants)
and reference it from both schema files.

### F7 residual (open, trivial): one `formatPln` duplicate remains

**File:** `frontend-web/src/routes/mexc-bot/index.tsx` (lines ~66-71)

The original finding ("`formatPln` defined 8 times locally") is resolved everywhere else —
11 files now import `formatPln` from `@investing-app/shared` (shared-ts). `mexc-bot/index.tsx`
is the sole remaining file with a local `plnFormatter`/`formatPln` redefinition. Replace with
`import { formatPln } from "@investing-app/shared"` and delete the local function.

**Correction to the historical record:** the old Phase-1 plan (P1-4) instructed removing
`formatPln` from the shared package's public API as dead code. That instruction is now
**wrong and must not be followed** — `formatPln` is the single most widely-imported utility
from `@investing-app/shared` in the frontend today. Only `usePositionsStore` and
`useSettingsStore` (the other two exports P1-4 named) were actually dead, and both are
  already deleted from `shared-ts` (confirmed: no matches anywhere in `shared-ts/src` or
  `frontend-web/src`).

### F30 residual (open, low priority): `BuyLegRow` still defined inline

**File:** `frontend-web/src/components/RebalancingTransferDialog.tsx` (523 lines)

The harmful part of the original finding is fixed: `BuyLegRow` now has a proper
`BuyLegRowProps` type (no `any`) and reads form state via `useFormContext<TransferFormValues>()`
instead of receiving `register`/`setValue`/`watch`/`errors` as untyped props — this also
resolves F1 and F12, which described the same component. What's left is purely organizational:
`BuyLegRow` (~100 lines) is still defined in the same file as the `RebalancingTransferDialog`
parent component rather than extracted to its own file. Low priority — the component is
correct and well-typed as-is.

---

## Superseded / Moot (feature deleted — no action needed)

These items described real problems at the time but the code they refer to no longer exists.
Listed so a future reader doesn't waste time "fixing" something that was deleted out from
under the bug.

- **ISM PMI gate ticker mismatch** (Original Issue #8, New Issue B2, Phase 3 item P3-1).
  `IsmPmiGate` was keyed by short ticker names (`"BTC"`) while `positions.ticker` stores
  `"BTC-USD"`-style values, so overrides never applied. Rather than fix the lookup, the
  gate was **replaced outright**: backtests showed ISM PMI has no predictive value for a
  crypto-heavy book, so `IsmPmiGate` and the `ism_pmi_value`/`ism_pmi_updated_at` settings
  were deleted and replaced by `MacroDeriskGate` — a liquidity-driven (Fed net liquidity +
  Global M2 deceleration) de-risk override, keyed by full `positions.ticker` values (no
  short-key mismatch possible) and sum-conserving (freed crypto % routes to gold/bonds
  instead of vanishing). `IsmPmiGate.java` and `IsmPmiGateTest.java` are deleted from the
  working tree this session; `MacroDeriskGate.java` is the live replacement.

- **`AsyncConfig.regressionWeightExecutor`** (Original Issue #14, New Issue J4, Phase 1
  item P1-5). The regression-band DCA weighting (`regression_weight`, `RegressionWeightService`)
  was removed because backtesting found it added no return uplift while increasing
  concentration and drawdown — MAIN rebalancing is now pure-proportional buy-only DCA,
  identical in shape to RETIREMENT. Its removal left `regressionWeightExecutor` as the sole
  bean in `AsyncConfig` with no consumers, so the entire class was deleted. `@EnableAsync`
  remains required for `CacheWarmupListener`, which still runs on Spring's default
  virtual-thread `@Async` executor. Confirmed: no `AsyncConfig.java` anywhere in
  `backend/src/main`.

- **`updateMonthlyEntry()` null-guard gap** (New Issue B4) and **year/month path-var bounds**
  (New Issue V3). The entire `monthly_entries` feature (table, entity, controller, service)
  was dropped in migration `V26__drop_monthly_entries.sql`. No `MonthlyEntry*` class exists
  anywhere in `backend/src/main` or `backend/src/test`. Both findings are moot.

---

## Resolved (for reference)

Everything below was checked against the current code (not just trusted from the prior
audit's "Resolved" label) and confirmed fixed. Kept as a compact index — full before/after
detail has been pruned since 07-adrs.md (where the original decisions were recorded) is
being retired; the "what/why" is summarized inline instead of pointed at an ADR number.

### Backend — concurrency & transactions

> Backend tables in this section (through "Database quality issues") describe the Java implementation as
> audited on 2026-07-06 — history only; the code was deleted in Phase 9 (ADR-098). The Go port re-implemented
> the behaviors (e.g. price refresh with goroutines instead of virtual threads, no Spring proxy caveats).

| Item | What was wrong | How it's fixed now |
|---|---|---|
| Original #1, Phase P2-1 | `@Async` on a `@Scheduled` method is a silent no-op (Spring's scheduler calls the target directly, bypassing the AOP proxy) | `PriceCacheService.scheduledRefresh()` carries no `@Async`; a code comment explains why |
| Original #2, #3, T2, Phase P2-2/P2-4 | `upsertPriceCache()`/`getRateToPln()` held `@Transactional` across blocking HTTP calls (2-5s DB connection hold on the RPi); FX `forceRefreshAll()` fetched 4 currencies sequentially | Both services now split into non-transactional HTTP fetch + `TransactionTemplate`-scoped read/write; FX refresh runs all 4 currencies in parallel via `Executors.newVirtualThreadPerTaskExecutor()` |
| Original #5, Phase P2-3 | `PriceCacheService` recreated a fixed 8-thread pool every 15-minute refresh cycle | Replaced with `newVirtualThreadPerTaskExecutor()`, no pool sizing needed |
| New Issue T1 | `HistoriaScheduler.snapshotPortfolio()` was `@Transactional` but invoked via `CompletableFuture.runAsync()` from the daily scheduler — proxy bypass, same as #1 | Method carries no `@Transactional`; only the DB-write step is wrapped in `TransactionTemplate.execute()`, with a Javadoc explaining why |

### Backend — architecture / SRP / DRY

| Item | What was wrong | How it's fixed now |
|---|---|---|
| New Issue A1 | `HistoriaController` injected `HistoriaRepository`/`HistoriaMapper` directly, skipping the service layer | Controller now injects only `HistoriaService` |
| New Issue A2 | `MarketDataController` injected `PriceCacheRepository`/`FxRateCacheRepository` directly | Controller now injects only `PriceCacheService`, `FxRateCacheService`, `MacroDataService` |
| New Issue D1 | `applyDelta()` duplicated verbatim in `PurchaseSessionService` and `RebalancingTransferService` | Extracted to `PositionDeltaApplicator` |
| New Issue D2 | FX rate snapshot setup (6 lines) copy-pasted 3× | Consolidated into one `getLatestFxRates()` helper |
| New Issue D3 | `computeLimits()` took `ikeLimit`/`ikzeLimit` as params instead of looking them up | `computeLimits(int year)` now calls `settingService.getIkeAnnualLimitPln()`/`getIkzeAnnualLimitPln()` itself |
| New Issue D4 | `sumAmountByAccountTypeAndYear()` called twice per account type in `createSession()` | `computeLimits()` calls it once per account type, called once per request |
| New Issue B1 | `DepositService` hardcoded IKE/IKZE annual caps, ignoring the settings table | Now reads `settingService.getIkeAnnualLimitPln()`/`getIkzeAnnualLimitPln()` |
| New Issue B3, Original #4 | `listPositions(null, pageable)` only batch-fetched companions for the first portfolio present on the page | When `portfolio == null`, `PositionRepository.findAllByCompanionParentTickerIn(tickers)` fetches companions across *all* portfolios in one query; the portfolio-scoped `...AndPortfolio` variant is used only when a single portfolio is requested |

### Backend — REST / validation

| Item | What was wrong | How it's fixed now |
|---|---|---|
| New Issue V1 | `GET /market-data/prices?tickers=` accepted arbitrary strings passed straight to Yahoo Finance URLs | Each ticker validated against `[A-Z0-9.\-]{1,50}` before use, `IllegalArgumentException` on mismatch |
| New Issue V2 | `POST /cash-positions` nested list items lacked `@Valid` | `CreateCashPositionRequest.positions` is `@NotNull @NotEmpty @Valid List<CashPositionItemRequest>` |
| New Issue R1, R2, R3 | `GET .../prices?forceRefresh=true` and `GET .../fx-rates?forceRefresh=true` side-effected from a GET; no POST refresh endpoint existed for FX | Both GETs are now side-effect-free (doc comment: "No side effects — never triggers a cache refresh"); `POST /market-data/prices/refresh` and `POST /market-data/fx-rates/refresh` both exist |
| New Issue Y1 | `YahooFinanceClient.fetchPrices(List<String>)` (sequential, unused) | Method no longer exists — only the single-ticker `fetchPrice()` remains |
| New Issue Y2 | `PurchaseCurrencyResolver` was a bean wrapping a 1-line getter | Class no longer exists anywhere in `backend/src/main` |

### Backend — Java modernization & structural

| Item | What was wrong | How it's fixed now |
|---|---|---|
| Phase P1-1 | `ResponseFactory` duplicated `GlobalExceptionHandler.buildMeta()` | `ResponseFactory` deleted; `Meta.create()` static factory exists on `dto/common/Meta.java` and is used everywhere |
| Phase P1-2 | 12 near-identical list-response records | `dto/common/PagedResponse.java` and `dto/common/ListResponse.java` exist and are used by all list endpoints |
| Phase P1-3 | `computeUnitsTotal()`/`sumAllUnits()` duplicated in `PositionService` and `ValueComputationService` | Confirmed consolidated (denormalized unit columns themselves are an intentional design choice, not a bug — five account/currency combinations are fixed by Polish financial regulation, and normalizing would add joins to every value computation at ~30-position scale) |
| Phase P3-2 | `RebalancingService`/`PurchaseSessionService` bypassed `SettingService`, calling `SettingRepository` directly, with IKE/IKZE limit lookups repeated 3× | `SettingService` now exposes typed accessors (`getIkeAnnualLimitPln()`, `getIkzeAnnualLimitPln()`, `getRetirementMonthlyContributionPln()`, `isMacroDeriskEnabled()`) |
| Phase P4-1 | No entity carried `@Table(indexes = ...)` | `Position`, `Historia`, `AssetPurchase`, `Deposit`, `Inflation`, `TaxReturn`, `CashPosition`, and others now declare indexes matching the surviving (post-V19) migration index names |
| Phase P4-3 | `CacheWarmupListener`'s FX-before-prices ordering was undocumented | Inline comment now explains `PriceCacheService.convertToPln()` depends on a warm FX cache |
| New Issue J1 | 9+ `CreateXxx`/`UpdateXxx` request DTOs used manual getters/setters | All converted to records except `UpdateSessionItemsRequest` (still open, see above) |
| New Issue J2 | `AccountLimitsDto` used `@Data` with setters | Now a record |
| New Issue J3 | Setting key strings scattered as magic literals | `SettingKeys` constants class exists (`RETIREMENT_MONTHLY_CONTRIBUTION`, `MACRO_DERISK_ENABLED`, `IKE_ANNUAL_LIMIT`, `IKZE_ANNUAL_LIMIT`) |
| Phase P4-2 | `BuyListItemDto`, `DonutSliceDto`, `ExchangeSummaryItemDto` used Lombok instead of records | All three are now records |

### Database quality issues (DB1-DB12)

All resolved except DB11 (acknowledged above) and DB12 (a note about `04-db-schema.md`'s
header, a different file — already marked "fixed in place" by whichever pass touched that
doc).

| Item | Issue | Resolved by |
|---|---|---|
| DB1 | 6 duplicate indexes from V16+V17 on top of V1+V9 originals | `V19__drop_duplicate_indexes.sql` drops all 6; entity `@Table(indexes)` now reference only the surviving original index names |
| DB2 | Missing `UNIQUE(session_id, portfolio, ticker, account_type)` on `purchase_session_items` | Added in `V18__add_missing_constraints.sql` |
| DB3 | `fx_rate_cache.source` had no CHECK constraint | Added in V18 (`CHECK (source IN ('YAHOO_FINANCE', 'NBP_API'))`) |
| DB4 | `crypto_trades.total_amount` missing `CHECK (> 0)` | Added in V18 |
| DB5, DB6 | `purchase_session_items`/`purchase_sessions` had no `updated_at` | Added in `V20__add_audit_timestamps.sql`, with triggers |
| DB7 | `CashPosition.recordedAt` had no `@CreationTimestamp`, risking a cryptic NOT NULL failure | Resolved differently than proposed: `recordedAt` is actually a client-supplied, `@NotNull`-validated field on `CreateCashPositionRequest` (the snapshot timestamp is meaningful to the caller, e.g. backdating), so a missing value now fails with a clean 400 validation error at the DTO layer rather than a raw SQL exception — `@CreationTimestamp` would have been the wrong fix |
| DB8 | `Deposit.comment`, `CryptoTrade.notes`, `Setting.value`, `PurchaseSession.notes` mapped without `columnDefinition = "TEXT"` | All four now specify it |
| DB9 | `MonthlyEntry`/`InflationEntry`/`TaxReturn`/`CashPosition` had no `@Table(indexes)` | `MonthlyEntry` no longer exists (V26); `Inflation`, `TaxReturn`, `CashPosition` all now declare indexes |
| DB10 | Entities referenced V16/V17 index names slated for deletion | Entity index names now match the post-V19 survivors (verified `Position`, `Historia`, `AssetPurchase`, `Deposit`, `PriceCache`, `FxRateCache`) |

---

## Frontend — resolved (for reference)

| Item | Issue | Resolved by |
|---|---|---|
| F1, F12 | `BuyLegRow` took `register`/`setValue`/`watch`/`errors` as `any` props | Typed `BuyLegRowProps`; reads form state via `useFormContext<TransferFormValues>()` |
| F2 | `<>` Fragment missing `key` in `PositionsTable.map()` | `PositionsTable` was replaced by the common `GroupedPositionsTable` (task 170), which keys groups by exposure and asset rows with `<Fragment key={row.id}>` |
| F3 | Compiled `.js` committed alongside `.tsx`; no `noEmit` | No stray `.js` found; `tsconfig.json` has `"noEmit": true` |
| F4 | No `staleTime` on 8 of 10 page queries | `staleTime` now appears in 19 files across the query layer |
| F5 | No central `queryKeys.ts`; `RetirementPage` used a raw inconsistent key | `frontend-web/src/lib/queryKeys.ts` exists with a full factory (positions, history, deposits, binanceTransfers, xirr, cryptoTrades, purchaseSessions, cashPositions, settings, fxRates, macroState, linearRegression, mexcBot, news); `RetirementPage` now uses `queryKeys.positions.list("RETIREMENT")` consistently |
| F6 | `AppLayout` called raw `apiClient.post()` for price refresh, bypassing TanStack Query | Now uses `useQuery` |
| F7 | `formatPln` defined 8× locally; shared export unused | 11 files now import from `@investing-app/shared`; 1 residual duplicate remains (`mexc-bot/index.tsx` — see Open list above) |
| F8 | `aria-expanded={false}` hardcoded on mobile menu button | Now `aria-expanded={menuOpen}` |
| F9 | `HistoriaDialog` duplicated verbatim in `PortfolioPage` and `RetirementPage` | Single shared `components/charts/HistoriaDialog.tsx`; since task 170 both routes render one container, `components/portfolio/MainPortfolioScreen.tsx`, which imports it |
| F10 | `PurchasesPage` was 703 lines with 6 inlined sub-components | Split into `routes/purchases/index.tsx` (43 lines) + `schemas.ts` + `_components/{ActiveSessionView,NoSessionView,PortfolioItemsTable,HistorySection}.tsx` |
| F11 | `CryptoPage` was 481 lines with inline schema/form/table | Split into `routes/crypto/index.tsx` (33 lines) + `schemas.ts` + `_components/{TradeEntryForm,TradesHistoryTable,BinanceTransfersTable,MexcBotDepositsTable}.tsx` + `tax.tsx` |
| F13 | Zod schemas defined inline in route files | Moved to co-located `schemas.ts` in `deposits/`, `crypto/`, `purchases/`; no remaining inline `z.object()` found in route index files |
| F14 | `CryptoPage` form errors went to toast only, not rendered in DOM | `TradeEntryForm.tsx` now renders `<p role="alert">` for field errors alongside the toast |
| F15 | No `React.lazy`/`Suspense` on routes | `App.tsx` lazy-imports every route (`PortfolioPage`, `DepositsPage`, `RetirementPage`, `HistoryPage`, `CryptoPage`, `CryptoTaxPage`, `SettingsPage`, `PurchasesPage`, `RegressionPage`, `MexcBotPage`, `NewsPage`) |
| F16 | `MonthlyEntriesTable.buildColumns` not memoized | Moot — the whole monthly-entries feature is deleted. The portfolio table has no column builder since task 170; its model (`buildPositionsModel`, filtering) is `useMemo`-wrapped in `usePositionsView` |
| F17 | `useEffect` missing deps in `RebalancingTransferDialog` | All three `useEffect` hooks in the current file have complete dependency arrays |
| F18 | Unsafe `as { response?: { status } }` casting for 404 detection | `NoSessionView.tsx` now uses `createMutation.error instanceof Error` |
| F19 | No error boundaries | `components/ErrorBoundary.tsx` exists, used in `AppLayout` |
| F20 | Sortable headers lacked `aria-sort` | Moot for the portfolio page: since task 170 its grouped table has no sortable columns (rows are ordered by value inside each exposure group, design decision D3 in `diagrams/170-portfolio-redesign.md` §14) |
| F21 | Chart data transforms not memoized | `PortfolioValueLine` and `AllocationTreemap` both use `useMemo` for derived chart data |
| F22 | `AllocationDonut` defined but never imported | Component no longer exists in the codebase |
| F23 | `usePositionsStore`/`useSettingsStore` dead Zustand code | Both fully deleted — no matches anywhere in `shared-ts` or `frontend-web` |
| F24 | `shared-ts/utils/rebalancing.ts` and `utils/finance.ts` never imported | Both files no longer exist (`shared-ts/src/utils/` now only has `currency.ts` + `index.ts`) |
| F25 | `currentYear` computed in 4 separate files | Centralized as `CURRENT_YEAR` in `frontend-web/src/lib/dateConstants.ts` |
| F27 | Loading skeletons lacked `role="status"`/`aria-label` | Present across the portfolio page's `KpiBento` and `PageShell` (task 170), `HistoriaDialog`, `BinanceTransfersTable`, `history/index.tsx`, and others |
| F28 | No route constants file; paths duplicated in `App.tsx`/`AppLayout.tsx` | `frontend-web/src/lib/routes.ts` exports a `ROUTES` const covering every route |
| F29 | `createSessionSchema.cashOverridePln` used a messy `.or(z.literal(""))` union | Rewritten with `z.preprocess((val) => (val === "" || val == null ? undefined : Number(val)), z.number().min(0).optional())` |

**Not independently re-verified** (spot-check budget spent on the above; flagged for a future
pass rather than assumed correct or incorrect): the exact staleTime *value* chosen per query
(only presence was checked, not whether 15 min/60 min/etc. values are sensible per endpoint).

---

## What Is NOT in Scope

Evaluated and deliberately excluded (re-checked against current code):

- **Normalized units table** — `positions` intentionally keeps five denormalized unit columns
  (`units_ike`, `units_ikze`, `units_regular_eur/usd/pln`) instead of a normalized holdings
  table. The five account-type/currency combinations are fixed by Polish financial regulation,
  and normalizing would force a join into every value computation for ~30 positions total —
  not worth it at this scale.
- **Sealed class hierarchy for rebalancing strategies** — the two strategies are named
  `MainRebalancingStrategy` and `RetirementRebalancingStrategy` (renamed from
  `Inwestycje`/`Emerytura` when the portfolio enum was renamed) and remain separate classes
  with no shared interface. A sealed `RebalancingStrategy` hierarchy would add abstraction
  without simplification given there are exactly two strategies; the portfolio-based dispatch
  in `RebalancingService.calculate()` is the correct location for routing.
- **`GlobalExceptionHandler` coverage** — now covers 11 exception types (grew since the last
  audit): `MethodArgumentNotValidException`, `ConstraintViolationException`,
  `NoSuchElementException`, `DataIntegrityViolationException`, `IllegalArgumentException`,
  `PriceUnavailableException`, `PlanSessionAlreadyExistsException`,
  `SessionAlreadyDoneException`, `RebalancingInvariantException`, `ResponseStatusException`,
  and `Exception` (catch-all). Coverage is complete; no gaps found.
- **`CacheWarmupListener` correctness** — the `@Async` + `@EventListener` combination works
  correctly (unlike `@Async` + `@Scheduled`, which is a documented no-op elsewhere in this
  file). No fix needed.
- **DTO record conversion for `PositionDto`** — already a record; `withComputed()` in
  `PositionService` creates a new record by spreading fields, which is idiomatic when
  computed fields are added after construction.
- **`PurchaseSessionDetail` staying mutable** — unlike `UpdateSessionItemsRequest` (now fixed,
  see Open list), this DTO genuinely needs post-construction mutation:
  `PurchaseSessionService` sets `currencyBreakdown` and `retirementLimitsRemaining`/
  `mainLimitsRemaining` after the base object is built. A builder or companion constructor
  would work but isn't worth the churn for a single internal DTO.
- **`Thread.sleep()` rate-limiting** — moot. The regression-band DCA weighting
  (`RegressionWeightService`, which used `Thread.sleep()` to rate-limit Yahoo Finance calls
  during its nightly trend refresh) was removed entirely: backtests found the "weight assets
  further below their own long-run price trend" signal carried ~zero predictive value while
  increasing portfolio concentration and drawdown. MAIN rebalancing is now pure-proportional
  buy-only DCA, same shape as RETIREMENT. No `Thread.sleep()` remains in the codebase for this
  reason.
