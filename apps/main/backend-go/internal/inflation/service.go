package inflation

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgerr"
	"investing-app/common-go/pgnum"
)

// Queries is the narrow slice of *sqlcgen.Queries List/Create call, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres connection
// (integration_test.go still exercises the real *sqlcgen.Queries against testcontainers-go,
// per the "never mock the database" gate). The GUS sync goes through Adapter instead.
type Queries interface {
	ListInflationPage(ctx context.Context, arg sqlcgen.ListInflationPageParams) ([]sqlcgen.Inflation, error)
	CountInflation(ctx context.Context) (int64, error)
	ListInflationPageByYear(ctx context.Context, arg sqlcgen.ListInflationPageByYearParams) ([]sqlcgen.Inflation, error)
	CountInflationByYear(ctx context.Context, year int32) (int64, error)
	CreateInflationEntry(ctx context.Context, arg sqlcgen.CreateInflationEntryParams) (sqlcgen.Inflation, error)
}

// EntryDTO is the service-layer shape of one inflation row — free of sqlc row types and
// openapi response types (doc 13's layering rule).
type EntryDTO struct {
	ID        int64
	Year      int
	Month     int
	Cpi       decimal.Decimal
	CreatedAt time.Time
}

// PageDTO is one paginated listInflation result.
type PageDTO struct {
	Items  []EntryDTO
	Total  int64
	Number int
	Size   int
}

// defaultPageSize mirrors InflationController's @PageableDefault(size = 24).
const defaultPageSize = 24

// Service is the inflation read/manual-entry service layer: handler -> Service -> sqlc
// Queries. The GUS sync (POST /inflation/sync + the monthly scheduler) is the shared
// common-go/inflationsync.Service over Adapter (task 178).
type Service struct {
	queries Queries
}

// NewService wires the queries (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries) *Service {
	return &Service{queries: queries}
}

// List returns one page of CPI entries, newest first. year == nil means all years. Out-of-range
// page/size answers 400 (the legacy PageRequest.of message texts, verbatim — the GET endpoints
// document no 422 in the spec).
func (s *Service) List(ctx context.Context, year *int, page, size *int) (PageDTO, error) {
	p, l := 0, defaultPageSize
	if page != nil {
		p = *page
	}
	if size != nil {
		l = *size
	}
	if p < 0 {
		return PageDTO{}, httpx.NewValidationError("page", "Page index must not be less than zero")
	}
	if l < 1 {
		return PageDTO{}, httpx.NewValidationError("size", "Page size must not be less than one")
	}
	limit, offset := int32(l), int32(p*l)
	if year != nil {
		rows, err := s.queries.ListInflationPageByYear(ctx, sqlcgen.ListInflationPageByYearParams{
			Limit: limit, Offset: offset, Year: int32(*year),
		})
		if err != nil {
			return PageDTO{}, fmt.Errorf("inflation: list by year: %w", err)
		}
		total, err := s.queries.CountInflationByYear(ctx, int32(*year))
		if err != nil {
			return PageDTO{}, fmt.Errorf("inflation: count by year: %w", err)
		}
		return PageDTO{Items: toDTOs(rows), Total: total, Number: p, Size: l}, nil
	}
	rows, err := s.queries.ListInflationPage(ctx, sqlcgen.ListInflationPageParams{Limit: limit, Offset: offset})
	if err != nil {
		return PageDTO{}, fmt.Errorf("inflation: list: %w", err)
	}
	total, err := s.queries.CountInflation(ctx)
	if err != nil {
		return PageDTO{}, fmt.Errorf("inflation: count: %w", err)
	}
	return PageDTO{Items: toDTOs(rows), Total: total, Number: p, Size: l}, nil
}

// Create stores one manual CPI entry. Range rules: year 2000-2100, month 1-12; a (year, month)
// duplicate answers 409 DUPLICATE_ENTRY ("Resource already exists", the legacy text, verbatim).
func (s *Service) Create(ctx context.Context, year, month int, cpi decimal.Decimal) (EntryDTO, error) {
	if year < 2000 || year > 2100 {
		return EntryDTO{}, unprocessable("year", "year must be between 2000 and 2100")
	}
	if month < 1 || month > 12 {
		return EntryDTO{}, unprocessable("month", "month must be between 1 and 12")
	}
	row, err := s.queries.CreateInflationEntry(ctx, sqlcgen.CreateInflationEntryParams{
		Year: int32(year), Month: int32(month), CpiValue: pgnum.FromDecimal(cpi),
	})
	if err != nil {
		if pgerr.IsUniqueViolation(err) {
			return EntryDTO{}, httpx.NewConflictError(httpx.CodeDuplicateEntry, "Resource already exists")
		}
		return EntryDTO{}, fmt.Errorf("inflation: create: %w", err)
	}
	return toDTO(row)
}

func toDTOs(rows []sqlcgen.Inflation) []EntryDTO {
	out := make([]EntryDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			// NOT NULL NUMERIC columns decode infallibly in practice; a failure here is a
			// data-integrity fault — surface it loudly rather than silently dropping the row.
			panic(fmt.Sprintf("inflation: decode row %d: %v", r.ID, err))
		}
		out = append(out, dto)
	}
	return out
}

func toDTO(r sqlcgen.Inflation) (EntryDTO, error) {
	cpi, err := pgnum.ToDecimal(r.CpiValue)
	if err != nil {
		return EntryDTO{}, err
	}
	return EntryDTO{
		ID: r.ID, Year: int(r.Year), Month: int(r.Month),
		Cpi: cpi, CreatedAt: r.CreatedAt.Time,
	}, nil
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi.yaml documents for this domain's POST validation failures (same helper shape as
// macro's).
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}
