package inflationsync

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// fakeQueries is an in-memory stand-in for a sibling's sqlc adapter — the same convention
// as root backend-go/internal/inflation/service_test.go (business logic table-tested here;
// real Postgres covered per-app by each sibling's own integration_test.go against its own
// migrated schema). Keyed exactly like the UNIQUE constraint: (year, month). mu makes it safe
// for the scheduler tests, where the scheduler goroutine writes while the test goroutine reads.
type fakeQueries struct {
	mu     sync.Mutex
	cpi    map[string]decimal.Decimal
	reads  int
	writes int
}

func rowKey(year, month int) string {
	return fmt.Sprintf("%d|%d", year, month)
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{cpi: map[string]decimal.Decimal{}}
}

func (f *fakeQueries) GetCpi(_ context.Context, year, month int) (decimal.Decimal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	v, ok := f.cpi[rowKey(year, month)]
	if !ok {
		return decimal.Zero, ErrNotFound
	}
	return v, nil
}

func (f *fakeQueries) CreateEntry(_ context.Context, year, month int, cpi decimal.Decimal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	f.cpi[rowKey(year, month)] = cpi
	return nil
}

func (f *fakeQueries) UpdateCpi(_ context.Context, year, month int, cpi decimal.Decimal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	f.cpi[rowKey(year, month)] = cpi
	return nil
}

func (f *fakeQueries) ListAsc(_ context.Context) ([]YearMonth, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]YearMonth, 0, len(f.cpi))
	for k := range f.cpi {
		var y, m int
		if _, err := fmt.Sscanf(k, "%d|%d", &y, &m); err != nil {
			return nil, err
		}
		out = append(out, YearMonth{Year: y, Month: m})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year < out[j].Year
		}
		return out[i].Month < out[j].Month
	})
	return out, nil
}

// stubGus returns a fixed fetch list.
type stubGus struct{ entries []MomEntry }

func (s stubGus) FetchMomInflation(_ context.Context) []MomEntry { return s.entries }

func testService(fq *fakeQueries, entries []MomEntry) *Service {
	return NewService(fq, func(ctx context.Context, fn func(Queries) error) error {
		return fn(fq)
	}, stubGus{entries: entries})
}

func TestService_Sync_CountsOnlyRealChanges(t *testing.T) {
	fq := newFakeQueries()
	ctx := context.Background()
	svc := testService(fq, []MomEntry{
		{Year: 2024, Month: 1, Cpi: decimal.RequireFromString("101.5")},
		{Year: 2024, Month: 2, Cpi: decimal.RequireFromString("102.0")},
	})

	upserts, err := svc.Sync(ctx)
	if err != nil || upserts != 2 {
		t.Fatalf("expected 2 inserts, got %d,%v", upserts, err)
	}

	// Re-sync identical data: 0 upserts, nothing rewritten.
	writes := fq.writes
	upserts, err = svc.Sync(ctx)
	if err != nil || upserts != 0 {
		t.Fatalf("expected 0 upserts for unchanged data, got %d,%v", upserts, err)
	}
	if fq.writes != writes {
		t.Fatal("unchanged re-sync must not rewrite any row")
	}

	// One value same (scale-insensitive compare: 102.00 equals 102.0), one row new.
	svc2 := testService(fq, []MomEntry{
		{Year: 2024, Month: 1, Cpi: decimal.RequireFromString("101.5")},
		{Year: 2024, Month: 2, Cpi: decimal.RequireFromString("102.00")},
		{Year: 2024, Month: 3, Cpi: decimal.RequireFromString("103.1")},
	})
	upserts, err = svc2.Sync(ctx)
	if err != nil || upserts != 1 {
		t.Fatalf("expected 1 upsert (only the new row), got %d,%v", upserts, err)
	}

	// Changed value updates in place.
	svc3 := testService(fq, []MomEntry{{Year: 2024, Month: 1, Cpi: decimal.RequireFromString("99.9")}})
	upserts, err = svc3.Sync(ctx)
	if err != nil || upserts != 1 {
		t.Fatalf("expected 1 upsert for changed cpi, got %d,%v", upserts, err)
	}
	v, err := fq.GetCpi(ctx, 2024, 1)
	if err != nil || !v.Equal(decimal.RequireFromString("99.9")) {
		t.Fatalf("expected updated cpi 99.9, got %s,%v", v, err)
	}
}

func TestService_Sync_EmptyFeedIsZero(t *testing.T) {
	upserts, err := testService(newFakeQueries(), nil).Sync(context.Background())
	if err != nil || upserts != 0 {
		t.Fatalf("expected (0, nil) for empty feed, got (%d, %v)", upserts, err)
	}
}

func TestService_IsStale(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	stale, err := testService(newFakeQueries(), nil).IsStale(ctx, now)
	if err != nil || !stale {
		t.Fatalf("empty table must be stale, got %v,%v", stale, err)
	}

	fq := newFakeQueries()
	svc := testService(fq, nil)
	// Newest = 2026-08, threshold = 2026-08 → NOT before → fresh.
	for _, ym := range [][2]int{{2026, 7}, {2026, 8}} {
		if err := fq.CreateEntry(ctx, ym[0], ym[1], decimal.NewFromInt(100)); err != nil {
			t.Fatal(err)
		}
	}
	if stale, _ := svc.IsStale(ctx, now); stale {
		t.Fatal("2026-08 data at 2026-09-15 must be fresh (last !< now-1m)")
	}
	// Drop to 2026-07 newest → stale (mirrors Java's isBefore(now.minusMonths(1))).
	delete(fq.cpi, rowKey(2026, 8))
	if stale, _ := svc.IsStale(ctx, now); !stale {
		t.Fatal("2026-07 data at 2026-09-15 must be stale")
	}
}
