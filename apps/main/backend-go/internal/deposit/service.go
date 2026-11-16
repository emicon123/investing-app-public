package deposit

import (
	"fmt"
	"time"

	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/rebalancingcalc"
	"investing-app/common-go/historiaquery"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/xirr"
)

// Service is the deposit service layer: handler -> Service -> sqlc Queries. No TransactFunc
// (architect-output §4 — every write here is single-statement).
type Service struct {
	queries Queries
	prices  PriceSource
}

// NewService wires queries + price source (explicit constructor wiring, ADR-094 §2).
func NewService(queries Queries, prices PriceSource) *Service {
	return &Service{queries: queries, prices: prices}
}

const defaultPageSize = 20

// List is the direct port of DepositService.listDeposits.
func (s *Service) List(ctx context.Context, filter ListFilter, page, size int) ([]DepositDTO, int64, error) {
	if size <= 0 {
		size = defaultPageSize
	}
	arg := sqlcgen.ListDepositsParams{PageLimit: int32(size), PageOffset: int32(page * size)}
	countArg := sqlcgen.CountDepositsParams{}
	if filter.Portfolio != nil {
		v := sqlcgen.PortfolioEnum(*filter.Portfolio)
		arg.Portfolio, countArg.Portfolio = &v, &v
	}
	if filter.AccountType != nil {
		v := sqlcgen.AccountTypeEnum(*filter.AccountType)
		arg.AccountType, countArg.AccountType = &v, &v
	}
	if filter.Exchange != nil {
		v := sqlcgen.ExchangeEnum(*filter.Exchange)
		arg.Exchange, countArg.Exchange = &v, &v
	}
	if filter.Year != nil {
		v := int32(*filter.Year)
		arg.Year, countArg.Year = &v, &v
	}
	rows, err := s.queries.ListDeposits(ctx, arg)
	if err != nil {
		return nil, 0, fmt.Errorf("deposit: list: %w", err)
	}
	total, err := s.queries.CountDeposits(ctx, countArg)
	if err != nil {
		return nil, 0, fmt.Errorf("deposit: count: %w", err)
	}
	out := make([]DepositDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, dto)
	}
	return out, total, nil
}

// Create is the direct port of DepositService.createDeposit — a manually-entered deposit is by
// definition new EXTERNAL capital (ADR-059; no manual-entry path for an internal transfer).
func (s *Service) Create(ctx context.Context, in CreateInput) (DepositDTO, error) {
	if in.Amount.Sign() <= 0 {
		return DepositDTO{}, httpx.NewValidationError("amount", "amount must be positive")
	}
	if (in.AccountType == "IKE" || in.AccountType == "IKZE") && in.Currency != "PLN" {
		return DepositDTO{}, httpx.NewValidationError("currency", fmt.Sprintf(
			"IKE/IKZE deposits must be in PLN (statutory annual limits are PLN-denominated), got %s", in.Currency))
	}
	var comment *string
	if in.Comment != nil {
		comment = in.Comment
	}
	row, err := s.queries.InsertDeposit(ctx, sqlcgen.InsertDepositParams{
		DepositDate: pgtype.Date{Time: in.DepositDate, Valid: true}, Amount: pgnum.FromDecimal(in.Amount),
		Currency: sqlcgen.CurrencyEnum(in.Currency), Exchange: sqlcgen.ExchangeEnum(in.Exchange),
		AccountType: sqlcgen.AccountTypeEnum(in.AccountType), Portfolio: sqlcgen.PortfolioEnum(in.Portfolio),
		Comment: comment, CapitalSource: sqlcgen.CapitalSourceEnumEXTERNAL,
	})
	if err != nil {
		return DepositDTO{}, fmt.Errorf("deposit: create: %w", err)
	}
	return toDTO(row)
}

// GetAccountCapUsage is the direct port of DepositService.getAccountCapUsage. remainingPln is
// deliberately left UNCLAMPED here (differs from PurchaseSessionService.computeLimits' zero
// floor — see ADR-058).
func (s *Service) GetAccountCapUsage(ctx context.Context, year int) ([]AccountCapUsageItem, error) {
	items := make([]AccountCapUsageItem, 0, 2)
	for _, accountType := range []string{"IKE", "IKZE"} {
		cu, err := ComputeCapUsage(ctx, s.queries, accountType, year)
		if err != nil {
			return nil, err
		}
		items = append(items, AccountCapUsageItem{
			AccountType: accountType, Year: year, TotalDepositedPln: cu.TotalDepositedPln,
			AnnualCapPln: cu.AnnualCapPln, RemainingPln: cu.RemainingPln, UtilizationPct: cu.UtilizationPct,
		})
	}
	return items, nil
}

