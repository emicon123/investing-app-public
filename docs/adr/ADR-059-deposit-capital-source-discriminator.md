# ADR-059: `deposits.capital_source` Discriminator — Internal Wrapper Transfers Excluded from XIRR/Historia, Still Counted for Cap Usage

**Date:** 2026-07-27
**Status:** Accepted

## Context

`deposits` (`backend/src/main/java/pl/investing/entity/Deposit.java`) is read by two
conceptually different consumers that both currently sum every row unconditionally:

1. **IKE/IKZE annual statutory contribution-cap usage** —
   `AccountCapUsageService.computeCapUsage` /
   `DepositRepository.sumAmountByAccountTypeAndYear`
   (`backend/src/main/java/pl/investing/repository/DepositRepository.java:22-23`), consumed by
   `DepositService.getAccountCapUsage` and `PurchaseSessionService.computeLimits` (ADR-058).
   Polish law caps money **entering the tax wrapper each year regardless of where it came from
   within the user's own portfolio** — a REGULAR→IKE/IKZE internal reallocation legitimately
   *should* count here. This is correct today and must not change.
2. **Portfolio-level capital-committed / XIRR `totalInvestedPln`**
   (`XirrService.computeXirr`, `backend/src/main/java/pl/investing/service/XirrService.java:67`,
   `depositRepository.findAllByPortfolioOrderByDepositDateAsc(portfolio)`) and the nightly
   Historia snapshot's `total_deposits`
   (`HistoriaScheduler.snapshotPortfolio`,
   `backend/src/main/java/pl/investing/service/HistoriaScheduler.java:119-133`, same query). Both
   answer "how much *new external cash* has this portfolio received" — a REGULAR→IKE/IKZE
   internal transfer is the same money moving between two sub-accounts of the **same** MAIN
   portfolio and must **not** count here.

`RebalancingTransferService.executeTransfer()`
(`backend/src/main/java/pl/investing/service/RebalancingTransferService.java:160-172`, ADR-054,
2026-07-22) auto-creates a `Deposit` row whenever `sold.accountType() == REGULAR` and a buy
leg's `accountType()` is `IKE` or `IKZE`, with comment
`"Auto from rebalancing transfer: sold %s (REGULAR) to fund %s (%s)"`. That row is consumed
unconditionally by both use-cases above — there is no discriminator letting use-case 2 exclude
it. Grepping every `Deposit.builder()` call site confirms this comment prefix is produced by
exactly one code path (`PurchaseSessionService` writes `"Auto from session #" + sessionId`
instead), so it is a safe, unambiguous identifier for the affected rows.

**This is structurally the same problem ADR-035 already solved once**, for a different
dual-purpose collision on this same table: `deposits` rows tagged `exchange = BINANCE` were
being read both as real capital (XIRR) and as the PIT-38 acquisition-cost ledger, and ADR-035's
fix was to give the PIT-38 figure its own table (`binance_transfers`) so `deposits` could go back
to meaning exactly one thing. ADR-054, written five days before this ADR, reintroduced the same
shape of conflation without applying that lesson — it added a second "capital, but only for one
of the two readers" row-producer to `deposits` without adding a discriminator. ADR-055
(`docs/adr/ADR-055-deposit-amount-uses-planned-not-actual.md`, 2026-07-23) explicitly reviewed
this exact `RebalancingTransferService` deposit-creation block and concluded "needs no change" —
that review was scoped to whether the row's *amount* used the right planned-vs-actual value; it
did not ask whether the row should be counted by XIRR/Historia at all, so it did not catch this.

**Live evidence** (queried against `infra-db-1`, table `deposits`): 8 rows created 2026-07-27 via
4 separate transfer submissions, comment prefix `Auto from rebalancing transfer:%`, summing to
**several thousand PLN**, all `portfolio = MAIN`, all `account_type = IKE` — currently inflating
displayed Wpłacone/XIRR by that amount against zero corresponding external cash inflow.

`PurchaseSessionService.completeSession()`'s own auto-deposit block (ADR-033/ADR-035/ADR-055's
migration-sell netting) was also audited and is **not** affected by this bug — it already nets a
migration-sell item's value against its group's buy sum before creating any deposit, so a
same-wrapper migration re-buy nets to zero and never produces a phantom deposit in the first
place. This ADR is scoped specifically to `RebalancingTransferService`'s REGULAR→IKE/IKZE
auto-deposit.

