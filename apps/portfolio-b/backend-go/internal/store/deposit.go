package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore"
	"portfolio-b/backend-go/db/sqlcgen"
)

// ListDeposits: newest first; the portfolio is ignored (one book).
func (s *Queries) ListDeposits(ctx context.Context, _ siblingcore.Portfolio, year *int, limit, offset int) ([]siblingcore.Deposit, error) {
	rows, err := s.q.ListDeposits(ctx, sqlcgen.ListDepositsParams{Year: year32(year), PageLimit: int32(limit), PageOffset: int32(offset)})
	if err != nil {
		return nil, err
	}
	return depositsFromRows(rows)
}

// CountDeposits counts the deposits ListDeposits pages over.
func (s *Queries) CountDeposits(ctx context.Context, _ siblingcore.Portfolio, year *int) (int64, error) {
	return s.q.CountDeposits(ctx, year32(year))
}

// ListDepositsAsc: every deposit, oldest first (deposit_date, id).
func (s *Queries) ListDepositsAsc(ctx context.Context, _ siblingcore.Portfolio) ([]siblingcore.Deposit, error) {
	rows, err := s.q.ListAllDepositsOrderByDateAsc(ctx)
	if err != nil {
		return nil, err
	}
	return depositsFromRows(rows)
}

// InsertDeposit writes d (ID, Portfolio and CreatedAt are ignored on input).
func (s *Queries) InsertDeposit(ctx context.Context, d siblingcore.Deposit) (siblingcore.Deposit, error) {
	row, err := s.q.InsertDeposit(ctx, sqlcgen.InsertDepositParams{
		DepositDate: pgtype.Date{Time: d.DepositDate, Valid: true}, Amount: pgnum.FromDecimal(d.Amount), Comment: d.Comment,
	})
	if err != nil {
		return siblingcore.Deposit{}, err
	}
	return depositFromRow(row)
}

// ListAssets: every live asset (ORDER BY id); the portfolio is ignored.
func (s *Queries) ListAssets(ctx context.Context, _ siblingcore.Portfolio) ([]siblingcore.Asset, error) {
	rows, err := s.q.ListAllAssets(ctx)
	if err != nil {
		return nil, err
	}
	return assetsFromRows(rows)
}

// ListSubassets: every live positions row (ORDER BY ticker); the portfolio is ignored.
func (s *Queries) ListSubassets(ctx context.Context, _ siblingcore.Portfolio) ([]siblingcore.Subasset, error) {
	rows, err := s.q.ListAllPositions(ctx)
	if err != nil {
		return nil, err
	}
	return subassetsFromRows(rows)
}
