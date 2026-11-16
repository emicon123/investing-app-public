package rebalancing

import (
	"context"
	"fmt"

	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
)

// History returns p's purchase sessions valued at today's cached prices; a deleted asset's lots
// keep their name (the lookup includes soft-deleted rows).
func (s *Service) History(ctx context.Context, p siblingcore.Portfolio) ([]rebalancecalc.PurchaseSessionResult, error) {
	if err := s.cfg.CheckPortfolio("portfolio", p); err != nil {
		return nil, err
	}
	lots, err := s.store.ListPurchaseLots(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("rebalancing: list purchase lots: %w", err)
	}
	if len(lots) == 0 {
		return nil, nil
	}
	subs, err := s.store.ListSubassetsIncludingDeleted(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("rebalancing: list positions: %w", err)
	}
	byTicker := make(map[string]rebalancecalc.PositionLookup, len(subs))
	for _, sub := range subs {
		byTicker[sub.Ticker] = rebalancecalc.PositionLookup{Name: sub.Name, YahooTicker: sub.YahooTicker}
	}
	prices, err := s.prices.PricePlnByYahooTicker(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebalancing: load prices: %w", err)
	}
	inputs := make([]rebalancecalc.PurchaseLotInput, 0, len(lots))
	for _, l := range lots {
		inputs = append(inputs, rebalancecalc.PurchaseLotInput{
			SessionID: l.SessionID, PurchasedAt: l.PurchasedAt, CreatedAt: l.CreatedAt,
			Ticker: l.Ticker, Units: l.Units, PricePln: l.PricePln, CostPln: l.CostPln,
		})
	}
	return rebalancecalc.BuildPurchaseHistory(inputs, byTicker, prices), nil
}
