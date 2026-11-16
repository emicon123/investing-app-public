# ADR-057: Trade-History Chart — `trade_type` Classification, Backfill, and FX-Aware Markers

**Date:** 2026-07-24
**Status:** Accepted

## Context

The user wants a per-asset chart (mirroring the existing log-linear regression chart at
`GET /market-data/linear-regression` / `frontend-web/src/routes/regression/index.tsx`) that
overlays every buy/sell of that asset as a dot on the price series — green for a buy, red for
a sell, dot size scaled to trade size (Michael Saylor's BTC purchase-history chart is the
explicit reference), with a distinct shade/color for a rebalancing-trim reinvestment buy vs. a
regular DCA buy, and hover detail per dot (amount spent, exact date, etc.).

**This is a read-only visualization feature, not a new write path.** Every buy and sell already
lands in `asset_purchases` (`backend/src/main/java/pl/investing/entity/AssetPurchase.java`,
table defined in `V9__purchase_transactions.sql`, `session_id` made nullable in
`V15__make_asset_purchase_session_nullable.sql`): `id, session_id (nullable FK), portfolio,
ticker, account_type, units_delta (positive=buy, negative=sell), price_pln (PLN per unit,
CHECK > 0), total_pln, purchased_at`. Three write paths feed it today:

1. **`PurchaseSessionService.completeSession()`** — regular monthly DCA buys, and IKE/IKZE
   migration-triple buy/sell legs (flagged via `PurchaseSessionItem.isMigrationSell()` /
   `isMigrationBuy()` — a flag on the *session item*, not copied onto the `AssetPurchase` row
   itself). `session_id` is set to the completed session.
2. **`RebalancingTransferService.executeTransfer()`** — the manual "Sprzedaj" (trim-rule) flow
   documented in `05-rebalancing-algorithm.md` §7.4–7.5 (ADR-045/048/049): reduce an overweight
   position back toward its effective target and reinvest the proceeds elsewhere, at the user's
   own discretion via the UI's "Sprzedaj" button — never automated. Every leg (the sold leg and
   each reinvestment buy leg) is written with `session_id = NULL` — the only write path that
   leaves `session_id` NULL for a non-seed row.
