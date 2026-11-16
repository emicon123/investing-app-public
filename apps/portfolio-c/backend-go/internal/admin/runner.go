package admin

import (
	"context"

	"investing-app/common-go/historiascheduler"
	"investing-app/common-go/lifecycle"
)

// StartBackfillRunner runs one backfill pass asynchronously (ported from the Java
// HistoriaBackfillRunner, ADR-094). main.go launches it as the one-shot "historia-backfill"
// lifecycle worker (asynchronous, so readiness never waits on Yahoo history fetches). Fail-soft: a
// failure logs a warn and never takes the process down. The run is wrapped in lifecycle.Guard
// (ADR-110): a panic is logged at ERROR and the process keeps serving (the backfill transaction has
// already rolled back by then — pgx.BeginFunc).
func StartBackfillRunner(ctx context.Context, svc *Service) {
	// Guard logs a recovered panic itself; RunBackfillOnce logs every ordinary failure.
	_ = lifecycle.Guard("historia-backfill", func() error {
		historiascheduler.RunBackfillOnce(ctx, backfillAdapter{svc})
		return nil
	})
}

// backfillAdapter maps portfolioC's own *Service onto the shared Backfiller seam —
// explicit field mapping, not a cast, keeping the per-app/service boundary
// greppable.
type backfillAdapter struct{ svc *Service }

func (a backfillAdapter) Backfill(ctx context.Context) (historiascheduler.BackfillResult, error) {
	r, err := a.svc.Backfill(ctx)
	if err != nil {
		return historiascheduler.BackfillResult{}, err
	}
	return historiascheduler.BackfillResult{Updated: r.Updated, Skipped: r.Skipped, Warnings: r.Warnings}, nil
}
