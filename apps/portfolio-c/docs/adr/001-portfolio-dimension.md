# ADR-001: Portfolio dimension replaces account_type; request-scoped deposit amount

**Date:** 2026-07-29
**Status:** Accepted

## Context

`portfolio-c` started as a mechanical copy of `portfolio-b` (package
`pl.portfolio-c`, class `Portfolio CApp`, npm scope `@portfolio-c/*`) with every occurrence of
"portfolio-b" string-replaced with "portfolio-c". That rename was purely mechanical — the copied
domain model still describes Portfolio B's actual domain: a single retirement portfolio
split into four tax-significant sub-accounts (`IKE_TATA`, `IKE_MAMA`, `IKZE_TATA`,
`IKZE_MAMA`) plus a historical catch-all (`INNE`), a single global fixed monthly
contribution setting, and a single-row `account_cash_snapshot` with one balance column
per sub-account.

Portfolio C's actual domain is different in a way that isn't a superficial rename:

1. **Two portfolios, not one, and they never mix.** A VARIABLE portfolio (targets:
   50% SXRV.DE / 20% ICGA.DE / 30% 4GLD.DE) and a FIXED portfolio (targets: 33.34%
   ETNVIRBTCP.WA / 33.33% ICGA.DE / 33.33% SXRV.DE) — separate positions, separate
   cash, separate deposit ledger, separate history, separate settings.
2. **No sub-accounts at all.** There is no IKE/IKZE concept in this app — each
   portfolio is one flat book with a single cash balance and a single units column
   per position. Portfolio B's `account_type_enum` (a *sub-account-within-one-portfolio*
   dimension) doesn't describe Portfolio C's domain; Portfolio C needs a
   *which-of-two-portfolios* dimension instead.
3. **The two portfolios fund themselves differently.** FIXED deposits a constant
   400 PLN/month (changeable, but has a real default). VARIABLE's deposit varies
   every month with no default at all — forcing a monthly amount onto it would be
   actively wrong, not just redundant.

Continuing to model this as "swap `account_type_enum`'s five values for two
differently-named values" would leave every consumer (5 unit columns on `positions`,
a 4-column single-row `account_cash_snapshot`, an account-priority-fill step in the
rebalancing algorithm, per-leg `account_type` on transfers) carrying machinery this
domain doesn't have, while missing the actual constraint that matters: two portfolios
must never see each other's positions, cash, or history, and only one of them has a
concept of "the usual monthly amount."

## Decision

**1. Replace `account_type_enum` with `portfolio_enum` (`VARIABLE`, `FIXED`), and
drop the sub-account dimension entirely — it does not exist in this domain.**