3. **`V9__purchase_transactions.sql` PART 7** — a synthetic one-time backfill session ("Initial
   import from Google Sheets", `id = 1` in production, `planned_at = done_at =
   2024-01-01T00:00:00Z`), seeding one `asset_purchases` row per pre-existing position ×
   non-zero account-type bucket at migration time. These rows have no corresponding
   `purchase_session_items` rows at all — V9 wrote them directly.

**Gap: no column captures *why* a trade happened**, which the chart's dot color/shade
explicitly needs. This ADR adds that classification, retroactively, to every existing row.

**Verified against the live production DB before finalizing this design** (not assumed from
code reading alone — see queries run against `infra-db-1` during design):

- `asset_purchases` currently has 109 rows: 69 `SEED_IMPORT`-shaped (seed session `id=1`),
  36 real-session rows (all `is_migration_sell = is_migration_buy = false` today — no
  migration triple has fired yet, so `MIGRATION_BUY`/`MIGRATION_SELL` are exercised by the
  classification *logic* but have zero live rows as of this writing), and 4 `session_id IS
  NULL` rows (2 negative/2 positive — exactly one completed rebalancing transfer with one
  sold leg + one reinvest-buy leg... actually two transfers' worth of legs, confirmed 2-and-2).
- **Join-key uniqueness, empirically confirmed, not just inferred from the schema:** every one
  of the 36 real-session `asset_purchases` rows matches **exactly one**
  `purchase_session_items` row on `(session_id, portfolio, ticker, account_type)` — `min(match
  count) = max(match count) = 1` across all 36. This is also guaranteed structurally by the
  `psi_session_portfolio_ticker_account_uq` unique constraint on `purchase_session_items`
  (added V18) combined with `completeSession()` writing at most one `AssetPurchase` per
  processed item — but confirming it against live data, not just the constraint's existence,
  is what makes the backfill safe to run as a plain `UPDATE ... FROM` join with no dedup logic.
- **The ticker-picker exclusion cases are real, not hypothetical:** of 42 distinct tickers ever
  present in `asset_purchases`, exactly one (`PL.ETNRIVETH`) has no matching `positions` row at
  all, and exactly one live position (`PLN`, the PLN-cash bucket) has a NULL `yahoo_ticker`.
  Both the `NO_POSITION_MATCH` and `MISSING_YAHOO_TICKER` exclusion paths designed below are
  exercised by real data today, confirming this isn't speculative defensive coding.

**Currency-mismatch problem to solve.** `asset_purchases.price_pln` is always PLN. The sibling
regression endpoint plots Yahoo's raw close price in the ticker's **native** currency (BTC-USD
in USD, `ICGA.DE` in EUR, etc.) — it does not convert to PLN. Plotting a PLN trade price
directly against a native-currency Y-axis would place every non-PLN trade marker at the wrong
height. `Position.priceCurrency` (`EUR`/`USD`/`CHF`/`PLN`/`GBP`) and `Position.yahooTicker`
carry what's needed to convert; `FxRateCacheService.YAHOO_FX_TICKERS` already maps
`EUR→EURPLN=X`, `USD→USDPLN=X`, `CHF→CHFPLN=X`, `GBP→GBPPLN=X`, and
`YahooFinanceClient.fetchOHLCByRange`/`fetchAllOHLC` accept an arbitrary ticker string — they
work identically for an FX pair as for an equity/crypto ticker.

## Decision

### 1. `trade_type_enum` — six values

```sql
CREATE TYPE trade_type_enum AS ENUM (
    'DCA_BUY',                   -- regular purchase-session buy, no migration flags
    'MIGRATION_BUY',              -- purchase_session_items.is_migration_buy = true
    'MIGRATION_SELL',             -- purchase_session_items.is_migration_sell = true
    'REBALANCING_TRIM_SELL',      -- RebalancingTransferService sold leg (session_id NULL, units_delta < 0)
    'REBALANCING_REINVEST_BUY',   -- RebalancingTransferService buy leg  (session_id NULL, units_delta > 0)
    'SEED_IMPORT'                 -- V9 Part 7 synthetic backfill — not a real trade event
);
```

Added as a new column `asset_purchases.trade_type trade_type_enum NOT NULL` (nullable during
the migration's backfill steps, `SET NOT NULL` at the end — see Migration Spec below). Six
values, not the naive three (buy/sell/seed), because the user explicitly asked for a rebalancing
reinvestment buy to render differently from a DCA buy, and — since the data already supports
it — a rebalancing-trim sell to render differently from an ETF-migration sell. Collapsing
`MIGRATION_BUY`/`MIGRATION_SELL` into one `MIGRATION` value was considered and rejected: the
chart's dot color already needs the buy/sell distinction from `units_delta`'s sign for every
other type, so keeping migration split the same way is one line of frontend color-mapping, not
extra complexity, and it's a strictly more informative hover tooltip that comes for free.

**`SEED_IMPORT` is not a real trade** — it is a single synthetic session-completion event dated
`2024-01-01` standing in for "however many actual purchases happened before this app existed,"
priced at `avg_buy_price` (or `1.0` as a last resort), not a real execution price on a real
date. It is included in the enum (rather than silently excluded at the DB layer) so the API
surfaces it honestly and the frontend can decide how to render it — muted/grey and excluded
from the default view is the recommendation (§Open Decisions).

### 2. Backfill classification order (mirrors the write paths exactly)

1. **`session_id IS NULL`** → `RebalancingTransferService` is the only writer that produces
   this shape for a non-seed row. `units_delta < 0` ⇒ `REBALANCING_TRIM_SELL`; `units_delta >
   0` ⇒ `REBALANCING_REINVEST_BUY`.
2. **`session_id` = the seed session** (join `purchase_sessions` on `notes = 'Initial import
   from Google Sheets'` — more robust than `planned_at = '2024-01-01'`, since a date match is
   coincidental while the notes string is the actual identifying marker V9 wrote, and it's a
   single well-known literal, not a range comparison) → `SEED_IMPORT`. This step must run
   **before** step 3's join, because the seed session has no `purchase_session_items` rows at
   all — if step 3 ran first (or if the two were merged into one `CASE`), every seed row would
   silently fall through to the `DCA_BUY` default instead.