## Decision

### 1. Mechanism: a discriminator column on `deposits`, not a side-ledger table

Add `deposits.capital_source` — a Postgres enum, `EXTERNAL` or `INTERNAL_TRANSFER` — and filter
XIRR/Historia to `EXTERNAL` only. `AccountCapUsageService`/`sumAmountByAccountTypeAndYear`
continue summing **every** row regardless of this flag.

**Why a discriminator column (option a) over a second side-ledger table mirroring
`binance_transfers` (option b):**

- **The two readers already agree on every column that matters except "should this count as
  external capital."** `binance_transfers` earned its own table because it isn't just a filtered
  view of `deposits` — it has a different shape entirely (`amount_native`/`native_currency`, no
  `portfolio`, no `account_type`, sourced from a different sheet table). This row has the exact
  same shape as every other deposit (`deposit_date`, `amount`, `currency`, `exchange`,
  `account_type`, `portfolio`) — the *only* thing distinguishing it is a yes/no fact about
  provenance. A discriminator column is the smaller change precisely because the data model
  doesn't actually diverge; only the query.
- **Zero blast radius on the cap-usage path.** `AccountCapUsageService.computeCapUsage`,
  `DepositRepository.sumAmountByAccountTypeAndYear`, `DepositService.getAccountCapUsage`,
  `PurchaseSessionService.computeLimits`, and the newly-added `account_cap_adjustment`
  (ADR-058) all keep reading `deposits` exactly as they do today — this change touches none of
  that code. Option (b) would require cap-usage to union two tables (`deposits` +
  the new side-ledger) everywhere it currently reads one, multiplying the change surface across
  every one of those call sites for no behavioral gain (cap usage's answer doesn't change
  either way).
- **One filtered `WHERE`, not a second writer/reader pair to keep in sync.** Option (b) means a
  new entity, repository, and (if the app ever needs to list/audit these rows the way
  `GET /binance-transfers` does) a new endpoint — machinery earning its keep for
  `binance_transfers` because that table serves an independent reporting need (PIT-38 yearly
  sums) that has nothing to do with `deposits`. This internal-transfer flag serves no such
  independent need; it exists purely to be excluded from two queries.
- **Consistent with how the codebase already handles this exact kind of fork.**
  `asset_purchases.trade_type` (ADR-057, `V45__asset_purchase_trade_type.sql`) added an enum
  discriminator to distinguish `REBALANCING_TRIM_SELL`/`REBALANCING_REINVEST_BUY`/`DCA_BUY`/etc.
  on the *same* table rather than splitting `asset_purchases` into per-trade-type tables — this
  ADR applies the identical, already-precedented pattern to `deposits`.

Option (b) is not wrong in the abstract — ADR-035 shows the codebase is willing to reach for a
side-ledger table when the situation calls for it — but the deciding difference is that
`binance_transfers` needed a different shape and a genuinely separate reporting surface, and this
case needs neither. Reaching for a new table here would be applying that pattern by rote instead
of by fit.

### 2. Schema

New Flyway migration `backend/src/main/resources/db/migration/V47__deposit_capital_source.sql`
(next version after `V46__account_cap_adjustment.sql`):

```sql
CREATE TYPE capital_source_enum AS ENUM ('EXTERNAL', 'INTERNAL_TRANSFER');

ALTER TABLE deposits
    ADD COLUMN capital_source capital_source_enum NOT NULL DEFAULT 'EXTERNAL';

-- Backfill: the only rows that are NOT genuine external capital today are the 8 auto-created
-- by RebalancingTransferService's REGULAR->IKE/IKZE auto-deposit (ADR-054). Every other
-- existing row (manually entered, or auto-created by PurchaseSessionService.completeSession())
-- is real external capital and correctly keeps the column default of 'EXTERNAL'.
UPDATE deposits
SET capital_source = 'INTERNAL_TRANSFER'
WHERE comment LIKE 'Auto from rebalancing transfer:%';
```

`DEFAULT 'EXTERNAL'` on the `ADD COLUMN` means every pre-existing row — including the 8 affected
ones, until the `UPDATE` runs immediately after in the same migration — is classified correctly
with no separate backfill step needed for the general case; only the targeted `UPDATE` above is
required, and it is scoped by the same comment-prefix match already audited as unique to this one
call site. No `session_id`-style linkage is needed the way ADR-055's backfill needed one for
`PurchaseSessionService` — the comment prefix alone is a sufficient, unambiguous key here because
`RebalancingTransferService` never writes any other kind of deposit.

