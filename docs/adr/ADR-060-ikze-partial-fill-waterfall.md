# ADR-060: Partial IKE/IKZE Fill Waterfall for Purchase-Planning Buy Items

**Date:** 2026-07-27
**Status:** Accepted

## Context

`PurchasePlanningService.processPortfolio`
(`backend/src/main/java/pl/investing/service/PurchasePlanningService.java:115-192`) decides,
per buy-list item, which account (`IKE`, `IKZE`, or fallback) absorbs that item's
`allocatedPln`. Until this ADR, the check was **all-or-nothing against the item's entire
amount**:

```java
if (ikeIkzeEligible && ikeLeft[0].compareTo(item.allocatedPln()) >= 0) {
    // full amount to IKE
} else if (ikeIkzeEligible && ikzeLeft[0].compareTo(item.allocatedPln()) >= 0) {
    // full amount to IKZE
} else {
    // ENTIRE amount falls to the migration triple or a plain REGULAR purchase
}
```

If an item's full `allocatedPln` didn't fit *entirely* in the remaining IKE room, and didn't fit
*entirely* in the remaining IKZE room either, the whole amount fell through — even when IKE or
IKZE still had some partial room left. `05-rebalancing-algorithm.md` §8 documented this
explicitly: "This is an all-or-nothing check per item at the account level — there is no partial
IKE/IKZE fill of the original buy amount."

**Reported symptom.** A real purchase session suggested ~8000 PLN of a Nasdaq ETF into IKZE,
leaving ~3000 PLN of IKZE annual room for the year. The *next* buy-list item — a China ETF —
needed more than 3000 PLN, so neither "fits fully in IKE" (IKE room was already 0 for that
session) nor "fits fully in IKZE" (only 3000 of the needed amount was available) held, and the
**entire** China ETF allocation fell through to a plain REGULAR purchase, wasting the 3000 PLN
of IKZE room that was sitting right there.

The user's own framing of the desired fix: *"over the limit goes to regular"* — i.e. use up
whatever room exists, and only the excess above that room should fall through.

## Decision

Replace the all-or-nothing check with an **ordered, cumulative partial-fill waterfall**,
processed per buy-list item, in this exact sequence:

1. **IKE first.** If `ikeRemaining > 0`:
   `ikeFillPln = min(ikeRemaining, allocatedPln)` — the full amount if IKE alone covers it,
   otherwise every remaining PLN of IKE room (partial). Book one `PurchaseSessionItem` row for
   `ikeFillPln` under `AccountType.IKE` (companion ticker/price substituted per the existing
   `useCompanion` rule, unchanged). `ikeRemaining -= ikeFillPln`.
2. **IKZE next, for whatever's left.** Let `afterIke = allocatedPln - ikeFillPln`. If
   `afterIke > 0` and `ikzeRemaining > 0`:
   `ikzeFillPln = min(ikzeRemaining, afterIke)`, same full-or-partial rule, booked under
   `AccountType.IKZE`. `ikzeRemaining -= ikzeFillPln`.
3. **True remainder falls through unchanged in kind.**
   `remaining = allocatedPln - ikeFillPln - ikzeFillPln`. If `remaining > 0`, it — not the
   original `allocatedPln` — is what feeds the existing fallback: the companion ETF
   migration-sell/buy triple (BTC/ETH/SOL/XRP/Gold) or a plain REGULAR purchase, exactly as
   before, just sized to the true leftover.

**IKE always gets first claim on an item's allocation, even when IKZE alone could have covered
the item in full.** This is a deliberate priority order, not just "try either account": an item
that lands on an IKE-then-IKZE split it would not have gotten under the old check is the
intended new behavior, not a side effect — see the no-op scope below for exactly which cases are
and are not affected.

### No-op guarantee

