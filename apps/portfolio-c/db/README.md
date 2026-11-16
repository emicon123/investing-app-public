# db/ — schema source of truth (portfolio-c, database `portfolio-c`)

`db/migration/` holds the **Flyway DDL migrations** (`V1__…` to `V8__…`) for this app's `portfolio-c`
database (ADR-098 Decision 1). It is the single source of truth for that schema.

- **Owner:** the Architect. A schema change is a new `V<N+1>__…sql`; never edit or rename an applied
  file (Flyway checksums it against `flyway_schema_history`).
- **Nothing but `V*.sql` files inside `db/migration/`** — Flyway scans it recursively and
  `packages/common/go/common-go/testsupport` copies every top-level file of it into the Flyway
  container. Documentation lives here, one level above.
- **Consumers:** `backend-go/sqlc.yaml` (explicit, numerically ordered file list — re-generate it with
  the root repo's `backend-go/hack/gen-sqlc-schema-list.sh` when a migration is added) and this app's Go
  integration tests (`testsupport.StartMigratedPostgres`, same pinned `flyway/flyway` CLI image as production).

**Not to be confused with `backend-go/db/`**, which holds the sqlc *queries*, the generated Go code and
the migration smoke test — none of that is schema.
