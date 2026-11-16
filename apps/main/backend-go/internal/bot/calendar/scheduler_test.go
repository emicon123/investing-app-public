package calendar

import (
	"testing"
)

// TestSchedulerDisabledByDefault pins the shadow-mode safety posture: without an
// explicit BOT_SCHEDULER_ENABLED=true, neither the 1s release watcher nor the 04:00
// purge may run — Java stays the single actor until the flip.
func TestSchedulerDisabledByDefault(t *testing.T) {
	t.Setenv("BOT_SCHEDULER_ENABLED", "")
	if SchedulerEnabled() {
		t.Fatal("expected scheduler OFF with env unset")
	}
	t.Setenv("BOT_SCHEDULER_ENABLED", "0")
	if SchedulerEnabled() {
		t.Fatal("expected scheduler OFF with '0'")
	}
	t.Setenv("BOT_SCHEDULER_ENABLED", "true")
	if !SchedulerEnabled() {
		t.Fatal("expected scheduler ON with 'true'")
	}
}
