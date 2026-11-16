package testsupport

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestStartMigratedPostgres_RunsFlywayAgainstRealFlywayCLI proves the fixture mechanism itself
// end-to-end: Postgres starts, Flyway (the real CLI image, not a hand-rolled migration runner)
// applies testdata/migrations/V1__testsupport_smoke.sql, and a plain pgx connection can then see
// the migrated schema. Every backend-go module's own integration tests reuse this exact fixture
// pointed at that app's real Flyway migration directory instead of this toy one.
func TestStartMigratedPostgres_RunsFlywayAgainstRealFlywayCLI(t *testing.T) {
	pg := StartMigratedPostgres(t, "testdata/migrations")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	if _, err := conn.Exec(ctx, "INSERT INTO testsupport_smoke (name) VALUES ($1)", "phase-0"); err != nil {
		t.Fatalf("insert into flyway-migrated table: %v", err)
	}

	var name string
	if err := conn.QueryRow(ctx, "SELECT name FROM testsupport_smoke WHERE id = 1").Scan(&name); err != nil {
		t.Fatalf("query migrated table: %v", err)
	}
	if name != "phase-0" {
		t.Fatalf("expected name %q, got %q", "phase-0", name)
	}

	if _, err := conn.Exec(ctx, "SELECT * FROM flyway_schema_history LIMIT 1"); err != nil {
		t.Fatalf("expected flyway_schema_history to exist (proves Flyway, not a hand-rolled runner, applied the migration): %v", err)
	}
}
