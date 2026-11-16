package cashpositions

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as Phase 1/1B
// (business logic table-tested here; real Postgres covered by integration_test.go).
type fakeQueries struct {
	rows   []sqlcgen.CashPosition
	nextID int64
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{nextID: 1}
}

func (f *fakeQueries) LatestCashRecordedAt(_ context.Context) (pgtype.Timestamptz, error) {
	if len(f.rows) == 0 {
		return pgtype.Timestamptz{}, pgx.ErrNoRows
	}
	latest := f.rows[0].RecordedAt.Time
	for _, r := range f.rows[1:] {
		if r.RecordedAt.Time.After(latest) {
			latest = r.RecordedAt.Time
		}
	}
	return pgtype.Timestamptz{Time: latest, Valid: true}, nil
}

func (f *fakeQueries) CashPositionsByRecordedAt(_ context.Context, recordedAt pgtype.Timestamptz) ([]sqlcgen.CashPosition, error) {
	out := []sqlcgen.CashPosition{}
	for _, r := range f.rows {
		if r.RecordedAt.Time.Equal(recordedAt.Time) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeQueries) CreateCashPosition(_ context.Context, arg sqlcgen.CreateCashPositionParams) (sqlcgen.CashPosition, error) {
	amount, _ := pgnum.ToDecimal(arg.AmountPln)
	row := sqlcgen.CashPosition{
		ID: f.nextID, Location: arg.Location,
		AmountPln:  pgnum.FromDecimal(amount),
		RecordedAt: arg.RecordedAt,
	}
	f.nextID++
	f.rows = append(f.rows, row)
	return row, nil
}

func testService(fq *fakeQueries) *Service {
	return NewService(fq, func(ctx context.Context, fn func(Queries) error) error {
		return fn(fq)
	})
}

func at(hour int) time.Time {
	return time.Date(2026, 6, 8, hour, 0, 0, 0, time.UTC)
}

func TestService_LatestSnapshot_EmptyIsEmptyNotError(t *testing.T) {
	got, err := testService(newFakeQueries()).LatestSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil slice, got %+v", got)
	}
}

func TestService_CreateSnapshot_BatchSharesRecordedAt(t *testing.T) {
	fq := newFakeQueries()
	svc := testService(fq)
	ctx := context.Background()

	saved, err := svc.CreateSnapshot(ctx, []Item{
		{Location: "mBank", AmountPln: decimal.NewFromInt(15000)},
		{Location: "portfel", AmountPln: decimal.NewFromInt(200)},
	}, at(8))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[0].ID == 0 || saved[1].ID == 0 {
		t.Fatalf("expected 2 saved rows with ids, got %+v", saved)
	}

	latest, err := svc.LatestSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 {
		t.Fatalf("expected latest snapshot of 2, got %+v", latest)
	}
	for _, d := range latest {
		if !d.RecordedAt.Equal(at(8)) {
			t.Fatalf("expected shared recordedAt, got %v", d.RecordedAt)
		}
	}

	// A newer snapshot supersedes: latest returns ONLY the new batch.
	if _, err := svc.CreateSnapshot(ctx, []Item{{Location: "mBank", AmountPln: decimal.NewFromInt(1)}}, at(9)); err != nil {
		t.Fatal(err)
	}
	latest, err = svc.LatestSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 1 || latest[0].Location != "mBank" {
		t.Fatalf("expected only the newer snapshot, got %+v", latest)
	}
}

func TestService_CreateSnapshot_ValidationIs422(t *testing.T) {
	svc := testService(newFakeQueries())
	ctx := context.Background()
	d := decimal.NewFromInt
	long := ""
	for range 101 {
		long += "x"
	}
	cases := []struct {
		name  string
		items []Item
		when  time.Time
	}{
		{"empty positions", nil, at(8)},
		{"zero recordedAt", []Item{{Location: "mBank", AmountPln: d(1)}}, time.Time{}},
		{"blank location", []Item{{Location: "  ", AmountPln: d(1)}}, at(8)},
		{"location too long", []Item{{Location: long, AmountPln: d(1)}}, at(8)},
		{"negative amount", []Item{{Location: "mBank", AmountPln: d(-1)}}, at(8)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.CreateSnapshot(ctx, c.items, c.when)
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) || appErr.Status != 422 || appErr.Code != httpx.CodeValidationError {
				t.Fatalf("expected 422 VALIDATION_ERROR, got %v", err)
			}
		})
	}

	// Zero amount is valid (@DecimalMin("0.0")).
	if _, err := svc.CreateSnapshot(ctx, []Item{{Location: "mBank", AmountPln: d(0)}}, at(8)); err != nil {
		t.Fatalf("zero amount must be valid, got %v", err)
	}
}
