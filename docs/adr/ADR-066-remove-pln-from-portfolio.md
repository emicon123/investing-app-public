# ADR-066 — Remove PLN cash from the portfolio

**Date:** 2026-08-04
**Status:** Accepted
**Supersedes (partially):** ADR-050's "PLN cash stays flat at 5.0 in both regimes" constraint
**Related:** ADR-036 (5% cash buffer), ADR-044 (sliding regime blend), ADR-045 (buy-only), ADR-050/051/052/054 (target-table revisions), ADR-053 (hard-zero sell flag)

## Context

PLN cash was modelled as a MAIN `positions` row (`ticker = 'PLN'`, `asset_class = CURRENCY`,
`exposure = CASH`) carrying a `target_allocation_pct` of 5.0 in both regimes. Its value was
not the row's own units but a special case: `ValueComputationService`, `PositionService`
(twice) all substituted "the latest `cash_positions` snapshot sum" whenever they saw that
ticker in MAIN.

Cash is a funding buffer, not an investment, and the codebase already half-believed that —
three separate call sites filtered the `"PLN"` key back out of the value map:

| Call site | Filtered PLN out? |
|---|---|
| `XirrService` (drives the "Wartość portfela" summary bar) | yes |
| `HistoriaScheduler` (daily portfolio-value snapshot) | yes |
| `RebalancingService.totalPortfolioValuePln` (the 5% buffer base) | yes |
| `PositionService.computePortfolioTotals` (the `currentPct` denominator) | **no** |

That inconsistency was the actual defect. Every position's `currentPct` on `/portfolio` was
measured against a total that *included* cash, while its target was one of a set summing to
100 that also included cash's 5.0 — self-consistent in isolation, but reporting a different
"portfolio value" than the summary bar directly above it and than the number the rebalancer
sized purchases against.

A second, quieter defect lived in the rebalancer. `MainRebalancingStrategy` received the PLN
row like any other, with `N_i` = the cash balance and `C_i` = 0.05. Step 3's
`O_total = Σ N_i + A` therefore included the entire cash balance, and Step 4's
`P_i = C_i × O_total` inflated every position's target value by its share of that cash. The
Step-6 invariant still held (`ΣC_i = 1`, so `ΣQ_i = A`) and the buy-only invariant was never
at risk, so this never threw — it just skewed the *relative* deficits, biasing allocation
toward high-target positions. The PLN row could also, in principle, surface in the buy list
as a "buy PLN" entry whenever cash sat below 5%.

## Decision

**PLN is no longer a position.** The row is deleted outright, and its 5.0 percentage points
are redistributed within the MAIN target tables.

The user chose deletion over the zero-target-not-delete pattern applied to `CHFPLN=X`
(ADR-050) and to the original Bonds zeroing (V39). Those preserved rows because the holding
and its history were real investments with cost basis worth keeping; the PLN row held no
such history — its units column was a synthetic `SEED_IMPORT` figure from the one-time
Google Sheets backfill, and the live cash balance lives in `cash_positions` regardless.
Keeping it at an explicit `0.0` target would also have collided with ADR-053: a hard-zero
target with nonzero value trips the "Sprzedaj" badge, which is meaningless advice for cash
and would have required a `PLN`-shaped carve-out in the exact formula ADR-053 added to
*close* such carve-outs.

### Redistribution

The user pinned three buckets, which fixes where the freed weight can land:

