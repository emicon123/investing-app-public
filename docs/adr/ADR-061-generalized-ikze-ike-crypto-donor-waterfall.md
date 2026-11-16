# ADR-061: Generalized IKE/IKZE Crypto-Donor Capacity Waterfall

**Date:** 2026-07-28
**Status:** Accepted — implemented 2026-07-28. A gap in §4/§7's collision analysis (a 6th
case, missed here) caused a same-day production `psi_session_portfolio_ticker_account_uq`
violation; fixed by **ADR-062**, which amends the write-layer mechanics in §2/§3 below
(donor-selection logic — §3's ranking/tiebreak, §4's self-exclusion — is unchanged). Read
ADR-062 alongside this document. **Owner decision 2026-11-09 (task 192):** the donor ranking in §3 is a
fixed order, `PL.ETNVIRBTCP` > `PL.ETNVIRETH` > `PL.ETNVIRSOL` > `PL.ETNVIRXRP` > other crypto wrappers
alphabetically, with no deviation input; the deviation ranking described in §3 is superseded.

## Context

`PurchasePlanningService` (`backend/src/main/java/pl/investing/service/PurchasePlanningService.java`)
today has two layered mechanisms for getting a buy-list item into a tax-advantaged account:

1. **ADR-060's new-deposit waterfall** — IKE first, then IKZE, cumulative partial fill against
   the item's `allocatedPln`, sized to whatever annual statutory room (`ikeLeft`/`ikzeLeft`) is
   actually left.
2. **The "migration triple"** — when a positive `remaining` survives step 1, and *only when the
   item itself has a companion row* (`companion != null` — today that's `BTC-USD`/`ETH-USD`/
   `SOL-USD`/`XRP-USD` via their crypto ETPs, and `ETCGLDRMAU.PL` via its gold ETC companions
   `IGLN.L`/`4GLD.DE`), the service sells the most-over-target companion **anywhere in the
   portfolio** from IKZE, uses the freed capacity to buy the item's own tax-wrapper-eligible
   ticker into IKZE, and books a compensating buy of `companion.companionParentTicker()` on
   Binance/REGULAR for whatever's left.

Two defects in mechanism 2, both fixed by this ADR:

- **Gate too narrow.** `companion != null` excludes every non-companion item (a US ETF, a China
  ETF, any plain XTB stock) even though such an item is otherwise `ikeIkzeEligible` (it's XTB-
  listed) and could perfectly well benefit from freed IKE/IKZE room — it just falls straight to
  REGULAR today, wasting tax-advantaged capacity that's sitting right there in the form of
  over-target crypto.
- **Compensating buy targets the wrong ticker.** `companion.companionParentTicker()` in step 3 is
  always the **item being processed**'s own companion's parent (`findCompanion` is called on
  `pos.getTicker()`, i.e. the item's own ticker) — *not* the crypto that step 1 actually sold.
  Concretely: today, if BTC-USD's shortfall is filled by selling ETH's companion
  (`PL.ETNVIRETH`), the compensating buy on REGULAR is booked for **BTC-USD**, not ETH-USD. ETH's
  real exposure is never restored — it's silently trimmed as a side effect of funding BTC. This
  was never flagged as a bug in prior ADRs because the existing tests only assert on the
  *processed item's* own total exposure (which does balance, e.g.
  `migrationTriple_cryptoCompanion_noDoubleBuyAndNoIllegalIkzeCrypto`'s
  `totalBtcExposure == 1000.00` assertion) — they never check what happened to the donor.

This ADR also resolves the standing "architect decision needed" ask in
`.claude/tasks/ikze-migration-same-companion-collision.task.md` (a same-companion sell/buy
collision discovered as a follow-up to ADR-060) — see §5 below. That task file is superseded by
this ADR and moved to `.claude/tasks/done/` as part of this work.

The user's own framing (task file, confirmed, not re-litigated): tax efficiency ranking is
**IKE > IKZE > Crypto Exchange (Binance) > Regular exchange (XTB)**. Any held crypto sitting in
IKE/IKZE that's over target is fair game to sell in order to free room for *any* item — not just
its own family — because the crypto gets rebought immediately on Crypto Exchange anyway; letting
it sit in IKZE while a US ETF pays REGULAR-account taxes on the same session is strictly worse.

## Decision

### 1. New mutable state, seeded once per `computePlan` call

```java
private record SellKey(Portfolio portfolio, AccountType accountType, String companionTicker) {}
private record OwedKey(Portfolio portfolio, String cryptoTicker) {}
```

Three maps, created once at the top of `computePlan` (alongside the existing `ikeLeft`/`ikzeLeft`
arrays) and threaded — by reference, mutable — through **both** `processPortfolio` calls
(RETIREMENT then MAIN), exactly like `ikeLeft`/`ikzeLeft` already are:

- `Map<SellKey, BigDecimal> sellableCryptoPln` — live, decrementing sellable balance per
  (portfolio, account, companion ticker).
