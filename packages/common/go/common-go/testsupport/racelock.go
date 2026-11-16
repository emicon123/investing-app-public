package testsupport

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RaceWhilePositionLocked makes a lost-update race deterministic (task-174 finding F3.1,
// ADR-111). It holds a row lock (SELECT … FOR UPDATE) on positions.id = positionID, starts the
// fns one at a time — each is blocked on a lock inside PostgreSQL (that row lock, or the
// money-write advisory lock a serialised transaction takes first) before the next starts, so
// locks are granted in argument order — then releases the row and returns each fn's error in
// order.
//
// Without serialisation every fn has already read the pre-lock state when the row is released,
// so the LAST fn's write deterministically overwrites the earlier ones; with
// pgtx.SerializedRunner each later fn reads the state the earlier ones committed. The
// positions table exists in all three apps' schemas.
func RaceWhilePositionLocked(ctx context.Context, t *testing.T, pool *pgxpool.Pool, positionID int64, fns ...func() error) []error {
	t.Helper()
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("testsupport: begin row-lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT id FROM positions WHERE id = $1 FOR UPDATE`, positionID); err != nil {
		t.Fatalf("testsupport: lock position %d: %v", positionID, err)
	}

	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Go(func() { errs[i] = fn() })
		waitForLockWaiters(ctx, t, pool, i+1)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("testsupport: release position %d: %v", positionID, err)
	}
	wg.Wait()
	return errs
}

// waitForLockWaiters polls pg_stat_activity until want backends of this database wait on a
// lock, failing after 30 s.
func waitForLockWaiters(ctx context.Context, t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatalf("testsupport: count lock waiters: %v", err)
		}
		if waiting >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("testsupport: only %d of %d writers reached a lock wait within 30s", waiting, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
