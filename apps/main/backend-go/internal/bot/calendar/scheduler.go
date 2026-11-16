package calendar

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"investing-app/common-go/lifecycle"
)

// SchedulerEnabled reports whether this process may run the nightly bot-calendar
// retention purge. Default false — until this domain's Nginx block flips, Java
// remains the single writer/purger and a double-fire is impossible by construction.
// Shares the BOT_SCHEDULER_ENABLED flag with the release watcher (one flag for all
// bot background work, so shadow mode is a single switch).
func SchedulerEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("BOT_SCHEDULER_ENABLED")))
	return v == "true" || v == "1"
}

// StartScheduler purges old calendar events nightly at 04:00 Warsaw time (before the
// 05:50 calendar sync) until ctx is cancelled (started under a lifecycle.Group) — the Go equivalent of
// BotCalendarService.purgeOldEvents' @Scheduled(cron 04:00, Europe/Warsaw). Same
// stdlib next-fire shape as prior phases' schedulers; failures are fail-soft.
func StartScheduler(ctx context.Context, svc *Service) {
	for {
		next := nextPurgeFire(time.Now())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case fire := <-timer.C:
			slog.Info("bot calendar: scheduled purge starting", "at", fire.Format(time.RFC3339))
			// One purge per Guard: a panic costs this night's purge, not the scheduler (F1.3).
			if err := lifecycle.Guard("bot-calendar-purge", func() error {
				_, err := svc.PurgeOldEvents(ctx)
				return err
			}); err != nil {
				slog.Warn("bot calendar: scheduled purge failed", "error", err)
			}
		}
	}
}

func nextPurgeFire(now time.Time) time.Time {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		loc = time.UTC
	}
	y, m, d := now.In(loc).Date()
	next := time.Date(y, m, d, 4, 0, 0, 0, loc)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