- `Map<SellKey, PurchaseSessionItem> sellRowsByKey` — tracks the `PurchaseSessionItem` row
  already booked for a given sell tuple, so a second sell from the same donor+account (by a
  *different* recipient item, later in the run) merges instead of violating
  `psi_session_portfolio_ticker_account_uq` (§7c below — this is a new hazard this ADR
  introduces and fixes, not one ADR-060 already covered).
- `Map<OwedKey, BigDecimal> owedCompensatingBuyPln` — accumulates, per (portfolio, sold crypto's
  own parent ticker), how much PLN of compensating real-crypto buy is owed. Reconciled once,
  after both portfolio passes (§6).

**Unlike `ikeLeft`/`ikzeLeft` (a genuinely portfolio-agnostic shared annual PLN cap — the same
physical statutory account regardless of which app-level portfolio a buy is tagged with), these
three maps are keyed *by* `Portfolio`.** RETIREMENT's own held crypto (e.g. its own
`PL.ETNVIRBTCP` row, seeded independently in `V6__seed_positions.sql` under `EMERYTURA`, distinct
from MAIN's own `PL.ETNVIRBTCP` row seeded under `INWESTYCJE`) is a completely separate donor pool
from MAIN's — selling from one never affects the other's balance. Threading one shared object
through both passes (rather than reseeding per call) is purely an implementation-consistency
choice, mirroring the `ikeLeft`/`ikzeLeft` pattern; it has no cross-portfolio behavioral effect,
because the keys never collide across portfolios.

**Seeding** (`seedSellableCryptoPln`, called once before either `processPortfolio` call):

```
for each p in allPositions where p.isCompanionRow():
    parent = findPositionByTicker(p.getCompanionParentTicker(), p.getPortfolio(), allPositions)
    if parent == null || parent.getAssetClass() != AssetClass.CRYPTO:
        continue   // NOT a crypto companion — Gold's IGLN.L/4GLD.DE never enter this map at all (§8)
    companionPricePln = pricePlnMap.get(p.getYahooTicker())
    if companionPricePln == null || companionPricePln <= 0:
        log.warn(...); continue   // soft-skip: an unpriced donor just can't be sold this session,
                                   // not a reason to fail the whole plan (unlike
                                   // resolveCompanionPricePln's hard-throw when a companion is
                                   // actively being SUBSTITUTED for a real buy)
    sellableCryptoPln.put(SellKey(p.getPortfolio(), IKE,  p.getTicker()), p.getUnitsIke()  * companionPricePln)
    sellableCryptoPln.put(SellKey(p.getPortfolio(), IKZE, p.getTicker()), p.getUnitsIkze() * companionPricePln)
```

**Must use the existing `findPositionByTicker(ticker, portfolio, allPositions)` helper for the
parent lookup, never the flat `positionByTicker` map built inside `processPortfolio`.** That map
is `allPositions.stream().collect(toMap(Position::getTicker, p -> p, (a,b) -> a))` — it collapses
same-ticker rows across portfolios (e.g. `PL.ETNVIRBTCP` exists as both a MAIN row and a
RETIREMENT row with the same ticker), silently keeping whichever one the stream visits first.
It's safe for its existing use (resolving *this portfolio's own* buy-list item, inside a single
`processPortfolio` call already scoped to one portfolio) but would resolve the *wrong* portfolio's
parent here, corrupting the crypto-vs-not classification for one of the two portfolios.

### 2. The per-item waterfall — five steps, IKE fully (new-deposit *and* sell) before IKZE at all

For each buy-list item `i` (existing `useCompanion`/`companion`/`ikeIkzeEligible` computation
unchanged), track two row references local to this item — `ikeRowRef`, `ikzeRowRef` — both `null`
initially (extends ADR-060's single `waterfallIkzeRow` to both accounts, since the sell layer can
now write to IKE too):

1. **IKE new-deposit** (ADR-060, unchanged). If `ikeIkzeEligible && ikeLeft[0] > 0`:
   `ikeFillPln = min(ikeLeft[0], remaining)`; book/assign `ikeRowRef`; `ikeLeft[0] -= ikeFillPln`;
   `remaining -= ikeFillPln`.
2. **IKE crypto-sell (NEW).** If `ikeIkzeEligible && remaining > 0`: call
   `findBestCryptoDonor(portfolio, IKE, allPositions, pricePlnMap, buyList, sellableCryptoPln,
   /*excludeParentTicker=*/ item.ticker())` (§4). If a donor is found with live balance `> 0`:
   `sellPln = min(remaining, balance)`; decrement `sellableCryptoPln`; book-or-merge the sell row
   via `sellRowsByKey` (§7c); if `ikeRowRef != null` merge `sellPln` into it (recompute
   `plannedUnits` fresh from the new total, never sum independently-rounded units — same rule
   ADR-060 already established), else create it via `ikeIkzeBuyItem(..., IKE, sellPln)` and assign
   `ikeRowRef`; `owedCompensatingBuyPln.merge(OwedKey(portfolio, donor.getCompanionParentTicker()),
   sellPln, +)`; `remaining -= sellPln`.
3. **IKZE new-deposit** (ADR-060, unchanged, just now running against `remaining` *after* step 2
   instead of directly after step 1). If `ikeIkzeEligible && remaining > 0 && ikzeLeft[0] > 0`:
   `ikzeFillPln = min(ikzeLeft[0], remaining)`; book/assign `ikzeRowRef`; `ikzeLeft[0] -=
   ikzeFillPln`; `remaining -= ikzeFillPln`.
