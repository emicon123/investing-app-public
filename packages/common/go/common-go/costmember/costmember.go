// Package costmember adapts the pure costbasis kernel to the trade services' in-memory position
// rows: build kernel rows from members, and write the kernel's result back onto them.
// It is the ONLY place that mutates caller state; costbasis itself stays value-in/value-out.
//
// Each app keeps its own unit model (main and portfolioB five account buckets, portfolioC one units
// column) behind Holding, and instantiates Member with its concrete *PositionUnits
// (type CostMember = costmember.Member[*PositionUnits]), so callers keep reading and changing
// their own fields with no type assertion. The sqlc-typed parts — reading price_cache, grouping
// positions rows into members, writing total_spent_pln — stay per app.
package costmember

import (
	"github.com/shopspring/decimal"

	"investing-app/common-go/costbasis"
)

// Holding is what the glue needs from one positions row's mutable unit state: the units held
// over every account bucket, and the recorded cost (total_spent_pln; nil = NULL).
type Holding interface {
	UnitsTotal() decimal.Decimal
	CostPln() *decimal.Decimal
	SetCostPln(c *decimal.Decimal)
}

// Member is one positions row of an asset as the trade services keep it in memory: the ids and
// price key the kernel needs plus the row's mutable Holding, so a cost result written back here
// is visible to every later step of the same transaction.
type Member[H Holding] struct {
	ID          int64
	AssetID     int64
	YahooTicker *string
	Units       H
}

// Rows builds the kernel rows of an asset's members from their CURRENT in-memory state — call it
// BEFORE the trade's own unit delta is applied (kernel rows carry pre-trade units). A member with
// no yahoo ticker or no cached price is unpriced (nil price). The cost is copied, never aliased.
func Rows[H Holding](members []*Member[H], prices map[string]decimal.Decimal) []costbasis.Row {
	rows := make([]costbasis.Row, 0, len(members))
	for _, m := range members {
		row := costbasis.Row{PositionID: m.ID, AssetID: m.AssetID, Units: m.Units.UnitsTotal(), CostPln: copyDec(m.Units.CostPln())}
		if m.YahooTicker != nil && *m.YahooTicker != "" {
			if p, ok := prices[*m.YahooTicker]; ok {
				row.PricePln = &p
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// Apply copies the kernel's result (same rows as before, only costs possibly different) onto the
// members' in-memory Holding and returns the members whose cost changed — the ones that must be
// persisted.
func Apply[H Holding](members []*Member[H], before, after []costbasis.Row) []*Member[H] {
	byID := make(map[int64]*Member[H], len(members))
	for _, m := range members {
		byID[m.ID] = m
	}
	var changed []*Member[H]
	for _, r := range costbasis.ChangedRows(before, after) {
		if m, ok := byID[r.PositionID]; ok {
			m.Units.SetCostPln(copyDec(r.CostPln))
			changed = append(changed, m)
		}
	}
	return changed
}

// PlanTransfer runs costbasis.PlanTransfer over the sold asset and every asset a buy leg touches
// (members grouped by asset_id, as they stand BEFORE any write of the transfer), applies the new
// costs and returns the members whose cost changed: sold asset first, then the legs' assets in
// leg order, each asset once (deterministic write order).
func PlanTransfer[H Holding](members map[int64][]*Member[H], prices map[string]decimal.Decimal,
	soldAssetID int64, t costbasis.Transfer) []*Member[H] {
	before := map[int64][]costbasis.Row{}
	order := []int64{soldAssetID}
	before[soldAssetID] = Rows(members[soldAssetID], prices)
	for _, leg := range t.Buys {
		if _, ok := before[leg.AssetID]; !ok {
			before[leg.AssetID] = Rows(members[leg.AssetID], prices)
			order = append(order, leg.AssetID)
		}
	}
	after := costbasis.PlanTransfer(before, soldAssetID, t)
	var changed []*Member[H]
	for _, assetID := range order {
		changed = append(changed, Apply(members[assetID], before[assetID], after[assetID])...)
	}
	return changed
}

func copyDec(d *decimal.Decimal) *decimal.Decimal {
	if d == nil {
		return nil
	}
	v := *d
	return &v
}
