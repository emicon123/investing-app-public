package rebalancing

import (
	"sort"

	"github.com/shopspring/decimal"

	"investing-app/common-go/costbasis"
	"investing-app/common-go/costmember"
	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
)

type holdingUpdate struct {
	id      int64
	holding siblingcore.Holding
}

// confirmPlan is everything one Confirm writes, computed before the first write.
type confirmPlan struct {
	updates []holdingUpdate
	lots    []PurchaseLot // Portfolio/SessionID/PurchasedAt filled in by Confirm
	paid    map[siblingcore.Bucket]decimal.Decimal
}

// buildConfirmPlan applies each item's positive per-bucket units to its row (units summed per
// bucket, PLN rounded once per bucket), records a purchase lot for the units bought into
// TracksLots buckets when the ticker is priced, and plans the asset-level cost (one costbasis
// leg per item over every bucket). An item that buys nothing is skipped; an unknown ticker is a
// 404.
func buildConfirmPlan(cfg siblingcore.Config, items []ConfirmItem, subs []siblingcore.Subasset, prices map[string]decimal.Decimal) (confirmPlan, error) {
	byTicker := make(map[string]siblingcore.Subasset, len(subs))
	for _, s := range subs {
		byTicker[s.Ticker] = s
	}
	plan := confirmPlan{paid: map[siblingcore.Bucket]decimal.Decimal{}}
	var legs []costbasis.Leg
	for _, item := range items {
		sub, ok := byTicker[item.Ticker]
		if !ok {
			return confirmPlan{}, httpx.NewNotFoundError("Position not found: " + item.Ticker)
		}
		price := decimal.Zero
		if sub.YahooTicker != nil {
			if p, ok := prices[*sub.YahooTicker]; ok {
				price = p
			}
		}
		holding := sub.Holding
		bought, paid, lotUnits := decimal.Zero, decimal.Zero, decimal.Zero
		for _, spec := range cfg.Buckets {
			u := item.Units.Of(spec.Bucket)
			if u.Sign() <= 0 {
				continue
			}
			holding.Units = holding.Units.Add(spec.Bucket, u)
			pln := u.Mul(price).Round(4)
			bought, paid = bought.Add(u), paid.Add(pln)
			plan.paid[spec.Bucket] = plan.paid[spec.Bucket].Add(pln)
			if spec.TracksLots {
				lotUnits = lotUnits.Add(u)
			}
		}
		if bought.Sign() == 0 {
			continue
		}
		plan.updates = append(plan.updates, holdingUpdate{id: sub.ID, holding: holding})
		if paid.Sign() > 0 { // no derivable amount paid (unpriced) = cost untouched
			value := paid
			legs = append(legs, costbasis.Leg{PositionID: sub.ID, AssetID: sub.AssetID, Units: bought, PricePln: price, ValuePln: &value})
		}
		if lotUnits.Sign() > 0 && price.Sign() > 0 {
			plan.lots = append(plan.lots, PurchaseLot{Ticker: item.Ticker, Units: lotUnits, PricePln: price, CostPln: lotUnits.Mul(price).Round(4)})
		}
	}
	applyBuyCosts(plan.updates, legs, subs, prices)
	return plan, nil
}

// applyBuyCosts runs the cost kernel over the bought assets' rows as they stood before this
// confirm and copies each updated row's new cost onto its update (only the bought rows are
// written; their siblings' costs are never touched by a buy).
func applyBuyCosts(updates []holdingUpdate, legs []costbasis.Leg, subs []siblingcore.Subasset, prices map[string]decimal.Decimal) {
	if len(legs) == 0 {
		return
	}
	touched := make(map[int64]bool, len(legs))
	for _, leg := range legs {
		touched[leg.AssetID] = true
	}
	var rows []siblingcore.Subasset
	for _, s := range subs {
		if touched[s.AssetID] {
			rows = append(rows, s)
		}
	}
	members := siblingcore.MembersByAsset(rows)
	before := make(map[int64][]costbasis.Row, len(members))
	for assetID, ms := range members {
		sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
		before[assetID] = costmember.Rows(ms, prices)
	}
	newCost := map[int64]*decimal.Decimal{}
	for assetID, after := range costbasis.PlanTrades(before, nil, legs) {
		for _, m := range costmember.Apply(members[assetID], before[assetID], after) {
			newCost[m.ID] = m.Units.TotalSpentPln
		}
	}
	for i := range updates {
		if cost, ok := newCost[updates[i].id]; ok {
			updates[i].holding.TotalSpentPln = cost
		}
	}
}
