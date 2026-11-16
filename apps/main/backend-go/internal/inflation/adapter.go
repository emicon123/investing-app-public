package inflation

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/inflationsync"
	"investing-app/common-go/pgnum"
)

// Adapter satisfies inflationsync.Queries over main's generated *sqlcgen.Queries, so the
// GUS sync and its scheduler are the shared kernel (task 178). The `inflation` table shape
// is the siblings' (INTEGER year/month, NUMERIC(7,4) cpi_value), so this file matches
// portfolioB's and portfolioC's adapter after module-path normalization; any divergence here is a
// schema-drift finding, not a tuning commit.
type Adapter struct{ q *sqlcgen.Queries }

// NewAdapter wraps a (possibly tx-scoped, via WithTx) *sqlcgen.Queries.
func NewAdapter(q *sqlcgen.Queries) *Adapter { return &Adapter{q: q} }

// GetCpi reads one month's CPI, mapping sqlc's pgx.ErrNoRows to the kernel's
// inflationsync.ErrNotFound sentinel.
func (a *Adapter) GetCpi(ctx context.Context, year, month int) (decimal.Decimal, error) {
	row, err := a.q.GetInflationByYearMonth(ctx, sqlcgen.GetInflationByYearMonthParams{
		Year: int32(year), Month: int32(month),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, inflationsync.ErrNotFound
		}
		return decimal.Zero, err
	}
	return pgnum.ToDecimal(row.CpiValue)
}

// CreateEntry inserts one month's CPI.
func (a *Adapter) CreateEntry(ctx context.Context, year, month int, cpi decimal.Decimal) error {
	_, err := a.q.CreateInflationEntry(ctx, sqlcgen.CreateInflationEntryParams{
		Year: int32(year), Month: int32(month), CpiValue: pgnum.FromDecimal(cpi),
	})
	return err
}

// UpdateCpi overwrites one month's CPI.
func (a *Adapter) UpdateCpi(ctx context.Context, year, month int, cpi decimal.Decimal) error {
	_, err := a.q.UpdateInflationCpi(ctx, sqlcgen.UpdateInflationCpiParams{
		Year: int32(year), Month: int32(month), CpiValue: pgnum.FromDecimal(cpi),
	})
	return err
}

// ListAsc returns every (year, month) pair oldest first, for the staleness check.
func (a *Adapter) ListAsc(ctx context.Context) ([]inflationsync.YearMonth, error) {
	rows, err := a.q.ListInflationAsc(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]inflationsync.YearMonth, 0, len(rows))
	for _, r := range rows {
		out = append(out, inflationsync.YearMonth{Year: int(r.Year), Month: int(r.Month)})
	}
	return out, nil
}
