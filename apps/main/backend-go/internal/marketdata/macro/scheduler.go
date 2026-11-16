package macro

import (
	"context"
	"log/slog"
	"time"

	"investing-app/common-go/lifecycle"
)

// This file runs the macro refresh's three triggers (ported from the Java MacroDataScheduler) —
// deliberately staggered so no two can coincide even when the 1st of a month falls on a Friday:
//
//   - weekly, Friday 18:00 Europe/Warsaw (net liquidity; FRED H.4.1 publishes Thursdays ~16:30 ET)
//   - monthly, 1st 17:30 (primary Global-M2 refresh)
//   - monthly, 5th 18:30 (catches late FRED/ECB/BoE/BoJ/chinadata.live releases)
//
// All three call the same idempotent, fail-soft Refresh (upserts keyed by (series_code,
// observation_date)) — running it twice in a month is harmless. Implemented on stdlib time
// (next-fire computation + sleep) rather than a cron dependency: three fixed schedules need no
// expression parser, and time.LoadLocation("Europe/Warsaw") already handles the DST transitions.

// StartScheduler runs Refresh on the three macro schedules until ctx is cancelled — the Go
// equivalent of MacroDataScheduler, mirroring main.go's runScheduledPriceRefresh shape. Each
// fire runs under lifecycle.Guard (ADR-110): a panic is logged at ERROR, reported as that
// fire's failure, and the loop keeps running.
func StartScheduler(ctx context.Context, svc *Service) {
	for {
		next := nextMacroFire(warsawNow())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case fire := <-timer.C:
			slog.Info("macro: scheduled refresh starting", "at", fire.Format(time.RFC3339))
			err := lifecycle.Guard("macro-refresh", func() error {
				_, err := svc.Refresh(ctx)
				return err
			})
			if err != nil {
				slog.Error("macro: scheduled refresh failed", "error", err)
			} else {
				slog.Info("macro: scheduled refresh completed")
			}
		}
	}
}

func warsawNow() time.Time {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return time.Now().UTC()
	}
	return time.Now().In(loc)
}

// nextMacroFire returns the next scheduled fire time strictly after now.
func nextMacroFire(now time.Time) time.Time {
	candidates := []time.Time{
		nextWeekdayAt(now, time.Friday, 18, 0),
		nextMonthDayAt(now, 1, 17, 30),
		nextMonthDayAt(now, 5, 18, 30),
	}
	next := candidates[0]
	for _, c := range candidates[1:] {
		if c.Before(next) {
			next = c
		}
	}
	return next
}

func nextWeekdayAt(now time.Time, day time.Weekday, hour, min int) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
	delta := (int(day) - int(d.Weekday()) + 7) % 7
	d = d.AddDate(0, 0, delta)
	if !d.After(now) {
		d = d.AddDate(0, 0, 7)
	}
	return d
}

func nextMonthDayAt(now time.Time, day, hour, min int) time.Time {
	d := time.Date(now.Year(), now.Month(), day, hour, min, 0, 0, now.Location())
	if !d.After(now) {
		d = time.Date(now.Year(), now.Month()+1, day, hour, min, 0, 0, now.Location())
	}
	return d
}
