package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/rebalancing"
	"investing-app/common-go/siblingcore/transfer"
	"portfolio-c/backend-go/db/sqlcgen"
)

var (
	_ transfer.Store    = (*Queries)(nil)
	_ rebalancing.Store = (*Queries)(nil)
)

// GetSubassetByTicker: p's live row with ticker.
func (s *Queries) GetSubassetByTicker(ctx context.Context, p siblingcore.Portfolio, ticker string) (siblingcore.Subasset, error) {
	row, err := s.q.GetPositionByTickerAndPortfolio(ctx, sqlcgen.GetPositionByTickerAndPortfolioParams{Ticker: ticker, Portfolio: enum(p)})
	if err != nil {
		return siblingcore.Subasset{}, err
	}
	return subassetFromRow(row)
}

// WriteHolding is the one canonical delta write (UpdatePositionDelta): units from the single
// bucket plus the recorded cost, in one statement.
func (s *Queries) WriteHolding(ctx context.Context, subassetID int64, h siblingcore.Holding) error {
	_, err := s.q.UpdatePositionDelta(ctx, positionDeltaParams(subassetID, h))
	return err
}

func positionDeltaParams(id int64, h siblingcore.Holding) sqlcgen.UpdatePositionDeltaParams {
	return sqlcgen.UpdatePositionDeltaParams{
		ID: id, Units: pgnum.FromDecimal(h.Units.Of(siblingcore.SingleBucket)), TotalSpentPln: pgnum.FromDecimalPtr(h.TotalSpentPln),
	}
}

// WriteCost writes total_spent_pln alone (UpdatePositionCost).
func (s *Queries) WriteCost(ctx context.Context, subassetID int64, cost *decimal.Decimal) error {
	return s.q.UpdatePositionCost(ctx, sqlcgen.UpdatePositionCostParams{ID: subassetID, TotalSpentPln: pgnum.FromDecimalPtr(cost)})
}

// InsertTransferLeg stores the leg's portfolio (there is no account column).
func (s *Queries) InsertTransferLeg(ctx context.Context, r transfer.LegRecord) error {
	_, err := s.q.InsertPositionTransfer(ctx, sqlcgen.InsertPositionTransferParams{
		TransferID: pgtype.UUID{Bytes: r.TransferID, Valid: true}, Portfolio: enum(r.Portfolio), Ticker: r.Ticker,
		UnitsDelta: pgnum.FromDecimal(r.UnitsDelta), PricePln: pgnum.FromDecimal(r.PricePln), TotalPln: pgnum.FromDecimal(r.TotalPln),
		TransferredAt: pgtype.Timestamptz{Time: r.TransferredAt, Valid: true},
	})
	return err
}

// ListSubassetsIncludingDeleted: every positions row of p (ORDER BY ticker).
func (s *Queries) ListSubassetsIncludingDeleted(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error) {
	rows, err := s.q.ListPositionsByPortfolioIncludingDeleted(ctx, enum(p))
	if err != nil {
		return nil, err
	}
	return subassetsFromRows(rows)
}

// ListPurchaseLots: p's purchase lots.
func (s *Queries) ListPurchaseLots(ctx context.Context, p siblingcore.Portfolio) ([]rebalancing.PurchaseLot, error) {
	rows, err := s.q.ListPurchaseLotsByPortfolio(ctx, enum(p))
	if err != nil {
		return nil, err
	}
	out := make([]rebalancing.PurchaseLot, 0, len(rows))
	for _, r := range rows {
		lot, err := lotFromRow(siblingcore.Portfolio(r.Portfolio), r.ID, r.SessionID, r.PurchasedAt, r.Ticker, r.Units, r.PricePln, r.CostPln, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, lot)
	}
	return out, nil
}

func lotFromRow(p siblingcore.Portfolio, id int64, session pgtype.UUID, purchasedAt pgtype.Date, ticker string, units, price, cost pgtype.Numeric, createdAt pgtype.Timestamptz) (rebalancing.PurchaseLot, error) {
	lot := rebalancing.PurchaseLot{ID: id, Portfolio: p, SessionID: uuid.UUID(session.Bytes), PurchasedAt: purchasedAt.Time, Ticker: ticker, CreatedAt: timeOf(createdAt)}
	var err error
	if lot.Units, err = pgnum.ToDecimal(units); err != nil {
		return lot, fmt.Errorf("store: decode units for lot %d: %w", id, err)
	}
	if lot.PricePln, err = pgnum.ToDecimal(price); err != nil {
		return lot, fmt.Errorf("store: decode price_pln for lot %d: %w", id, err)
	}
	if lot.CostPln, err = pgnum.ToDecimal(cost); err != nil {
		return lot, fmt.Errorf("store: decode cost_pln for lot %d: %w", id, err)
	}
	return lot, nil
}

// InsertPurchaseLot writes one lot (ID and CreatedAt are ignored on input).
func (s *Queries) InsertPurchaseLot(ctx context.Context, lot rebalancing.PurchaseLot) error {
	_, err := s.q.InsertPurchaseLot(ctx, sqlcgen.InsertPurchaseLotParams{
		Portfolio: enum(lot.Portfolio), SessionID: pgtype.UUID{Bytes: lot.SessionID, Valid: true}, PurchasedAt: pgtype.Date{Time: lot.PurchasedAt, Valid: true},
		Ticker: lot.Ticker, Units: pgnum.FromDecimal(lot.Units), PricePln: pgnum.FromDecimal(lot.PricePln), CostPln: pgnum.FromDecimal(lot.CostPln),
	})
	return err
}
