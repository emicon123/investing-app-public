package historia

import (
	"context"
	"log/slog"

	"investing-app/common-go/lifecycle"
)

// The nightly trigger (cron "0 0 23 * * *" in Europe/Warsaw) and the
// HISTORIA_SCHEDULER_ENABLED gate are common-go/historiascheduler's RunDaily and
// SchedulerEnabled since task 178; main.go launches
// historiascheduler.RunDaily(ctx, svc.SnapshotAll).

// SnapshotAll is one night's job: every tracked portfolio, sequentially, each in its own
// transaction. Fail-soft per portfolio: an error logs a warn and the next portfolio still runs;
// each portfolio also runs under lifecycle.Guard (ADR-110), so a panic costs that portfolio's
// night, not the other portfolio. Always nil, so RunDaily logs "completed" unconditionally.
func (s *Service) SnapshotAll(ctx context.Context) error {
	for _, portfolio := range trackedPortfolios {
		err := lifecycle.Guard("historia-snapshot", func() error {
			return s.SnapshotPortfolio(ctx, portfolio)
		})
		if err != nil {
			slog.Warn("historia: scheduled snapshot failed", "portfolio", portfolio, "error", err)
		}
	}
	return nil
}