3. **Otherwise** (`session_id` set, real session) → join to `purchase_session_items` on
   `(session_id, portfolio, ticker, account_type)` — confirmed unique and confirmed 1:1 against
   every live row, see Context above. Neither migration flag set ⇒ `DCA_BUY`; `is_migration_sell`
   ⇒ `MIGRATION_SELL`; `is_migration_buy` ⇒ `MIGRATION_BUY`.
4. **Safety net**, not expected to fire on any current row (verified: zero rows would hit it
   today): any row still `NULL` after steps 1–3 defaults to `DCA_BUY` rather than leaving `NULL`
   and failing the closing `SET NOT NULL`. If this step ever does affect a row, that indicates a
   data shape this ADR's three cases didn't anticipate and is worth investigating by hand, not
   silently swallowing — noted inline in the migration SQL.

No new index on `trade_type` — `asset_purchases` is a personal-app-scale table (109 rows
today, low-hundreds for years), and the existing `ap_portfolio_ticker_date_idx` already covers
the chart endpoint's actual query pattern (`WHERE ticker = ? ORDER BY purchased_at`). Same
"tiny table, functional index would be over-engineering" reasoning `04-db-schema.md` already
applies to `DepositRepository`'s year-extraction queries.

### 3. FX conversion for chart markers

When the charted ticker's `Position.priceCurrency != PLN`: fetch **one** historical FX OHLC
series for the matching Yahoo pair (`FxRateCacheService.YAHOO_FX_TICKERS`: `EURPLN=X` /
`USDPLN=X` / `CHFPLN=X` / `GBPPLN=X`) via `YahooFinanceClient.fetchOHLCByRange`, covering the
same date window as the requested price series (the chart's effective `[fromDate, toDate]`
after defaulting — same defaulting rule `LinearRegressionService.fetchOHLC` already uses).
Convert each trade's `price_pln` to native currency via **nearest-calendar-date lookup** in
that FX series (ties broken toward the earlier date). This is at most 2 Yahoo HTTP calls total
per chart request (1 price series + 1 FX series) — no per-trade external call. When
`priceCurrency == PLN`, skip the FX fetch entirely: `priceNative = pricePln` for every marker,
0 extra calls.

