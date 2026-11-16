package taxreturns

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as Phase 1/1B
// (business logic table-tested here; real Postgres covered by integration_test.go). Keyed
// exactly like the UNIQUE constraint: year.
type fakeQueries struct {
	rows   map[int32]sqlcgen.TaxReturn
	nextID int64
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{rows: map[int32]sqlcgen.TaxReturn{}, nextID: 1}
}

func (f *fakeQueries) ListTaxReturnsPage(_ context.Context, arg sqlcgen.ListTaxReturnsPageParams) ([]sqlcgen.TaxReturn, error) {
	all := []sqlcgen.TaxReturn{}
	for _, r := range f.rows {
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Year > all[j].Year })
	start := int(arg.Offset)
	if start > len(all) {
		return []sqlcgen.TaxReturn{}, nil
	}
	end := min(start+int(arg.Limit), len(all))
	return all[start:end], nil
}

func (f *fakeQueries) CountTaxReturns(_ context.Context) (int64, error) {
	return int64(len(f.rows)), nil
}

func (f *fakeQueries) GetTaxReturnByYear(_ context.Context, year int32) (sqlcgen.TaxReturn, error) {
	r, ok := f.rows[year]
	if !ok {
		return sqlcgen.TaxReturn{}, pgx.ErrNoRows
	}
	return r, nil
}

func (f *fakeQueries) CreateTaxReturn(_ context.Context, arg sqlcgen.CreateTaxReturnParams) (sqlcgen.TaxReturn, error) {
	if _, ok := f.rows[arg.Year]; ok {
		return sqlcgen.TaxReturn{}, &pgconn.PgError{Code: "23505", Message: `duplicate key value violates unique constraint "tax_returns_uq"`}
	}
	now := pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	row := sqlcgen.TaxReturn{
		ID: f.nextID, Year: arg.Year,
		GrossGain: arg.GrossGain, TaxPaid: arg.TaxPaid, EffectiveRatePct: arg.EffectiveRatePct,
		CreatedAt: now, UpdatedAt: now,
	}
	f.nextID++
	f.rows[arg.Year] = row
	return row, nil
}

func TestService_List_DefaultsAndOrder(t *testing.T) {
	fq := newFakeQueries()
	ctx := context.Background()
	svc := NewService(fq)
	for _, y := range []int{2023, 2024, 2025} {
		if _, err := svc.Create(ctx, y, decimal.NewFromInt(int64(y*1000)), decimal.NewFromInt(190), decimal.NewFromInt(19)); err != nil {
			t.Fatal(err)
		}
	}

	page, err := svc.List(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 || page.Number != 0 || page.Size != 20 {
		t.Fatalf("expected default page (0/20, total 3), got %+v", page)
	}
	if page.Items[0].Year != 2025 || page.Items[2].Year != 2023 {
		t.Fatalf("expected year DESC, got %+v", page.Items)
	}

	page, err = svc.List(ctx, new(1), new(2))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Year != 2023 {
		t.Fatalf("expected second page with 2023 only, got %+v", page)
	}

	if _, err := svc.List(ctx, new(-1), nil); !isValidation400(err) {
		t.Fatalf("expected 400 for negative page, got %v", err)
	}
}

func isValidation400(err error) bool {
	var appErr *httpx.AppError
	return errors.As(err, &appErr) && appErr.Status == 400 && appErr.Code == httpx.CodeValidationError
}

func TestService_Create_ValidationIs422(t *testing.T) {
	svc := NewService(newFakeQueries())
	ctx := context.Background()
	d := decimal.NewFromInt
	cases := []struct {
		name             string
		year             int
		gross, tax, rate decimal.Decimal
	}{
		{"year too low", 2020, d(100), d(10), d(10)},
		{"year too high", 2101, d(100), d(10), d(10)},
		{"negative taxPaid", 2024, d(100), d(-1), d(10)},
		{"rate below zero", 2024, d(100), d(10), d(-1)},
		{"rate above hundred", 2024, d(100), d(10), d(101)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Create(ctx, c.year, c.gross, c.tax, c.rate)
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) || appErr.Status != 422 || appErr.Code != httpx.CodeValidationError {
				t.Fatalf("expected 422 VALIDATION_ERROR, got %v", err)
			}
		})
	}
}

func TestService_Create_DuplicateYearIs409(t *testing.T) {
	svc := NewService(newFakeQueries())
	ctx := context.Background()
	d := decimal.NewFromInt
	if _, err := svc.Create(ctx, 2024, d(50000), d(9500), d(19)); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, 2024, d(1), d(1), d(1))
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Code != httpx.CodeDuplicateEntry {
		t.Fatalf("expected 409 DUPLICATE_ENTRY, got %v", err)
	}
}

func TestService_Create_PrecisionSurvives(t *testing.T) {
	svc := NewService(newFakeQueries())
	dto, err := svc.Create(context.Background(), 2024,
		decimal.RequireFromString("12345.6789"),
		decimal.RequireFromString("2345.6789"),
		decimal.RequireFromString("19.0000"))
	if err != nil {
		t.Fatal(err)
	}
	if !dto.EffectiveRatePct.Equal(decimal.RequireFromString("19")) {
		t.Fatalf("expected rate 19, got %s", dto.EffectiveRatePct)
	}
	if dto.UpdatedAt.IsZero() || dto.CreatedAt.IsZero() {
		t.Fatal("expected audit timestamps set")
	}
	_ = pgnum.FromDecimal(dto.GrossGain)
}
