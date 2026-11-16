package taxreturns

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

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres connection
// (integration_test.go still exercises the real *sqlcgen.Queries against testcontainers-go,
// per the "never mock the database" gate).
type Queries interface {
	ListTaxReturnsPage(ctx context.Context, arg sqlcgen.ListTaxReturnsPageParams) ([]sqlcgen.TaxReturn, error)
	CountTaxReturns(ctx context.Context) (int64, error)
	GetTaxReturnByYear(ctx context.Context, year int32) (sqlcgen.TaxReturn, error)
	CreateTaxReturn(ctx context.Context, arg sqlcgen.CreateTaxReturnParams) (sqlcgen.TaxReturn, error)
}

// ReturnDTO is the service-layer shape of one tax_returns row — free of sqlc row types and
// openapi response types (doc 13's layering rule). NUMERIC columns ride as decimal.Decimal
// end-to-end (never float64); the 4-decimal-place YoY/rate precision survives the
// Postgres round-trip exactly (asserted in integration_test.go).
type ReturnDTO struct {
	ID               int64
	Year             int
	GrossGain        decimal.Decimal
	TaxPaid          decimal.Decimal
	EffectiveRatePct decimal.Decimal
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PageDTO is one paginated listTaxReturns result.
type PageDTO struct {
	Items  []ReturnDTO
	Total  int64
	Number int
	Size   int
}

// defaultPageSize mirrors TaxReturnsController's @PageableDefault(size = 20).
const defaultPageSize = 20

// Service is the tax-returns service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries Queries
}

// NewService wires queries (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries) *Service {
	return &Service{queries: queries}
}

// List returns one page of annual tax returns, newest year first. Out-of-range page/size answers
// 400 (the legacy PageRequest.of message texts, verbatim — the GET endpoint documents no 422 in the
// spec).
func (s *Service) List(ctx context.Context, page, size *int) (PageDTO, error) {
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
	rows, err := s.queries.ListTaxReturnsPage(ctx, sqlcgen.ListTaxReturnsPageParams{
		Limit: int32(l), Offset: int32(p * l),
	})
	if err != nil {
		return PageDTO{}, fmt.Errorf("taxreturns: list: %w", err)
	}
	total, err := s.queries.CountTaxReturns(ctx)
	if err != nil {
		return PageDTO{}, fmt.Errorf("taxreturns: count: %w", err)
	}
	return PageDTO{Items: toDTOs(rows), Total: total, Number: p, Size: l}, nil
}

// Create records one annual tax return. Range rules are those documented in the spec (note
// grossGain has no sign constraint — any sign passes, kept as-is from the original, not "fixed"); a
// duplicate year answers 409 DUPLICATE_ENTRY ("Resource already exists", the legacy text,
// verbatim). updated_at is written by the tax_returns_updated_at trigger — Go never sends it (the
// INSERT column list omits both audit columns), it only reads back what the trigger wrote via
// RETURNING.
func (s *Service) Create(ctx context.Context, year int, grossGain, taxPaid, effectiveRatePct decimal.Decimal) (ReturnDTO, error) {
	if year < 2021 || year > 2100 {
		return ReturnDTO{}, unprocessable("year", "year must be between 2021 and 2100")
	}
	if taxPaid.Cmp(decimal.Zero) < 0 {
		return ReturnDTO{}, unprocessable("taxPaid", "taxPaid must be greater than or equal to 0")
	}
	if effectiveRatePct.Cmp(decimal.Zero) < 0 || effectiveRatePct.Cmp(decimal.NewFromInt(100)) > 0 {
		return ReturnDTO{}, unprocessable("effectiveRatePct", "effectiveRatePct must be between 0 and 100")
	}
	row, err := s.queries.CreateTaxReturn(ctx, sqlcgen.CreateTaxReturnParams{
		Year:             int32(year),
		GrossGain:        pgnum.FromDecimal(grossGain),
		TaxPaid:          pgnum.FromDecimal(taxPaid),
		EffectiveRatePct: pgnum.FromDecimal(effectiveRatePct),
	})
	if err != nil {
		if pgerr.IsUniqueViolation(err) {
			return ReturnDTO{}, httpx.NewConflictError(httpx.CodeDuplicateEntry, "Resource already exists")
		}
		return ReturnDTO{}, fmt.Errorf("taxreturns: create: %w", err)
	}
	return toDTO(row)
}

func toDTOs(rows []sqlcgen.TaxReturn) []ReturnDTO {
	out := make([]ReturnDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			// NOT NULL NUMERIC columns decode infallibly in practice; a failure here is a
			// data-integrity fault — surface it loudly rather than silently dropping the row.
			panic(fmt.Sprintf("taxreturns: decode row %d: %v", r.ID, err))
		}
		out = append(out, dto)
	}
	return out
}

func toDTO(r sqlcgen.TaxReturn) (ReturnDTO, error) {
	gross, err := pgnum.ToDecimal(r.GrossGain)
	if err != nil {
		return ReturnDTO{}, err
	}
	tax, err := pgnum.ToDecimal(r.TaxPaid)
	if err != nil {
		return ReturnDTO{}, err
	}
	rate, err := pgnum.ToDecimal(r.EffectiveRatePct)
	if err != nil {
		return ReturnDTO{}, err
	}
	return ReturnDTO{
		ID: r.ID, Year: int(r.Year),
		GrossGain: gross, TaxPaid: tax, EffectiveRatePct: rate,
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
	}, nil
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi.yaml documents for this domain's POST validation failures (same helper shape as
// macro's).
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}
