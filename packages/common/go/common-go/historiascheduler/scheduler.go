// Package historiascheduler is the shared background-job launch plumbing for the historia lifecycle
// (portfolioB and portfolioC): the nightly snapshot loop (RunDaily, 23:00 Europe/Warsaw) plus the startup
// position_values backfill (RunBackfillOnce), sharing one home. Deliberately NOT historiaquery:
// that package is scoped as the read kernel ("paginated list query plus inflation enrichment");
// process-lifetime job launching is a different concern. Warsaw fire-timing math is reused from
// historiaquery (shared→shared import) instead of re-owned.
//
// Each app's main.go gates its goroutine on SchedulerEnabled. The flag is OFF by default and set to
// true in every compose file (ADR-097): with it off, no process writes historia snapshots at all.
package historiascheduler

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/lifecycle"
)

// SchedulerEnabled reports whether this process may run the daily historia snapshot job. Default
// false; enabled by HISTORIA_SCHEDULER_ENABLED=true (or 1), which production sets in every compose
// file (ADR-097).
func SchedulerEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("HISTORIA_SCHEDULER_ENABLED")))
	return v == "true" || v == "1"
}

// RunDaily runs job every night at 23:00 Warsaw time until ctx is cancelled — the timer skeleton
// both apps' schedulers share. Fail-soft per job run: a job error logs a warn and the loop
// continues; "completed" is logged iff job returns nil. Each night's job runs under lifecycle.Guard
// (ADR-110), so a panic is logged at ERROR, reported as that night's warn, and costs one night, not
// the loop.
//
// The job seam is a snapshot closure (NOT a scope list): portfolioB's
// SnapshotPortfolio takes no portfolio argument (single-portfolio app), so a
// scope list would force a sentinel value invented for sharing's sake (YAGNI).
// Each app keeps its exact loop shape — portfolioB passes its SnapshotPortfolio
// method value directly, portfolioC wraps its portfolio loop in a closure that warns
// per portfolio and returns nil (so the unconditional "completed" log is
// preserved).
func RunDaily(ctx context.Context, job func(context.Context) error) {
	for {
		next := historiaquery.NextDailyFire(historiaquery.NowInWarsaw())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case fire := <-timer.C:
			slog.Info("historia: scheduled snapshot starting", "at", fire.Format(time.RFC3339))
			if err := lifecycle.Guard("historia-snapshot", func() error { return job(ctx) }); err != nil {
				slog.Warn("historia: scheduled snapshot failed", "error", err)
			} else {
				slog.Info("historia: scheduled snapshot completed")
			}
		}
	}
}
