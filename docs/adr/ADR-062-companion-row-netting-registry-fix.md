# ADR-062: Companion-Row Netting Registry — Fix for the ADR-061 Same-Tuple Collision

**Date:** 2026-07-28
**Status:** Accepted — patches a live production bug in code shipped earlier today (ADR-061,
implemented in `PurchasePlanningService.java`)

## Context

`POST /purchase-sessions` is throwing in production:

```
ERROR: duplicate key value violates unique constraint "psi_session_portfolio_ticker_account_uq"
Detail: Key (session_id, portfolio, ticker, account_type)=(56, MAIN, PL.ETNVIRXRP, IKE) already exists.
```

Deterministic given current portfolio data (identical failure on session IDs 56 and 57), not a
race condition.

### Root cause, confirmed against the current code

Read in full: `PurchasePlanningService.processPortfolio` (steps 1–5), `applyCryptoSellStep`,
`bookOrMergeSellRow`, `findBestCryptoDonor`, and ADR-061 §4 and §7.

ADR-061 §4's self-donation exclusion (`excludeParentTicker = item.ticker()`, passed from
`applyCryptoSellStep` into `findBestCryptoDonor`) only stops the item **currently being
processed** from selling its own companion to fund itself. It does not stop a **different**
buy-list item's donor-sell from picking that companion as a donor when the companion's own owner
is *also* a buy-list item that independently books its own recipient row for the identical
`(portfolio, ticker, accountType)` tuple via steps 1/3's ordinary new-deposit fill
(`ikeIkzeBuyItem` → `plainItem(companion.getTicker(), ...)`, `PurchasePlanningService.java:207–209`
and `:228–230`).

Concretely, with `XRP-USD` in the buy list (companion `PL.ETNVIRXRP`, `PurchasePlanningService.java:
179–191` computes `useCompanion = true`): if `XRP-USD` is processed while `ikeLeft[0] > 0`, step 1
(`:202–210`) creates a plain row for `PL.ETNVIRXRP`/IKE (`isMigrationSell = false`) and adds it to
`result` directly — it never registers with `sellRowsByKey`. Separately, if some *other* buy-list
item `Y` reaches its own step 2/4 and `findBestCryptoDonor` ranks `PL.ETNVIRXRP` as best donor for
that same account (`Y.ticker() != "XRP-USD"`, so `excludeParentTicker` doesn't fire),
`bookOrMergeSellRow` (`:311–331`) checks `sellRowsByKey` — finds nothing there, because step 1's
write never went through it — and inserts a **second**, independent row for the identical tuple
(`isMigrationSell = true`). Two `INSERT`s for `(session_id, MAIN, PL.ETNVIRXRP, IKE)` → constraint
violation. This is symmetric in processing order: if `Y` is processed first, its sell row is
created and tracked in `sellRowsByKey`, but then `XRP-USD`'s own step 1 *still* writes an
unconditional fresh `plainItem` row without ever consulting `sellRowsByKey` — same collision,
opposite order.

This is a **sixth collision case**, missing from ADR-061 §7's table (which covers (a) same-item
new-deposit↔sell-buy merge via `ikeRowRef`/`ikzeRowRef`, (b) the IKZE equivalent, (c) two different
recipients drawing the same donor via `sellRowsByKey`, (d) reconciliation merging into an existing
REGULAR row, (e) an item donating to itself, guarded by exclusion). None of (a)–(e) cover: *a
companion ticker's own owner's step-1/3 recipient row colliding with a donor-sell of that same
companion, initiated by a different item, in either processing order.*

Diagnosis confirmed as stated in the bug report — no correction needed.

## Decision

### Chosen fix: a unified, signed-netting companion-row registry — scoped to companion tickers only

Two candidate directions were considered.

