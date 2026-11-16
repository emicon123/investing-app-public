package inflationsync

import (
	"context"
	"log/slog"
	"time"

	"investing-app/common-go/lifecycle"
)

// This file implements the two triggers — the monthly cron plus the startup stale check — on stdlib
// time (next-fire computation + sleep) rather than a cron dependency (all three apps run this one
// since task 178): one fixed monthly schedule needs no expression parser, and
// time.LoadLocation("Europe/Warsaw") already handles the DST transitions.
//
// Cron "0 0 6 15 * *" Europe/Warsaw = 06:00 on the 15th of every month.

// StartScheduler runs the startup stale check once, then Sync on every monthly fire until ctx is
// cancelled. Every failure is fail-soft (warn log, keep the process up) — a dead GUS endpoint must
// never crash-loop the backend. The startup check and every fire run under lifecycle.Guard
// (ADR-110): a panic is logged at ERROR and costs that one run, not the loop.
func StartScheduler(ctx context.Context, svc *Service) {
	// Guard already logged a recovered panic at ERROR; the check's own failures log below.
	_ = lifecycle.Guard("inflation-sync startup check", func() error {
		syncIfStale(ctx, svc)
		return nil
	})

	for {
		next := nextInflationFire(warsawNow())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case fire := <-timer.C:
			slog.Info("inflation: scheduled GUS sync starting", "at", fire.Format(time.RFC3339))
			err := lifecycle.Guard("inflation-sync", func() error {
				_, err := svc.Sync(ctx)
				return err
			})
			if err != nil {
				slog.Warn("inflation: scheduled GUS sync failed", "error", err)
			} else {
				slog.Info("inflation: scheduled GUS sync completed")
			}
		}
	}
}

// syncIfStale is the startup half of StartScheduler: sync from GUS once iff the table is
// stale, fail-soft.
func syncIfStale(ctx context.Context, svc *Service) {
	stale, err := svc.IsStale(ctx, warsawNow())
	if err != nil {
		slog.Warn("inflation: startup staleness check failed", "error", err)
		return
	}
	if !stale {
		return
	}
	slog.Info("inflation: data stale at startup, syncing from GUS")
	if _, err := svc.Sync(ctx); err != nil {
		slog.Warn("inflation: startup GUS sync failed", "error", err)
	}
}

func warsawNow() time.Time {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return time.Now().UTC()
	}
	return time.Now().In(loc)
}

// nextInflationFire returns the next 15th-of-month 06:00 (Europe/Warsaw wall time)
// strictly after now.
func nextInflationFire(now time.Time) time.Time {
	fire := time.Date(now.Year(), now.Month(), 15, 6, 0, 0, 0, now.Location())
	if !fire.After(now) {
		fire = time.Date(now.Year(), now.Month()+1, 15, 6, 0, 0, 0, now.Location())
	}
	return fire
}
