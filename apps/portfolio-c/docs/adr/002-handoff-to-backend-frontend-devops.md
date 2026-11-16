# ADR-002: Handoff — files touched by the AccountType → Portfolio replacement

**Date:** 2026-07-29
**Status:** Accepted (this is a handoff record, not a design decision — see ADR-001 for
the actual decision and rationale)

This is the concrete "what needs to change" list for Backend, Frontend-web, and
DevOps, produced so those agents don't have to rediscover it by re-reading every
file. It was compiled by grepping the pre-existing (Portfolio B-derived) codebase for
`AccountType` / `account_type` / `IKE_TATA` / `IKE_MAMA` / `IKZE_TATA` / `IKZE_MAMA` /
`tata` / `mama` (roughly 30 hits across `.java` / `.ts` / `.tsx`, including tests),
plus a second pass for files that don't mention those terms but still need a
`portfolio` parameter threaded through them as a consequence (repositories/services
that currently operate over "all positions" with no scoping at all).

The clean schema and seed data are already written: see
`backend/src/main/resources/db/migration/V1__init.sql` and `V2__seed_data.sql`
(Portfolio B's V1-V15 were deleted — this is a brand-new empty database). Everything
below is Java/TS application code that still assumes the old schema/enum.

## 1. Delete outright (no sub-account concept survives)

| File | Why |
|---|---|
| `backend/src/main/java/pl/portfolio-c/entity/enums/AccountType.java` | Replaced by a new `entity/enums/Portfolio.java` (`VARIABLE`, `FIXED`) |
| `backend/src/main/java/pl/portfolio-c/dto/rebalancing/AccountAllocDto.java` | Per-account buy-list breakdown — nothing to allocate *between* once a portfolio is one flat book |
| `backend/src/main/java/pl/portfolio-c/dto/purchases/AccountUnitAllocDto.java` | Same — per-account unit allocation on a confirm item no longer applies |
| `RebalancingStrategy.allocateToAccounts(...)` and the `AccountType[] accounts = { IKE_TATA, ... }` priority-fill block | Greedy IKE_TATA→IKE_MAMA→IKZE_TATA→IKZE_MAMA fill has no equivalent — a portfolio has one cash balance, not four |

## 2. Entities

| File | Change |
|---|---|
| `entity/Position.java` | Drop `unitsIkeTata`/`unitsIkeMama`/`unitsIkzeTata`/`unitsIkzeMama`/`unitsInne` → single `units` field. Add `portfolio` (`@Enumerated` `Portfolio`, `columnDefinition = "portfolio_enum"`). `computeUnitsTotal()` collapses to `return units;` (or delete it and use `getUnits()` at call sites — same effect, less indirection). Delete `unitsFor(AccountType)` entirely — nothing calls a per-account getter anymore. |
| `entity/Deposit.java` | Add `portfolio` field/column. |
| `entity/AccountCashSnapshot.java` | Replace `@Id Integer id = 1` with `@Id @Enumerated Portfolio portfolio`. Collapse `ikeTata`/`ikeMama`/`ikzeTata`/`ikzeMama` → single `balancePln`. Table stays named `account_cash_snapshot` (that name is fine now that "account" just means "the portfolio's brokerage cash," not IKE/IKZE) — just one row per portfolio instead of a single `id=1` row. |
| `entity/PurchaseLot.java` | Add `portfolio` field/column. |
| `entity/PositionTransfer.java` | Replace `accountType` field with `portfolio`. Drop the `AccountType` import. |
| `entity/Historia.java` | Add `portfolio` field/column; the unique constraint moves from `snapshot_date` alone to `(portfolio, snapshot_date)` (matches `V1__init.sql`'s `historia_portfolio_date_uq`) — update the `@Table` unique constraint / `@Column(unique = true)` on `snapshotDate` accordingly. |

## 3. DTOs

| File | Change |
|---|---|
| `dto/position/PositionDto.java` | Drop the 5 `unitsIke*/unitsInne` fields → single `units`. Add `portfolio` (read-only, useful for display/filtering context). |
| `dto/position/SubPositionDto.java` | Same 5→1 collapse. Given `Position.units` is now singular, `effectiveUnits` and `units` become the same value for a non-companion row — keep both if companion rows are ever reintroduced (a companion's units still need summing into the parent's total), otherwise consider collapsing to one field. |
| `dto/position/UpdatePositionRequest.java` | Drop the 5 `unitsIke*/unitsInne` fields → single optional `units`. |
| `dto/position/CreatePositionRequest.java` | Add `portfolio` (required — a position must declare which book it belongs to at creation). |
| `dto/accountcash/AccountCashDto.java` | Drop 4 balance fields → single `balancePln`. Add `portfolio`. |
| `dto/accountcash/AccountCashUpdateRequest.java` | Drop 4 balance fields → single `balancePln` (portfolio comes from the path variable, not the body — see §5). |
| `dto/purchases/ConfirmPurchasesRequest.java` | Add `portfolio` (required) and `depositAmountPln` (required — see ADR-001 §2; this is what gets auto-recorded as the deposit, replacing the old "re-read `monthly_contribution_pln` inside `confirm()`" behavior). |
| `dto/purchases/ConfirmPurchaseItemDto.java` | Replace `List<AccountUnitAllocDto> accountAllocations` with a single `BigDecimal units` — no sub-account split to record. |
| `dto/purchases/PurchaseHistoryResponse.java` / `PurchaseHistoryDataDto.java` | No direct `AccountType` reference, but the underlying query is now portfolio-scoped — thread a `portfolio` param through `PurchaseHistoryService` (see §4) into whatever query backs this DTO. |
| `dto/rebalancing/BuyListItemDto.java` | Drop `List<AccountAllocDto> accountAllocations` field entirely. |
| `dto/rebalancing/RebalancingResultDto.java` | Add a `portfolio` field (constructor + getter) so the response is self-describing. |
| `dto/transfer/SoldLegDto.java` / `BuyLegDto.java` | Drop `accountType` field — a leg is just `ticker`/`units`/`pricePln` now. |
| `dto/transfer/PositionTransferRequest.java` | Add `portfolio` (required) at the request root — both the sold leg and all buy legs happen inside one portfolio. |
| `dto/deposit/CreateDepositRequest.java` | Add `portfolio` (required). |
| `dto/deposit/DepositDto.java` | Add `portfolio` (read-only). |
| `dto/historia/HistoriaEntryDto.java` | Add `portfolio` (read-only) if the entry doesn't already carry enough context for the frontend to know which book it's for. |

## 4. Services (business logic + the "invisible" portfolio-scoping ripple)

These weren't caught by the `AccountType`/IKE/IKZE grep but need a `portfolio`
parameter threaded through regardless, because they currently operate over "all
positions" / "all deposits" / "all history" with zero scoping — which is exactly
the bug ADR-001 exists to close (two portfolios must never see each other's data).

| File | Change |
|---|---|
| `service/PositionDeltaApplicator.java` | Collapse the 5-way `switch (accountType)` to a single `position.setUnits(position.getUnits().add(delta))`. Drop the `AccountType` parameter from `apply(...)`. |
| `service/PurchaseConfirmService.confirm(...)` | Rewrite the per-item loop: no more `deductIkeTata`/`deductIkeMama`/`deductIkzeTata`/`deductIkzeMama` accumulators or `switch (alloc.accountType())` — one `units` value per item, one running `deductPln` total. Deduct from `AccountCashService` for the request's single `portfolio`. Replace the "auto-record monthly deposit via `settingService.getMonthlyContributionPln()`" step with "record `request.depositAmountPln()`" (see ADR-001 §2 — this removes `PurchaseConfirmService`'s dependency on `SettingService` entirely). **Also fix `positionRepository.findByTicker(item.ticker())`** — see the correctness flag below. |
| `service/PositionTransferService.executeTransfer(...)` | Drop every `sold.accountType()` / `leg.accountType()` reference — `positionDeltaApplicator.apply(position, delta)` no longer takes an account type. `soldPosition.unitsFor(sold.accountType())` becomes `soldPosition.getUnits()`. **Also fix the two `positionRepository.findByTicker(...)` calls** — see the correctness flag below. |
| `service/rebalancing/ValueComputationService.computeValues()` | Currently calls `positionRepository.findAll()` with no filter — must become `computeValues(Portfolio portfolio)` calling a portfolio-scoped repository method, or every value/rebalancing calculation silently mixes both books' positions together. |
| `service/rebalancing/RebalancingStrategy.calculate(...)` | Drop the `allocateToAccounts` step (§1) and the `AccountCashBalances` 4-field shape it consumes — once `AccountCashService` returns a single balance per portfolio, this whole method simplifies to "how much of the deposit goes to this ticker," full stop. |
| `service/RebalancingService.calculate(...)` | Add a `Portfolio portfolio` and `BigDecimal depositAmountOverride` parameter (nullable). Implements the FIXED-falls-back-to-setting / VARIABLE-required validation from ADR-001 §2. |
| `service/AccountCashService.java` | `getCurrent()`/`updateManual()`/`deductPurchases()`/`getCurrentBalances()` all take a `Portfolio` parameter and look up the single row keyed by that portfolio instead of the hardcoded `id=1`. `AccountCashBalances` record collapses from 4 fields to 1 (`balancePln`) — or gets deleted if callers can just use `BigDecimal` directly. |
| `service/PositionService.java` | `listPositions`/`getPosition`/`updatePosition` all need a `Portfolio` parameter (or the repository query itself needs to be pre-scoped) — `computePortfolioTotal()` in particular must sum only the requested portfolio's positions, not both books'. |
| `service/DepositService.java` | Needs a `Portfolio` parameter on list/create; `createDeposit` (called both from the controller and from `PurchaseConfirmService`) needs to know which book the deposit belongs to. |
| `service/HistoriaService.java` / `HistoriaScheduler.java` | Needs a `Portfolio` parameter (list) and must snapshot **both** portfolios separately on each scheduled run (two `historia` rows per day, not one). |
| `service/PurchaseHistoryService.java` | Needs a `Portfolio` parameter — `purchase_lots` is portfolio-scoped now. |
| `service/SettingKeys.java` | Drop `IKE_ANNUAL_LIMIT` / `IKZE_ANNUAL_LIMIT` entirely (no IKE/IKZE limits in this domain) and rename `MONTHLY_CONTRIBUTION` → `FIXED_MONTHLY_DEPOSIT_DEFAULT = "fixed_monthly_deposit_default_pln"` (matches the seeded key in `V2__seed_data.sql`). |
| `service/SettingService.java` | Delete `getIkeAnnualLimitPln()` / `getIkzeAnnualLimitPln()`. Rename `getMonthlyContributionPln()` → `getFixedMonthlyDepositDefaultPln()`, default value 400 (unchanged). Simplify `validateSettingValue`'s switch to the one remaining key. |
| `mapper/PositionMapper.java` | The `@AfterMapping setDefaults` collapses from 5 null-checks to 1 (`units`). |

### Correctness flag: `PositionRepository.findByTicker(String)` is now ambiguous

`PositionRepository.findByTicker(String ticker)` returns a single `Optional<Position>`
and is called from `PurchaseConfirmService.confirm()` and
`PositionTransferService.executeTransfer()`. Once `ticker` is unique only per
`(ticker, portfolio)` — not globally — this method is silently wrong for any ticker
held by both portfolios (ICGA.DE and SXRV.DE both are, per the seed data). It will
return *some* row (JPA won't error on a `findOne`-style query even if there's no
enforced single-result guarantee at the JPA level unless you use
`findByTicker` backed by a unique DB constraint it can no longer rely on) or throw
a `NonUniqueResultException` depending on the exact query method shape — either way
it needs to become **`findByTickerAndPortfolio(String ticker, Portfolio portfolio)`**,
and every call site needs the portfolio value threaded in from the enclosing
request. This is a real correctness bug waiting to happen, not just a rename — flag
it prominently in the backend task/PR description.

## 5. Controllers — proposed route/contract shapes

Architect is not implementing these, but here is the exact contract shape assumed
by the DTO changes above, so Backend doesn't have to re-derive it:

| Controller | Old | New |
|---|---|---|
| `PositionsController` | `GET /positions`, `GET/PUT /positions/{id}` | `GET /positions?portfolio=VARIABLE` (required query param); `GET/PUT /positions/{id}` unchanged path, but bodies drop the 5 unit fields per §3 |
| `RebalancingController` | `POST /purchases/calculate` (no body); `POST /purchases/confirm`; `GET /purchases/history` | `POST /purchases/calculate` body `{ portfolio, depositAmountPln? }` (validation error if VARIABLE + omitted); `POST /purchases/confirm` body gains `portfolio` + `depositAmountPln`; `GET /purchases/history?portfolio=VARIABLE` (required query param) |
| `DepositsController` | `GET /deposits`, `POST /deposits` | Both need `portfolio` — query param on GET, body field on POST |
| `HistoriaController` | `GET /history` | Add required `portfolio` query param |
| `AccountCashController` | `GET/PUT /account-cash`, `POST /account-cash/deduct` | `GET/PUT /account-cash/{portfolio}`, `POST /account-cash/{portfolio}/deduct` — portfolio as a path variable since the resource is now one-per-portfolio, not a singleton |
| `PositionTransferController` | `POST /position-transfers` | Body gains `portfolio` at the root |
| `SettingsController` | `GET /settings`, `PUT /settings/{key}` | No path change — only the key name changes (§4) |

## 6. shared-ts (Zod schemas — web + mobile clients both consume these)

| File | Change |
|---|---|
| `shared-ts/src/schemas/enums.ts` | Replace `accountTypeSchema` (`IKE_TATA`/`IKE_MAMA`/`IKZE_TATA`/`IKZE_MAMA`/`INNE`) with `portfolioSchema = z.enum(["VARIABLE", "FIXED"])`. Update the exported `AccountType` type → `Portfolio`. |
| `shared-ts/src/schemas/account-cash.ts` | Collapse `ikeTata`/`ikeMama`/`ikzeTata`/`ikzeMama` → single `balancePln` in both `accountCashSchema` and `accountCashUpdateSchema`. Add `portfolio`. |
| `shared-ts/src/schemas/positions.ts` | Collapse the 4 `unitsIke*`/etc. fields (appearing 3 times — base schema, response schema, update-request schema per the earlier grep at lines 22-25/42-45/78-81) → single `units` in each. Add `portfolio`. |

## 7. Frontend-web components

| File | Change |
|---|---|
| `components/AccountCashCard.tsx` | Currently shows 4 account balance inputs (IKE_TATA/IKE_MAMA/IKZE_TATA/IKZE_MAMA) — collapses to a single balance input, and the whole card needs to be rendered once per portfolio (or gain a portfolio switcher) since there's no longer one global cash snapshot. |
| `components/EditPositionDialog.tsx` | Drop the 4 per-account unit inputs → single units input. (This is also the dialog the user's seed-time note referenced: avg_buy_price stays user-editable here exactly as it was in Portfolio B.) |
| `components/PositionTransferDialog.tsx` | Drop account-type selectors on both the sold leg and buy leg forms — a leg is just ticker + units + price now. |
| `components/tables/PositionsTable.tsx` | Drop any per-account unit columns/breakdown; the SELL/OVER/UNDER/ON_TARGET badge logic (deviationPct thresholds) itself is unaffected — it's account-agnostic already. |
| `components/RebalancingPanel.tsx` | Needs a portfolio switcher (or the app needs two routes/pages, one per portfolio) and a deposit-amount input that's optional-with-default for FIXED and required-with-no-default for VARIABLE, matching ADR-001 §2's validation rule. |
| `lib/formConstants.ts` | Replace any `AccountType`-keyed label maps/dropdown options with a `Portfolio`-keyed equivalent (`VARIABLE`/`FIXED` display labels). |

## 8. Tests (will not compile against the new schema/DTOs as-is)

| File | Note |
|---|---|
| `backend/src/test/java/pl/portfolio-c/service/PositionTransferIntegrationTest.java` | References `AccountType`, seeds/asserts against `IKE_TATA` units, and calls `SoldLegDto`/`BuyLegDto` with an `accountType` argument — needs a full rewrite against the new single-`units`/`portfolio` shape, not a patch. |
| `backend/src/test/java/pl/portfolio-c/service/PurchaseHistoryIntegrationTest.java` | Same — references `AccountType`-shaped confirm requests; needs a rewrite against `ConfirmPurchaseItemDto`'s new single-`units` shape and a `portfolio` parameter on `PurchaseHistoryService`. |

## 9. DevOps flags (not this agent's deliverable, but caught while reading the copied infra)

- `infra/compose.yml` (already copied into `portfolio-c`) still has **stale
  ports**: `db-portfolio-c` publishes `5433:5432` and `backend-portfolio-c` publishes
  `8081:8080`. Per the confirmed infra decision, these must become **`5434:5432`**
  and **`8082:8080`** respectively. `frontend-portfolio-c` correctly has no published host
  port already (joins `investing-shared` only) — no change needed there.
- `infra/.env.example` is missing the `GOOGLE_CLIENT_ID`, `ALLOWED_EMAIL_1`,
  `ALLOWED_EMAIL_2`, `JWT_SECRET`, and `VITE_GOOGLE_CLIENT_ID` placeholders that
  `compose.yml` already references as environment variables — the auth wiring is
  real (see ADR context in `docs/architecture.md` "Authentication") but the example
  env file doesn't document it yet. `ALLOWED_EMAIL_1=szczyrbus21@gmail.com`,
  `ALLOWED_EMAIL_2=second-user@example.com` per the confirmed allowlist.
- Main `investing-app`'s `infra/nginx/nginx.conf` proxies Portfolio B via
  `proxy_pass http://backend-portfolio-b:8080/api/` and
  `proxy_pass http://frontend-portfolio-b:80/` (both internal container ports, unrelated
  to the host-port numbers above). The equivalent `/portfolio-c/` block DevOps adds should
  follow the exact same pattern: `proxy_pass http://backend-portfolio-c:8080/api/;` and
  `proxy_pass http://frontend-portfolio-c:80/;` — internal container ports never change
  regardless of the host-port renumbering above.
