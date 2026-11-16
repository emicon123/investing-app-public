package marketdatacore

import (
	"context"
	"log/slog"
	"time"

	"investing-app/common-go/lifecycle"
)

// StartPriceRefreshScheduler runs an initial warm-up shortly after startup (5s), then a refresh
// every 15 minutes, for as long as ctx is not cancelled. Shared by all three apps' main.go, the
// same way common-go/inflationsync.StartScheduler generalizes a per-app cron loop.
func StartPriceRefreshScheduler(ctx context.Context, service *Service) {
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return
	}
	refreshPricesOnce(ctx, service)

	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			refreshPricesOnce(ctx, service)
		case <-ctx.Done():
			return
		}
	}
}

// refreshPricesOnce runs one scheduled refresh under lifecycle.Guard (ADR-110): a panic in the
// scheduler goroutine is logged at ERROR and reported as a failed refresh, and the next tick
// still runs.
func refreshPricesOnce(ctx context.Context, service *Service) {
	slog.Info("price cache: scheduled refresh starting")
	var result RefreshResult
	err := lifecycle.Guard("price-refresh", func() error {
		var err error
		result, err = service.ForceRefreshAllPrices(ctx)
		return err
	})
	if err != nil {
		slog.Error("price cache: scheduled refresh failed", "error", err)
		return
	}
	if len(result.Failures) > 0 {
		slog.Warn("price cache: scheduled refresh completed with failures",
			"failed", len(result.Failures), "total", result.Total)
	} else {
		slog.Info("price cache: scheduled refresh complete",
			"succeeded", result.Succeeded, "total", result.Total)
	}
}
