# db/ — schema source of truth (portfolio-b, database `portfolio-b`)

`db/migration/` holds the **Flyway DDL migrations** for this app's `portfolio-b` database (ADR-098
Decision 1): `V1` to `V6` and `V8` to `V19`.

- **There is no `V7` file.** It was applied on the live `portfolio-b` database but lost before
  this repo existed (the app was merged in as a squashed subtree), and the live `V2`/`V5`
  checksums once differed from these files (ADR-098 blocker B5). **Closed 2026-09-29
  (task 134):** after a `pg_dump` backup and a green scratch rehearsal, a metadata-only
  `flyway repair` on live rewrote the `V2`/`V5` checksums and marked `V7` DELETED (tombstone
  row; V7's effects were already gone — no `account_balances` table, its settings keys
  removed by `V10`). `flyway validate` now passes with default settings and `migrate` is a
  no-op at `V19`; the runner carries no validation override.
- **Owner:** the Architect. A schema change is a new `V<N+1>__…sql`; never edit or rename an applied file.
- **Nothing but `V*.sql` files inside `db/migration/`** — Flyway scans it recursively and
  `packages/common/go/common-go/testsupport` copies every top-level file of it into the Flyway
  container. Documentation lives here, one level above.
- **Consumers:** `backend-go/sqlc.yaml` (explicit, numerically ordered file list) and this app's Go
  integration tests (`testsupport.StartMigratedPostgres`).

**Not to be confused with `backend-go/db/`**, which holds the sqlc *queries*, the generated Go code and
the migration smoke test — none of that is schema.
