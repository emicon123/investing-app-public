# ADR-065: Cross-Portfolio IKE/IKZE Crypto-Donor Waterfall

**Date:** 2026-07-31
**Status:** Accepted — extends ADR-061 (read that ADR first; this document only covers the delta)

## Context

ADR-061 (`docs/adr/ADR-061-generalized-ikze-ike-crypto-donor-waterfall.md`) generalized
`PurchasePlanningService`'s IKE/IKZE crypto-donor mechanism: any crypto companion
held in IKE or IKZE is a valid sell candidate to free tax-wrapper capacity for *any* buy-list
item, with the compensating rebuy resolved once, at the end of `computePlan`, against the crypto
that was actually sold. That ADR is implemented and live in `PurchasePlanningService` today (the
five-step per-item waterfall, `findBestCryptoDonor`, `applyCryptoSellStep`,
`reconcileOwedCompensatingBuys` — `docs/architecture/05-rebalancing-algorithm.md` §8's earlier
"spec decided, not yet implemented" banner is stale as of this ADR and is corrected alongside it).

ADR-061 deliberately kept donor search **portfolio-scoped**. Its own "Alternatives considered"
section says so explicitly:

> **Cross-portfolio donor pool** (let a MAIN recipient draw on RETIREMENT's held crypto or vice
> versa). Rejected — sell-candidate search stays portfolio-scoped, matching
> `findBestEtfToSell(portfolio, ...)`'s existing signature and every other portfolio-scoped lookup
> in this file... The task's worked example never crosses portfolios and this was explicitly not
> one of the three things the user was asked about — noted here as a design call being made, not
> left open, per the task's own instruction.

That rejection was scoped to "not asked about," not "considered and found wrong." This ADR
revisits it, because the app's own data model makes the portfolio boundary a **logical**
distinction, not a **physical** one: `positions` has a `(ticker, exchange, portfolio)` unique
constraint, so the exact same real brokerage holding — e.g. one Bitcoin Prime ETP position held
in one real IKE account at XTB — can be, and often is, split across two `Position` rows: one with
`portfolio = MAIN`, one with `portfolio = RETIREMENT`. `PurchasePlanningService.computePlan`
processes RETIREMENT's buy list and MAIN's buy list as two independent passes
(`processPortfolio(retirementList, RETIREMENT, ...)` then `processPortfolio(mainList, MAIN, ...)`),
and `findBestCryptoDonor` filters candidates to `p.getPortfolio() == portfolio` — the pass
currently running. The practical consequence: a **MAIN-only** purchase session (the user didn't
select RETIREMENT this time, so `retirementList` is empty and RETIREMENT's `processPortfolio` pass
never runs at all) can never draw on RETIREMENT's donor pool, even though — physically — it is
sitting in the same account, untouched, doing nothing. The user asked for this restriction lifted.

This ADR does **not** revisit anything else ADR-061 decided — the five-step waterfall shape, the
crypto-only donor-pool restriction, the IKE-before-IKZE ordering, or the deferred reconciliation
timing are all unchanged and are assumed here.

## Decision

### 1. Self-portfolio-first, then cross-portfolio fallback

`findBestCryptoDonor` becomes **two-tier**:

1. **Self-portfolio tier (unchanged from ADR-061).** Search the recipient item's own portfolio's
   donor pool first — same live-balance filter, same self-donation exclusion (narrowed in scope,
   see §3), same ranking (a fixed order since 2026-11-09, ADR-061 §3).
