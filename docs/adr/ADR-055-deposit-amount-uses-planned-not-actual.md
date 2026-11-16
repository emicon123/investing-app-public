# ADR-055: Deposit-Sum Aggregation Prefers Planned Value, Not Actual (Fixes Wpłata Under-Counting)

**Date:** 2026-07-23
**Status:** Accepted

## Context

`PurchaseSessionService.completeSession()` has two independent blocks that both resolve an "effective PLN value" per `PurchaseSessionItem` using the same fallback pattern — `actualValuePln ?? plannedValuePln` — but the two blocks feed semantically different things, and only one of them should use that preference order.

**Block A — cost basis (correct, unchanged).** Around lines 317–319, the effective value feeds `AssetPurchase.totalPln` and, via `positionDeltaApplicator.updateTotalSpentPln(...)`, `positions.total_spent_pln` (cost basis). Preferring `actualValuePln` here is correct: cost basis and P&L must reflect what was *really* paid — the real execution price × real units, after broker fees and currency-conversion slippage. This is the number `profit_pct` and `avg_buy_price` are built on, and it should not pretend fees didn't happen.

**Block B — deposit-sum aggregation (the bug).** Around lines 371–403 ("Auto-create Deposit records grouped by `(portfolio, accountType, exchange)` — ADR-035"), the same `effectiveValuePln = actualValuePln ?? plannedValuePln` expression is used to build `depositSums`, which becomes each auto-created `Deposit.amount`. This is wrong. `Deposit.amount` drives two things, both of which are about *capital committed*, not *invested asset value*:

1. `DepositRepository.sumAmountByAccountTypeAndYear` — the IKE/IKZE annual statutory contribution-limit ("wpłata") usage, surfaced as `AccountLimitsDto` / `GET /deposits/account-cap-usage`.
2. `XirrService`'s `totalInvestedPln` / Historia's `total_deposits` — the cash-flow-in series for XIRR, for **both** MAIN and RETIREMENT (`completeSession()` has no portfolio-specific branch in this block; the bug is portfolio-agnostic).

Polish IKE/IKZE law defines the annual limit against how much money you actually transferred into the tax-advantaged wrapper as capital — the target/planned amount you decided to commit — not against however much of that capital survived broker fees and currency-conversion costs to become invested asset value. If the user decides to invest 1000 PLN into an IKZE-wrapped asset and 21 PLN of that is eaten by broker fees and FX slippage on execution, they still contributed 1000 PLN of capital into the wrapper; the fee is a cost incurred *inside* the account, not a shrinkage of the wpłata itself.

**Observed symptom.** A RETIREMENT purchase that should have brought the running annual total to exactly 10 000 PLN instead showed 9 979 PLN in the UI — a ~21 PLN gap that is fee/rounding drag between the planned and actual PLN value of the session's items, wrongly treated by the current code as "the deposit was smaller than planned."

