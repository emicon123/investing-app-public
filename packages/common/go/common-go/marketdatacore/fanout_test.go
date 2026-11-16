package marketdatacore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shopspring/decimal"
)

// The F4.1 bound (task 178, D174 §6.3): never more than refreshConcurrency calls against
// the unofficial Yahoo API at once. synctest makes the provider's sleeps instant and fails
// the test if any goroutine is still blocked when it ends (the goroutine-leak check).

// countingProvider records the peak number of concurrent GetPrice calls; each call holds
// its slot for a second of (bubble) time. Safe for concurrent use.
type countingProvider struct {
	inFlight, peak, calls atomic.Int32
}

func (p *countingProvider) GetPrice(context.Context, string) (decimal.Decimal, error) {
	p.calls.Add(1)
	n := p.inFlight.Add(1)
	for {
		old := p.peak.Load()
		if n <= old || p.peak.CompareAndSwap(old, n) {
			break
		}
	}
	time.Sleep(time.Second)
	p.inFlight.Add(-1)
	return decimal.NewFromInt(10), nil
}

func (p *countingProvider) Name() string { return "COUNTING" }

// blockingProvider holds every call until ctx is cancelled, like an HTTP client honouring it.
type blockingProvider struct{ calls atomic.Int32 }

func (p *blockingProvider) GetPrice(ctx context.Context, _ string) (decimal.Decimal, error) {
	p.calls.Add(1)
	<-ctx.Done()
	return decimal.Zero, ctx.Err()
}

func (p *blockingProvider) Name() string { return "BLOCKING" }

func plnTickers(n int) []TrackedTicker {
	out := make([]TrackedTicker, n)
	for i := range out {
		out[i] = TrackedTicker{YahooTicker: fmt.Sprintf("T%02d.WA", i), PriceCurrency: "PLN", Exchange: "XTB"}
	}
	return out
}

func TestForceRefreshAllPrices_AtMostFourConcurrentFetches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newFakeStore()
		store.tickers = plnTickers(20)
		provider := &countingProvider{}
		result, err := NewService(store, provider, provider).ForceRefreshAllPrices(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if result.Total != 20 || result.Succeeded != 20 || len(result.Failures) != 0 {
			t.Fatalf("want 20/20 refreshed, got %+v", result)
		}
		if peak := provider.peak.Load(); peak != refreshConcurrency {
			t.Fatalf("want exactly %d concurrent fetches at peak, got %d", refreshConcurrency, peak)
		}
	})
}

func TestForceRefreshAllPrices_CancelledContextFailsNotYetStartedTickers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newFakeStore()
		store.tickers = plnTickers(20)
		provider := &blockingProvider{}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan RefreshResult)
		go func() {
			result, err := NewService(store, provider, provider).ForceRefreshAllPrices(ctx)
			if err != nil {
				t.Error(err)
			}
			done <- result
		}()

		synctest.Wait() // every slot taken, the other 16 tickers waiting for one
		if got := provider.calls.Load(); got != refreshConcurrency {
			t.Fatalf("want %d fetches in flight before the cancel, got %d", refreshConcurrency, got)
		}
		cancel()
		result := <-done

		if got := provider.calls.Load(); got != refreshConcurrency {
			t.Fatalf("no ticker may start after the cancel; fetches: %d", got)
		}
		if result.Total != 20 || result.Succeeded != 0 || len(result.Failures) != 20 {
			t.Fatalf("want one failure per ticker (4 in flight + 16 not started), got %+v", result)
		}
		for _, f := range result.Failures {
			if !strings.Contains(f.Reason, context.Canceled.Error()) {
				t.Fatalf("want a context-cancelled failure, got %+v", f)
			}
		}
		if len(store.prices) != 0 {
			t.Fatalf("nothing may be persisted, got %d rows", len(store.prices))
		}
	})
}

func TestForceRefreshAllFxRates_CancelledContextFetchesNothing(t *testing.T) {
	store := newFakeStore()
	provider := &blockingProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	NewService(store, provider, provider).ForceRefreshAllFxRates(ctx)
	if got := provider.calls.Load(); got != 0 {
		t.Fatalf("a cancelled refresh must not call Yahoo, got %d calls", got)
	}
	if len(store.fxRates) != 0 {
		t.Fatalf("nothing may be persisted, got %d rows", len(store.fxRates))
	}
}

func TestFanOut_EveryItemRunsOrIsCancelledExactlyOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran, skipped atomic.Int32
	fanOut(ctx, make([]int, 9), func(int) { ran.Add(1) }, func(_ int, err error) {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("want context.Canceled, got %v", err)
		}
		skipped.Add(1)
	})
	if ran.Load() != 0 || skipped.Load() != 9 {
		t.Fatalf("cancelled: want 0 ran / 9 skipped, got %d / %d", ran.Load(), skipped.Load())
	}
	ran.Store(0)
	fanOut(context.Background(), make([]int, 9), func(int) { ran.Add(1) }, func(int, error) { t.Error("not cancelled") })
	if ran.Load() != 9 {
		t.Fatalf("live: want 9 ran, got %d", ran.Load())
	}
}
