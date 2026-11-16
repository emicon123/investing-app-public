package position

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/costmember"
	"investing-app/common-go/pgnum"
)

// Task 161a-2 glue between the trade services (internal/purchasesession, internal/rebalancing) and
// the shared pure kernel common-go/costbasis (design docs/architecture/diagrams/161-asset-entry-
// price.md §5). Every trade reads the asset's rows plus price_cache prices INSIDE its transaction
// (DB only — never a network call), builds kernel rows from the in-memory position state as it
// stood BEFORE the trade's units are applied, calls the kernel, copies the result back onto the
// in-memory rows and writes the changed rows atomically with the unit changes: the trade's own
// row(s) through UpdatePositionDelta (units + cost in one statement), every other changed
// sibling through UpdatePositionCost.

// CostMember is one positions row of an asset as the trade services keep it in memory: the ids
// and price key the kernel needs plus a pointer to the row's mutable PositionUnits, so a cost
// result written back (costmember.Apply) is visible to every later step of the same transaction.
type CostMember = costmember.Member[*PositionUnits]

var _ costmember.Holding = (*PositionUnits)(nil)

// PricePlnMap turns price_cache rows into yahoo ticker -> PLN price. Rows without a PLN price
// are left out, which makes the matching subasset "unpriced" (skipped when the asset is valued).
func PricePlnMap(rows []sqlcgen.GetAllPriceCacheRow) (map[string]decimal.Decimal, error) {
	out := make(map[string]decimal.Decimal, len(rows))
	for _, r := range rows {
		p, err := pgnum.ToDecimalPtr(r.PricePln)
		if err != nil {
			return nil, fmt.Errorf("position: decode price_cache.price_pln for %s: %w", r.Ticker, err)
		}
		if p != nil {
			out[r.Ticker] = *p
		}
	}
	return out, nil
}

// CostWriter is the one sqlc write PersistCosts needs.
type CostWriter interface {
	UpdatePositionCost(ctx context.Context, arg sqlcgen.UpdatePositionCostParams) error
}

// PersistCosts writes total_spent_pln of every changed member, skipping the ids the caller
// already persists itself through UpdatePositionDelta (which carries the cost in the same
// statement as the units).
func PersistCosts(ctx context.Context, w CostWriter, changed []*CostMember, writtenByDelta ...int64) error {
	skip := make(map[int64]bool, len(writtenByDelta))
	for _, id := range writtenByDelta {
		skip[id] = true
	}
	for _, m := range changed {
		if skip[m.ID] {
			continue
		}
		if err := w.UpdatePositionCost(ctx, sqlcgen.UpdatePositionCostParams{
			ID: m.ID, TotalSpentPln: pgnum.FromDecimalPtr(m.Units.TotalSpentPln),
		}); err != nil {
			return fmt.Errorf("position: write cost for position %d: %w", m.ID, err)
		}
	}
	return nil
}
