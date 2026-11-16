package inflation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/inflationsync"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as Phase 1/1B
// (business logic table-tested here; real Postgres covered by integration_test.go). Keyed
// exactly like the UNIQUE constraint: (year, month).
type fakeQueries struct {
	rows   map[string]sqlcgen.Inflation
	nextID int64
}

func rowKey(year, month int32) string {
	return fmt.Sprintf("%d|%d", year, month)
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{rows: map[string]sqlcgen.Inflation{}, nextID: 1}
}

func fakeRow(id int64, year, month int32, cpi decimal.Decimal) sqlcgen.Inflation {
	return sqlcgen.Inflation{
		ID: id, Year: year, Month: month,
		CpiValue:  pgnum.FromDecimal(cpi),
		CreatedAt: pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}
}

func (f *fakeQueries) sorted() []sqlcgen.Inflation {
	out := []sqlcgen.Inflation{}
	for _, r := range f.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year > out[j].Year
		}
		return out[i].Month > out[j].Month
	})
	return out
}

func (f *fakeQueries) ListInflationPage(_ context.Context, arg sqlcgen.ListInflationPageParams) ([]sqlcgen.Inflation, error) {
	all := f.sorted()
	start := int(arg.Offset)
	if start > len(all) {
		return []sqlcgen.Inflation{}, nil
	}
	end := min(start+int(arg.Limit), len(all))
	return all[start:end], nil
}

func (f *fakeQueries) CountInflation(_ context.Context) (int64, error) {
	return int64(len(f.rows)), nil
}

func (f *fakeQueries) ListInflationPageByYear(_ context.Context, arg sqlcgen.ListInflationPageByYearParams) ([]sqlcgen.Inflation, error) {
	filtered := []sqlcgen.Inflation{}
	for _, r := range f.sorted() {
		if r.Year == arg.Year {
			filtered = append(filtered, r)
		}
	}
	start := int(arg.Offset)
	if start > len(filtered) {
		return []sqlcgen.Inflation{}, nil
	}
	end := min(start+int(arg.Limit), len(filtered))
	return filtered[start:end], nil
}

func (f *fakeQueries) CountInflationByYear(_ context.Context, year int32) (int64, error) {
	var n int64
	for _, r := range f.rows {
		if r.Year == year {
			n++
		}
	}
	return n, nil
}

func (f *fakeQueries) CreateInflationEntry(_ context.Context, arg sqlcgen.CreateInflationEntryParams) (sqlcgen.Inflation, error) {
	if _, ok := f.rows[rowKey(arg.Year, arg.Month)]; ok {
		return sqlcgen.Inflation{}, &pgconn.PgError{Code: "23505", Message: `duplicate key value violates unique constraint "inflation_uq"`}
	}
	cpi, _ := pgnum.ToDecimal(arg.CpiValue)
	r := fakeRow(f.nextID, arg.Year, arg.Month, cpi)
	f.nextID++
	f.rows[rowKey(arg.Year, arg.Month)] = r
	return r, nil
}

// stubGus returns a fixed fetch list for the shared inflationsync.Service.
type stubGus struct{ entries []inflationsync.MomEntry }

func (s stubGus) FetchMomInflation(_ context.Context) []inflationsync.MomEntry { return s.entries }

func testService(fq *fakeQueries) *Service { return NewService(fq) }

func TestService_List_PaginationAndYearFilter(t *testing.T) {
	fq := newFakeQueries()
	ctx := context.Background()
	for _, ym := range [][2]int32{{2024, 1}, {2024, 2}, {2025, 1}} {
		if _, err := fq.CreateInflationEntry(ctx, sqlcgen.CreateInflationEntryParams{
			Year: ym[0], Month: ym[1], CpiValue: pgnum.FromDecimal(decimal.NewFromInt(100)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	svc := testService(fq)

	page, err := svc.List(ctx, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 || page.Number != 0 || page.Size != 24 {
		t.Fatalf("expected default page (0/24, total 3), got %+v", page)
	}
	if page.Items[0].Year != 2025 || page.Items[2].Month != 1 || page.Items[2].Year != 2024 {
		t.Fatalf("expected DESC order, got %+v", page.Items)
	}

	y2024 := 2024
	page, err = svc.List(ctx, &y2024, new(0), new(1))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].Month != 2 {
		t.Fatalf("expected year filter + page size 1 (newest first), got %+v", page)
	}

	if _, err := svc.List(ctx, nil, new(-1), nil); !isValidation400(err) {
		t.Fatalf("expected 400 for negative page, got %v", err)
	}
	if _, err := svc.List(ctx, nil, nil, new(0)); !isValidation400(err) {
		t.Fatalf("expected 400 for zero size, got %v", err)
	}
}

func isValidation400(err error) bool {
	var appErr *httpx.AppError
	return errors.As(err, &appErr) && appErr.Status == 400 && appErr.Code == httpx.CodeValidationError
}

func TestService_Create_RangeValidationIs422(t *testing.T) {
	svc := testService(newFakeQueries())
	ctx := context.Background()
	for _, tc := range []struct {
		name        string
		year, month int
	}{
		{"year too low", 1999, 1},
		{"year too high", 2101, 1},
		{"month zero", 2024, 0},
		{"month thirteen", 2024, 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(ctx, tc.year, tc.month, decimal.NewFromFloat(4.2))
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) || appErr.Status != 422 {
				t.Fatalf("expected 422, got %v", err)
			}
		})
	}
}

func TestService_Create_DuplicateIs409(t *testing.T) {
	fq := newFakeQueries()
	svc := testService(fq)
	ctx := context.Background()
	if _, err := svc.Create(ctx, 2024, 3, decimal.NewFromFloat(2.5)); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, 2024, 3, decimal.NewFromFloat(2.6))
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Code != httpx.CodeDuplicateEntry {
		t.Fatalf("expected 409 DUPLICATE_ENTRY, got %v", err)
	}
}
