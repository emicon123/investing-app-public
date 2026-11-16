package store

import (
	"context"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore/snapshot"
	"portfolio-b/backend-go/db/sqlcgen"
)

var _ snapshot.Store = (*Queries)(nil)

// UpsertSnapshot writes one historia row (the query's ON CONFLICT decides the key).
func (q *Queries) UpsertSnapshot(ctx context.Context, s snapshot.Snapshot) error {
	return q.q.UpsertHistoria(ctx, sqlcgen.UpsertHistoriaParams{
		SnapshotDate: pgnum.Date(s.Date), TotalDeposits: pgnum.FromDecimal(s.TotalDeposits),
		PortfolioValue: pgnum.FromDecimal(s.PortfolioValue), Xirr: pgnum.FromDecimalPtr(s.Xirr), PositionValues: s.PositionValuesJSON,
	})
}
