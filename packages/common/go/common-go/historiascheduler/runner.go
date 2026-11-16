package historiascheduler

import (
	"context"
	"log/slog"
)

// BackfillResult mirrors HistoriaPositionValuesBackfillService.BackfillResult
// (field-identical in both apps: Updated/Skipped/Warnings).
type BackfillResult struct {
	Updated  int
	Skipped  int
	Warnings []string
}

// Backfiller is the one method the startup backfill runner needs from the
// per-app admin *Service — a small consumer-defined seam (the settingscore.Store
// precedent: separate Go modules with their own BackfillResult types, so the
// seam uses this shared shape and each app adapts with an explicit field
// mapping, not a cast).
type Backfiller interface {
	Backfill(context.Context) (BackfillResult, error)
}

// RunBackfillOnce runs one backfill pass at startup (ported from the Java HistoriaBackfillRunner,
// ADR-094). Callers launch it after the server goroutine is up (runner async after Listen), so
// readiness never waits on Yahoo history fetches. Fail-soft: a failure logs a warn and never takes
// the process down. The per-app backfill orchestration (portfolioB: cutoff, subasset folding, CHF
// pricing; portfolioC: one pass per portfolio) stays per app; both rewind units through purchase lots
// and position transfers with siblingcore/backfill, and only this launch plumbing is shared here.
func RunBackfillOnce(ctx context.Context, svc Backfiller) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	result, err := svc.Backfill(ctx)
	if err != nil {
		slog.Warn("admin: startup backfill failed", "error", err)
		return
	}
	if result.Updated > 0 {
		slog.Info("admin: startup backfill",
			"updated", result.Updated, "skipped", result.Skipped, "warnings", result.Warnings)
	} else {
		slog.Info("admin: startup backfill: nothing to do", "skipped", result.Skipped)
	}
}
