package siblingcore

import (
	"context"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
)

// MembersByAsset groups subassets into costbasis members by asset id, each group ordered by row
// id so cost writes are deterministic. Every member holds a copy of its row's Holding, so a cost
// result written back (costmember.Apply) changes the member, never the caller's slice.
func MembersByAsset(subs []Subasset) map[int64][]*CostMember {
	out := make(map[int64][]*CostMember)
	for _, s := range subs {
		h := s.Holding
		out[s.AssetID] = append(out[s.AssetID], &CostMember{ID: s.ID, AssetID: s.AssetID, YahooTicker: s.YahooTicker, Units: &h})
	}
	for _, ms := range out {
		sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
	}
	return out
}

// CostWriter persists a recorded cost on one subasset row (UpdatePositionCost).
type CostWriter interface {
	WriteCost(ctx context.Context, subassetID int64, cost *decimal.Decimal) error
}

// PersistCosts writes every changed member's cost except the rows in writtenByDelta (those were
// already written together with their units).
func PersistCosts(ctx context.Context, w CostWriter, changed []*CostMember, writtenByDelta ...int64) error {
	skip := make(map[int64]bool, len(writtenByDelta))
	for _, id := range writtenByDelta {
		skip[id] = true
	}
	for _, m := range changed {
		if skip[m.ID] {
			continue
		}
		if err := w.WriteCost(ctx, m.ID, m.Units.TotalSpentPln); err != nil {
			return fmt.Errorf("write cost for position %d: %w", m.ID, err)
		}
	}
	return nil
}
