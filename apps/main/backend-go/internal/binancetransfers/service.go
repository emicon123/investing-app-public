package binancetransfers

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
//
// NOTE on the partial-port split: the ONLY INSERT here is the manual-entry one —
// its SQL hardcodes the manual origin with session_id=NULL, and its params struct
// has no origin/session fields at all. Any future query adding a second INSERT
// into this table breaks service_test.go's negative test by design.
type Queries interface {
	ListBinanceTransfers(ctx context.Context) ([]sqlcgen.BinanceTransfer, error)
	ListBinanceTransfersByYear(ctx context.Context, year int32) ([]sqlcgen.BinanceTransfer, error)
	SumAmountPlnByYear(ctx context.Context) ([]sqlcgen.SumAmountPlnByYearRow, error)
	InsertBinanceTransferManual(ctx context.Context, arg sqlcgen.InsertBinanceTransferManualParams) (sqlcgen.BinanceTransfer, error)
	ExistsBinanceTransferById(ctx context.Context, id int64) (bool, error)
	DeleteBinanceTransfer(ctx context.Context, id int64) error
}

// Service is the binance-transfers service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries Queries
}

// NewService wires queries (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries) *Service {
	return &Service{queries: queries}
}

// List returns the ledger rows (optionally filtered to one calendar year) plus the
// per-year PLN sums over the WHOLE table — the direct port of
// BinanceTransferService.list. The sums deliberately ignore the year filter (the UI
// renders the year selector and the PIT-38 total from this single call).
func (s *Service) List(ctx context.Context, year *int) (ListResult, error) {
	var rows []sqlcgen.BinanceTransfer
	var err error
	if year != nil {
		rows, err = s.queries.ListBinanceTransfersByYear(ctx, int32(*year))
	} else {
		rows, err = s.queries.ListBinanceTransfers(ctx)
	}
	if err != nil {
		return ListResult{}, fmt.Errorf("binancetransfers: list: %w", err)
	}
	sumRows, err := s.queries.SumAmountPlnByYear(ctx)
	if err != nil {
		return ListResult{}, fmt.Errorf("binancetransfers: sums: %w", err)
	}
	data := make([]TransferDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			return ListResult{}, fmt.Errorf("binancetransfers: decode row %d: %w", r.ID, err)
		}
		data = append(data, dto)
	}
	sums := make([]YearlySum, 0, len(sumRows))
	for _, sr := range sumRows {
		total, err := pgnum.ToDecimal(sr.TotalPln)
		if err != nil {
			return ListResult{}, fmt.Errorf("binancetransfers: decode sum for year %d: %w", sr.Year, err)
		}
		sums = append(sums, YearlySum{Year: int(sr.Year), TotalPln: total})
	}
	return ListResult{Data: data, YearlySums: sums}, nil
}

// Create records one manual ledger row. The manual origin and NULL session link are forced below
// the service (SQL literals), so a caller can never smuggle in a completion-linked row. Value
// rules: transferDate required, amountPln > 0, amountNative > 0 when given, and
// amountNative/nativeCurrency supplied together; every violation answers 422 per docs/openapi.yaml.
func (s *Service) Create(ctx context.Context, in CreateInput) (TransferDTO, error) {
	if in.TransferDate.IsZero() {
		return TransferDTO{}, unprocessable("transferDate", "must not be null")
	}
	if in.AmountPln.Cmp(decimal.Zero) <= 0 {
		return TransferDTO{}, unprocessable("amountPln", "must be greater than 0")
	}
	// amountNative and nativeCurrency must be supplied together (isNativePairConsistent).
	if (in.AmountNative == nil) != (in.NativeCurrency == nil) {
		return TransferDTO{}, unprocessable("", "amountNative and nativeCurrency must be supplied together")
	}
	if in.AmountNative != nil && in.AmountNative.Cmp(decimal.Zero) <= 0 {
		return TransferDTO{}, unprocessable("amountNative", "must be greater than 0")
	}
	var nativeCurrency *sqlcgen.CurrencyEnum
	if in.NativeCurrency != nil {
		c := sqlcgen.CurrencyEnum(*in.NativeCurrency)
		switch c {
		case sqlcgen.CurrencyEnumPLN, sqlcgen.CurrencyEnumEUR, sqlcgen.CurrencyEnumUSD,
			sqlcgen.CurrencyEnumCHF, sqlcgen.CurrencyEnumGBP:
		default:
			return TransferDTO{}, unprocessable("nativeCurrency", "must not be null")
		}
		nativeCurrency = &c
	}
	amountNative := pgtype.Numeric{Valid: false}
	if in.AmountNative != nil {
		amountNative = pgnum.FromDecimal(*in.AmountNative)
	}
	row, err := s.queries.InsertBinanceTransferManual(ctx, sqlcgen.InsertBinanceTransferManualParams{
		TransferDate:   pgtype.Date{Time: in.TransferDate, Valid: true},
		AmountPln:      pgnum.FromDecimal(in.AmountPln),
		AmountNative:   amountNative,
		NativeCurrency: nativeCurrency,
		Comment:        in.Comment,
	})
	if err != nil {
		return TransferDTO{}, fmt.Errorf("binancetransfers: create: %w", err)
	}
	return toDTO(row)
}

// Delete removes any ledger row (manual or completion-linked). Deleting a completion-linked row is
// an explicit manual-correction facility for this single-user app: it touches neither the completed
// session nor its deposits/positions. Unknown id answers 404.
func (s *Service) Delete(ctx context.Context, id int64) error {
	exists, err := s.queries.ExistsBinanceTransferById(ctx, id)
	if err != nil {
		return fmt.Errorf("binancetransfers: exists: %w", err)
	}
	if !exists {
		return httpx.NewNotFoundError(fmt.Sprintf("Binance transfer %d not found", id))
	}
	if err := s.queries.DeleteBinanceTransfer(ctx, id); err != nil {
		return fmt.Errorf("binancetransfers: delete: %w", err)
	}
	return nil
}

func toDTO(r sqlcgen.BinanceTransfer) (TransferDTO, error) {
	amountPln, err := pgnum.ToDecimal(r.AmountPln)
	if err != nil {
		return TransferDTO{}, err
	}
	var amountNative *decimal.Decimal
	if r.AmountNative.Valid {
		v, err := pgnum.ToDecimal(r.AmountNative)
		if err != nil {
			return TransferDTO{}, err
		}
		amountNative = &v
	}
	var nativeCurrency *string
	if r.NativeCurrency != nil {
		s := string(*r.NativeCurrency)
		nativeCurrency = &s
	}
	return TransferDTO{
		ID: r.ID, TransferDate: r.TransferDate.Time,
		AmountPln: amountPln, AmountNative: amountNative,
		NativeCurrency: nativeCurrency, Comment: r.Comment,
		Source: r.Source, SessionID: r.SessionID,
		CreatedAt: r.CreatedAt.Time,
	}, nil
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi.yaml documents for this domain's POST validation failures (same helper shape as
// macro's, tax-returns' and crypto-trades').
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}
