package trade

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/lifecycle"
)

// TestStartWatcher_WaitsForInFlightExecution is the shutdown contract of ADR-114: SIGTERM stops
// the watcher group, but Group.Wait does not return while an execution is between the fill and
// its record. The commit phase runs on a detached context, so once released it still places the
// stop-loss, records the trade and marks the signals executed.
func TestStartWatcher_WaitsForInFlightExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eventTime := time.Now().UTC().Truncate(time.Second)
		fx := newWatcherFixture(beatLadder(eventTime), true)
		release := make(chan struct{})
		stopping := make(chan struct{})
		fx.okx.onStop = func() {
			close(stopping)
			<-release
		}

		group := lifecycle.NewGroup(context.Background())
		group.Go("bot-watcher", func(ctx context.Context) { StartWatcher(ctx, fx.watcher) })

		<-stopping // the first tick found the cluster; the entry filled; the SL call is in flight
		group.Stop()

		short, cancelShort := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShort()
		if err := group.Wait(short); err == nil {
			t.Fatal("Wait returned while an execution was still in flight")
		}

		close(release)
		long, cancelLong := context.WithTimeout(context.Background(), time.Minute)
		defer cancelLong()
		if err := group.Wait(long); err != nil {
			t.Fatalf("Wait after the execution finished: %v", err)
		}

		if len(fx.trades.trades) != 1 || fx.trades.trades[0].Status != "OPEN" ||
			fx.trades.trades[0].ScalpSessionStatus != "PENDING" {
			t.Fatalf("expected the trade recorded OPEN/PENDING, got %+v", fx.trades.trades)
		}
		if !contains(fx.okx.calls, "stop") {
			t.Fatalf("expected the stop-loss to be placed, got %v", fx.okx.calls)
		}
		if !fx.signals.rows[0].Executed {
			t.Fatal("expected the contributing signal marked executed after shutdown began")
		}
	})
}

// A panic inside one tick costs that tick, not the watcher goroutine.
func TestStartWatcher_PanicInTickDoesNotKillTheWatcher(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eventTime := time.Now().UTC().Truncate(time.Second)
		fx := newWatcherFixture(beatLadder(eventTime), true)
		panicked := false
		fx.okx.onLeverage = func() {
			panicked = true
			panic("tick panic")
		}
		counting := &countingWatcherQueries{WatcherQueries: fx.watcher.queries}
		fx.watcher.queries = counting

		group := lifecycle.NewGroup(context.Background())
		group.Go("bot-watcher", func(ctx context.Context) { StartWatcher(ctx, fx.watcher) })

		time.Sleep(3 * time.Second) // three ticks of fake time
		group.Stop()
		if err := group.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
		if !panicked {
			t.Fatal("the panicking path never ran")
		}
		if counting.snapshots < 3 {
			t.Fatalf("the watcher stopped ticking after the panic: %d snapshot loads", counting.snapshots)
		}
	})
}

// countingWatcherQueries counts the per-tick snapshot loads, i.e. how many ticks ran.
type countingWatcherQueries struct {
	WatcherQueries
	snapshots int
}

func (c *countingWatcherQueries) WatcherSnapshots(ctx context.Context, a sqlcgen.WatcherSnapshotsParams) ([]sqlcgen.WatcherSnapshotsRow, error) {
	c.snapshots++
	return c.WatcherQueries.WatcherSnapshots(ctx, a)
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