### 3. Exact call sites to change

| File | Change |
|---|---|
| `backend/src/main/java/pl/investing/entity/Deposit.java` | Add `capitalSource` field: `@Enumerated(EnumType.STRING) @JdbcTypeCode(SqlTypes.NAMED_ENUM) @Column(name = "capital_source", nullable = false, columnDefinition = "capital_source_enum") private CapitalSource capitalSource;` — mirrors the existing `currency`/`exchange`/`accountType`/`portfolio` field style in this same entity. New enum `pl.investing.entity.enums.CapitalSource { EXTERNAL, INTERNAL_TRANSFER }`, alongside the existing `AccountType`/`Exchange`/`Currency`/`Portfolio` enums in that package. |
| `backend/src/main/resources/db/migration/V47__deposit_capital_source.sql` | New migration per §2 above. |
| `RebalancingTransferService.java:162-171` | Set `.capitalSource(CapitalSource.INTERNAL_TRANSFER)` on this specific `Deposit.builder()` call (the REGULAR→IKE/IKZE auto-deposit). No other `Deposit.builder()` call site in this file exists. |
| `PurchaseSessionService.java` (its `Deposit.builder()` call, ~line 421) | Set `.capitalSource(CapitalSource.EXTERNAL)` explicitly (don't rely on the entity/DB default in application code that constructs the object directly — the JPA entity has no Java-level default for a `@Builder` field, only the DB column does). |
| Any admin/manual deposit-creation path (`DepositService.createDeposit`) | Set `.capitalSource(CapitalSource.EXTERNAL)` explicitly, same reasoning — a hand-entered deposit is by definition new external capital; there is no manual-entry path for recording an internal transfer, since `RebalancingTransferService` is the sole producer of that classification. |
| `DepositRepository.java` | New method: `@Query("SELECT d FROM Deposit d WHERE d.portfolio = :portfolio AND d.capitalSource = pl.investing.entity.enums.CapitalSource.EXTERNAL ORDER BY d.depositDate ASC") List<Deposit> findAllByPortfolioAndCapitalSourceExternalOrderByDepositDateAsc(@Param("portfolio") Portfolio portfolio)` — or equivalent Spring-Data-derived name (`findAllByPortfolioAndCapitalSourceOrderByDepositDateAsc(Portfolio, CapitalSource)`, called with `CapitalSource.EXTERNAL` at both call sites below). Either is acceptable; pick whichever is more consistent with this repository's existing mix of derived and `@Query` methods. **`sumAmountByAccountTypeAndYear` and `findAllByAccountTypeAndYear` are unchanged** — no `capitalSource` filter, no new parameter — see §4. |
| `XirrService.java:67` | Replace `depositRepository.findAllByPortfolioOrderByDepositDateAsc(portfolio)` with the new `EXTERNAL`-filtered query. Also update the adjacent comment (lines 65-66, `"ADR-035: ALL deposits count..."`) — it is no longer accurate; replace with a comment pointing at ADR-059 and explaining that internal wrapper transfers are excluded here (but still counted for cap usage). |
| `HistoriaScheduler.java:119` | Same replacement as `XirrService.java:67` — `findAllByPortfolioOrderByDepositDateAsc(portfolio)` → the new `EXTERNAL`-filtered query, for the `total_deposits` computation at lines 119-133. |
| `AccountCapUsageService.java` | **No change.** `computeCapUsage` (line 70-82) calls `depositRepository.sumAmountByAccountTypeAndYear(accountType, year)` (line 72), which must keep summing every row regardless of `capitalSource` — confirmed by §1's Context: internal REGULAR→IKE/IKZE transfers are genuine statutory contributions and must keep counting toward the annual cap. `DepositRepository.sumAmountByAccountTypeAndYear`/`findAllByAccountTypeAndYear` themselves also need no `capitalSource` filter added — see §4. |

### 4. `AccountCapUsageService` and the underlying queries need no change — confirmed

`AccountCapUsageService.computeCapUsage` and `upsertAdjustment` (ADR-058) both go through
`DepositRepository.sumAmountByAccountTypeAndYear`, which sums unconditionally by
`(accountType, year)` with no reference to provenance. That is exactly the desired behavior per
§1 use-case 1 — the 8 existing `INTERNAL_TRANSFER` rows must keep counting here, and any future
`INTERNAL_TRANSFER` row must too. No filter is added, no new parameter, no overload. The only
queries that gain an `EXTERNAL` filter are the two feeding XIRR/Historia (§3).

## Backfill spec

Covered in full in §2 — the `UPDATE ... WHERE comment LIKE 'Auto from rebalancing transfer:%'`
statement in `V47__deposit_capital_source.sql` **is** the backfill; there is no separate backfill
migration the way ADR-055 needed one (`V44`), because:

- The scope is a single, already-audited, unambiguous comment-prefix match (confirmed unique to
  `RebalancingTransferService` by grepping every `Deposit.builder()`/`.comment(...)` call site in
  the codebase — see Context above) rather than ADR-055's problem of needing to parse a session
  id out of free text and re-derive per-item recomputation groups.
- There is exactly one column being set (`capital_source`), to exactly one of two possible enum
  values, with no arithmetic and nothing to get subtly wrong — unlike ADR-055's backfill, which
  had to recompute a `NUMERIC` sum per group and therefore needed the defensive `RAISE
  WARNING`/cardinality-mismatch machinery `V44` used.

**Historia self-corrects, same as ADR-055 noted.** `historia.total_deposits` is recomputed fresh
every night from whatever `deposits` currently contains — it is not a stored running total — so
once `HistoriaScheduler.java:119` reads through the `EXTERNAL`-filtered query, the very next
23:00 Warsaw-time run writes the corrected `total_deposits` automatically. No Historia backfill
migration is needed; today's already-written Historia rows for 2026-07-27 (and any other date
computed while the bug was live) are simply superseded by tomorrow's nightly run, same as any
other day the underlying deposit data changes.

