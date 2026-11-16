package rebalancing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/httpx"
	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
)

// settleFunc is Funding.Settle bound to the transaction's adapter.
type settleFunc func(ctx context.Context, req ConfirmRequest, paid map[siblingcore.Bucket]decimal.Decimal) (decimal.Decimal, error)

// Service is the rebalancing service. The Funding strategy is bound to the app's adapter type in
// NewService, so the service itself is not generic.
type Service struct {
	cfg    siblingcore.Config
	store  Store
	prices siblingcore.PriceSource
	budget func(ctx context.Context, p siblingcore.Portfolio, override *decimal.Decimal) (Budget, error)
	check  func(req ConfirmRequest) error
	tx     func(ctx context.Context, fn func(q Store, settle settleFunc) error) error
	now    func() time.Time
}

// NewService: tx must be the app's serialised money-write runner; now nil =
// historiaquery.NowInWarsaw (the confirm's purchase and deposit date).
func NewService[S Store](cfg siblingcore.Config, store S, tx siblingcore.TxFunc[S], prices siblingcore.PriceSource, funding Funding[S], now func() time.Time) *Service {
	if now == nil {
		now = historiaquery.NowInWarsaw
	}
	return &Service{
		cfg: cfg, store: store, prices: prices, now: now,
		budget: func(ctx context.Context, p siblingcore.Portfolio, override *decimal.Decimal) (Budget, error) {
			return funding.Budget(ctx, store, p, override)
		},
		check: funding.Check,
		tx: func(ctx context.Context, fn func(Store, settleFunc) error) error {
			return tx(ctx, func(q S) error {
				return fn(q, func(ctx context.Context, req ConfirmRequest, paid map[siblingcore.Bucket]decimal.Decimal) (decimal.Decimal, error) {
					return funding.Settle(ctx, q, req, paid)
				})
			})
		},
	}
}

// Calculate allocates the deployable cash proportionally to the assets' deficits — a buy list
// only, never a sell (manual-sell-only rule). Read-only: no transaction, no lock.
func (s *Service) Calculate(ctx context.Context, p siblingcore.Portfolio, depositOverride *decimal.Decimal) (Result, error) {
	if err := s.cfg.CheckPortfolio("portfolio", p); err != nil {
		return Result{}, err
	}
	budget, err := s.budget(ctx, p, depositOverride)
	if err != nil {
		return Result{}, err
	}
	fxRates, err := s.prices.AllRatesToPln(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("rebalancing: load fx rates: %w", err)
	}
	prices, err := s.prices.PricePlnByYahooTicker(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("rebalancing: load prices: %w", err)
	}
	subs, err := s.store.ListSubassets(ctx, p)
	if err != nil {
		return Result{}, fmt.Errorf("rebalancing: list positions: %w", err)
	}
	assets, err := s.store.ListAssets(ctx, p)
	if err != nil {
		return Result{}, fmt.Errorf("rebalancing: list assets: %w", err)
	}
	inputs := siblingcore.PositionInputs(assets, subs)
	computation := rebalancecalc.ComputeValues(inputs, fxRates, prices)
	total := rebalancecalc.TotalPortfolioValuePln(computation)
	buyList := rebalancecalc.AllocateProportional(computation.Tradable, budget.DeployablePln, fxRates, total, budget.Allocate, budget.Balances)

	fetchedAt, err := s.prices.FetchedAtByYahooTicker(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("rebalancing: load price fetched_at: %w", err)
	}
	result := Result{
		Portfolio:              p,
		DeployableCashPln:      budget.DeployablePln.Round(4),
		TotalPortfolioValuePln: total.Round(4),
		PricesRefreshedAt:      rebalancecalc.PricesRefreshedAt(inputs, fetchedAt, time.Now()),
		BuyList:                buyItems(subs, buyList),
		DonutData:              donutData(computation.Tradable, total),
	}
	if budget.CashOnHandPln != nil {
		cash := budget.CashOnHandPln.Round(4)
		result.CashOnHandPln = &cash
	}
	return result, nil
}