This is a **no-op versus pre-ADR-060 behavior whenever, for a given item, `ikeRemaining` and
`ikzeRemaining` were each already `0` or already `>= allocatedPln`** — every case that used to
resolve as a clean full-fill-or-skip decision on each account produces the identical single row
it always did (same account, same amount, same ticker). Behavior changes *only* in the
genuinely-partial-room case: `0 < ikeRemaining < allocatedPln` (or the equivalent for IKZE after
IKE's contribution). Concretely, applied to the motivating scenario: China ETF item,
`ikeRemaining = 0`, `ikzeRemaining = 3000`, `allocatedPln = 5000` (illustrative). Step 1 is
skipped (`ikeRemaining = 0`). Step 2 books `ikzeFillPln = min(3000, 5000) = 3000` to IKZE.
`remaining = 2000` falls through to the fallback — sized to 2000, not 5000.

### A correctness hazard this waterfall introduces, and its fix: merge, don't duplicate

The existing migration-triple fallback (for companion-eligible items — BTC/ETH/SOL/XRP/Gold)
*always* books its own step-2 buy under `AccountType.IKZE`, for the **same ticker** the
waterfall's own step 2 would have used for that item (companion ticker when `useCompanion`, the
item's own ticker otherwise — both resolve from the same `useCompanion`/`companion` inputs).

Before this ADR, that was never a problem: the migration triple only ran when *no* IKE/IKZE row
had been booked for the item at all (all-or-nothing meant either one full-amount IKE/IKZE row,
or zero of them). After this ADR, it is possible for **both** to fire for the same item: the
waterfall's step 2 books a partial IKZE row, exhausts `ikzeRemaining`, and a positive `remaining`
still flows into the migration triple — whose own step 2 tries to book a *second* row for the
identical `(portfolio, ticker, IKZE)` tuple. `purchase_session_items` has a unique constraint on
exactly that tuple (`psi_session_portfolio_ticker_account_uq` —
`backend/src/main/java/pl/investing/entity/PurchaseSessionItem.java:14-17`), so a second
`INSERT` for the same tuple within one session is a hard failure, not a silent duplicate.

**Fix: when the waterfall already booked an IKZE row for this item's ticker (`ikzeFillPln >
0`), the migration triple's own step 2 must merge into that existing row instead of adding a
new one** — `plannedValuePln += cappedPln`, with `plannedUnits` recomputed fresh from the new
total PLN over price (not summed as two independently-rounded unit figures, to avoid
double-rounding drift). When `ikzeFillPln == 0` (the pre-existing case: IKE/IKZE room was
already fully exhausted before this item was even reached), step 2 adds a new row exactly as it
always has — this path is unaffected. `PurchaseSessionItem` is a mutable `@Getter @Setter` JPA
entity (not a record), so mutating the already-added row in place is straightforward — the
caller only needs to keep a reference to it instead of discarding the return value of the
`result.add(...)` call the waterfall's step 2 makes.

This hazard cannot arise on the IKE side: the migration triple's sell/buy is hardcoded to
`AccountType.IKZE` throughout (it only ever sells and buys back inside IKZE; the compensating
Binance/REGULAR buy targets `AccountType.REGULAR`, never `IKE`), so an IKE row booked by the
waterfall's step 1 never collides with anything the fallback produces.

### Whole-item accounting identity

For every item, regardless of which branches fire:

```
ikeFillPln + ikzeFillPln + cappedPln + remainder == allocatedPln
```

(`cappedPln`/`remainder` are 0 when the migration triple doesn't fire; the plain-REGULAR
fallback is the degenerate case where `cappedPln = 0` and `remainder = remaining` in full.) This
is the same conservation invariant the migration-triple fix (commit `c5d84b9`) already
established for `cappedPln + remainder == allocatedPln` when there was no prior IKE/IKZE fill;
this ADR extends it to account for the waterfall's own fills consuming part of `allocatedPln`
before the fallback ever sees it.

### Rounding: preserve exact no-op precision

`BuyListItemDto.unitsToBuy` is precomputed as `allocatedPln / price` (see
`BuyListItemDto.java:13`). When a waterfall step's fill amount equals the item's full
`allocatedPln` (the no-op case), reuse `item.unitsToBuy()` directly rather than recomputing via
a fresh division — this guarantees byte-identical output versus the pre-ADR-060 code path, with
zero risk of a rounding-mode/scale mismatch introducing a new discrepancy. Only genuinely
partial fills (`fillPln < allocatedPln`) need a fresh `fillPln.divide(price, 8,
RoundingMode.HALF_UP)` — matching the scale/rounding mode already used everywhere else in this
file.

### Cross-portfolio interaction (RETIREMENT → MAIN) is unchanged

`ikeLeft`/`ikzeLeft` are already shared, mutable arrays threaded through both the RETIREMENT and
MAIN passes of `computePlan` (RETIREMENT processed first, matching the spreadsheet order). This
was already true before ADR-060: a RETIREMENT item's full IKE/IKZE consumption already reduced
what a later MAIN item could claim. ADR-060 doesn't change that ordering or sharing — it only
changes what happens *within* a single item's own account-priority decision. A RETIREMENT item
can now leave a partial `ikeLeft`/`ikzeLeft` behind (instead of always leaving either the full
amount or nothing), and a later MAIN item's own waterfall will correctly pick up wherever that
partial room was left off, same as it always picked up a fully-exhausted or fully-untouched
balance.

## Alternatives considered

- **IKZE-first, or "whichever account has more room" priority.** Rejected — not what the user
  asked for. The user's confirmed ordering is IKE first, unconditionally, then IKZE for the
  remainder. IKE and IKZE have different statutory contribution limits and different withdrawal
  rules in Polish law; consistently prioritizing one over the other (rather than a
  room-dependent heuristic) keeps the algorithm predictable and auditable, and matches how the
  spreadsheet-era manual process already worked.
- **Cap the waterfall at "fill only if the resulting row would be above some minimum PLN
  threshold" (to avoid tiny dust rows).** Rejected — out of scope for this fix, adds a magic
  number with no stated requirement behind it, and the existing migration-triple `cappedPln`
  logic already tolerates small amounts without a floor. Can be revisited later if dust rows
  turn out to be a real UX annoyance.
- **Leave the migration-triple collision unresolved and simply skip step 2 entirely whenever
  `ikzeFillPln > 0`** (i.e. never top up further once the waterfall touched IKZE for this item).
  Rejected — it would silently under-fill the migration triple's own contribution (the ETF-sale
  freed capacity would go unused for this item's own buy), which is a correctness regression the
  merge approach avoids entirely while still honoring the unique-constraint requirement.

## Consequences

- Buy-list items that used to fall through to REGULAR (or the migration triple) in full now
  correctly use up any partial IKE/IKZE room first — the reported Nasdaq/China-ETF scenario is
  fixed.
- No behavior change for the two pre-existing clean cases (full fit on one account, or zero room
  on both) — verified by the no-op guarantee above.
- `PurchasePlanningService` must track, per item, whether its waterfall step booked an IKZE row
  (and a reference to that row) so the migration triple's own step 2 can merge into it rather
  than violate `psi_session_portfolio_ticker_account_uq`. This is new state the implementation
  didn't need before (previously the two code paths were mutually exclusive by construction).
- `ikeIkzeBuyItem` (and the migration triple's own item-builder calls) must accept a **fill
  amount** parameter distinct from `item.allocatedPln()` — every call site sizing a
  `PurchaseSessionItem` off the item's DTO directly instead of the actual fill/remaining amount
  is now a bug.
- Regression tests must cover: (a) a plain (non-companion) item split across IKZE + REGULAR when
  only partial IKZE room remains (the reported scenario); (b) a companion-eligible item where
  the waterfall's partial IKZE fill and the migration triple's own IKZE buy must merge into one
  row, not two; (c) both pre-existing full-fill and zero-room cases remain byte-identical.
