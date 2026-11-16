# ADR-058: Manual Correction for IKE/IKZE Cap-Usage Figures — Additive Adjustment, Not Override

**Date:** 2026-07-27
**Status:** Accepted

## Context

The app already surfaces how much of the statutory IKE/IKZE annual contribution limit is
left, computed independently in two places:

1. `DepositService.getAccountCapUsage(year)` (`backend/src/main/java/pl/investing/service/DepositService.java`,
   ~lines 81–110) — sums `deposits.amount` for `accountType ∈ {IKE, IKZE}` in the given year
   (`DepositRepository.findAllByAccountTypeAndYear`), reads the cap from `settings`
   (`ike_annual_limit_pln` / `ikze_annual_limit_pln` via `SettingService`), and returns
   `AccountCapUsageItem(accountType, year, totalDepositedPln, annualCapPln, remainingPln,
   utilizationPct)` via `GET /deposits/account-cap-usage`. `remainingPln = annualCapPln −
   totalDepositedPln`, **not** floored at zero.
2. `PurchaseSessionService.computeLimits(year)` (~lines 575–588) — same cap read, but sums
   deposits via `DepositRepository.sumAmountByAccountTypeAndYear` and returns
   `AccountLimitsDto(ikeUsedPln, ikeRemainingPln, ikzeUsedPln, ikzeRemainingPln,
   ikeAnnualLimitPln, ikzeAnnualLimitPln)`, embedded in every purchase-session response and
   consumed by `PurchasePlanningService` to cap how much of a session's budget can be
   allocated to IKE/IKZE. `ikeRemainingPln`/`ikzeRemainingPln` **are** floored at zero
   (`.max(BigDecimal.ZERO)`) — a purchase plan must never be handed a negative IKE/IKZE
   budget.

This is already a duplicated computation (same cap source, same deposit sum, two independent
`BigDecimal` arithmetic call sites with one small, presumably-accidental behavioral
divergence — the zero-floor). The statutory annual cap itself
(`ike_annual_limit_pln`/`ikze_annual_limit_pln`) is already user-editable via the generic
`PUT /settings/{key}`, and that is **not** what's being changed here.

**The new problem:** the user's own bookkeeping — `deposits` rows entered by hand or
auto-created by `completeSession()`/`RebalancingTransferService` — can drift from the real
broker- and tax-office-tracked remaining limit by a few grosze, from rounding accumulated
over many small transactions across a year. The user wants to correct the *computed*
remaining/used figure for a given `(accountType, year)`, without touching the statutory cap
and without hand-editing or backfilling `deposits` rows to force the sum to match (which would
falsify the transaction history for a rounding artifact).

**UX revision (2026-07-27, same day):** the first pass of this feature exposed a signed
correction delta (`adjustmentPln`) plus an optional note directly in the API and UI — the user
would have had to compute "how far off is the app" themselves and type the difference. Feedback
during implementation was direct: *"I should just be able to edit how much deposits I have
left — this should auto adjust real total IKE/IKZE deposits number. That's all — I just type
3000, 3000 PLN deposits is left. The correction thing is unnecessary."* The API and UI were
revised accordingly (see §5) — the user types the real remaining amount they know is correct,
and the backend solves for the internal delta. The underlying storage mechanism (§1–§4,
additive delta persisted in `account_cap_adjustment`) is unchanged and still the right
implementation — only the *input* changed from "type the correction" to "type the answer."
The `note` column was dropped for the same simplicity reason and never shipped.

## Decision

### 1. Additive adjustment, not an overridden final value

Store a small **signed additive PLN correction**, keyed by `(account_type, year)`, applied on
top of the computed total at read time — not a directly-overridden `remainingPln`.

**Why additive wins over override:**

