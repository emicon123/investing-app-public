// Package db has no non-test code yet — no domain queries exist until this app's own migration
// phase (Phase 6) lands (sqlc.yaml, db/queries/*.sql). This file only proves the one thing
// genuinely load-bearing for Phase 0: portfolio-b's real 16-migration Flyway history
// (see sqlc.yaml's schema: path) applies cleanly through common-go/testsupport's fixture, using
// the same Flyway CLI image as prod.
package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"investing-app/common-go/testsupport"
)

func TestRealFlywayMigrationsApplyCleanly(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../db/migration")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var count int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM flyway_schema_history WHERE success = true").Scan(&count); err != nil {
		t.Fatalf("query flyway_schema_history: %v", err)
	}
	if count < 2 {
		t.Fatalf("expected at least 2 successfully applied migrations (V1 baseline schema + V2 reference data), got %d", count)
	}

	// idempotency_keys: ADR-111 (task 177).
	for _, table := range []string{"positions", "deposits", "historia", "idempotency_keys"} {
		var exists bool
		err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s exists: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist after migration", table)
		}
	}

	// Task 173 (ADR-109): soft-delete columns exist, are nullable, and every seeded row is live.
	for _, table := range []string{"assets", "positions"} {
		var nullable string
		if err := conn.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns
			WHERE table_name = $1 AND column_name = 'deleted_at' AND data_type = 'timestamp with time zone'`,
			table).Scan(&nullable); err != nil {
			t.Fatalf("%s.deleted_at missing: %v", table, err)
		}
		if nullable != "YES" {
			t.Errorf("%s.deleted_at must be nullable", table)
		}
		var deleted int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE deleted_at IS NOT NULL").Scan(&deleted); err != nil {
			t.Fatalf("count deleted %s: %v", table, err)
		}
		if deleted != 0 {
			t.Errorf("fresh DB has %d soft-deleted %s rows, want 0", deleted, table)
		}
	}
}
