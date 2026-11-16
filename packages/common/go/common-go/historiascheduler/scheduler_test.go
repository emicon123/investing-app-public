package historiascheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestRunDaily_PanickingJobCostsOneNightNotTheLoop: the first nightly job panics, the loop
// survives it (lifecycle.Guard per tick) and the next night's job runs.
func TestRunDaily_PanickingJobCostsOneNightNotTheLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var calls atomic.Int32
		job := func(context.Context) error {
			if calls.Add(1) == 1 {
				panic("snapshot blew up")
			}
			return nil
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			RunDaily(ctx, job)
		}()

		time.Sleep(49 * time.Hour) // at least two 23:00 Warsaw fires
		if got := calls.Load(); got < 2 {
			t.Fatalf("expected the loop to run the job again after a panic, ran %d time(s)", got)
		}

		cancel()
		<-done
	})
}

// Moved from main internal/historia/scheduler_test.go (task 178).
func TestSchedulerEnabledDefaultsOff(t *testing.T) {
	t.Setenv("HISTORIA_SCHEDULER_ENABLED", "")
	if SchedulerEnabled() {
		t.Fatal("scheduler must be OFF by default (single-writer Java until the flip)")
	}
	for _, v := range []string{"true", "TRUE", "1", " true "} {
		t.Setenv("HISTORIA_SCHEDULER_ENABLED", v)
		if !SchedulerEnabled() {
			t.Fatalf("expected enabled for %q", v)
		}
	}
	t.Setenv("HISTORIA_SCHEDULER_ENABLED", "false")
	if SchedulerEnabled() {
		t.Fatal("expected disabled for \"false\"")
	}
}
