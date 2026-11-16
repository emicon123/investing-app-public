-- Trivial fixture migration used only by testsupport's own smoke test
-- (postgres_test.go), to prove the Postgres+Flyway fixture mechanism works generically without
-- depending on any specific app's real schema. Each backend-go module's own tests point
-- StartMigratedPostgres at that app's REAL Flyway migration directory instead.
CREATE TABLE testsupport_smoke (
    id   SERIAL PRIMARY KEY,
    name TEXT NOT NULL
);