**Rejected: broadening the exclusion filter (reserve every crypto item's own companion ticker,
portfolio-wide, from ever donating to a different item).** This is unsafe, not just conservative —
it breaks ADR-061's own worked example. In that example `ikeLeft = 0` from the start and `ikzeLeft`
is fully consumed by `US-ETF` (item 1) before `BTC-USD` (item 2, the owner of the donor `BTC-ETP`)
is even reached — `BTC-USD`'s own step 1/3 never fires, so under a portfolio-wide upfront
reservation `BTC-ETP` would be excluded from `US-ETF`'s donor search too, and `US-ETF` would get
`IKE/US-ETF = 0` and a smaller `IKZE/US-ETF` instead of the ADR's stated `1000`/`1500`. The
worked example's entire point — an over-target crypto sitting in IKE/IKZE funds a *different*
item precisely because its own owner never claims that room — is exactly the case this exclusion
would forbid. Rejected outright, not adopted with caveats.

**Chosen: a scoped version of the unified netting registry.** Not the full rewrite of
`ikeRowRef`/`ikzeRowRef`/`mergeSellPlnIntoRow` — those already work correctly for same-item
merges and for non-companion tickers (a non-companion ticker can never be a donor candidate,
since `findBestCryptoDonor`'s candidates are always `p.isCompanionRow()`, so it can never
collide this way — no need to touch that path). The registry is needed **only** for the two write
paths that can legitimately target the *same* `(portfolio, accountType, companionTicker)` tuple
from *different* buy-list items: a companion's own owner's step-1/2/3/4 recipient row, and any
item's donor-sell of that companion. This is materially smaller than a full generalized rewrite —
fast enough to ship today — while still being correct under arbitrary processing order, which a
static exclusion cannot be.

This mirrors the reasoning ADR-061 §4 already used to reject differentiating the unique
constraint for self-donation: two write attempts for the same real-world instrument, in the same
account, in the same session, should net to the true final position, not fight over which one
gets the database row.

### Implementation

**1. Repurpose `sellRowsByKey` → `companionRowsByKey`.** Same key shape (`SellKey(Portfolio,
AccountType, String)`, unchanged, still also used for `sellableCryptoPln`'s donor-balance
tracking, which is untouched by this fix). The map's value now holds **any** row booked against a
companion ticker — buy or sell — not just sells. Threaded through `processPortfolio` exactly as
`sellRowsByKey` was.

**2. New method, replaces `bookOrMergeSellRow`:**

```java
/**
 * Book a signed PLN delta against the shared companion-ticker row registry. Every write to
 * a companion ticker's (portfolio, accountType) row goes through here — whether it's that
 * companion's own owner's step-1/2/3/4 recipient buy (positive delta) or a donor-sell of
 * that companion by any item (negative delta). Two writes to the identical tuple, in EITHER
 * processing order, net into at most one row; a net-zero result removes any row already
 * booked and emits nothing (ADR-062, closing ADR-061 §7's missed 6th collision case).
 */
private PurchaseSessionItem upsertCompanionRow(
        SellKey key, AccountType accountType, BigDecimal signedDeltaPln,
        BigDecimal tickerPricePln, Map<SellKey, PurchaseSessionItem> companionRowsByKey,
        List<PurchaseSessionItem> result) {

    PurchaseSessionItem existing = companionRowsByKey.get(key);
    BigDecimal priorSigned = BigDecimal.ZERO;
    if (existing != null) {
        priorSigned = existing.isMigrationSell()
                ? existing.getPlannedValuePln().negate()
                : existing.getPlannedValuePln();
        result.remove(existing);
        companionRowsByKey.remove(key);
    }

    BigDecimal newSigned = priorSigned.add(signedDeltaPln);
    if (newSigned.compareTo(BigDecimal.ZERO) == 0) {
        return null; // fully netted — nothing to book
    }

    BigDecimal absPln = newSigned.abs();
    BigDecimal units = absPln.divide(tickerPricePln, 8, RoundingMode.HALF_UP);
    PurchaseSessionItem row = newSigned.compareTo(BigDecimal.ZERO) < 0
            ? migrationSellItem(key.companionTicker(), key.portfolio(), accountType, absPln, units.negate(), tickerPricePln)
            : plainItem(key.companionTicker(), key.portfolio(), accountType, absPln, units, tickerPricePln);

    companionRowsByKey.put(key, row);
    result.add(row);
    return row;
}
```