- `positions`, `deposits`, `historia`, `account_cash_snapshot`, `purchase_lots`, and
  `position_transfers` all gain a `portfolio` column (or, for `account_cash_snapshot`,
  `portfolio` becomes the primary key — replacing Portfolio B's single `id=1` row).
- Every per-sub-account column collapses to one: `positions` goes from
  `units_ike_tata` / `units_ike_mama` / `units_ikze_tata` / `units_ikze_mama` /
  `units_inne` to a single `units` column; `account_cash_snapshot` goes from four
  balance columns to a single `balance_pln` column (one row per portfolio, not one
  row total).
- `positions.ticker` is **no longer globally unique** — uniqueness moves to
  `(ticker, portfolio)`, because the same instrument can legitimately sit in both
  books at different target weights (ICGA.DE is 20% of VARIABLE and 33.33% of
  FIXED; SXRV.DE is 50% of VARIABLE and 33.33% of FIXED). `purchase_lots` and
  `position_transfers` reference positions via the composite
  `(ticker, portfolio)` foreign key for the same reason.
- `PositionDeltaApplicator`'s five-way switch over `AccountType`, the
  account-priority-fill step in `RebalancingStrategy` (greedy IKE_TATA → IKE_MAMA →
  IKZE_TATA → IKZE_MAMA), `AccountAllocDto`/`AccountUnitAllocDto`, and
  `account_type` on `SoldLegDto`/`BuyLegDto`/`position_transfers` are all deleted
  outright, not adapted — there is nothing to allocate *between* once a portfolio
  is one flat book. See `docs/adr/002-handoff-to-backend-frontend-devops.md` for
  the exact file list.
- **Settings stay a plain global key/value table, not a generic `(key, portfolio)`
  table.** Only one portfolio (FIXED) has a configurable default value at all — a
  single well-named key (`fixed_monthly_deposit_default_pln`) says exactly that,
  and is simpler than a scoping mechanism built for a single scoped row. If a
  second portfolio-scoped setting appears later, revisit with a proper scoping
  table then (YAGNI, not an oversight).

**2. `POST /purchases/calculate` becomes request-scoped on both portfolio and
deposit amount, instead of reading one global fixed setting.**

Portfolio B's `RebalancingService.calculate()` took no arguments — it always read the
single global `monthly_contribution_pln` setting. That can't survive two portfolios
with different funding models:

- Request body gains `portfolio` (required) and `depositAmountPln` (optional).
- **FIXED**: if `depositAmountPln` is omitted, fall back to the stored
  `fixed_monthly_deposit_default_pln` setting (400 by default). If supplied, the
  override wins for that one call — the stored default is unchanged.
- **VARIABLE**: there is no stored default and none should be invented.
  `depositAmountPln` is required; omitting it is a `VALIDATION_ERROR`, not a
  silent zero or an implicit fallback to FIXED's number.
- `POST /purchases/confirm` carries the same `portfolio` and the
  `depositAmountPln` that was actually used for the preceding `calculate()` call,
  so the auto-recorded deposit row is correct for either portfolio without
  `PurchaseConfirmService` re-deriving or re-guessing an amount. This also means
  `confirm()` no longer needs to call `SettingService` at all.
- `GET /purchases/history` gains a required `portfolio` query parameter (lot
  history is now per-portfolio, since `purchase_lots` is portfolio-scoped).

## Consequences

- Two portfolios can never accidentally see or affect each other's data — every
  query that used to be "give me the positions/cash/history" must now be "give me
  the positions/cash/history *for portfolio X*." This is a real behavior change,
  not just a rename, and touches roughly 22 backend/frontend/shared-ts files (see
  ADR-002 for the itemized list) plus the entire migration set (Portfolio B's V1-V15
  history was discarded and replaced with a clean `V1__init.sql` + `V2__seed_data.sql`,
  since this is a brand-new empty database with nothing to migrate forward).
- `positions.ticker` losing its standalone uniqueness is a real constraint
  loosening — any code that assumed "ticker uniquely identifies a position" (there
  wasn't much, since `PurchaseLot`/`PositionTransfer` already treated `ticker` as a
  plain unenforced-relationship column) must now think in `(ticker, portfolio)`
  pairs. Composite foreign keys make this enforced at the DB level, not just a
  convention.
- Losing the sub-account priority-fill logic in `RebalancingStrategy` is a net
  simplification, not a loss of functionality Portfolio C's domain actually needs — there
  was never a "which sub-account has room" question to answer here.
- `avg_buy_price` / `total_spent_pln` are seeded `NULL`, not a guessed number,
  because a Flyway migration authored today cannot know the live market price at
  whatever future moment it actually runs against a real database. Backend must
  add a one-time startup reconciliation (documented in the `V2__seed_data.sql`
  header and in ADR-002) that sets both fields from the first live price fetch for
  any position with `units > 0` and `total_spent_pln IS NULL` — that is what
  delivers the "0 unrealized gain to start" behavior the user asked for.
- The `ETNVIRBTCP.PL` → `ETNVIRBTCP.WA` ticker suffix correction is seeded as a
  best guess backed by the sibling Portfolio B app's own data (which already uses
  `.WA` for the same family of VirETP notes), but is explicitly flagged for
  Backend to verify against a live Yahoo Finance lookup before shipping the
  market-data integration — this ADR does not resolve it, only flags it.