## Consequences

- **MAIN portfolio's Wpłacone/XIRR total drops by the sum of those rows** once this ships — back to the
  correct, pre-transfer figure. This is a **currently-displayed number changing to a smaller,
  correct one** — flag it to the user as the fix taking effect, not a regression or data loss.
  Nothing about the underlying positions, units, or actual cash changes; only the
  capital-committed denominator used by XIRR/Wpłacone.
- **IKE annual cap usage for 2026 is completely unaffected** — `AccountCapUsageService` keeps
  summing all 8 rows (§4), so `GET /deposits/account-cap-usage` and every purchase-session's
  embedded `AccountLimitsDto` report the exact same numbers before and after this change ships.
- **Every future REGULAR→IKE/IKZE rebalancing transfer is automatically classified correctly**
  going forward — `RebalancingTransferService` sets `capitalSource = INTERNAL_TRANSFER` at
  creation time (§3), so no recurring backfill process is ever needed again; this is a one-time
  historical correction plus a permanent fix to the writer.
- **No change to `binance_transfers`, `PurchaseSessionService`'s own auto-deposit block, or any
  manually-entered deposit** — all continue to be classified `EXTERNAL` (the default) and behave
  exactly as documented before this ADR.
- **Additive-only schema change** — one new Postgres enum type, one new `NOT NULL DEFAULT`
  column on an existing table, no column removed or retyped, no existing query's result changed
  except the two (XIRR, Historia) deliberately being fixed. Any client reading `Deposit` JSON
  directly would need to tolerate one new field, if `capitalSource` is ever exposed via the API
  (not required by this ADR — it is an internal filtering fact, not something the UI needs to
  show; if a future ADR wants to surface "this deposit was an internal transfer" in the Deposits
  page, that is a separate, additive API change).

## Backend implementation note

**Do not implement this in the same pass as writing this ADR** — the backend agent should base
its Java/SQL changes on this document. The exact `RAISE NOTICE`-style row-count logging used in
`V44`/`V45` is optional polish here (this migration has no cardinality ambiguity to defend
against), but adding a single `RAISE NOTICE 'ADR-059: % row(s) reclassified as
INTERNAL_TRANSFER', (SELECT COUNT(*) FROM deposits WHERE capital_source = 'INTERNAL_TRANSFER')`
after the `UPDATE` is cheap, auditable, and consistent with this repo's existing migration style.
