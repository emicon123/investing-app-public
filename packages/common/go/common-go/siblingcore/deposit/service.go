package deposit

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/httpx"
	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/xirr"
)

const defaultPageSize = 50

// Service is the deposit service. Every write is one statement (InsertDeposit), so it takes no
// transaction runner; duplicates are handled by the Idempotency-Key middleware (ADR-111).
type Service struct {
	cfg    siblingcore.Config
	store  Store
	prices siblingcore.PriceSource
	now    func() time.Time
}

// NewService wires the service; now nil = historiaquery.NowInWarsaw (the injected XIRR valuation
// date, ADR-112).
func NewService(cfg siblingcore.Config, store Store, prices siblingcore.PriceSource, now func() time.Time) *Service {
	if now == nil {
		now = historiaquery.NowInWarsaw
	}
	return &Service{cfg: cfg, store: store, prices: prices, now: now}
}

// List returns one page of p's deposits, newest first, and the total count (size <= 0 = 50).
func (s *Service) List(ctx context.Context, p siblingcore.Portfolio, year *int, page, size int) ([]siblingcore.Deposit, int64, error) {
	if err := s.cfg.CheckPortfolio("portfolio", p); err != nil {
		return nil, 0, err
	}
	if size <= 0 {
		size = defaultPageSize
	}
	items, err := s.store.ListDeposits(ctx, p, year, size, page*size)
	if err != nil {
		return nil, 0, fmt.Errorf("deposit: list: %w", err)
	}
	total, err := s.store.CountDeposits(ctx, p, year)
	if err != nil {
		return nil, 0, fmt.Errorf("deposit: count: %w", err)
	}
	return items, total, nil
}

// Create records one deposit (amount must be positive).
func (s *Service) Create(ctx context.Context, d siblingcore.Deposit) (siblingcore.Deposit, error) {
	if d.Amount.Sign() <= 0 {
		return siblingcore.Deposit{}, httpx.NewValidationError("amount", "amount must be positive")
	}
	if err := s.cfg.CheckPortfolio("portfolio", d.Portfolio); err != nil {
		return siblingcore.Deposit{}, err
	}
	created, err := s.store.InsertDeposit(ctx, d)
	if err != nil {
		return siblingcore.Deposit{}, fmt.Errorf("deposit: create: %w", err)
	}
	return created, nil
}

// ComputeXirr summarises p's deposits (negated, oldest first) against p's current value on
// today's Warsaw date. The current value degrades to zero on any read or pricing failure.
func (s *Service) ComputeXirr(ctx context.Context, p siblingcore.Portfolio) (xirr.Result, error) {
	if err := s.cfg.CheckPortfolio("portfolio", p); err != nil {
		return xirr.Result{}, err
	}
	deposits, err := s.store.ListDepositsAsc(ctx, p)
	if err != nil {
		return xirr.Result{}, fmt.Errorf("deposit: list deposits: %w", err)
	}
	flows := make([]xirr.Flow, 0, len(deposits))
	for _, d := range deposits {
		flows = append(flows, xirr.Flow{Date: d.DepositDate, Amount: d.Amount.Neg()})
	}
	return xirr.Summarize(flows, s.currentValue(ctx, p), s.now()), nil
}

func (s *Service) currentValue(ctx context.Context, p siblingcore.Portfolio) decimal.Decimal {
	subs, err := s.store.ListSubassets(ctx, p)
	if err != nil {
		return decimal.Zero
	}
	assets, err := s.store.ListAssets(ctx, p)
	if err != nil {
		return decimal.Zero
	}
	prices, err := s.prices.PricePlnByYahooTicker(ctx)
	if err != nil {
		return decimal.Zero
	}
	rates, err := s.prices.AllRatesToPln(ctx)
	if err != nil {
		return decimal.Zero
	}
	values := rebalancecalc.ComputeValues(siblingcore.PositionInputs(assets, subs), rates, prices)
	return rebalancecalc.TotalPortfolioValuePln(values)
}
