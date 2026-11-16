package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore"
	"portfolio-c/backend-go/db/sqlcgen"
)

// ListDeposits: p's deposits, newest first.
func (s *Queries) ListDeposits(ctx context.Context, p siblingcore.Portfolio, year *int, limit, offset int) ([]siblingcore.Deposit, error) {
	rows, err := s.q.ListDeposits(ctx, sqlcgen.ListDepositsParams{Portfolio: enum(p), Year: year32(year), PageLimit: int32(limit), PageOffset: int32(offset)})
	if err != nil {
		return nil, err
	}
	return depositsFromRows(rows)
}

// CountDeposits counts the deposits ListDeposits pages over.
func (s *Queries) CountDeposits(ctx context.Context, p siblingcore.Portfolio, year *int) (int64, error) {
	return s.q.CountDeposits(ctx, sqlcgen.CountDepositsParams{Portfolio: enum(p), Year: year32(year)})
}

// ListDepositsAsc: p's deposits, oldest first.
func (s *Queries) ListDepositsAsc(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Deposit, error) {
	rows, err := s.q.ListDepositsByPortfolioOrderByDateAsc(ctx, enum(p))
	if err != nil {
		return nil, err
	}
	return depositsFromRows(rows)
}

// InsertDeposit writes d into d.Portfolio (ID and CreatedAt are ignored on input).
func (s *Queries) InsertDeposit(ctx context.Context, d siblingcore.Deposit) (siblingcore.Deposit, error) {
	row, err := s.q.InsertDeposit(ctx, sqlcgen.InsertDepositParams{
		Portfolio: enum(d.Portfolio), DepositDate: pgtype.Date{Time: d.DepositDate, Valid: true},
		Amount: pgnum.FromDecimal(d.Amount), Comment: d.Comment,
	})
	if err != nil {
		return siblingcore.Deposit{}, err
	}
	return depositFromRow(row)
}

// ListAssets: p's live assets (ORDER BY id).
func (s *Queries) ListAssets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Asset, error) {
	rows, err := s.q.ListAssetsByPortfolio(ctx, enum(p))
	if err != nil {
		return nil, err
	}
	return assetsFromRows(rows)
}

// ListSubassets: p's live positions rows (ORDER BY ticker).
func (s *Queries) ListSubassets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error) {
	rows, err := s.q.ListPositionsByPortfolio(ctx, enum(p))
	if err != nil {
		return nil, err
	}
	return subassetsFromRows(rows)
}
