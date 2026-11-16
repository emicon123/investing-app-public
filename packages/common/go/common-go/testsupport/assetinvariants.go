package testsupport

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// RowQuerier is the one pgx method AssertAssetInvariants needs (*pgxpool.Pool, *pgx.Conn and
// pgx.Tx all satisfy it).
type RowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// AssertAssetInvariants checks the asset/subasset soft-delete invariants of task 173 (ADR-109,
// design docs/architecture/diagrams/173-asset-soft-delete.md §3) against a real database. The
// assets/positions tables have the same shape for these columns in all three apps.
//
//   - I1: a live subasset always belongs to a live asset.
//   - I2: a live asset has exactly one live primary subasset.
//   - I3: a soft-deleted row inside a live asset is never primary.
func AssertAssetInvariants(t *testing.T, ctx context.Context, db RowQuerier) {
	t.Helper()
	checks := []struct {
		name, sql string
	}{
		{"I1 live subasset under a deleted asset", `SELECT count(*) FROM positions p JOIN assets a ON a.id = p.asset_id
			WHERE p.deleted_at IS NULL AND a.deleted_at IS NOT NULL`},
		{"I2 live asset without exactly one live primary", `SELECT count(*) FROM assets a WHERE a.deleted_at IS NULL AND
			(SELECT count(*) FROM positions p WHERE p.asset_id = a.id AND p.deleted_at IS NULL AND p.is_primary) <> 1`},
		{"I3 deleted primary inside a live asset", `SELECT count(*) FROM positions p JOIN assets a ON a.id = p.asset_id
			WHERE a.deleted_at IS NULL AND p.deleted_at IS NOT NULL AND p.is_primary`},
	}
	for _, c := range checks {
		var n int
		if err := db.QueryRow(ctx, c.sql).Scan(&n); err != nil {
			t.Fatalf("invariant %s: %v", c.name, err)
		}
		if n != 0 {
			t.Errorf("invariant violated: %s (%d rows)", c.name, n)
		}
	}
}