- **Gold** stays `20.0` risk-on / `30.0` risk-off.
- **Bonds** stay `0.0` risk-on / `10.0` risk-off (still a risk-off-only sink, ADR-050).
- **Crypto** stays `50.0` risk-on / `15.0` risk-off — the 50% risk-on cap holds, and every
  alt→BTC and miner→BTC internal relation (ADR-052's shape) is untouched.
- **Experiments** stays `2.5` flat, as it has since ADR-047.

**EQUITY absorbs the full 5.0 in both regimes**, consistent with ADR-047's and ADR-050's
standing preference for routing freed weight into equities rather than cash or bonds.

The split between `NQSE.DE` and `ICGA.DE` is **pro-rata**, preserving their existing ratio
exactly. This departs from the 50/50 convention ADR-050 and ADR-051 used for the three
earlier swings, and the departure is deliberate and user-directed: the 50/50 rule was an
implementing session's judgment call (ADR-051 says so explicitly), and applying it here
would have drifted the risk-on Nasdaq:China ratio from 2.46 to 2.06 — a China overweight
nobody asked for, arriving as a side effect of a cash-removal change. The instruction for
this ADR was that inter-asset percentage relations should survive wherever a pin does not
force them to move.

| | scale | NQSE.DE | ICGA.DE | Σ |
|---|---|--:|--:|--:|
| risk-on | × 27.5/22.5 | 16.0 → **19.5556** | 6.5 → **7.9444** | 27.5 |
| risk-off | × 42.5/37.5 | 23.5 → **26.6333** | 14.0 → **15.8667** | 42.5 |

Both columns still sum to exactly 100.0000, which the blend mechanism (§7.2/§7.3 of
`05-rebalancing-algorithm.md`) requires — `Σ effectiveTarget = (1−blend)×100 + blend×100`
holds for every `blend ∈ [0,1]` only if both tables independently sum to 100.

### The 5% cash rule is unchanged

`deployableCashPln = (cashOnHandPln − 0.05 × totalPortfolioValuePln) / 1.05` (ADR-036) is
untouched. It never read the PLN *position* — `cashOnHandPln` comes from the
`cash_positions` snapshot (or the request override) and `totalPortfolioValuePln` already
excluded PLN. Cash is still entered and edited in Settings, and still leaves exactly 5% of
the post-purchase portfolio unspent.

## Consequences

**`/positions` percentages shift, correctly.** `computePortfolioTotals` no longer includes
cash, so every position's `currentPct` rises by a factor of `1 / (1 − cashShare)`. It now
matches the summary bar, historia, and the rebalancer's own denominator — four totals
collapse into one. Existing `historia` rows are unaffected (they never stored a `PLN` key).

**Dead special-casing removed**, rather than left as defensive no-ops:

- `ValueComputationService` — the cash-snapshot substitution and its `PLN_TICKER` constant.
- `PositionService` — both PLN branches, `PLN_TICKER`, `loadCashSnapshotTotal()`, and the
  now-unused `CashPositionRepository` constructor dependency.
- `RebalancingService`, `XirrService`, `HistoriaScheduler` — the three `!"PLN".equals(key)`
  filters, now guarding against a key that cannot exist.

`ValueComputationService.loadCashSnapshotTotal()` **remains** — it is the single source of
the cash balance for the rebalancing budget.

**`exposure_enum` keeps `CASH` with zero live rows**, the same state `QUANTUM` has been in
since V37. Postgres has no `ALTER TYPE ... DROP VALUE`, and the value stays available in
`CreatePositionDialog` should a non-PLN cash-like position ever be wanted.

**The seed purchase is deleted too.** `asset_purchases` has no FK to `positions`, so the
single `SEED_IMPORT` row for PLN (from the go-live import) would have lingered as an orphan
ticker in the trade-history chart's `excluded` list. No deposit, XIRR, or IKE/IKZE
cap-usage figure derives from it — those all read `deposits`.

**Reversal cost is low but not zero.** Re-adding PLN means re-creating the row, restoring
the cash-snapshot valuation special case, and clawing 5.0 back out of EQUITY. The
`cash_positions` history is untouched by this ADR, so no cash data is lost.

## Implementation

Flyway `V50__remove_pln_position.sql`, which deletes both rows, applies the two EQUITY
updates, and ends with a `DO $$` block that raises unless both target columns sum to exactly
100.0000 — failing the migration rather than letting `MainRebalancingStrategy` throw
`RebalancingInvariantException` on the next planning run.