**Reviewed and confirmed not affected.** `RebalancingTransferService.recordTransfer()` (~lines 148–169, ADR-054's auto-deposit for a REGULAR-sold leg funding an IKE/IKZE buy leg) computes its deposit amount as `buy.units() × buy.pricePln()` directly from `BuyLegDto`, which carries a single units+price pair with no separate "planned" field at all — there is no planned/actual distinction in that DTO to get wrong. This flow needs no change.

## Decision

1. In `PurchaseSessionService.completeSession()`'s deposit-sum aggregation block only (the `depositSums` loop, ~lines 379–403), flip the fallback order to `effectiveValuePln = plannedValuePln ?? actualValuePln` — prefer the planned/target PLN value decided upon before execution, falling back to `actualValuePln` only when `plannedValuePln` is itself null (e.g. a hand-added session item that was never planned). This applies uniformly to every group regardless of `portfolio` (MAIN and RETIREMENT both use this code path with no branch between them) and regardless of `accountType` (IKE, IKZE, REGULAR) or `exchange`.
2. The migration-sell netting logic in the same block is unaffected in structure — it still nets a migration-sell item's effective value (now planned-preferred, same as any other item) against its group's buy sum, floored at 0, so the migration-triple's intra-account re-buy still nets to zero and creates no phantom deposit.
3. Block A (cost basis: `AssetPurchase.totalPln`, `positions.total_spent_pln` via `updateTotalSpentPln`) is explicitly **not** changed — it continues to prefer `actualValuePln`, falling back to `plannedValuePln`. This is a deliberate, permanent difference between the two blocks, not a temporary inconsistency to reconcile later: cost basis answers "what did this actually cost", deposit-sum answers "how much capital did I commit to the wrapper", and post-fee execution price genuinely only matters for the former.
4. No DB schema change. `deposits.amount` keeps its existing type/constraints (`NUMERIC(18,4)`, CHECK > 0); only the application-layer computation of the value written into it changes.
5. **Historical backfill is required, not optional.** Every `Deposit` row already auto-created by a past `completeSession()` run under the old (buggy) preference order under-counts (or, in principle, over-counts, if actual ever exceeded planned) capital committed whenever `actualValuePln != plannedValuePln` for at least one contributing item. These rows must be corrected in place so today's IKE/IKZE cap totals and XIRR figures are accurate immediately, not just for sessions completed after the fix ships. See "Backfill spec" below.

## Backfill spec (for the implementing backend agent)

This is a data-correction pass over already-completed sessions, not a new endpoint. It should run once (either as a Flyway versioned migration doing the recomputation in SQL, or as a one-off admin data-correction step invoked by the backend agent — either is acceptable; a Flyway migration is preferred for this repo's convention of "Flyway is the single source of schema/data-truth" and because it leaves an auditable, repeatable record).

**Scope — which `Deposit` rows are affected.** Only rows created by `completeSession()`'s auto-deposit block, identifiable by `comment LIKE 'Auto from session #%'` (the exact format string is `"Auto from session #" + sessionId`, i.e. `Auto from session #<id>` with no further punctuation — parse the trailing integer as the session id). Manually-entered deposits and ADR-054's rebalancing-transfer auto-deposits (comment `"Auto from rebalancing transfer: ..."`) are out of scope — the former were never computed by this code path, the latter's DTO has no planned/actual pair to have gotten wrong (see Context above).

**Note on data model:** `deposits` has no `session_id` column (unlike `binance_transfers`, which does) — the only link back to the originating session is the `comment` text. The backfill must parse the session id out of `comment` to know which session's items to re-aggregate.

**Per affected session, recompute using the exact same shape `completeSession()` uses today, with the fixed preference order:**

1. Load `purchase_session_items` for that `session_id`.
2. Group by `(portfolio, accountType, exchange)` — same key as `completeSession()`, `exchange` derived from each item's `Position` (default `XTB` if the position lookup fails).
3. For each item, compute `effectiveValuePln = plannedValuePln ?? actualValuePln` (the corrected order) and `effectiveUnits = actualUnits ?? plannedUnits`, matching the live code's null-fallback semantics.
4. For non-migration-sell items with `effectiveUnits > 0`: add `effectiveValuePln` to the group sum.
5. For migration-sell items: subtract `|effectiveValuePln|` from the same group's sum (same netting as the live code).
6. Floor each group's sum at 0 — a group that nets to ≤ 0 corresponds to no deposit (this should match a group that today has no corresponding `Deposit` row; if such a group *does* have a row today, that would itself indicate a second, independent bug worth flagging back rather than silently overwriting).
7. Match each recomputed group back to its corresponding existing `Deposit` row via `(portfolio, account_type, exchange)` + the session id parsed from `comment`, and `UPDATE deposits SET amount = <recomputed sum>` for that row. One row should correspond to exactly one recomputed group per session (this is the same grouping key `completeSession()` used to create the row in the first place, so the cardinality should match 1:1; if it doesn't — e.g. a row that no longer nets to a positive amount under the corrected order, or a group with no matching row — treat it as a discrepancy to log and review by hand rather than delete/insert automatically, since this is real financial history for a single-user app and hand review is cheap).
8. Do not touch `deposit_date`, `currency`, `comment`, `created_at`, or any other column — only `amount` is wrong today.

**Out of scope for the backfill:** `AssetPurchase.totalPln`, `positions.total_spent_pln`, `historia` snapshot rows already written under the old (technically-correct, since XIRR/Historia only ever read whatever was in `deposits` at the time) figures. Historia rows will self-correct going forward the next time the nightly scheduler runs (`total_deposits` is recomputed fresh each night from the current `deposits` table, not stored incrementally), so no separate Historia backfill is needed — the corrected `deposits.amount` values are picked up automatically.

## Consequences

- **IKE/IKZE annual cap-usage totals move upward** for any year/account-type combination where at least one contributing session had `actualValuePln < plannedValuePln` on a non-sell item (the common case: fees/slippage reduce actual below planned). This is the fix — the RETIREMENT 9 979→10 000 PLN example is the motivating case. In the rarer case `actualValuePln > plannedValuePln` (e.g. favorable FX movement between planning and execution), the corrected total moves down for that group instead; the direction always follows "capital committed" rather than "value that survived execution."
- **`totalInvestedPln` (XIRR) and Historia's `total_deposits` shift by the same deltas**, for both MAIN and RETIREMENT — this changes historical XIRR/simple-return figures slightly (typically upward, per the fee-drag direction above) once the backfill runs, since `totalInvestedPln` sums the corrected `deposits.amount` directly.
- **No behavior change to cost basis, P&L, or `avg_buy_price`** — Block A is untouched, so `profit_pct`/`deviation_pct`/position values on `/api/positions` are unaffected by this change.
- **No schema change** — `V{n}__` migration (if the backend agent chooses the Flyway route for the backfill) is a data-correction migration only, no `ALTER TABLE`.
- **One-time backfill, not a recurring job** — once corrected, all future sessions are created correctly under the new code, so no ongoing reconciliation process is needed. The discrepancy only exists for `Deposit` rows created before this fix ships.
- **Manually-entered deposits (comment not matching `'Auto from session #%'`) are entirely unaffected** — they were never computed by this buggy code path in the first place.
