package cashpositions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres connection
// (integration_test.go still exercises the real *sqlcgen.Queries against testcontainers-go,
// per the "never mock the database" gate).
type Queries interface {
	LatestCashRecordedAt(ctx context.Context) (pgtype.Timestamptz, error)
	CashPositionsByRecordedAt(ctx context.Context, recordedAt pgtype.Timestamptz) ([]sqlcgen.CashPosition, error)
	CreateCashPosition(ctx context.Context, arg sqlcgen.CreateCashPositionParams) (sqlcgen.CashPosition, error)
}

// TransactFunc runs fn inside a single DB transaction — the snapshot batch commits
// atomically, the direct port of CashPositionService.createSnapshot's saveAll atomicity
// (a partial snapshot must never be visible). Production passes a pgx Begin/Commit closure;
// unit tests pass a direct call-through.
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// PositionDTO is the service-layer shape of one cash_positions row — free of sqlc row
// types and openapi response types (doc 13's layering rule).
type PositionDTO struct {
	ID         int64
	Location   string
	AmountPln  decimal.Decimal
	RecordedAt time.Time
}

// Item is one validated snapshot line.
type Item struct {
	Location  string
	AmountPln decimal.Decimal
}

// maxLocationLen mirrors CashPositionItemRequest's @Size(max = 100).
const maxLocationLen = 100

// Service is the cash-positions service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries  Queries
	transact TransactFunc
}

// NewService wires queries + transaction runner (explicit constructor wiring, ADR-094 §2 —
// no DI framework).
func NewService(queries Queries, transact TransactFunc) *Service {
	return &Service{queries: queries, transact: transact}
}

// LatestSnapshot returns every row sharing the newest recorded_at — the direct port of
// CashPositionService.getLatestSnapshot. An empty table answers an empty (non-nil) slice:
// the handler renders 200 {data:[],meta}, never 404 — there is simply no snapshot yet.
func (s *Service) LatestSnapshot(ctx context.Context) ([]PositionDTO, error) {
	latest, err := s.queries.LatestCashRecordedAt(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []PositionDTO{}, nil
		}
		return nil, fmt.Errorf("cashpositions: latest recorded_at: %w", err)
	}
	rows, err := s.queries.CashPositionsByRecordedAt(ctx, latest)
	if err != nil {
		return nil, fmt.Errorf("cashpositions: snapshot read: %w", err)
	}
	return toDTOs(rows), nil
}

// CreateSnapshot stores a whole snapshot batch under one recorded_at. Validation: non-empty batch,
// non-blank bounded fields, non-negative amounts (422 per the spec); every row is inserted in ONE
// transaction and the saved rows (with ids) are returned.
func (s *Service) CreateSnapshot(ctx context.Context, items []Item, recordedAt time.Time) ([]PositionDTO, error) {
	if len(items) == 0 {
		return nil, unprocessable("positions", "positions must not be empty")
	}
	if recordedAt.IsZero() {
		return nil, unprocessable("recordedAt", "must not be null")
	}
	for i, it := range items {
		if err := validateItem(i, it); err != nil {
			return nil, err
		}
	}
	var saved []PositionDTO
	err := s.transact(ctx, func(q Queries) error {
		saved = make([]PositionDTO, 0, len(items))
		for _, it := range items {
			row, err := q.CreateCashPosition(ctx, sqlcgen.CreateCashPositionParams{
				Location:   it.Location,
				AmountPln:  pgnum.FromDecimal(it.AmountPln),
				RecordedAt: pgtype.Timestamptz{Time: recordedAt.UTC(), Valid: true},
			})
			if err != nil {
				return fmt.Errorf("cashpositions: insert %q: %w", it.Location, err)
			}
			dto, err := toDTO(row)
			if err != nil {
				return err
			}
			saved = append(saved, dto)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

func validateItem(i int, it Item) *httpx.AppError {
	field := fmt.Sprintf("positions[%d]", i)
	if len([]rune(it.Location)) == 0 || len(strings.TrimSpace(it.Location)) == 0 {
		return unprocessable(field+".location", "must not be blank")
	}
	if len([]rune(it.Location)) > maxLocationLen {
		return unprocessable(field+".location", "size must be between 0 and 100")
	}
	if it.AmountPln.Cmp(decimal.Zero) < 0 {
		return unprocessable(field+".amountPln", "amountPln must be greater than or equal to 0")
	}
	return nil
}

func toDTOs(rows []sqlcgen.CashPosition) []PositionDTO {
	out := make([]PositionDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			// NOT NULL NUMERIC columns decode infallibly in practice; a failure here is a
			// data-integrity fault — surface it loudly rather than silently dropping the row.
			panic(fmt.Sprintf("cashpositions: decode row %d: %v", r.ID, err))
		}
		out = append(out, dto)
	}
	return out
}

func toDTO(r sqlcgen.CashPosition) (PositionDTO, error) {
	amount, err := pgnum.ToDecimal(r.AmountPln)
	if err != nil {
		return PositionDTO{}, err
	}
	return PositionDTO{
		ID: r.ID, Location: r.Location,
		AmountPln: amount, RecordedAt: r.RecordedAt.Time,
	}, nil
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi.yaml documents for this domain's POST validation failures (same helper shape as
// macro's).
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}