2. **Cross-portfolio fallback tier (new).** Only if tier 1 finds no eligible candidate (empty
   after filtering), search the *other* portfolio's donor pool for the same account type, ranked
   the same way (originally against *that portfolio's own* deviation data, §4; a fixed order since 2026-11-09).

`computePlan`'s pass order is unchanged — RETIREMENT's `processPortfolio` call still runs before
MAIN's, matching the spreadsheet order. `sellableCryptoPln` remains one shared, run-wide
decrementing map (ADR-061 §1), so the fallback tier automatically sees whatever the other
portfolio's pass has *already* consumed by the time it runs:

- If both lists are present this session, RETIREMENT services its own needs first (tier 1, then
  tier 2 into MAIN's still-fresh pool if its own tier 1 comes up short); only genuinely leftover
  RETIREMENT capacity is visible to MAIN's later tier-2 lookups.
- If RETIREMENT's list is empty this session (not selected by the user), RETIREMENT's `processPortfolio`
  pass never runs, so its entire donor pool is untouched and available **in full** to MAIN's
  fallback tier.
- Symmetrically, because RETIREMENT's pass runs *first*, if RETIREMENT's own tier 1 comes up
  short, MAIN's pool is entirely untouched at that point in the run and is available to
  RETIREMENT's tier 2 in full. This is a natural, accepted consequence of the unchanged pass
  ordering — not a special case requiring extra logic, and not asymmetric in any way that needs
  separate handling.

This is the same "first-come-first-served against a shared pool" shape ADR-061 already
established for `ikeLeft`/`ikzeLeft` and for `sellableCryptoPln` itself within one portfolio's
pass — this ADR extends that shape across the portfolio boundary, it does not introduce a new
kind of nondeterminism (see §6).

### 2. Donor-side writes key off the donor's actual portfolio, not the pass's portfolio parameter

Today, inside `applyCryptoSellStep`, both the live-balance key and the owed-compensating-buy key
are built from `portfolio` — the parameter identifying whichever pass is currently running:

```java
SellKey donorKey = new SellKey(portfolio, accountType, donor.getTicker());
...
owedCompensatingBuyPln.merge(new OwedKey(portfolio, donor.getCompanionParentTicker()), sellPln, BigDecimal::add);
```

Under ADR-061 this was always correct, because `findBestCryptoDonor` only ever returned a
same-portfolio donor (`donor.getPortfolio() == portfolio` was an invariant). Once tier 2 (§1) can
return a donor from the *other* portfolio, that invariant no longer holds, and both lines above
must switch to `donor.getPortfolio()`:

```java
SellKey donorKey = new SellKey(donor.getPortfolio(), accountType, donor.getTicker());
...
owedCompensatingBuyPln.merge(new OwedKey(donor.getPortfolio(), donor.getCompanionParentTicker()), sellPln, BigDecimal::add);
```

This is necessary for two independent reasons:

- **Reading/decrementing the correct live balance.** `sellableCryptoPln` is keyed by the donor's
  own portfolio (ADR-061 §1, unchanged) — looking it up under the *pass's* portfolio when the two
  differ would either miss the entry entirely (`null` balance) or, worse, silently read and
  decrement the *recipient's own* same-ticker entry instead of the actual donor's.
- **Attributing the sell row and the compensating buy to the party whose exposure was actually
  sold.** `upsertCompanionRow(donorKey, ...)` builds the `PurchaseSessionItem` for the sell using
  `key.portfolio()` — if `donorKey` used the pass's portfolio, a RETIREMENT-owned position's sale
  would be booked as a MAIN transaction, corrupting RETIREMENT's own position/audit trail. The
  compensating buy has the identical problem: it exists to *restore* the donor's exposure, so it
  must be owed to, and eventually booked against, the donor's own portfolio — never the
  recipient's.

**Worked numeric example.** MAIN's `BTC-USD` buy-list item needs 2000 PLN more IKE capacity than
MAIN's own IKE new-deposit room and MAIN's own IKE crypto-donor pool can supply. Tier 2 finds
RETIREMENT's `PL.ETNVIRBTCP` holding in IKE, currently worth 5000 PLN, as the best cross-portfolio
donor. The sell: `donorKey = SellKey(RETIREMENT, IKE, "PL.ETNVIRBTCP")`, decremented from 5000 to
3000; the row booked via `upsertCompanionRow` is `(portfolio=RETIREMENT, ticker=PL.ETNVIRBTCP,
accountType=IKE, plannedValuePln=-2000)`. The freed 2000 PLN buys MAIN's own `PL.ETNVIRBTCP` into
MAIN's IKE (recipient-side row, unaffected by this section — still keyed by MAIN, as always). The
owed compensating buy: `OwedKey(RETIREMENT, "BTC-USD") += 2000` — reconciled at the end of
`computePlan` into a `REGULAR`/`RETIREMENT`/`BTC-USD` row (new or merged), **not** a
`REGULAR`/`MAIN`/`BTC-USD` row, even though it was MAIN's shortfall that triggered the sale.
RETIREMENT ends the session having swapped 2000 PLN of IKE-wrapped `PL.ETNVIRBTCP` for 2000 PLN of
`REGULAR`-account `BTC-USD` — a real, if less tax-efficient, wrapper change for RETIREMENT, funded
entirely by RETIREMENT's own subsequent compensating buy, not MAIN's budget.

Recipient-side writes (the item's own new-deposit row from steps 1/3, and the companion
substitution row when `useCompanion`) are **unaffected** — they stay keyed by the pass's own
`portfolio` parameter exactly as today, because the item being purchased still belongs to whichever
portfolio's buy list produced it. Only the donor's own sell row and its owed compensating buy
switch to `donor.getPortfolio()`.

### 3. Self-donation exclusion narrowed to the same-portfolio tier

ADR-061 §4's self-donation exclusion (`excludeParentTicker = item.ticker()`) exists for one
reason: selling and immediately rebuying the *identical* `(portfolio, ticker, accountType)` tuple
for the *same recipient* would collide with `psi_session_portfolio_ticker_account_uq` and,
economically, is a pointless trade (ignoring spread/fee cost, zero net position change). That
collision is only reachable when donor and recipient share a portfolio — the sell row and the buy
row are then the same tuple.

**Once tier 2 draws from the *other* portfolio, the same collision is structurally impossible**:
a MAIN recipient drawing on RETIREMENT's own holding of the recipient's own crypto family (e.g.
MAIN's `BTC-USD` item selling RETIREMENT's `PL.ETNVIRBTCP`) produces two different tuples —
`(RETIREMENT, PL.ETNVIRBTCP, accountType)` sold vs. `(MAIN, PL.ETNVIRBTCP, accountType)` bought —
by construction of the two-tier filter (tier 2 candidates are always filtered to the portfolio
*other than* the recipient's own). It is also not the economically pointless trade the
same-portfolio exclusion guards against: RETIREMENT's exposure genuinely moves to a compensating
`REGULAR`-account rebuy (a real wrapper downgrade RETIREMENT now carries) while MAIN gains
IKE/IKZE placement it would not otherwise have gotten (§2's worked example) — exactly the
reallocation this ADR exists to enable, not a degenerate case to suppress.

**Decision: the exclusion filter applies only within the same-portfolio (tier 1) candidate pool.
Tier 2 (cross-portfolio) applies no self-donation exclusion at all.** This is a deliberate
architect judgment call — none of the settled decisions in the originating task spelled this out
explicitly — flagged here because naively reusing the same exclusion predicate unconditionally
across both tiers would silently and incorrectly block a legitimate, collision-free trade.

### 4. Cross-portfolio tiebreak needs the other portfolio's own deviation data

> **Superseded 2026-11-09 (task 192):** the donor ranking is now a fixed order (ADR-061 §3), so no
> deviation data is threaded into `findBestCryptoDonor` and the session computes the rebalancing
> buy list only for the portfolios it includes. This section is kept as the record of the original
> design.

ADR-061 §3's ranking ("most over-target parent first, by the buy list's `deviationPct`") reads a
deviation map built from whichever buy list is threaded into `findBestCryptoDonor` — today, always
the currently-processing pass's own list. Tier 2 needs the *other* portfolio's deviation data for
its own ranking, and that portfolio's buy list may be **empty this session** (not selected by the
user) — the very case where tier 2 has the most to offer.

**Decision:** replace the single, buy-list-derived `deviationMap` with a caller-supplied
`Map<Portfolio, Map<String ticker, BigDecimal deviationPct>>` — call it `deviationByPortfolio` —
populated for **both** portfolios unconditionally, threaded through `computePlan` →
`processPortfolio` → `applyCryptoSellStep` → `findBestCryptoDonor` (replacing the
`List<BuyListItemDto> buyList` parameter used today purely to build that map; nothing else in the
signature chain needs `buyList` itself). Each tier's ranking reads
`deviationByPortfolio.get(candidatePortfolio)`.

**Caller change required in `PurchaseSessionService.createSession`.** Today, `rebalancingService.calculate(...)`
is only invoked for a portfolio when `includeRetirement`/`includeMain` is true — the buy list and
the budget are both gated on session selection. This ADR requires computing **both** portfolios'
rebalancing results unconditionally, so a deviation map is always available for whichever
portfolio might end up as a tier-2-only donor source. The existing `includeRetirement`/`includeMain`
gating is otherwise **unchanged**: `retirementList`/`mainList` (the buy-list arrays actually turned
into purchase rows) and `retirementBudgetPln`/`mainBudgetPln` still stay empty/zero when a
portfolio isn't included this session — only the deviation-map extraction becomes unconditional.
`RebalancingService.calculate` is a pure, side-effect-free computation over ~30 in-memory
positions (the same guarantee the existing portfolio views already rely on), so invoking it an
extra time purely for its deviation numbers carries no correctness, persistence, or meaningful
performance risk.

### 5. Not a rebalancing sell, and no relationship to ADR-045

This ADR extends ADR-061's crypto-donor waterfall — an IKE/IKZE tax-wrapper account-capacity
shuffle that exists purely to fund a *buy*, never to reduce a position's target — purely by
**widening where it is allowed to search for a donor**. It does not touch, reopen, weaken, or
otherwise interact with ADR-045's permanent invariant that `MainRebalancingStrategy`,
`RetirementRebalancingStrategy`, and `PurchasePlanningService`'s *rebalancing* output never
compute or emit an algorithmic sell of an overshooting position — that remains exclusively a
manual, user-initiated action via the "Sprzedaj" button (`docs/architecture/05-rebalancing-algorithm.md`
§7.4–§7.5). ADR-061 already carved the crypto-donor sell out of that invariant, with its own
explicit "not a portfolio-rebalancing sell" callout (§8.2); this ADR only widens the *scope* of an
already-accepted exception, and changes nothing about *what kind* of sell it performs, *why* it
fires, or *when* it is triggered. "The purchases endpoint never sells to rebalance the portfolio"
and "the purchases endpoint sometimes sells a crypto companion, now possibly from the other
logical portfolio's holdings, to shuffle tax-advantaged capacity" remain two independent, both-true
statements — do not conflate them.

### 6. Accepted order-dependence — explicitly out of scope for mitigation

Cross-**session** order-dependence (a MAIN session this week partially draining RETIREMENT's donor
pool, then a RETIREMENT session next week finding that pool already partly consumed) is explicitly
accepted as fine, per the user's own framing — "whatever comes first." **No reservation, locking,
priority, or fairness mechanism is designed here, and none should be added.** This mirrors the
intra-session order-dependence ADR-061 already documented for its own shared `ikeLeft`/`ikzeLeft`/
`sellableCryptoPln` pools (a first-come-first-served greedy allocation, order-dependent at the
per-row level but not at the conservation-total level) — this ADR extends the identical shape
across the session boundary and across the portfolio boundary; it does not introduce a materially
new kind of nondeterminism, just a wider surface for the same, already-accepted kind.

## Deposit accounting: a necessary, ADR-061-invalidating consequence (not requested, but required)

ADR-061 §8 (echoed in `docs/architecture/05-rebalancing-algorithm.md` §8.2, "Downstream deposit
accounting needs no change") proved that `PurchaseSessionService`'s Step 4 auto-deposit logic
(`docs/architecture/06-data-flow.md` §Phase 3 Step 4) — which groups non-sell items by
`(portfolio, accountType, exchange)`, sums them, and nets migration-sell PLN found *in that same
group* against the group total (floored at 0) — remains correct without any code change. That
proof rested on one load-bearing fact: **a donor and the recipient it financed always shared a
`portfolio`, so the sell row and the buy row it financed always landed in the same group.**

This ADR breaks that fact by construction (§2): a donor's sell row is now booked under the
*donor's* portfolio, while the buy row it financed is booked under the *recipient's* portfolio,
which — in exactly the case this ADR exists for — can differ. Concretely, with the §2 worked
example (RETIREMENT donor sells 2000 PLN of `PL.ETNVIRBTCP`/IKE to fund MAIN's buy of the same
ticker/IKE):

- **RETIREMENT's `(RETIREMENT, IKE, XTB)` group** now contains a −2000 PLN migration-sell with
  *no offsetting buy in the same group* (the offsetting buy is in MAIN's group). If RETIREMENT
  also had, say, 500 PLN of its own genuine new-deposit fills in that same group this session, the
  existing formula computes `max(0, 500 − 2000) = 0` — silently discarding a real 500 PLN deposit
  that RETIREMENT actually needs recorded for cap-usage and XIRR purposes.
- **MAIN's `(MAIN, IKE, XTB)` group** now contains a +2000 PLN buy row with *no offsetting sell in
  the same group* (the offsetting sell is in RETIREMENT's group) — the existing formula has
  nothing to net it against, so it is wrongly counted as 2000 PLN of MAIN's own real new capital,
  when in fact zero new cash moved into the account at all (this was an in-account swap; the real
  new capital, correctly, is the *compensating buy* landing later in `REGULAR`/RETIREMENT).

Both errors independently corrupt IKE/IKZE cap-usage tracking and per-portfolio XIRR the moment a
cross-portfolio donation occurs. This was not something the originating task asked about, but it
is a direct, load-bearing consequence of §2's own (necessary, correct) fix, and is squarely an
architecture-level correctness gap — flagged and fixed here rather than left for backend to
discover as a silent numbers bug later, in the same spirit ADR-061 itself flagged its own
previously-unnoticed "compensating buy targets the wrong ticker" defect.

**Root cause.** The old formula never actually needed to track "how much of this group's total is
sell-financed" directly — it reconstructed that figure after the fact by exploiting the coincidence
that a sell and the buy it financed always shared a group. Once that coincidence no longer holds,
post-hoc reconstruction from a flat `(portfolio, accountType, exchange)`-grouped row list is not
recoverable in general (a single merged sell row, per ADR-062's netting registry, can itself
aggregate draws from multiple recipients across both portfolios).

**Fix: track genuine new-deposit PLN directly, at the source, instead of reconstructing it.**
`PurchasePlanningService.computePlan` must expose, alongside its existing
`List<PurchaseSessionItem>` result, an authoritative per-bucket figure for exactly how much
**real, no-sell-attached** new-deposit PLN was contributed to each `(Portfolio, AccountType)`
bucket this session — call it `newDepositPlnByBucket`. This is trivial to compute correctly
because it already exists as an intermediate value inside `processPortfolio`: it is exactly the
sum of `ikeFillPln` (step 1) and `ikzeFillPln` (step 3) for that portfolio's own IKE/IKZE
buckets — the two new-deposit waterfall steps ADR-060 introduced, which by definition never
involve a donor sale and are computed *before* any sell-financed top-up is merged into the same
row. (Exchange is always `XTB` for every IKE/IKZE-eligible row — direct XTB positions and every
companion ETP alike — so the bucket key needs no `Exchange` dimension; `REGULAR`/exchange-scoped
buckets are unaffected, see below.)

`PurchaseSessionService`'s Step 4 changes from "sum the group, then net sells found in the same
group against it" to a direct lookup:

- For `accountType ∈ {IKE, IKZE}` groups: deposit amount = `newDepositPlnByBucket.get((portfolio, accountType))`,
  looked up directly — **no group-level sell-netting computation at all**. `is_migration_sell`
  items remain excluded from ever contributing to a deposit (unchanged), but they are no longer
  *subtracted* from anything either — the new-deposit figure was never contaminated by
  sell-financing in the first place, regardless of which portfolio donated.
- For `accountType = REGULAR` groups: **unchanged** — sum of all non-sell items in the group,
  exactly as today. `REGULAR` rows are never sell rows (sells only ever happen on IKE/IKZE) and the
  compensating buy (already correctly attributed to the donor's own portfolio by §2) always lands
  here as genuine new capital, matching physical reality (buying crypto on Binance/XTB-regular to
  replace what was sold really does require new cash) — this was already true and is untouched.

**Why this is provably correct, including under cross-portfolio donation.** For any `(accountType,
exchange)` physical bucket, pooling *both* portfolios' totals this session:
`Σ(all buy PLN) − Σ(all sell PLN) = Σ(each portfolio's own new-deposit fills)` always holds exactly,
because every sell PLN funds exactly one buy PLN 1:1 by construction (the whole-item accounting
identity, ADR-061 §2, unaffected by this ADR) — so the sell-financed and buy-financed components
cancel in aggregate regardless of which portfolios they crisscross. Reading each portfolio's own
new-deposit figure *directly*, rather than trying to recover it by netting within one portfolio's
group alone, sidesteps the need to prove that recovery is possible from a flat, already-merged row
list — it simply never discards the information in the first place.

**No schema/Flyway change required.** `newDepositPlnByBucket` is an in-memory return value from
`PurchasePlanningService`, not a persisted column — consistent with ADR-060/061's own preference
for solving this class of problem in application logic rather than the schema. `computePlan`'s
signature changes from returning `List<PurchaseSessionItem>` to a small wrapper (e.g. a record
pairing the existing list with this new map); no `purchase_session_items` column changes.

## Alternatives considered

- **Keep donor search portfolio-scoped (status quo, ADR-061's own explicit choice).** Rejected —
  does not reflect the physical-account reality the user described; real IKE/IKZE tax-advantaged
  capacity sits idle in the other portfolio's book while a same-session item elsewhere falls back
  to `REGULAR` unnecessarily.
- **Merge MAIN and RETIREMENT into one undifferentiated donor pool (search the union always, no
  self-first preference).** Rejected — discards the sequencing meaning ("RETIREMENT services its
  own needs first, matching the spreadsheet order," §1) and would let a portfolio that didn't even
  need the fallback opportunistically raid the other's holdings ahead of its own pool, with no
  offsetting benefit. Self-first-then-fallback strictly dominates.
- **Net deposit accounting by pooling groups across portfolios and splitting the result
  proportionally.** Considered as a fix for the deposit-accounting gap above; rejected in favor of
  tracking `newDepositPlnByBucket` directly — proportional splitting is an approximation
  (introduces rounding/attribution ambiguity) where an exact figure is already available for free
  at the point `PurchasePlanningService` computes it.
- **Apply the self-donation exclusion uniformly across both tiers** (§3) — rejected: over-excludes
  a legitimate, collision-free, economically meaningful cross-portfolio trade for no correctness
  benefit; the exclusion's entire purpose (avoiding a real tuple collision) does not apply
  cross-portfolio.
- **Reservation/priority system for cross-session fairness** — rejected per §6, explicitly out of
  scope per the user's own instruction.

## Consequences

- **Code changes confined to two files:** `PurchasePlanningService` (two-tier `findBestCryptoDonor`,
  `deviationByPortfolio` replacing the `buyList` parameter used for ranking, `donorKey`/`OwedKey`
  construction switched to `donor.getPortfolio()`, `computePlan`'s return type gains
  `newDepositPlnByBucket`) and `PurchaseSessionService` (`createSession` computes both portfolios'
  `RebalancingResultDto` unconditionally for deviation context; `completeSession`'s Step 4 rewritten
  to use the new bucket map for IKE/IKZE groups).
- **No schema/Flyway change** — same conclusion as ADR-061, for the same reason: nothing here adds
  a column, a table, or changes a constraint.
- **Tests required** (full list in the accompanying task file,
  `.claude/tasks/cross-portfolio-crypto-donor-waterfall.task.md`): a MAIN-only session drawing on
  RETIREMENT's untouched pool with the compensating buy correctly attributed to
  RETIREMENT/`REGULAR`; a combined RETIREMENT-then-MAIN session where RETIREMENT partially drains
  its own pool and MAIN falls back into the leftover; same-crypto-family cross-portfolio donation
  proceeding without being blocked by the (now same-portfolio-only) self-exclusion; same-portfolio
  self-donation still correctly blocked (regression); and the corrected deposit/cap-usage/XIRR
  figures for both portfolios in a cross-portfolio scenario (the specific gap this ADR's "Deposit
  accounting" section fixes).
- **Frontend:** `ActiveSessionView.tsx`'s table-visibility gating (`showEmerytura`/`showInwestycje`,
  keyed purely off `session.portfoliosIncluded`) must change to key off actual item presence
  (`emeryturaItems.length > 0` / `inwestycjeItems.length > 0`) — otherwise a MAIN-only session that
  now legitimately contains RETIREMENT-tagged rows (the donor sell, the compensating buy) would
  render an empty MAIN-only view with no RETIREMENT table, hiding exactly the cross-portfolio
  activity the user needs to review before pressing "Zakupy zrobione." See the task file for the
  full frontend spec, including a review-surface note/badge for cross-portfolio rows.
- **Doc follow-ups, both applied alongside this ADR:** `docs/architecture/05-rebalancing-algorithm.md`
  §8/§8.2 (the two-tier fallback, the donor-portfolio-attributed compensating buy, the narrowed
  self-exclusion, the tiebreak-context threading) and `docs/architecture/06-data-flow.md`'s Phase 3
  Step 4 description (the `newDepositPlnByBucket` replacement for group-level sell-netting) are
  both updated directly as living, current-target specification — not left pointing back at this
  ADR for the "what," only for the "why."
- **Housekeeping alongside this ADR:** `docs/architecture/05-rebalancing-algorithm.md` §7.8's
  "spec decided (ADR-063), not yet implemented" banner and §8's "spec decided (ADR-061), not yet
  implemented" banner were both stale — `RebalancingTransferService`/`RebalancingTransferDialog.tsx`
  (ADR-063) and `PurchasePlanningService`'s five-step waterfall (ADR-061) are both already
  implemented in the working tree. Both banners are corrected as part of this same documentation
  pass. `.claude/tasks/ikze-crypto-donor-capacity-waterfall.task.md` and
  `.claude/tasks/rebalancing-transfer-portfolio-scoping.task.md` are both fully implemented and are
  moved to `.claude/tasks/done/`.