4. **IKZE crypto-sell** (generalizes the old migration-triple's step 1–2; same shape as step 2
   above, targeting IKZE). If `ikeIkzeEligible && remaining > 0`: same donor search, this time
   `accountType = IKZE`; same sell/merge/owed bookkeeping against `ikzeRowRef`.
5. **True remainder → plain REGULAR/Crypto-Exchange purchase of the item's own ticker.** If
   `remaining > 0`: `result.add(plainItem(item.ticker(), portfolio, REGULAR, remaining,
   unitsForFill(item, remaining, pricePln), pricePln))`. (`isMigrationBuy = false` — this is the
   item's own ordinary purchase, not a compensating buy; see §6's flag-flip rule for what happens
   if the *reconciliation* pass later merges into this exact row.)

This is the "IKE fully — new-deposit *and* any crypto it holds that can be sold — before IKZE is
touched at all" ordering the task's confirmed decision 1 requires: steps 1–2 (both IKE layers) run
to exhaustion before step 3 (IKZE new-deposit) is even attempted, and step 4 (IKZE sell) only ever
sees whatever `remaining` steps 1–3 left behind.

**Whole-item accounting identity** (extends ADR-060's identity with two new terms):

```
ikeFillPln + ikeSellPln + ikzeFillPln + ikzeSellPln + trueRemainderPln == allocatedPln
```

always holds, for every item, regardless of which steps fire (`ikeSellPln`/`ikzeSellPln` are `0`
when no donor is found or the item isn't `ikeIkzeEligible`).

### 3. `findBestCryptoDonor` — replaces `findBestEtfToSell`

```java
private Position findBestCryptoDonor(
        Portfolio portfolio, AccountType accountType,
        List<Position> allPositions, Map<String, BigDecimal> pricePlnMap,
        List<BuyListItemDto> buyList, Map<SellKey, BigDecimal> sellableCryptoPln,
        String excludeParentTicker) {

    Map<String, BigDecimal> deviationMap = /* unchanged: buyList → parent ticker → deviationPct */;

    List<Position> candidates = allPositions.stream()
            .filter(p -> p.isCompanionRow() && p.getPortfolio() == portfolio)
            .filter(p -> {
                BigDecimal bal = sellableCryptoPln.get(new SellKey(portfolio, accountType, p.getTicker()));
                return bal != null && bal.compareTo(BigDecimal.ZERO) > 0;
            })
            .filter(p -> !p.getCompanionParentTicker().equals(excludeParentTicker))  // §5
            .toList();

    if (candidates.isEmpty()) return null;

    return candidates.stream()
            .max(Comparator
                    .comparing((Position p) -> deviationMap.getOrDefault(p.getCompanionParentTicker(), ZERO))
                    .thenComparing(/* unchanged tiebreak: PL.ETNVIRBTCP before PL.ETNVIRETH */))
            .orElse(null);
}
```

Three changes from today's `findBestEtfToSell`: (a) an `accountType` parameter — candidates are
filtered by the **live** `sellableCryptoPln` balance for that specific account, not the static
`Position.getUnitsIkze()` snapshot; (b) the filter is implicitly crypto-only, because
`sellableCryptoPln` was only ever seeded for crypto-parent companions (§1) — Gold's companions are
never present in the map, so they can never be selected, full stop, with no extra filter needed
here; (c) the new `excludeParentTicker` filter (§5).

**Ranking (current behaviour, replaces the deviation ranking this section originally described).**
Any held crypto is a valid sell candidate, buy-list or not (decision 3), and a position being over
target plays no part. Among the eligible candidates the first in this fixed order donates:
`PL.ETNVIRBTCP`, `PL.ETNVIRETH`, `PL.ETNVIRSOL`, `PL.ETNVIRXRP`; a crypto wrapper not in the list
(for example `ETNVIRLINK.PL`) ranks after XRP, alphabetically by ticker. (The Java sketch above shows
the original deviation comparator and is kept as history. The ranking the first version of this
section described, "most over-target parent first", never operated as described: over-target assets
are not on the buy list, so their deviation defaulted to 0. Owner decision 2026-11-09, task 192,
`docs/architecture/05-rebalancing-algorithm.md` §8.2.)

**Kept as today, not extended:** a single sell step tries **one** candidate (the best) and caps at
its capacity — it does not fall through to a second-best donor if the best one's balance doesn't
fully cover `remaining`. The worked example only ever needs one donor asset; generalizing to a
multi-donor waterfall within one step is a real possible future enhancement but is out of scope
here — nothing in the task or the confirmed decisions asks for it, and adding it would be
un-derived scope creep.

### 4. Self-donation exclusion — resolves the queued collision task

`.claude/tasks/ikze-migration-same-companion-collision.task.md` asked the architect to pick among
three options for the case where a crypto's own companion is also the best (or only) sell
candidate for funding that same crypto's own purchase — which today makes the migration-sell and
the companion-substituted IKZE buy target the identical `(session, portfolio, ticker, IKZE)`
tuple, tripping `psi_session_portfolio_ticker_account_uq`.

**Decision: option 2 — exclude the item's own parent-ticker family from its own donor
candidate pool**, via the `excludeParentTicker = item.ticker()` filter in §3. Concretely: while
processing `BTC-USD`, `PL.ETNVIRBTCP` (whose `companionParentTicker == "BTC-USD"`) is never a
candidate — regardless of account, regardless of how over-target it is. If a *different* donor
exists (e.g. `PL.ETNVIRETH`), that one is used instead; if none exists, the sell step is simply
skipped for that item (falls through toward `remaining` unchanged, same as "no donor found").

**Why option 2, not option 3 ("detect the degenerate case after computing `cappedPln`, treat as a
no-op and fall back to a plain purchase").** Option 3 as literally worded gives up on the *entire*
donor mechanism for that item the moment the top-ranked candidate happens to be its own family —
even when a second-best, genuinely different donor is available and would have worked fine.
Option 2's upfront exclusion instead falls through to the next-best *eligible* candidate
naturally, via the existing ranking/filter pipeline — strictly more capital-efficient, and no
extra code path is needed to detect the "degenerate" case after the fact, because it's simply
never selected in the first place.

**Why not option 1 ("differentiate the unique constraint on `isMigrationSell`/`isMigrationBuy`,
so a same-tuple sell and buy can coexist").** Rejected for the same reason ADR-060 rejected
schema-level workarounds for correctness problems solvable in application logic: it requires a
Flyway migration to weaken a constraint whose *purpose* (at most one row per
session/portfolio/ticker/account) is still correct and desirable — the actual problem is that
selling and immediately rebuying the identical instrument, in the identical account, for the
identical amount, is economically a no-op that achieves nothing (ignoring spread/fee cost the two
operations would separately incur, which real execution *does* incur, for zero net benefit) — the
constraint isn't wrong, the algorithm was proposing a pointless trade. Option 2 prevents the
pointless trade from ever being proposed.

**Confirmed necessary, not just cleaner, by the worked example.** Point 6 of the task's worked
example states IKZE still holds 500 PLN of unsold crypto after the session, specifically *because*
BTC-USD (the only other buy-list item) can't draw on its own remaining IKZE holding to fund
itself. Section "Worked Example Walkthrough" below traces this precisely — without the exclusion,
the numbers in the acceptance scenario do not come out as specified.

This resolves `.claude/tasks/ikze-migration-same-companion-collision.task.md`'s "Decision needed
(architect)" ask; that file is moved to `.claude/tasks/done/` as part of this ADR.

### 5. Deferred reconciliation pass — runs once, after both portfolio passes

```java
// In computePlan, after BOTH processPortfolio(...) calls return:
for (Map.Entry<OwedKey, BigDecimal> entry : owedCompensatingBuyPln.entrySet()) {
    OwedKey key = entry.getKey();
    BigDecimal owed = entry.getValue();
    if (owed.compareTo(BigDecimal.ZERO) <= 0) continue;

    Optional<PurchaseSessionItem> existing = result.stream()
            .filter(r -> r.getPortfolio() == key.portfolio()
                    && r.getTicker().equals(key.cryptoTicker())
                    && r.getAccountType() == AccountType.REGULAR
                    && !r.isMigrationSell())
            .findFirst();

    if (existing.isPresent()) {
        PurchaseSessionItem row = existing.get();
        BigDecimal newTotal = row.getPlannedValuePln().add(owed);
        row.setPlannedValuePln(newTotal);
        row.setPlannedUnits(newTotal.divide(row.getPricePlnAtPlan(), 8, RoundingMode.HALF_UP));
        row.setMigrationBuy(true);   // see flag-flip rule below
    } else {
        Position crypto = findPositionByTicker(key.cryptoTicker(), key.portfolio(), allPositions);
        BigDecimal price = pricePlnMap.get(crypto.getYahooTicker());
        BigDecimal units = owed.divide(price, 8, RoundingMode.HALF_UP);
        result.add(migrationBuyItem(key.cryptoTicker(), key.portfolio(), AccountType.REGULAR,
                owed, units, price));
    }
}
```

Why deferred rather than resolved inline at sell-time (as the task's own reasoning states): the
crypto sold to fund item A might have its own buy-list entry processed *later* in the same loop,
or in the *other* portfolio's pass (RETIREMENT is processed before MAIN) — at sell-time there's no
way to know yet whether a REGULAR row for that crypto will exist to merge into. Resolving once,
after both passes, is the only point in `computePlan` where `result` is guaranteed complete.

**`isMigrationBuy` flag-flip rule on merge.** When reconciliation merges `owed` PLN into an
*existing* row, that row's `isMigrationBuy` is set `true` unconditionally — even if the row
started life as a plain item (`isMigrationBuy = false`, e.g. the item's own step-5 remainder from
§2). This is a deliberate call, not left implicit: `isMigrationBuy`/`isMigrationSell` drive
`PurchaseSessionService.classifyTradeType` (ADR-057's live trade-history-chart classification —
`MIGRATION_BUY` vs `DCA_BUY`). Once any part of a row's `plannedValuePln` represents a
compensating rebuy for sold tax-wrapper capacity, labeling the whole row `DCA_BUY` would be
strictly less informative than `MIGRATION_BUY`; there is no finer-grained field to split the two
sub-amounts (and the task's own framing — "merged... as ONE line, not two" — rules that out),
so the classification has to pick one label for the merged whole, and `MIGRATION_BUY` is the more
honest one. This differs from the pre-existing precedent (the old step-2 IKZE buy — today's
`waterfallIkzeRow` merge target — always stayed `isMigrationBuy = false` even when it absorbed
sell-freed capacity) because that precedent never had a case where the *merge target itself* could
be a plain, otherwise-unrelated row; this ADR's reconciliation pass introduces exactly that case
for the first time, so it needs its own explicit rule.

**Reconciliation never touches IKE/IKZE rows** — only ever searches/merges into `AccountType.REGULAR`
rows, so it cannot collide with anything §2's `ikeRowRef`/`ikzeRowRef` merges already resolved.

### 6. Gold's self-swap removed entirely

Gold (`ETCGLDRMAU.PL`) becomes a plain, non-`useCompanion` IKE/IKZE-eligible item — identical in
shape to a China ETF or US ETF in every code path in this file:

- `useCompanion` stays `false` for Gold (unchanged — gated on `pos.getAssetClass() == CRYPTO`,
  and Gold's asset class is `ETF`, never `CRYPTO`).
- Gold's own companions (`IGLN.L`, `4GLD.DE`) are **structurally excluded from
  `sellableCryptoPln` entirely** by §1's seeding filter (`parent.getAssetClass() != CRYPTO` for
  both) — they can never be a donor for Gold *or* for anything else. "Gold itself is never sold to
  free room for something else" isn't a separate rule to implement; it falls out of the seeding
  filter for free.
- Gold **can** be a *recipient* of the crypto-donor mechanism — steps 2/4 in §2 apply to Gold
  exactly like any other `ikeIkzeEligible` item, using Gold's own ticker directly (no
  substitution, `useCompanion = false`) for the freed-capacity buy.
- There is no more "self-funds-itself via its own companion" special case anywhere in the file —
  the old migration triple's step 2 non-crypto branch ("buy the parent directly, no substitution
  needed") is now just the ordinary `useCompanion = false` path through §2's steps 2/4, identical
  to how a China ETF is handled.

### 7. Unique-constraint discipline — every place two writes can now target the same tuple

`purchase_session_items` has one unique constraint:
`psi_session_portfolio_ticker_account_uq (session_id, portfolio, ticker, account_type)`. This ADR
introduces or extends **five** places two writes could collide on it — (a) and (b) are
ADR-060-shaped merges extended to a new account; (c) is genuinely new, introduced by the shared
decrementing donor pool; (d) is genuinely new, introduced by deferred reconciliation; (e) is a
*guard*, not a merge, because a sell and a buy for the same tuple are not summable (opposite
economic direction):

| # | Two writes racing for the same tuple | Resolution |
|---|---|---|
| (a) | Item's own IKE new-deposit fill (§2 step 1) vs. item's own IKE crypto-sell buy (§2 step 2) | Merge via `ikeRowRef` |
| (b) | Item's own IKZE new-deposit fill (§2 step 3) vs. item's own IKZE crypto-sell buy (§2 step 4) | Merge via `ikzeRowRef` — same pattern ADR-060 already established for the old migration triple's step 2, now generalized |
| (c) | **Two different recipient items**, in the same run, both drawing on the **same donor + same account** (e.g. item A sells 400 of `PL.ETNVIRBTCP`/IKE, item B later sells another 300 of the same donor from IKE because more balance remained) | Merge via `sellRowsByKey` (§1) — **new**, not covered by ADR-060, necessary precisely because the donor pool is a shared, run-wide decrementing balance multiple items can tap |
| (d) | End-of-run reconciliation (§5) merging `owedCompensatingBuyPln` into a REGULAR row that may already exist (that crypto's own ordinary buy-list purchase, if it had one) | Merge via the `result` scan in §5 — **new** |
| (e) | A recipient item would otherwise donate to itself — its own sell row and its own buy row would target the identical `(portfolio, ticker, account)` tuple | **Guarded, not merged** — §4's `excludeParentTicker` exclusion prevents the sell from ever being proposed |

Every merge recomputes `plannedUnits` fresh from the new total PLN over price — never sums two
independently-rounded unit figures — the same rule ADR-060 established for (a)/(b), extended
identically to (c)/(d).

### 8. Downstream compatibility — `PurchaseSessionService` needs no code changes

`PurchaseSessionService`'s Step 4 deposit auto-creation (`06-data-flow.md` §2, ADR-033/035/055)
already groups non-sell items by `(portfolio, accountType, exchange)`, sums `plannedValuePln`, and
**nets migration-sell proceeds against the matching group** — `groupSum -=
SUM(|effective PLN| of is_migration_sell items in that same group)`, floored at `0`. This formula
is already generic on `accountType` (it was never actually hardcoded to IKZE, even though only
IKZE sells were reachable before this ADR) and generic on *which tickers* are on the sell side vs.
the buy side within a group — it only cares about the group-level totals.

**Proof this still nets correctly under the new mechanism.** Within one `(portfolio, IKE|IKZE,
XTB)` group, `Σ(sellPln across every donor draw booked into that group)` always exactly equals
`Σ(the recipient buy PLN financed by those draws)`, by construction — §2 step 2/4 never sell more
than `sellPln` and never book less than `sellPln` into the recipient's row. So group-sum-before-
netting `= Σ(new-deposit fills) + Σ(sell-financed fills)`, and subtracting `Σ(migration-sell PLN)`
in the same group cancels the second term exactly, leaving `Σ(new-deposit fills)` — the correct
"real capital" figure — **regardless of how many distinct donors and recipients are mixed together
in that group**, and regardless of whether the sell happened in IKE or IKZE. The compensating buy
(§5) always lands in `AccountType.REGULAR`, a different group from any migration-sell (which is
always IKE or IKZE), so it is never netted against anything — it counts as a full deposit, exactly
matching the worked example's own conservation statement (§"Worked Example Walkthrough" below).
**No change to `PurchaseSessionService`, `06-data-flow.md`'s Step 4 formula, or the
`purchase_session_items` schema is required.**

One cross-reference in `06-data-flow.md` is now imprecise wording (not a logic bug): the sentence
"the migration-triple **IKZE** re-buy... creates no deposit" (§2, Step 4 bullet) should read
"IKE/IKZE re-buy" once this ADR ships, since IKE-side sells are now reachable. Left as a follow-up
doc touch-up at implementation time (§ Consequences) rather than changed here, since the mechanism
it describes isn't implemented yet.

## No-op / invariant guarantees

- **No-op vs. ADR-060 whenever no crypto is ever sellable** (`sellableCryptoPln` seeds to nothing,
  or every donor's balance is `0`, or the only donor for every remaining item is self-excluded):
  §2 steps 2 and 4 always find no donor and skip, collapsing exactly to ADR-060's two-step
  waterfall (`ikeFillPln`/`ikzeFillPln`, then straight to REGULAR). Both
  `partialIkzeRoom_splitsAcrossIkzeAndRegular_nasdaqChinaEtfScenario` and
  `ikeThenIkzeSplit_ikeGetsFirstClaimOnPartialRoom` (no crypto positions in their fixtures at all)
  exercise exactly this no-op path and should pass unmodified.
- **Whole-item accounting identity** (§2): `ikeFillPln + ikeSellPln + ikzeFillPln + ikzeSellPln +
  trueRemainderPln == allocatedPln`, always, for every item.
- **Session-wide conservation, independent of buy-list processing order:** total real new capital
  this session `= Σ(ikeFillPln + ikzeFillPln across every item, both portfolios) +
  Σ(all AccountType.REGULAR row totals in `result`, after reconciliation)` always equals the sum
  of every buy-list item's `allocatedPln` across both portfolios. This holds **regardless of which
  order buy-list items are processed in** — verified for both orderings of the worked example
  below. What is **not** order-independent is the exact per-row split (which item ends up with how
  much IKE vs. IKZE vs. REGULAR) — this was already true before this ADR (ADR-060's own new-
  deposit waterfall is a first-come-first-served greedy allocation against the shared
  `ikeLeft`/`ikzeLeft` pool); this ADR extends the same greedy, order-dependent shape to the
  sellable-crypto pool, it does not introduce order-dependence that wasn't already there.
- **The worked example's exact stated numbers require processing the recipient (US ETF) before
  its donor's own item (BTC-USD)** — matching the task's own stated order. A regression test must
  construct its buy list in that order to reproduce them exactly.

## Worked Example Walkthrough

Setup: `ikeLeft = 0`, `ikzeLeft = 1000`. One crypto family held (task's own simplification: "assume
all BTC"), companion ticker `BTC-ETP`, `sellableCryptoPln = {(MAIN,IKE,BTC-ETP): 1000,
(MAIN,IKZE,BTC-ETP): 1000}`. Buy list, processed **US-ETF then BTC-USD** (matches the task's
stated order):

**Item 1 — US-ETF, `allocatedPln = 2500`, non-crypto (`useCompanion = false`):**

| Step | Action | `remaining` after |
|---|---|---|
| 1. IKE new-deposit | `ikeLeft = 0` → skip | 2500 |
| 2. IKE crypto-sell | donor = BTC-ETP (not self-excluded — item ticker is `US-ETF`), balance 1000. `sellPln = min(2500, 1000) = 1000`. Sell 1000 IKE. New `ikeRowRef`: US-ETF/IKE = **1000**. `owed[(MAIN,"BTC-USD")] += 1000` | 1500 |
| 3. IKZE new-deposit | `ikzeFillPln = min(1000, 1500) = 1000`. New `ikzeRowRef`: US-ETF/IKZE = 1000. `ikzeLeft → 0` | 500 |
| 4. IKZE crypto-sell | donor = BTC-ETP, balance 1000. `sellPln = min(500, 1000) = 500`. Sell 500 IKZE. Merge into `ikzeRowRef`: US-ETF/IKZE = 1000 + 500 = **1500**. `owed[(MAIN,"BTC-USD")] += 500` → owed total **1500** | 0 |
| 5. Remainder | 0, skip | — |

`sellableCryptoPln` now: `(MAIN,IKE,BTC-ETP): 0`, `(MAIN,IKZE,BTC-ETP): 500`.

**Item 2 — BTC-USD, `allocatedPln = 2500`, crypto (`useCompanion = true`, own companion = BTC-ETP):**

| Step | Action | `remaining` after |
|---|---|---|
| 1. IKE new-deposit | `ikeLeft = 0` → skip | 2500 |
| 2. IKE crypto-sell | only candidate BTC-ETP has `companionParentTicker == "BTC-USD" == item.ticker()` → **excluded** (§4). No donor. Skip | 2500 |
| 3. IKZE new-deposit | `ikzeLeft = 0` (consumed by item 1) → skip | 2500 |
| 4. IKZE crypto-sell | same self-exclusion → no donor. Skip | 2500 |
| 5. Remainder | `plainItem`: BTC-USD/REGULAR = **2500** | — |

**Reconciliation:** `owed[(MAIN,"BTC-USD")] = 1500`. Existing REGULAR row for `(MAIN,"BTC-USD")`
found (the 2500 from item 2 step 5) → merge: `2500 + 1500 = 4000`; `isMigrationBuy = true`.

**Final plan:** `IKE/US-ETF = 1000`, `IKZE/US-ETF = 1500`, `REGULAR/BTC-USD = 4000` (merged, one
row) — **exactly** the task's acceptance numbers. Plus the two internal sell rows
(`IKE/BTC-ETP = -1000`, `IKZE/BTC-ETP = -500`), which never appear as "deposits" (§8).

**Conservation:** new capital `= ikzeFillPln(1000, item 1) + REGULAR total(4000) = 5000` = session
budget. ✓. **Post-session state:** IKE donor balance `0` (depleted); IKZE donor balance `500`
(untouched — item 2 was barred from touching it by self-exclusion) — matches the task's point 6
exactly.

**Order sensitivity (informational, not a contradiction):** processing BTC-USD before US-ETF
instead produces a *different* row-level split (BTC-USD would claim the 1000 PLN of IKZE new-
deposit room for itself via its own companion, leaving US-ETF to draw more heavily on the sell-
donor mechanism) — but the conservation identity above still holds (`5000` either way), as proven
in §"No-op / invariant guarantees." Implementers should not expect byte-identical rows regardless
of order — only the total.

## Alternatives considered

- **Multi-donor waterfall within one sell step** (try the 2nd-best candidate if the 1st doesn't
  fully cover `remaining`). Deferred — not required by the worked example or any confirmed
  decision; real added complexity (would need to loop `findBestCryptoDonor` with an accumulating
  exclusion set) for a scenario not yet demonstrated to occur in practice.
- **Allow self-donation, detect and skip only the degenerate exact-collision case** (the queued
  collision task's option 3). Rejected in favor of the strictly-better option 2 (§4) — see that
  section for the full comparison.
- **Differentiate the unique constraint** (collision task's option 1). Rejected — a schema change
  to work around a case application logic can prevent outright (§4).
- **Cross-portfolio donor pool** (let a MAIN recipient draw on RETIREMENT's held crypto or vice
  versa). Rejected — sell-candidate search stays portfolio-scoped, matching
  `findBestEtfToSell(portfolio, ...)`'s existing signature and every other portfolio-scoped
  lookup in this file (`findCompanion`, `findPositionByTicker`). The task's worked example never
  crosses portfolios and this was explicitly not one of the three things the user was asked about
  — noted here as a design call being made, not left open, per the task's own instruction.
- **Immediate/inline reconciliation** (resolve the compensating buy synchronously at sell-time,
  guessing whether a REGULAR row for that crypto exists yet). Rejected per the task's own
  reasoning — the crypto's own buy-list row might not have been processed yet, or might belong to
  the *other* portfolio's pass, which hasn't run yet when RETIREMENT is being processed.
- **Leave the compensating buy targeting the *recipient's* own ticker** (today's actual, if
  unintentional, behavior). Superseded — this silently fails to restore the donor's exposure; the
  whole point of this ADR's generalization is that the donor and recipient are legitimately
  different assets now, so the compensating buy must track the donor, not the recipient.

## Consequences

**Tests to rewrite** (`backend/src/test/java/pl/investing/unit/PurchasePlanningServiceTest.java`):

- `migrationTriple_cryptoCompanion_noDoubleBuyAndNoIllegalIkzeCrypto` — **must be rewritten.** The
  compensating buy now targets `ETH-USD` (the sold donor), not `BTC-USD` (the recipient): expect
  `IKZE/PL.ETNVIRETH sell = -500`, `IKZE/PL.ETNVIRBTCP buy = 500`, `REGULAR/BTC-USD = 500` (item's
  own remainder — unaffected by the mechanism since no compensating amount targets BTC-USD here),
  and a **new, standalone** `REGULAR/ETH-USD = 500` row (`isMigrationBuy = true`, since ETH-USD had
  no other buy-list entry to merge into) — 4 rows total, not 3.
- `migrationTriple_exactCover_noMigrationBuyEmitted` — **must be rewritten; its core assertion is
  now wrong.** "Exact cover" from the *recipient*'s perspective (SOL-USD's own remainder hits
  exactly 0) no longer means no migration-buy fires at all — the *donor* (XRP-USD, whose
  `PL.ETNVIRXRP` was sold) still needs its compensating buy via reconciliation. Expect 3 rows, not
  2: `IKZE/PL.ETNVIRXRP sell = -500`, `IKZE/PL.ETNVIRSOL buy = 500`, and a new standalone
  `REGULAR/XRP-USD = 500` (`isMigrationBuy = true`). `plan).noneMatch(isMigrationBuy)` must become
  the opposite assertion.
- `waterfallPartialIkzeFill_mergesWithMigrationTripleIkzeBuy_singleRowNotTwo` — **must be
  rewritten.** The merge-into-`ikzeRowRef` behavior it tests is still correct and still fires
  (`IKZE/PL.ETNVIRBTCP` still merges to 700), but the old assertion that the compensating buy
  targets `BTC-USD` for `300` must change to targeting `ETH-USD` for `500` (the sold donor,
  standalone, since ETH-USD has no other row to merge into), and BTC-USD's own remainder
  (`REGULAR/BTC-USD = 300`) is now a plain, unmerged row (`isMigrationBuy = false`) — 4 rows total
  (sell, IKZE buy, BTC's own REGULAR remainder, ETH's new compensating REGULAR row), not 3.
- `migrationTriple_goldCompanion_step3BooksRemainderOnly_noSubstitutionNeeded` — **delete**, per
  the task's confirmed decision 2. Replace with a plain non-companion-fallback scenario for Gold
  (same shape as `partialIkzeRoom_splitsAcrossIkzeAndRegular_nasdaqChinaEtfScenario`, Gold ticker
  instead of a China ETF) — optionally also add a case showing Gold benefiting as a *recipient* of
  a crypto donor, to cover §6's new "Gold can be a recipient" behavior explicitly.
- `findBestEtfToSell_usesParentDeviation_sellsMostOverTargetEtpFirst` — assertions likely
  **unaffected** (traced in §4: the self-exclusion happens to be a no-op here since the
  already-correct deviation ranking would pick `PL.ETNVIRETH` first regardless), but it exercises
  a method being renamed to `findBestCryptoDonor` with a new `accountType` parameter — rename/
  update the test and its docstring to reflect the new signature and to note the self-donation
  exclusion, even though the specific numbers don't change.
- `partialIkzeRoom_splitsAcrossIkzeAndRegular_nasdaqChinaEtfScenario`,
  `ikeThenIkzeSplit_ikeGetsFirstClaimOnPartialRoom`, `chfSavings_neverIkeEligible_staysRegular`,
  `binanceParentWithoutCompanion_staysRegular`, `ikeBuyWithCompanion_usesCompanionTickerUnitsAndPrice`,
  `binanceParentWithCompanion_isIkeEligibleThroughCompanion` — **unaffected**, no crypto donors
  present in their fixtures; should pass unmodified (no-op guarantee above).
- **New regression test required**, reproducing the worked example end-to-end exactly as traced
  above (buy list constructed as `List.of(usEtfItem, btcUsdItem)`, in that order; 1000 PLN IKZE
  room, 0 IKE room, 1000 PLN of BTC-ETP held in each of IKE/IKZE for the `MAIN` portfolio),
  asserting the exact rows (`IKE/US-ETF = 1000`, `IKZE/US-ETF = 1500`,
  `REGULAR/BTC-USD = 4000`) and the conservation identity (`Σ new capital == 5000`).

**No schema/Flyway change required** — this is pure application logic in `PurchasePlanningService`,
same as ADR-060; the unique constraint and the `is_migration_sell`/`is_migration_buy` mutual-
exclusivity CHECK are both satisfied by construction (§7).

**`PurchaseSessionService` requires no code change** (§8) — its existing group-based deposit
netting already generalizes correctly.

**Doc follow-ups, deferred to implementation time** (not changed by this ADR, since the mechanism
isn't implemented yet — flagged here per CLAUDE.md's "tell me if docs and code disagree," now
pre-emptively for when they will start to): `docs/architecture/06-data-flow.md` §2 Step 4 bullet
says "the migration-triple **IKZE** re-buy" — should read "IKE/IKZE" once implemented (§8 above).
`docs/architecture/02-domain-model.md` (lines ~269, ~475) and `docs/architecture/03-api-contract.md`
(lines ~127, ~164) describe the migration mechanism in prose that still matches the *shape*
(sell a companion, buy the freed capacity, compensate on Binance/REGULAR) but were written before
this generalization and don't mention the crypto-only donor restriction, the IKE-side sell, or the
reconciliation pass — worth a pass once the code lands, not urgent since nothing there is
factually wrong, just incomplete.

**`.claude/tasks/ikze-migration-same-companion-collision.task.md` is resolved by §4 above** and
moved to `.claude/tasks/done/` as part of this ADR.
