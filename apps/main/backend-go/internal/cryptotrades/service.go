package cryptotrades

import (
	"context"
	"fmt"
	"strings"

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
type Queries interface {
	ListCryptoTrades(ctx context.Context, arg sqlcgen.ListCryptoTradesParams) ([]sqlcgen.CryptoTrade, error)
	CountCryptoTrades(ctx context.Context, arg sqlcgen.CountCryptoTradesParams) (int64, error)
	FindCryptoTradesByYear(ctx context.Context, year int32) ([]sqlcgen.CryptoTrade, error)
	InsertCryptoTrade(ctx context.Context, arg sqlcgen.InsertCryptoTradeParams) (sqlcgen.CryptoTrade, error)
}

// defaultPageSize is the default page size (50). No server-side maximum is enforced — any size
// passes through (spec fix 2026-09-15: the old "maximum 100" never matched the implementation).
const defaultPageSize = 50

// Service is the crypto-trades service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries Queries
}

// NewService wires queries (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries) *Service {
	return &Service{queries: queries}
}

// List returns one page of crypto trades with optional ticker/year/exchange/side filters. A blank
// ticker means "no filter". Out-of-range page/size answers 400 (the legacy PageRequest.of message
// texts, verbatim). Unknown exchange/side values answer 400 VALIDATION_ERROR.
func (s *Service) List(ctx context.Context, ticker *string, year *int, exchange, side *string, page, size *int) (PageDTO, error) {
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
	var tickerFilter *string
	if ticker != nil && strings.TrimSpace(*ticker) != "" {
		tickerFilter = ticker
	}
	var yearFilter *int32
	if year != nil {
		y := int32(*year)
		yearFilter = &y
	}
	var exchangeFilter *sqlcgen.ExchangeEnum
	if exchange != nil {
		e := sqlcgen.ExchangeEnum(*exchange)
		if e != sqlcgen.ExchangeEnumXTB && e != sqlcgen.ExchangeEnumBINANCE {
			return PageDTO{}, httpx.NewValidationError("exchange", "Invalid exchange: "+*exchange)
		}
		exchangeFilter = &e
	}
	var sideFilter *sqlcgen.TradeSideEnum
	if side != nil {
		sd := sqlcgen.TradeSideEnum(*side)
		if sd != sqlcgen.TradeSideEnumBUY && sd != sqlcgen.TradeSideEnumSELL {
			return PageDTO{}, httpx.NewValidationError("side", "Invalid side: "+*side)
		}
		sideFilter = &sd
	}
	rows, err := s.queries.ListCryptoTrades(ctx, sqlcgen.ListCryptoTradesParams{
		Ticker: tickerFilter, Year: yearFilter,
		Exchange: exchangeFilter, Side: sideFilter,
		PageLimit: int32(l), PageOffset: int32(p * l),
	})
	if err != nil {
		return PageDTO{}, fmt.Errorf("cryptotrades: list: %w", err)
	}
	total, err := s.queries.CountCryptoTrades(ctx, sqlcgen.CountCryptoTradesParams{
		Ticker: tickerFilter, Year: yearFilter,
		Exchange: exchangeFilter, Side: sideFilter,
	})
	if err != nil {
		return PageDTO{}, fmt.Errorf("cryptotrades: count: %w", err)
	}
	items := make([]TradeDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			return PageDTO{}, fmt.Errorf("cryptotrades: decode row %d: %w", r.ID, err)
		}
		items = append(items, dto)
	}
	return PageDTO{Items: items, Total: total, Number: p, Size: l}, nil
}

