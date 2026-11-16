package inflationsync

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shopspring/decimal"
)

func TestNextInflationFire(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("Europe/Warsaw zone data unavailable")
	}
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			"early month fires on the 15th 06:00",
			time.Date(2026, 9, 1, 12, 0, 0, 0, loc),
			time.Date(2026, 9, 15, 6, 0, 0, 0, loc),
		},
		{
			"on the 15th before 06:00 fires same morning",
			time.Date(2026, 9, 15, 5, 59, 0, 0, loc),
			time.Date(2026, 9, 15, 6, 0, 0, 0, loc),
		},
		{
			"after the 15th 06:00 fires next month",
			time.Date(2026, 9, 15, 6, 0, 1, 0, loc),
			time.Date(2026, 10, 15, 6, 0, 0, 0, loc),
		},
		{
			"december rolls into january",
			time.Date(2026, 12, 20, 12, 0, 0, 0, loc),
			time.Date(2027, 1, 15, 6, 0, 0, 0, loc),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextInflationFire(c.now); !got.Equal(c.want) {
				t.Fatalf("expected %v, got %v", c.want, got)
			}
		})
	}
}

// panicOnceQueries panics on the first ListAsc call (the startup stale check), then behaves
// like the in-memory fakeQueries.
type panicOnceQueries struct {
	*fakeQueries
	listCalls atomic.Int32
}

func (q *panicOnceQueries) ListAsc(ctx context.Context) ([]YearMonth, error) {
	if q.listCalls.Add(1) == 1 {
		panic("staleness query blew up")
	}
	return q.fakeQueries.ListAsc(ctx)
}

// panicOnceGus panics on the first fetch, then returns one entry.
type panicOnceGus struct{ calls atomic.Int32 }

func (g *panicOnceGus) FetchMomInflation(context.Context) []MomEntry {
	if g.calls.Add(1) == 1 {
		panic("GUS client blew up")
	}
	return []MomEntry{{Year: 1999, Month: 12, Cpi: decimal.RequireFromString("100.2")}}
}

// TestStartScheduler_PanicsCostOneFireNotTheLoop: a panicking startup stale check and a
// panicking first monthly sync are each recovered (lifecycle.Guard), and the next monthly fire
// still syncs.
func TestStartScheduler_PanicsCostOneFireNotTheLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fq := &panicOnceQueries{fakeQueries: newFakeQueries()}
		gus := &panicOnceGus{}
		svc := NewService(fq, func(_ context.Context, fn func(Queries) error) error { return fn(fq) }, gus)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			StartScheduler(ctx, svc)
		}()

		// The bubble clock starts 2000-01-01: fires on Jan 15 (panics) and Feb 15 (syncs).
		time.Sleep(50 * 24 * time.Hour)
		if got := fq.listCalls.Load(); got != 1 {
			t.Fatalf("expected exactly one startup stale check, got %d", got)
		}
		if got := gus.calls.Load(); got != 2 {
			t.Fatalf("expected the monthly sync to run again after a panic, ran %d time(s)", got)
		}
		if _, err := fq.GetCpi(ctx, 1999, 12); err != nil {
			t.Fatalf("the post-panic sync did not write its entry: %v", err)
		}

		cancel()
		<-done
	})
}