`plannedValuePln` is always stored as a positive magnitude in both `plainItem` and
`migrationSellItem` today (confirmed by reading `PurchaseSessionItem`'s builders) — sign lives
only in `isMigrationSell`/`actualUnits`. Reconstructing `priorSigned` from `(isMigrationSell,
plannedValuePln)` is correct regardless of how many times a row has already been netted.

**3. Call sites — steps 1 and 3 (`processPortfolio`, currently lines 202–210 and 223–231).**
Branch on `useCompanion`: when `false`, keep the existing direct `ikeIkzeBuyItem` +
`result.add` unchanged (no collision risk, no registry). When `true`, route through
`upsertCompanionRow` with a **positive** delta:

```java
// Step 1, useCompanion branch:
BigDecimal companionPricePln = resolveCompanionPricePln(companion, pricePlnMap);
ikeRowRef = upsertCompanionRow(
        new SellKey(portfolio, AccountType.IKE, companion.getTicker()),
        AccountType.IKE, ikeFillPln, companionPricePln, companionRowsByKey, result);
// (else branch: useCompanion == false — unchanged, ikeIkzeBuyItem + result.add)
```

Same shape for step 3 against IKZE. Note `ikeRowRef`/`ikzeRowRef` can now legitimately come back
`null` (fully netted against a pre-existing donor-sell) — this is fine; step 2/4 below already
handle a `null` `rowRef` by creating fresh via the registry.

**4. Call site — `applyCryptoSellStep` (currently lines 263–302).**

- **Donor-side write** (currently `bookOrMergeSellRow(donorKey, ...)`, line 283): replace with
  `upsertCompanionRow(donorKey, accountType, sellPln.negate(), donorPricePln,
  companionRowsByKey, result)`. This also subsumes ADR-061 §7 case (c) — two different
  recipients drawing the same donor still accumulate correctly (negative delta onto an
  existing negative `priorSigned`), same result as today's `bookOrMergeSellRow`.
- **Recipient-side write** (currently lines 285–296, the `if (rowRef != null) mergeSellPlnIntoRow
  (...) else ikeIkzeBuyItem(...)` block): branch on `useCompanion`. When `false`, this logic is
  **unchanged** — non-companion tickers can't collide, so the existing local
  `rowRef`/`mergeSellPlnIntoRow` mechanism stays exactly as ADR-060/061 left it. When `true`,
  replace with `upsertCompanionRow(recipientKey, accountType, sellPln, companionPricePln,
  companionRowsByKey, result)` where `recipientKey = new SellKey(portfolio, accountType,
  companion.getTicker())` — this naturally handles both "first write for this item" and "merge
  into this item's own row from an earlier step" (case (a)/(b)), since `upsertCompanionRow`
  already looks up any existing entry.

**5. `mergeSellPlnIntoRow` and `bookOrMergeSellRow`.** `mergeSellPlnIntoRow` stays, but its call
sites narrow to the `useCompanion == false` path only (a non-companion recipient's own row can
only ever be extended by that same item's own sell steps — no cross-item writer exists for it).
`bookOrMergeSellRow` is deleted, fully replaced by `upsertCompanionRow`.

**6. `findBestCryptoDonor` and self-exclusion (§4) — unchanged.** Donor *selection* (deviation
ranking, tiebreak, self-exclusion via `excludeParentTicker`) is untouched; this fix operates
entirely in the write/booking layer, not selection. Self-exclusion in particular is **deliberately
kept**, not removed, even though the new registry would technically absorb a self-donation as a
double-netted no-op (sell delta then an equal-and-opposite recipient delta on the identical key
cancel out) — removing it would let step 4 fire in ADR-061's own worked example (where item 2's
IKZE self-sell is currently blocked only by this exclusion, per the walkthrough's explicit "item 2
was barred from touching it by self-exclusion" note), changing the worked example's donor-depletion
result and reintroducing the exact self-sell/self-buy churn ADR-061 §4 rejected on cost grounds.
Out of scope for a fix that must only close the missed case.

### Why this is still economically correct, not just collision-avoiding

The recipient-side accounting for whichever item is actually *funded* by a sell (`applyCryptoSellStep`'s
own-item booking, `owedCompensatingBuyPln`) is completely unaffected by how the *donor's own* row
happens to net against its owner's unrelated new-deposit fill — those are two independent
bookkeeping paths that only share the donor ticker's row presentation. A worked trace (see
regression test below) confirms whole-family exposure is still conserved exactly: the netted IKE
row plus the reconciled REGULAR compensating buy still sum to the donor-family's own
`allocatedPln`, even though the IKE row alone no longer shows that family's gross new-deposit
amount.

## Regression test — required, reproduces the exact production collision

Add to `PurchasePlanningServiceTest.java`, same fixture-builder conventions as the existing tests
(`parent`, `companion`, `nonCryptoParent`, `buyItem`):

**Fixture:**
- `xrp = parent("XRP-USD", "XRP-USD", Exchange.BINANCE)`
- `xrpEtp = companion("PL.ETNVIRXRP", "XRP-USD", "ETNVIRXRP.WA", unitsIke=new BigDecimal("20"), unitsIkze=BigDecimal.ZERO)` — 20 units × 100 PLN price = 2000 PLN genuinely sellable on IKE, independent of and larger than either item's own need.
- `usEtf = nonCryptoParent("US-ETF", "US-ETF.WA", AssetClass.ETF, Exposure.EQUITY)`
- `mainList = List.of(buyItem("XRP-USD", 500.00, -0.30), buyItem("US-ETF", 800.00, -0.20))` — **order matters**: `XRP-USD` (the companion's own owner) must process first so its step 1 claims real IKE new-deposit room *before* `US-ETF`'s step 2 tries to sell the same companion.
- `pricePlnMap`: `"XRP-USD" -> 4.00`, `"ETNVIRXRP.WA" -> 100.00`, `"US-ETF.WA" -> 100.00`
- `ikeRemaining = 500.00`, `ikzeRemaining = 0.00`

**Trace (this is what closing the case must produce, not what today's broken code produces):**

Item 1 (`XRP-USD`, `allocatedPln=500`): step 1 fires (`ikeLeft=500>0`), `ikeFillPln=500`,
`ikeLeft→0`, `remaining→0`. Registers a **buy** row `PL.ETNVIRXRP`/IKE = 500 via
`upsertCompanionRow` (registry was empty). Steps 2–5 skip (`remaining=0`).

Item 2 (`US-ETF`, `allocatedPln=800`): step 1 skips (`ikeLeft=0`). Step 2: `findBestCryptoDonor`
picks `PL.ETNVIRXRP` (only candidate, not self-excluded, live balance 2000 — unaffected by item
1's step 1, which was a new-deposit write, not a sell). `sellPln=min(800,2000)=800`. Donor-side
`upsertCompanionRow` nets `priorSigned(+500) + (-800) = -300` → replaces the buy row with a
**sell** row `PL.ETNVIRXRP`/IKE = 300 (magnitude), `isMigrationSell=true`. Recipient-side (US-ETF,
`useCompanion=false`, unaffected by this fix): fresh plain row `US-ETF`/IKE = 800.
`owed[(MAIN,"XRP-USD")] += 800`. `remaining→0`; steps 3–5 skip.

Reconciliation: no existing REGULAR row for `XRP-USD` → new standalone row `XRP-USD`/REGULAR =
800, `isMigrationBuy=true`.

**Required assertions (3 rows, no exception thrown — the collision itself no longer occurs):**
- Exactly one row for `(MAIN, "PL.ETNVIRXRP", IKE)`: `isMigrationSell=true`,
  `plannedValuePln = 300.00`, `plannedUnits = 3.00000000`.
- One row `(MAIN, "US-ETF", IKE)`: `plannedValuePln = 800.00`, not a migration row.
- One row `(MAIN, "XRP-USD", REGULAR)`: `plannedValuePln = 800.00`, `isMigrationBuy=true`.
- Conservation: `US-ETF` total = 800 = its own `allocatedPln`. `XRP-USD` family total = REGULAR
  buy (800) − PL.ETNVIRXRP net sell (300) = 500 = its own `allocatedPln`. Session total new
  capital = `ikeFillPln`(item 1, 500) + REGULAR total after reconciliation (800) = 1300 = sum of
  both items' `allocatedPln`.

Before this fix, step 2's donor-write for item 2 throws no in-process exception (this is a
unit-test-observable duplicate-row bug, not a caught exception) — `plan` would contain **two**
separate rows for `(MAIN, "PL.ETNVIRXRP", IKE)`. Assert `plan.stream().filter(i ->
"PL.ETNVIRXRP".equals(i.getTicker()) && i.getAccountType() == AccountType.IKE).count() ==
1` explicitly, since that is the exact invariant the production unique constraint enforces.

## Confirmation: ADR-061's worked example and its existing test are unaffected

Traced by hand end-to-end against the fix (full mechanical trace, not asserted by inspection):
in that fixture, `BTC-USD`'s own step 1/3 never fires at all — `ikeLeft=0` from the start, and
`ikzeLeft` is fully consumed by `US-ETF` (item 1) before `BTC-USD` (item 2) is reached. Since
`upsertCompanionRow` is only invoked by a companion's *own* recipient-side write when that item's
step 1/2/3/4 actually fires for that ticker, `PL.ETNVIRBTCP`'s registry entry is written **only**
by `US-ETF`'s two donor-sell calls (step 2 selling 1000 IKE, step 4 selling 500 IKZE) — each into
an *empty* registry slot, once per account, no netting against any pre-existing entry. The result
is byte-identical to today: `IKE/US-ETF = 1000`, `IKZE/US-ETF = 1500`,
`REGULAR/BTC-USD = 4000` (merged, `isMigrationBuy=true`). `workedExample_ikeIkzeCryptoDonorWaterfall_matchesAdr061Walkthrough`
requires no changes and passes as-is. Likewise hand-traced and confirmed unaffected: `migrationTriple_cryptoCompanion_noDoubleBuyAndNoIllegalIkzeCrypto`,
`migrationTriple_exactCover_noMigrationBuyEmitted`,
`waterfallPartialIkzeFill_mergesWithMigrationTripleIkzeBuy_singleRowNotTwo` (its own-item
IKZE merge, `200 + 500 = 700`, now flows through `upsertCompanionRow` instead of
`mergeSellPlnIntoRow` but produces the identical merged total since no other item ever touches
`PL.ETNVIRBTCP`'s registry slot in that fixture), and `goldAsRecipientOfCryptoDonor_neverDonatesItsOwnCompanion`
(Gold is always `useCompanion=false`, so its own row never touches the registry; only the single
donor-sell write does). None of the existing fixtures have both (a) a companion ticker's own
owner claiming real new-deposit room, and (b) a *different* item also drawing that same
companion as a donor on the same account — that combination is exactly what ADR-062's regression
test adds, and is the only scenario this fix changes behavior for.

## Known limitation, flagged not hidden

When a companion-ticker row nets to a **positive** (buy) total after absorbing a smaller
pre-existing sell from a different item (the mirror image of this ADR's own example, not exercised
by its regression test), `upsertCompanionRow` always books it as a plain row
(`isMigrationBuy=false`), even though part of its magnitude technically absorbed a donor-sell
that would otherwise have needed reconciliation accounting. This does not affect PLN/unit
correctness or `owedCompensatingBuyPln` (that bookkeeping is entirely independent of how the
donor's own row nets, as shown above) — it only means the trade-history chart classification
(ADR-057) could label that specific row `DCA_BUY` rather than `MIGRATION_BUY` in a case that isn't
reachable by either existing fixture or this ADR's own regression scenario. Not fixed here since it
doesn't reproduce the reported bug and doesn't affect a real dollar figure; worth a follow-up ADR
only if it's ever observed to matter in practice.

## No schema/Flyway change required

Pure application-logic fix, same as ADR-060/061 — the unique constraint is satisfied by
construction once every companion-ticker write goes through the shared registry.