// Create records one manual crypto trade. The server forces source=MANUAL (there is no source input
// at all — CreateInput has no such field, and the INSERT hardcodes the literal), so a caller can
// never smuggle in a MIGRATION-sourced row. Value rules: non-blank bounded ticker, required enum
// fields, positive quantity/prices/amounts; every violation answers 422 per docs/openapi.yaml.
func (s *Service) Create(ctx context.Context, in CreateInput) (TradeDTO, error) {
	if strings.TrimSpace(in.Ticker) == "" {
		return TradeDTO{}, unprocessable("ticker", "must not be blank")
	}
	if len(in.Ticker) > 20 {
		return TradeDTO{}, unprocessable("ticker", "size must be between 0 and 20")
	}
	switch in.Exchange {
	case string(sqlcgen.ExchangeEnumXTB), string(sqlcgen.ExchangeEnumBINANCE):
	default:
		return TradeDTO{}, unprocessable("exchange", "must not be null")
	}
	switch in.AccountType {
	case string(sqlcgen.AccountTypeEnumIKE), string(sqlcgen.AccountTypeEnumIKZE), string(sqlcgen.AccountTypeEnumREGULAR):
	default:
		return TradeDTO{}, unprocessable("accountType", "must not be null")
	}
	switch in.Side {
	case string(sqlcgen.TradeSideEnumBUY), string(sqlcgen.TradeSideEnumSELL):
	default:
		return TradeDTO{}, unprocessable("side", "must not be null")
	}
	switch in.PriceCurrency {
	case "PLN", "EUR", "USD", "CHF", "GBP":
	default:
		return TradeDTO{}, unprocessable("priceCurrency", "must not be null")
	}
	if in.Quantity.Cmp(decimal.Zero) <= 0 {
		return TradeDTO{}, unprocessable("quantity", "must be greater than 0")
	}
	if in.UnitPrice.Cmp(decimal.Zero) <= 0 {
		return TradeDTO{}, unprocessable("unitPrice", "must be greater than 0")
	}
	if in.TotalAmount.Cmp(decimal.Zero) <= 0 {
		return TradeDTO{}, unprocessable("totalAmount", "must be greater than 0")
	}
	if in.AmountPln.Cmp(decimal.Zero) <= 0 {
		return TradeDTO{}, unprocessable("amountPln", "must be greater than 0")
	}
	tradeDate := pgtype.Date{Time: in.TradeDate, Valid: true}
	row, err := s.queries.InsertCryptoTrade(ctx, sqlcgen.InsertCryptoTradeParams{
		TradeDate: tradeDate, Ticker: in.Ticker,
		Exchange:    sqlcgen.ExchangeEnum(in.Exchange),
		AccountType: sqlcgen.AccountTypeEnum(in.AccountType),
		Side:        sqlcgen.TradeSideEnum(in.Side),
		Quantity:    pgnum.FromDecimal(in.Quantity), UnitPrice: pgnum.FromDecimal(in.UnitPrice),
		PriceCurrency: sqlcgen.CurrencyEnum(in.PriceCurrency),
		TotalAmount:   pgnum.FromDecimal(in.TotalAmount), AmountPln: pgnum.FromDecimal(in.AmountPln),
		Notes: in.Notes,
	})
	if err != nil {
		return TradeDTO{}, fmt.Errorf("cryptotrades: create: %w", err)
	}
	return toDTO(row)
}

// TaxSummary returns the annual PIT-38 summary — the direct port of
// CryptoTradeService.getTaxSummary: fetch the year's rows (trade_date DESC, same as
// CryptoTradeRepository.findAllByYear) and run the pure TaxSummary computation over them.
func (s *Service) TaxSummary(ctx context.Context, year int) (Summary, error) {
	rows, err := s.queries.FindCryptoTradesByYear(ctx, int32(year))
	if err != nil {
		return Summary{}, fmt.Errorf("cryptotrades: tax summary: %w", err)
	}
	inputs := make([]TradeInput, 0, len(rows))
	for _, r := range rows {
		amount, err := pgnum.ToDecimal(r.AmountPln)
		if err != nil {
			return Summary{}, fmt.Errorf("cryptotrades: decode amount_pln for row %d: %w", r.ID, err)
		}
		inputs = append(inputs, TradeInput{Ticker: r.Ticker, Side: string(r.Side), AmountPln: amount})
	}
	return TaxSummary(year, inputs), nil
}

func toDTO(r sqlcgen.CryptoTrade) (TradeDTO, error) {
	quantity, err := pgnum.ToDecimal(r.Quantity)
	if err != nil {
		return TradeDTO{}, err
	}
	unitPrice, err := pgnum.ToDecimal(r.UnitPrice)
	if err != nil {
		return TradeDTO{}, err
	}
	totalAmount, err := pgnum.ToDecimal(r.TotalAmount)
	if err != nil {
		return TradeDTO{}, err
	}
	amountPln, err := pgnum.ToDecimal(r.AmountPln)
	if err != nil {
		return TradeDTO{}, err
	}
	return TradeDTO{
		ID: r.ID, TradeDate: r.TradeDate.Time, Ticker: r.Ticker,
		Exchange: string(r.Exchange), AccountType: string(r.AccountType), Side: string(r.Side),
		Quantity: quantity, UnitPrice: unitPrice, PriceCurrency: string(r.PriceCurrency),
		TotalAmount: totalAmount, AmountPln: amountPln,
		Source: r.Source, Notes: r.Notes, CreatedAt: r.CreatedAt.Time,
	}, nil
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi.yaml documents for this domain's POST validation failures (same helper shape as
// macro's and tax-returns').
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}
