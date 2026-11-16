package historiaquery

import (
	"testing"
	"time"
)

// Moved from main internal/historia/scheduler_test.go (task 178): main's nightly snapshot
// now runs on historiascheduler.RunDaily, which fires on NextDailyFire, and keys rows by
// WarsawDate.

func TestNextDailyFire(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("Europe/Warsaw zone data unavailable")
	}
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"morning fires same day 23:00", time.Date(2026, 9, 15, 8, 0, 0, 0, loc), time.Date(2026, 9, 15, 23, 0, 0, 0, loc)},
		{"just before 23:00 fires same evening", time.Date(2026, 9, 15, 22, 59, 59, 0, loc), time.Date(2026, 9, 15, 23, 0, 0, 0, loc)},
		{"exactly at 23:00 fires next day (strictly after)", time.Date(2026, 9, 15, 23, 0, 0, 0, loc), time.Date(2026, 9, 16, 23, 0, 0, 0, loc)},
		{"after 23:00 fires next day", time.Date(2026, 9, 15, 23, 0, 1, 0, loc), time.Date(2026, 9, 16, 23, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NextDailyFire(c.now); !got.Equal(c.want) {
				t.Fatalf("expected %v, got %v", c.want, got)
			}
		})
	}
}

func TestWarsawDate_NotUTC(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Warsaw"); err != nil {
		t.Skip("Europe/Warsaw zone data unavailable")
	}
	// 2025-12-31 23:30 UTC is already 2026-01-01 00:30 in Warsaw — business dates
	// follow WarsawTime, never UTC.
	if got := WarsawDate(time.Date(2025, 12, 31, 23, 30, 0, 0, time.UTC)).Format("2006-01-02"); got != "2026-01-01" {
		t.Fatalf("expected 2026-01-01, got %s", got)
	}
	// Midday UTC in summer (Warsaw +2) stays the same calendar date.
	if got := WarsawDate(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)).Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("expected 2026-06-01, got %s", got)
	}
}
