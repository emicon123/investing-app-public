# ADR-084: Flyway migrations must not assert absolute values on live ledger columns

**Date:** 2026-09-02
**Status:** Accepted

---

## Context

`V60__zpr1_ikze_topup_from_etfbcash_swap.sql` applies a relative delta
(`units_ike = units_ike + <delta>`, `units_ikze = units_ikze + <delta>`) to
`positions.units_ike`/`units_ikze` for ZPR1.DE — columns the running application
maintains at runtime as a live trading ledger via ordinary DCA/rebalancing buy flows,
not columns fully derived by the migration chain. It then hard-asserts the resulting
value is an exact absolute number (`RAISE EXCEPTION` if `units_ike != 6.39810000` /
`units_ikze != 0.42630000`).

That absolute value was only reachable because five weeks of real, correctly-recorded
trades (`asset_purchases` rows 97, 112, 120, 128) had already accumulated on production
between V41 (ZPR1.DE reinstated at `units_ike=0, units_ikze=0`) and V60's authoring date
(2026-08-26). A fresh database (Testcontainers, CI, a new dev machine) replays V41
through V59 cleanly but reaches V60 with `units_ike`/`units_ikze` still at 0, so the
post-update value comes out as `0.19730000`/`0.16990000` instead of the asserted
constants. The `RAISE EXCEPTION` fires, Flyway migration fails, Spring context startup
fails, and ~203 of 347 backend tests error out (`ApplicationContext failure threshold
exceeded`) as collateral damage — none of them real per-test failures.

Verified via a full from-scratch replay of V1→V61 against a disposable, throwaway
Postgres container (removed immediately after) plus a read-only cross-check of the live
production DB (`db-shared` / `investing`, `SELECT`-only): V1 through V59 apply cleanly;
V60 is the sole failure. The other migrations flagged as suspicious by the same
`RAISE EXCEPTION` grep — V50, V51, V56, V59 — were re-checked and do **not** share this
bug class: V50/V51 use absolute-literal `SET`s with sum/exact-value checks that are
fully derivable from the migration chain alone (no arithmetic on those columns anywhere
in V1–V49), and V56/V59 use `EXISTS`/`NOT EXISTS` state-existence checks immune to
starting values entirely.

This is **not** manual DDL/DML drift of the kind CLAUDE.md warns about (no out-of-band
`psql UPDATE` was found) — it is the expected, correct behavior of `units_ike`/
`units_ikze` as live ledger balances the app itself maintains outside Flyway. The actual
bug is that V60 treated a ledger balance as if it were a migration-derived constant.

## Decision

Drop V60's numeric `RAISE EXCEPTION` post-condition entirely (the `DO $$ ... END $$;`
block), keeping the `UPDATE`/`INSERT` statements — the actual correctly-recorded trade
data — untouched. The existing `positions_units_ike_nonneg`/`_ikze_nonneg` `CHECK`
constraints (from V1) already guard the only invariant worth enforcing generically
(non-negativity); a from-blank-reproducible exact-value assertion on a runtime-mutated
ledger column is not a safe invariant to encode in a migration at all.

Two other options were weighed and rejected:

- **Replay history via a corrective migration** (insert the real historical trades as
  explicit Flyway steps so the chain reproduces production state exactly) — rejected as
  the wrong precedent: routine DCA/rebalancing trades belong in `asset_purchases` via the
  app's normal buy flow, not retrofitted into migration history. Doing so here would
  imply every future trade needs a migration, which doesn't scale and contradicts how the
  rest of the chain already treats "live holdings" and "configured targets" as different
  categories.
- **Make the assertion tolerant** (check that a delta was applied, rather than an
  absolute number) — rejected as unnecessary complexity for a check that was never
  reliable even in the one environment it was written for (production's own
  `units_ikze` already moved past the asserted constant one day later, per row 175 in
  `asset_purchases`).

Because V60 already ran on production and Flyway recorded its checksum, editing the file
requires `flyway repair` against the live database to update the recorded checksum
without re-running the migration — no data changes on production, only the tracked
checksum for an already-applied migration.

## Consequences

Going forward, no migration may assert an exact absolute value against a column the
running application also writes at runtime outside Flyway — `units_ike`, `units_ikze`,
`units_regular_*`, `total_spent_pln`, `avg_buy_price`, and (flagged as a related risk,
not yet collided with) `target_allocation_pct`/`target_allocation_pct_risk_off`, which
are user-editable via `PUT /api/positions/{id}` even though no migration has hit this
specific failure mode for them yet. Such assertions must be state-existence-based
(`EXISTS`/`NOT EXISTS`, row count, non-negativity via existing `CHECK` constraints)
rather than exact-value-based, matching the precedent already established in V56 and
V59.

Fresh databases (Testcontainers, CI, a new dev machine) can once again replay the full
Flyway chain and start a Spring context successfully — restoring the CLAUDE.md
Testcontainers-against-real-Postgres quality gate for the affected ~203 tests. No
financial/portfolio data on production is altered by this change.
