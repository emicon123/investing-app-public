package marketdatacore

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// panicOnceStore panics on the first ListTrackedTickers call (the scheduler's own goroutine),
// then behaves like the in-memory fakeStore with no tracked tickers.
type panicOnceStore struct {
	*fakeStore
	calls atomic.Int32
}

func (s *panicOnceStore) ListTrackedTickers(ctx context.Context) ([]TrackedTicker, error) {
	if s.calls.Add(1) == 1 {
		panic("tracked-ticker query blew up")
	}
	return s.fakeStore.ListTrackedTickers(ctx)
}

// TestStartPriceRefreshScheduler_PanickingRefreshCostsOneTick: the warm-up refresh panics, the
// loop survives it (lifecycle.Guard per tick) and the 15-minute tick refreshes again.
func TestStartPriceRefreshScheduler_PanickingRefreshCostsOneTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &panicOnceStore{fakeStore: newFakeStore()}
		svc := NewService(store, &fakeProvider{name: "yahoo"}, &fakeProvider{name: "binance"})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			StartPriceRefreshScheduler(ctx, svc)
		}()

		time.Sleep(5*time.Second + 15*time.Minute + time.Second)
		if got := store.calls.Load(); got != 2 {
			t.Fatalf("expected the warm-up (panicked) plus one tick refresh, got %d refresh(es)", got)
		}

		cancel()
		<-done
	})
}