// buyItems maps the kernel's buy list and lists, per item, its ticker first and then the other
// subassets of the same asset (the tickers the user may buy instead).
func buyItems(subs []siblingcore.Subasset, list []rebalancecalc.BuyListItem) []BuyItem {
	byAsset := map[int64][]string{}
	primaryAsset := map[string]int64{}
	for _, s := range subs {
		byAsset[s.AssetID] = append(byAsset[s.AssetID], s.Ticker)
		if s.IsPrimary {
			primaryAsset[s.Ticker] = s.AssetID
		}
	}
	out := make([]BuyItem, 0, len(list))
	for _, item := range list {
		available := []string{item.Ticker}
		if assetID, ok := primaryAsset[item.Ticker]; ok {
			for _, t := range byAsset[assetID] {
				if t != item.Ticker {
					available = append(available, t)
				}
			}
		}
		var allocs []AccountAlloc
		for _, a := range item.AccountAllocations {
			allocs = append(allocs, AccountAlloc{Bucket: siblingcore.Bucket(a.AccountType), AllocPln: a.AllocPln, UnitsToBuy: a.UnitsToBuy})
		}
		out = append(out, BuyItem{
			Ticker: item.Ticker, Name: item.Name, Exchange: item.Exchange, PurchaseCurrency: item.PurchaseCurrency,
			AllocatedPln: item.AllocatedPln, AllocatedNative: item.AllocatedNative, UnitsToBuy: item.UnitsToBuy,
			CurrentPct: item.CurrentPct, TargetPct: item.TargetPct, DeviationPct: item.DeviationPct,
			AccountAllocations: allocs, AvailableTickers: available,
		})
	}
	return out
}

// Confirm applies a confirmed purchase session in one serialised transaction (ADR-111): every
// read that feeds a write (the portfolio's rows, the cached prices) happens inside it. Write
// order: holdings (units + cost), purchase lots, the funding policy's Settle, the deposit row.
func (s *Service) Confirm(ctx context.Context, req ConfirmRequest) error {
	if len(req.Items) == 0 {
		return httpx.NewValidationError("items", "at least one item is required")
	}
	if err := s.check(req); err != nil {
		return err
	}
	if err := s.cfg.CheckPortfolio("portfolio", req.Portfolio); err != nil {
		return err
	}
	for _, item := range req.Items {
		if err := s.cfg.CheckUnits("items.accountAllocations.accountType", item.Units); err != nil {
			return err
		}
	}
	return s.tx(ctx, func(q Store, settle settleFunc) error {
		subs, err := q.ListSubassets(ctx, req.Portfolio)
		if err != nil {
			return fmt.Errorf("rebalancing: list positions: %w", err)
		}
		prices, err := q.PricePlnByYahooTicker(ctx)
		if err != nil {
			return fmt.Errorf("rebalancing: load prices: %w", err)
		}
		plan, err := buildConfirmPlan(s.cfg, req.Items, subs, prices)
		if err != nil {
			return err
		}
		sessionID := uuid.New()
		today := s.now()
		for _, u := range plan.updates {
			if err := q.WriteHolding(ctx, u.id, u.holding); err != nil {
				return fmt.Errorf("rebalancing: apply delta for position %d: %w", u.id, err)
			}
		}
		for _, lot := range plan.lots {
			lot.Portfolio, lot.SessionID, lot.PurchasedAt = req.Portfolio, sessionID, today
			if err := q.InsertPurchaseLot(ctx, lot); err != nil {
				return fmt.Errorf("rebalancing: insert purchase lot for %s: %w", lot.Ticker, err)
			}
		}
		depositPln, err := settle(ctx, req, plan.paid)
		if err != nil {
			return err
		}
		if _, err := q.InsertDeposit(ctx, siblingcore.Deposit{Portfolio: req.Portfolio, DepositDate: today, Amount: depositPln}); err != nil {
			return fmt.Errorf("rebalancing: record deposit: %w", err)
		}
		return nil
	})
}