**Never silently default a missing FX rate to 1.0** — same principle
`FxRateCacheService.requireRate` / `PurchaseSessionService.requireRate` already enforce for the
currency-breakdown figures ("a silent 1.0 EUR/PLN rate corrupts every currency-breakdown
figure"). If the FX series fetch fails or returns no data covering a trade's date, the endpoint
fails loudly (`PriceUnavailableException` → 503, same mapping `FxRateCacheService` already
produces) rather than plotting that marker at a wrong height.

### 4. New endpoints

`GET /market-data/trade-chart/tickers` — ticker picker. Built from `DISTINCT
asset_purchases.ticker` (every ticker ever traded, by construction — not `positions.ticker`
alone, since a fully-liquidated position is not deleted in the normal flow but the picker
should not rely on that invariant holding forever) joined to `positions` for display name /
`yahooTicker` / `priceCurrency`. A ticker with no matching `positions` row, or whose matching
row has a NULL `yahoo_ticker`, is omitted from `data` and reported in a sibling `excluded[]`
array with a reason (`NO_POSITION_MATCH` | `MISSING_YAHOO_TICKER`) — never a crash, matching the
project's existing precedent of a *different* NULL-`yahoo_ticker` position silently zeroing the
whole portfolio summary (see memory: "Blank yahoo_ticker blanks summary") being treated as a bug
to avoid repeating, not a pattern to follow.

`GET /market-data/trade-chart` — price series (raw close, no regression fit, no log transform)
plus one marker per `asset_purchases` row for that ticker. Full contract in
`03-api-contract.md` / `docs/openapi/`.

**When a ticker exists as more than one `positions` row** (same ticker in both `MAIN` and
`RETIREMENT`, or as a distinct companion row — companions carry their own ticker, so this is
really only the MAIN/RETIREMENT case), display metadata (`name`, `yahooTicker`,
`priceCurrency`) is resolved from the lowest-`id` matching row — the same "first match wins"
convention `RebalancingTransferService`/`PurchaseSessionService` already use for ticker→position
lookups (`Collectors.toMap(Position::getTicker, Function.identity(), (a, b) -> a)`). In practice
this is a non-issue: `yahooTicker`/`priceCurrency` should be identical for the same real-world
asset regardless of which portfolio holds it; if they ever diverge, that is a data-entry
inconsistency worth fixing at the source, not a case the chart needs to reconcile per-request.

### 5. Scope: aggregate across portfolio and account type

A ticker can be held in both `MAIN`/`RETIREMENT` and any of `IKE`/`IKZE`/`REGULAR`
simultaneously. **Decision: `GET /market-data/trade-chart` aggregates ALL trades for a ticker
into one chart regardless of portfolio/account type** — matching the "I bought/sold this asset"
mental model the user described (and the Saylor-chart reference, which has no such split). The
per-trade `portfolio`/`accountType` breakdown is still fully present in each marker for the
hover tooltip; there is no separate filter toggle in v1. See Open Decisions below — this is a
recommendation the requesting session should confirm with the user, not a closed question.

### 6. This does not touch the buy-only / manual-sell-only invariant

This is a **read-only visualization** of trades that already happened, sourced entirely from
existing `asset_purchases` rows. It adds no new write path, no new automated sell/buy logic,
and does not interact with the hard rule that rebalancing/purchase-planning must never
auto-suggest sells (ADR-045; only the existing manual "Sprzedaj" button in
`RebalancingTransferDialog.tsx` produces a sell, and that flow is entirely unchanged by this
ADR). Noted here only so a future reader of this ADR doesn't mistake the new
`REBALANCING_TRIM_SELL`/`REBALANCING_REINVEST_BUY` enum values for a new automated mechanism —
they are a read-side *label* on an event the existing manual flow already produced.

## Migration Spec (for the implementing backend agent)

**File:** `backend/src/main/resources/db/migration/V45__asset_purchase_trade_type.sql` (latest
existing migration is `V44__backfill_deposit_amount_planned_value.sql`).

```sql
-- =============================================================
-- V45__asset_purchase_trade_type.sql
-- Adds trade_type classification to asset_purchases (ADR-057) and
-- backfills every existing row using the write path each one came from.
-- Flyway migration — do NOT run manually.
-- =============================================================

-- ─── PART 1: NEW ENUM ───────────────────────────────────────────
-- CREATE TYPE (a brand-new type, not ALTER TYPE ... ADD VALUE) is
-- transactional-safe — same pattern V9 used for session_status_enum.
-- =============================================================

CREATE TYPE trade_type_enum AS ENUM (
    'DCA_BUY',
    'MIGRATION_BUY',
    'MIGRATION_SELL',
    'REBALANCING_TRIM_SELL',
    'REBALANCING_REINVEST_BUY',
    'SEED_IMPORT'
);

-- ─── PART 2: NEW COLUMN (nullable until backfilled) ─────────────

ALTER TABLE asset_purchases
    ADD COLUMN trade_type trade_type_enum NULL;

-- ─── PART 3: BACKFILL ────────────────────────────────────────────
-- Order matters: each step only touches rows the previous step left
-- NULL. Verified against production data before writing this file —
-- 36 real-session rows (all match exactly 1 purchase_session_items
-- row), 4 session_id-NULL rows (2 negative/2 positive), 69 seed rows.
-- =============================================================

-- 3a. Rebalancing-transfer legs: RebalancingTransferService.executeTransfer()
-- is the only writer that leaves session_id NULL for a non-seed row (V15).
UPDATE asset_purchases
SET trade_type = CASE WHEN units_delta < 0
                       THEN 'REBALANCING_TRIM_SELL'::trade_type_enum
                       ELSE 'REBALANCING_REINVEST_BUY'::trade_type_enum
                  END
WHERE session_id IS NULL;

-- 3b. The synthetic V9 Part 7 backfill session. Must run before 3c's join:
-- this session has zero purchase_session_items rows (V9 wrote asset_purchases
-- directly), so if 3c ran first these rows would wrongly default to DCA_BUY.
UPDATE asset_purchases ap
SET trade_type = 'SEED_IMPORT'::trade_type_enum
FROM purchase_sessions ps
WHERE ap.trade_type IS NULL
  AND ap.session_id = ps.id
  AND ps.notes = 'Initial import from Google Sheets';

-- 3c. Real sessions: join to purchase_session_items on the unique
-- (session_id, portfolio, ticker, account_type) key
-- (psi_session_portfolio_ticker_account_uq, added V18) to read the
-- migration flags. Confirmed empirically 1:1 against every live row.
UPDATE asset_purchases ap
SET trade_type = CASE
        WHEN psi.is_migration_sell THEN 'MIGRATION_SELL'::trade_type_enum
        WHEN psi.is_migration_buy  THEN 'MIGRATION_BUY'::trade_type_enum
        ELSE 'DCA_BUY'::trade_type_enum
    END
FROM purchase_session_items psi
WHERE ap.trade_type IS NULL
  AND ap.session_id = psi.session_id
  AND ap.portfolio = psi.portfolio
  AND ap.ticker = psi.ticker
  AND ap.account_type = psi.account_type;

-- 3d. Safety net — not expected to affect any row today (verified). Any row
-- still NULL here defaults to DCA_BUY rather than failing PART 4's SET NOT
-- NULL. If this ever DOES affect a row, treat it as a discrepancy worth
-- reviewing by hand (a session_id pointing at neither the seed session nor
-- a session with a matching purchase_session_items row), not a fluke.
UPDATE asset_purchases
SET trade_type = 'DCA_BUY'::trade_type_enum
WHERE trade_type IS NULL;

-- ─── PART 4: ENFORCE NOT NULL ────────────────────────────────────

ALTER TABLE asset_purchases
    ALTER COLUMN trade_type SET NOT NULL;
```

**Entity change (informational, backend agent's to implement):** `AssetPurchase.tradeType`,
`@Enumerated(EnumType.STRING) @JdbcTypeCode(SqlTypes.NAMED_ENUM) @Column(name = "trade_type",
nullable = false, columnDefinition = "trade_type_enum")`, backed by a new `pl.investing.entity
.enums.TradeType` enum with the same six values. `AssetPurchase.builder()...tradeType(...)`
needs one new call site in each of the three writers (`PurchaseSessionService.completeSession()`
— `DCA_BUY` or derived from `item.isMigrationSell()`/`isMigrationBuy()`;
`RebalancingTransferService.executeTransfer()` — `REBALANCING_TRIM_SELL`/
`REBALANCING_REINVEST_BUY`; the V9 seed path is historical and not touched going forward).

## Consequences

- **`asset_purchases` gains a mandatory classification column**, backfilled once, computed
  going forward by the three existing writers at the point they already build each
  `AssetPurchase` row — no new write path, no behavior change to any existing endpoint's
  response shape (this field is additive).
- **Two new read-only `GET` endpoints** (`/market-data/trade-chart/tickers`,
  `/market-data/trade-chart`) — no new tables beyond the one column, no change to any existing
  endpoint's contract.
- **Up to 2 additional Yahoo Finance HTTP calls per chart request** (price series + FX series,
  only when `priceCurrency != PLN`) — same order of magnitude as the existing
  `linear-regression` endpoint's single call, well within the free-tier, cost-free constraint.
- **`SEED_IMPORT` rows will render as trade dots unless the frontend deliberately excludes or
  mutes them** — flagged as an open decision below since a fabricated 2024-01-01 price/date
  next to real trade dots could mislead if not visually distinguished.
- **No interaction with the buy-only/manual-sell-only invariant** (ADR-045) — see §6 above.
- **Historical `asset_purchases` rows keep their existing columns unchanged** — this is a purely
  additive backfill, not a correction (unlike ADR-055's `deposits.amount` fix). No prior figure
  (cost basis, XIRR, deposit totals) is affected by this migration.

## Open Decisions (flag to the user)

1. **Enum value names** (`DCA_BUY`, `MIGRATION_BUY`, `MIGRATION_SELL`, `REBALANCING_TRIM_SELL`,
   `REBALANCING_REINVEST_BUY`, `SEED_IMPORT`) — six values instead of the minimal three
   (buy/sell/seed). Chosen for tooltip/color richness; reversible before ship (just an enum
   literal set) but not after real usage accumulates against it.
2. **Aggregation scope (§5)**: chart aggregates across portfolio + account type by default, no
   filter toggle in v1, breakdown surfaces only in the hover tooltip. This was the requesting
   session's own recommendation, adopted here — worth the user's explicit sign-off since it's a
   product decision, not purely technical.
3. **`SEED_IMPORT` handling in the UI** is left to the frontend agent with a recommendation
   (muted color, excluded from default view) rather than a hard requirement — the user may
   prefer to see it either way.
4. **Y-axis scale**: this new endpoint always returns raw, untransformed close prices (no log
   fit, unlike `linear-regression`). Linear vs. log is purely a frontend `<YAxis scale=... />`
   rendering choice over the same numbers; recommendation is **linear by default** for this
   chart (the user's own phrasing — "can use a normal linear price axis too — not just log" —
   reads as wanting linear to be at least available, and this chart's purpose, dot-size
   comparison across trades, arguably reads more naturally on a linear axis than log). Not
   enforced by the API contract either way.
5. **Endpoint naming**: `/market-data/trade-chart` and `/market-data/trade-chart/tickers`,
   placed under the existing `market-data` tag/base path alongside `linear-regression` (same
   domain: derived, computed-on-read market/price data, not a stored resource). Alternatives
   considered: a standalone `/trades` resource — rejected because these endpoints return a
   composite (price series + markers), not a CRUD-style collection of trade records, and
   `asset_purchases` already has no dedicated REST resource of its own today (it's only ever
   surfaced as a side effect of `/purchase-sessions/{id}/complete` and
   `/rebalancing-transfers`) — introducing one now, only for chart consumption, would invite the
   question of why it isn't also independently listable/paginated, which is out of scope here.
6. **Error-handling codes reuse existing exception→status precedents** rather than introducing
   an unused `422`: `NoSuchElementException` → 404 (no trade history for the ticker, or no
   matching `positions` row — same message pattern `RebalancingTransferService` already uses,
   `"Position not found for ticker: X"`), `IllegalArgumentException` → 400 (position found but
   `yahoo_ticker` is NULL; or Yahoo returned no OHLC data at all — same as
   `LinearRegressionService`'s existing `"No data found for ticker: X"` case),
   `PriceUnavailableException` → 503 (FX series required but unavailable). The OpenAPI spec's
   existing (but currently unused anywhere in the codebase) `UnprocessableEntity`/422 response
   was deliberately **not** reused here, to avoid attaching a new endpoint to a response contract
   nothing in `GlobalExceptionHandler` actually produces today.

## Addendum (2026-08-22): `tradeCount` excludes `SEED_IMPORT`

Found via a live user report: the ticker-picker badge for COIN showed "13×" while the chart/
trades-table (which hide `SEED_IMPORT` by default, see Open Decision #3) only ever showed 6
trades. Root cause: `AssetPurchaseRepository.aggregateByTicker()` did a plain `COUNT(ap) GROUP
BY ap.ticker` over every row regardless of `trade_type` — for COIN specifically, 7 `SEED_IMPORT`
rows (one per portfolio × account-type bucket that held it at migration time) alongside 6 real
`DCA_BUY`/`REBALANCING_REINVEST_BUY` rows.

**Fix:** `tradeCount` now excludes `SEED_IMPORT` rows from the count (query takes
`excludedType` as a bind parameter, always `TradeType.SEED_IMPORT` from
`TradeChartService.listTickers()`), while still `GROUP BY`-ing over *every* row so a ticker with
zero real trades is not dropped from the picker entirely — a live-DB check found 13 tickers
today (4GLD.DE, ETH-USD, SOL-USD, MSTR, and others) that are currently-held positions with no
real trade since the migration; these must still be selectable in the picker (their price chart
is still viewable), just with `tradeCount = 0`. `firstTradeAt`/`lastTradeAt` are left computed
over all rows (unchanged) — they're not rendered anywhere in the frontend today, so there was no
observed confusion to fix, and changing their semantics would introduce a nullable-date case
(an all-`SEED_IMPORT` ticker) with no consumer needing it yet.

Regression coverage: `TradeChartServiceIT.listTickers_tradeCount_excludesSeedImportRows` and
`listTickers_tickerWithOnlySeedImportRows_stillAppearsWithZeroTradeCount`.