- **Survives new deposits.** If the user records a new IKE deposit in October for the year
  they corrected in August, an override of the final `remainingPln` would immediately be
  wrong (it doesn't know about the new deposit) and would need to be manually re-entered every
  time a new deposit lands for that year. An additive correction keeps applying on top of
  whatever the raw sum currently is — no re-entry needed.
- **Survives statutory cap edits.** If `ike_annual_limit_pln` changes (a new year's limit is
  published, or a past year's is corrected via `PUT /settings/{key}`), an override baked
  against the old cap goes stale silently. An additive correction is cap-agnostic — it
  corrects the *deposit-sum* side of the arithmetic, not the final number.
- **Stays auditable.** The raw computed total (what the app's own bookkeeping says) remains
  visible alongside the correction, so the user (or a future debugging session) can always see
  "the app computed X, a Y PLN correction was applied, so Z is what's shown" — rather than a
  single opaque final number with no way to tell how far off the raw computation was.

An override was considered and rejected for exactly the staleness reasons above — it is the
one shape that *looks* simpler but breaks the moment either input it depends on changes, which
for `remainingPln` is not a rare edge case, it is the normal case (deposits are added
continuously through the year).

### 2. New table: `account_cap_adjustment`

One row per `(account_type, year)` that has ever been corrected. **Absence of a row is the
common case and means "no correction" (`adjustment_pln = 0`)** — the table starts empty and
most `(account_type, year)` pairs will never have a row.

```sql
CREATE TABLE account_cap_adjustment (
    id             BIGSERIAL          PRIMARY KEY,
    account_type   account_type_enum  NOT NULL,
    year           INTEGER            NOT NULL,
    adjustment_pln NUMERIC(12,2)      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ        NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ        NOT NULL DEFAULT NOW(),
    CONSTRAINT account_cap_adjustment_type_check CHECK (account_type IN ('IKE', 'IKZE')),
    CONSTRAINT account_cap_adjustment_year_range CHECK (year >= 2021 AND year <= 2100),
    CONSTRAINT uq_account_cap_adjustment_type_year UNIQUE (account_type, year)
);
```

(No `note` column — dropped in the UX revision above; the correction is purely a number.)

- `adjustment_pln NUMERIC(12,2)` — same precision as `mexc_deposit.amount_pln`, the codebase's
  existing precedent for "small, hand-entered PLN amounts." **Signed** (positive or negative;
  no `CHECK ... >= 0`) since the drift can go either direction — the app's bookkeeping can be
  ahead of or behind the broker's true figure.
- `account_type` is restricted to `IKE`/`IKZE` at the DB level, not just the API layer —
  `REGULAR` has no statutory annual cap at all (`settings` has no
  `regular_annual_limit_pln`), so a `REGULAR` row here would be meaningless data, not just an
  unusual one.
- `year` reuses the `2021–2100` range already used by `tax_returns_year_range` — IKE/IKZE
  statutory-limit tracking in this app starts at 2021, same as PIT tracking.
- `UNIQUE (account_type, year)` is both the natural key and the only lookup pattern this table
  ever needs (upsert by that key, read by that key) — no separate index, same "tiny table,
  functional index would be over-engineering" reasoning already applied to `mexc_deposit` /
  `DepositRepository`'s year-extraction queries in `04-db-schema.md`.
- `updated_at` maintained by the existing shared `set_updated_at()` trigger function (defined
  V1, already reused by `positions`/`tax_returns`/`settings`/`purchase_sessions`/
  `purchase_session_items`/`asset_news`/`macro_observations`/`macro_state`) — no new trigger
  function needed.

Full migration: `backend/src/main/resources/db/migration/V46__account_cap_adjustment.sql`.

### 3. Application formula

For a given `(accountType, year)`:

```
totalDepositedPln  = SUM(deposits.amount) WHERE account_type = ? AND year(deposit_date) = ?   -- unchanged, raw, auditable
annualCapPln       = settings.{ike,ikze}_annual_limit_pln                                       -- unchanged
adjustmentPln      = account_cap_adjustment.adjustment_pln WHERE (account_type, year) = (?, ?)  -- 0 if no row
effectiveUsedPln   = totalDepositedPln − adjustmentPln
remainingPln        = annualCapPln − effectiveUsedPln   (== annualCapPln − totalDepositedPln + adjustmentPln)
utilizationPct      = effectiveUsedPln / annualCapPln × 100   (0 if annualCapPln == 0, same guard as today)
```

`totalDepositedPln` (`AccountCapUsageItem`) / `ikeUsedPln`/`ikzeUsedPln` (`AccountLimitsDto`)
**stay raw** — the correction is never folded back into the deposit-derived figure, so the
user can always see what the app's own bookkeeping computed versus what was corrected.
`utilizationPct` is now computed off `effectiveUsedPln`, not the raw total, so it stays
arithmetically consistent with `remainingPln` (`remainingPln + effectiveUsedPln ==
annualCapPln`, modulo the zero-floor below).

**The existing zero-floor divergence between the two call sites is preserved, deliberately,
as the one intentional difference layered on top of one shared computation** (see §4):
`AccountCapUsageItem.remainingPln` stays **unclamped** — a negative value is meaningful
information ("you've over-contributed by X PLN this year," which the raw arithmetic can
legitimately produce even before this ADR, e.g. from a manual over-limit deposit) and hiding
it behind a zero-floor would be a regression for the one place this is purely informational
display. `AccountLimitsDto.ikeRemainingPln`/`ikzeRemainingPln` stay **clamped at zero** at the
`PurchaseSessionService` call site — that figure feeds a real budget allocation and must never
hand `PurchasePlanningService` a negative number to plan against.

### 4. Extract the shared computation

**Recommendation: yes, extract.** Both call sites already compute the same three inputs (cap
from settings, deposit sum, and now the adjustment) and would otherwise implement the same new
join/lookup twice — the exact kind of duplication that let the zero-floor divergence appear
unnoticed in the first place. Introduce one shared method (e.g. a new `AccountCapUsageService`
or a method added to `SettingService`/a small new component — left to the backend agent's
judgment) with a signature along the lines of:

```java
record CapUsage(BigDecimal totalDepositedPln, BigDecimal annualCapPln,
                 BigDecimal adjustmentPln, BigDecimal remainingPln, BigDecimal utilizationPct) {}

CapUsage computeCapUsage(AccountType accountType, int year); // unclamped remainingPln
```

`DepositService.getAccountCapUsage` calls this once per account type and maps directly to
`AccountCapUsageItem` (unclamped). `PurchaseSessionService.computeLimits` calls the same
method and applies `.max(BigDecimal.ZERO)` to `remainingPln` at its own call site before
building `AccountLimitsDto` — preserving today's behavior exactly, but now as one visible,
intentional clamp at the one call site that needs it, instead of two independently-drifting
implementations. No reason found not to do this — it is a pure de-duplication with no
behavioral trade-off once the clamp difference above is preserved explicitly.

### 5. API surface (full detail in `03-api-contract.md`) — revised per the UX feedback above

- `AccountCapUsageItem` and `AccountLimitsDto` are **unchanged in shape** from before this ADR
  — no `adjustmentPln`/`adjustmentNote`/`ikeAdjustmentPln`/`ikzeAdjustmentPln` fields. The
  correction is invisible at the read DTO level; `remainingPln`/`ikeRemainingPln`/
  `ikzeRemainingPln` simply already reflect it, because they're computed from
  `effectiveUsedPln` (§3) regardless of whether a correction row exists.
- New endpoint `PUT /deposits/account-cap-usage/adjustment` — request body is
  `{ accountType, year, remainingPln }`: the user types the real remaining limit they know is
  correct (e.g. read off a broker statement), **not** the signed delta. The backend solves
  `adjustment_pln = remainingPln − annualCapPln + totalDepositedPln` (the algebraic inverse of
  §3's `remainingPln` formula) and persists only that delta — the request/response never carry
  a raw `adjustmentPln` value at all.
- Response is the freshly-recomputed `AccountCapUsageItem` for that `(accountType, year)` —
  reusing the existing read DTO rather than a bespoke response type, since there is nothing
  correction-specific left to expose once `adjustmentPln` isn't part of the contract. Its
  `remainingPln` equals exactly the `remainingPln` the caller just submitted, by construction.
- No new `GET` for the correction alone — it round-trips through the two existing read
  endpoints (`GET /deposits/account-cap-usage`, and embedded in every purchase-session
  response), which already show the corrected `remainingPln` directly — no separate "corrected
  by X PLN" annotation needed, since the corrected figure *is* the displayed figure.

## Consequences

- **One new tiny table**, empty by default, that only ever needs an upsert-by-natural-key
  write path (`INSERT ... ON CONFLICT (account_type, year) DO UPDATE`) and two read joins.
- **`AccountCapUsageItem` and `AccountLimitsDto` keep their existing shape** — no new fields,
  per the UX revision above. `remainingPln`/`ikeRemainingPln`/`ikzeRemainingPln` simply already
  incorporate any correction, so no client change is needed to see it.
- **The pre-existing zero-floor divergence between the two call sites is made intentional and
  documented**, rather than silently continuing as an unexplained inconsistency — see §3.
- **Extracting the shared computation is recommended and should happen in the same backend
  change** that adds the adjustment table — doing so *after* would mean writing the adjustment
  join twice first and de-duplicating later, strictly more work for the same end state.
- **The statutory annual cap (`settings.ike_annual_limit_pln`/`ikze_annual_limit_pln`) is
  completely untouched by this feature** — it remains the only thing `PUT /settings/{key}`
  edits; this ADR adds a second, narrower correction on top of the *usage* side of the
  arithmetic, not the cap side.
