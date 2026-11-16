package historia

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestSnapshotAll_PanicInOnePortfolioDoesNotSkipTheOther: MAIN's snapshot panics inside its
// transaction; lifecycle.Guard per portfolio means RETIREMENT is still snapshotted the same
// night, and the nightly job reports success so historiascheduler.RunDaily keeps its
// unconditional "completed" log (main's pre-task-178 loop behaviour).
func TestSnapshotAll_PanicInOnePortfolioDoesNotSkipTheOther(t *testing.T) {
	fq := newFakeQueries()
	var calls atomic.Int32
	transact := func(_ context.Context, fn func(Queries) error) error {
		if calls.Add(1) == 1 {
			panic("snapshot upsert blew up")
		}
		return fn(fq)
	}
	svc := NewService(fq, transact, stubFx{})
	if err := svc.SnapshotAll(context.Background()); err != nil {
		t.Fatalf("SnapshotAll is fail-soft per portfolio, got %v", err)
	}
	if got := int(calls.Load()); got != len(trackedPortfolios) {
		t.Fatalf("expected one snapshot transaction per portfolio despite the panic, got %d", got)
	}
}
