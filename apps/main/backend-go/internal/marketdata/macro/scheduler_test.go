package macro

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Scheduler tests pin MacroDataScheduler's three staggered triggers: weekly Friday 18:00,
// monthly 1st 17:30, monthly 5th 18:30 Europe/Warsaw — including the 1st-falls-on-Friday
// stagger case the Java scheduler's comment calls out.

func warsaw(t *testing.T, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestNextMacroFire_WeeklyFriday(t *testing.T) {
	// Monday 2026-09-14 → Friday 2026-09-18 18:00.
	if got := nextMacroFire(warsaw(t, "2026-09-14 12:00")); got.Format("2006-01-02 15:04") != "2026-09-18 18:00" {
		t.Fatalf("expected Fri 18:00, got %s", got.Format("2006-01-02 15:04"))
	}
	// Friday before 18:00 → same day 18:00.
	if got := nextMacroFire(warsaw(t, "2026-09-18 17:00")); got.Format("2006-01-02 15:04") != "2026-09-18 18:00" {
		t.Fatalf("expected same-day 18:00, got %s", got.Format("2006-01-02 15:04"))
	}
}

func TestNextMacroFire_MonthlyTriggers(t *testing.T) {
	// Sep 30 → Oct 1st 17:30 (monthly beats the next Friday).
	if got := nextMacroFire(warsaw(t, "2026-09-30 12:00")); got.Format("2006-01-02 15:04") != "2026-10-01 17:30" {
		t.Fatalf("expected Oct 1st 17:30, got %s", got.Format("2006-01-02 15:04"))
	}
	// Sat Oct 3 → Oct 5th 18:30 (monthly catch-up beats next Friday Oct 9).
	if got := nextMacroFire(warsaw(t, "2026-10-03 12:00")); got.Format("2006-01-02 15:04") != "2026-10-05 18:30" {
		t.Fatalf("expected Oct 5th 18:30, got %s", got.Format("2006-01-02 15:04"))
	}
}

func TestNextMacroFire_FirstFallsOnFriday_Staggered(t *testing.T) {
	// 2026-05-01 is a Friday: the 1st-of-month 17:30 fires BEFORE the weekly 18:00 —
	// the two can never coincide (MacroDataScheduler's stagger guarantee).
	if got := nextMacroFire(warsaw(t, "2026-05-01 12:00")); got.Format("2006-01-02 15:04") != "2026-05-01 17:30" {
		t.Fatalf("expected May 1st 17:30, got %s", got.Format("2006-01-02 15:04"))
	}
	// Just after the 1st-trigger → the weekly 18:00 same evening.
	if got := nextMacroFire(warsaw(t, "2026-05-01 17:45")); got.Format("2006-01-02 15:04") != "2026-05-01 18:00" {
		t.Fatalf("expected May 1st 18:00 weekly, got %s", got.Format("2006-01-02 15:04"))
	}
}

// TestStartScheduler_PanickingRefreshCostsOneFire: the first scheduled refresh panics inside
// its transaction, the loop survives it (lifecycle.Guard per fire) and the next fire refreshes.
func TestStartScheduler_PanickingRefreshCostsOneFire(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fq := newFakeQueries()
		var calls atomic.Int32
		transact := func(_ context.Context, fn func(Queries) error) error {
			if calls.Add(1) == 1 {
				panic("macro refresh blew up")
			}
			return fn(fq)
		}
		svc := NewService(fq, transact, nil, NewBenchmarkService()) // nil fetcher: no network
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			StartScheduler(ctx, svc)
		}()

		// The bubble clock starts Sat 2000-01-01: fires on the 1st 17:30, 5th 18:30, Fri 7th 18:00.
		time.Sleep(8 * 24 * time.Hour)
		if got := calls.Load(); got < 2 {
			t.Fatalf("expected the loop to refresh again after a panic, ran %d time(s)", got)
		}

		cancel()
		<-done
	})
}
