// Package db has no non-test code yet — no domain queries exist until Phase 1 (sqlc.yaml,
// db/queries/*.sql land here). This file only proves the one thing genuinely load-bearing for
// this phase: investing-app's real Flyway history (see sqlc.yaml's schema: path)
// applies cleanly through common-go/testsupport's fixture, using the same Flyway CLI image as
// prod — so Go integration tests assert against literally the same schema Java's Testcontainers
// tests do (per the Backend-Go agent's quality gate), verified now rather than assumed until
// Phase 1's first real query.
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

	// Asset-model invariants (task 156): EVERY positions row has an asset, EVERY asset has
	// exactly one primary subasset, and the legacy columns are gone. A fresh install from the
	// baseline has no rows, so the row checks hold trivially; the column check still bites.
	var orphans, badPrimaryCount, legacyCols int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM positions WHERE asset_id IS NULL").Scan(&orphans); err != nil {
		t.Fatalf("count orphan subassets: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM assets a
		WHERE (SELECT count(*) FROM positions p WHERE p.asset_id = a.id AND p.is_primary) <> 1`).Scan(&badPrimaryCount); err != nil {
		t.Fatalf("count assets without exactly one primary: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'positions' AND column_name IN
		('is_companion_row','companion_parent_ticker','target_allocation_pct','target_allocation_pct_risk_off')`).Scan(&legacyCols); err != nil {
		t.Fatalf("count legacy columns: %v", err)
	}
	if orphans != 0 || badPrimaryCount != 0 || legacyCols != 0 {
		t.Errorf("fresh-DB asset model invariants violated: orphans=%d assetsWithoutExactlyOnePrimary=%d legacyColumns=%d",
			orphans, badPrimaryCount, legacyCols)
	}

	// Spot-check a handful of tables the write-ownership audit (doc 13 §6.3) names, so a future
	// phase's first sqlc query has direct proof this fixture sees the real schema. NOTE: the
	// actual Flyway-created table is "asset_purchases" (plural) — doc 13 §6.3 and the master
	// task's cross-cutting rules both say "asset_purchase" (singular); same mismatch for
	// "purchase_session*" (actual: purchase_sessions, purchase_session_items) and
	// "binance_transfer" (actual: binance_transfers). Flagged for the Architect/next
	// phase — table-name correctness matters directly for Phase 4/7's write-ownership work.
	// idempotency_keys: ADR-111 (task 177, V210).
	for _, table := range []string{"positions", "deposits", "asset_purchases", "price_cache", "idempotency_keys"} {
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