// UpsertAccountCapAdjustment is the direct port of AccountCapUsageService.upsertAdjustment,
// exposed through DepositsController's own PUT endpoint.
func (s *Service) UpsertAccountCapAdjustment(ctx context.Context, accountType string, year int, remainingPln decimal.Decimal) (AccountCapUsageItem, error) {
	cu, err := UpsertAdjustment(ctx, s.queries, accountType, year, remainingPln)
	if err != nil {
		return AccountCapUsageItem{}, err
	}
	return AccountCapUsageItem{
		AccountType: accountType, Year: year, TotalDepositedPln: cu.TotalDepositedPln,
		AnnualCapPln: cu.AnnualCapPln, RemainingPln: cu.RemainingPln, UtilizationPct: cu.UtilizationPct,
	}, nil
}

// ComputeXirr computes the portfolio's XIRR. Returns a nil XirrPct (never an error) when there are
// insufficient data points or the solver fails to converge — callers treat nil as "unavailable".
func (s *Service) ComputeXirr(ctx context.Context, portfolio string) (XirrResult, error) {
	rows, err := s.queries.ListHistoriaExternalDeposits(ctx, sqlcgen.ListHistoriaExternalDepositsParams{
		Portfolio: sqlcgen.PortfolioEnum(portfolio), CapitalSource: sqlcgen.CapitalSourceEnumEXTERNAL,
	})
	if err != nil {
		return XirrResult{}, fmt.Errorf("deposit: list external deposits: %w", err)
	}
	if len(rows) == 0 {
		return XirrResult{TotalInvestedPln: decimal.Zero, CurrentValuePln: decimal.Zero}, nil
	}

	// ADR-066: PLN cash is no longer a position, so this sum is already invested-assets-only.
	currentValuePln, err := rebalancingcalc.TotalPortfolioValuePln(ctx, s.queries, s.prices, portfolio)
	if err != nil {
		return XirrResult{}, err
	}

	var cashFlows []CashFlow
	totalInvestedPln := decimal.Zero
	for _, r := range rows {
		amount, err := pgnum.ToDecimal(r.Amount)
		if err != nil {
			return XirrResult{}, fmt.Errorf("deposit: decode deposit amount: %w", err)
		}
		currency := string(r.Currency)
		rate, rateErr := s.prices.RateToPln(ctx, currency)
		if rateErr == nil {
			amountPln := amount.Mul(rate)
			totalInvestedPln = totalInvestedPln.Add(amountPln)
			cashFlows = append(cashFlows, CashFlow{Date: r.DepositDate.Time, Amount: amountPln.Neg()})
		} else if currency == "PLN" {
			// PLN's own rate lookup never actually fails; the structure is kept as the original
			// fallback shape.
			totalInvestedPln = totalInvestedPln.Add(amount)
			cashFlows = append(cashFlows, CashFlow{Date: r.DepositDate.Time, Amount: amount.Neg()})
		}
		// else: non-PLN with an unconvertible rate — silently dropped.
	}

	today := historiaquery.NowInWarsaw()
	cashFlows = append(cashFlows, CashFlow{Date: today, Amount: currentValuePln})

	var simpleReturnPct *decimal.Decimal
	if totalInvestedPln.Sign() > 0 {
		v := currentValuePln.DivRound(totalInvestedPln, 6).Sub(decimal.NewFromInt(1))
		simpleReturnPct = &v
	}

	firstDepositDate := rows[0].DepositDate.Time // ORDER BY deposit_date ASC per the query
	days := dateOnlyUTC(today).Sub(dateOnlyUTC(firstDepositDate)).Hours() / 24
	yearsElapsedVal := decimal.NewFromFloat(days).DivRound(decimal.RequireFromString("365.25"), 4)

	xirrPct := xirr.Solve(cashFlows)

	return XirrResult{
		XirrPct: xirrPct, TotalInvestedPln: totalInvestedPln, CurrentValuePln: currentValuePln,
		SimpleReturnPct: simpleReturnPct, YearsElapsed: &yearsElapsedVal,
	}, nil
}

func toDTO(r sqlcgen.Deposit) (DepositDTO, error) {
	amount, err := pgnum.ToDecimal(r.Amount)
	if err != nil {
		return DepositDTO{}, fmt.Errorf("deposit: decode amount: %w", err)
	}
	return DepositDTO{
		ID: r.ID, DepositDate: r.DepositDate.Time, Amount: amount, Currency: string(r.Currency),
		Exchange: string(r.Exchange), AccountType: string(r.AccountType), Portfolio: string(r.Portfolio),
		Comment: r.Comment, CreatedAt: r.CreatedAt.Time,
	}, nil
}

func dateOnlyUTC(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
