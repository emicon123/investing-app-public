package historia

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore/refdata"
	"portfolio-c/backend-go/db/sqlcgen"
	"portfolio-c/backend-go/internal/book"
)

// InflationBase is this app's fixed inflation-cumulation month — 2026-07, NOT
// portfolioB's 2017-01. Pinned by TestInflationBase_Pin (a silent "unification" of the
// two bases changes every cumulative/annualized figure this endpoint serves).
var InflationBase = historiaquery.YearMonth{Year: 2026, Month: 7}

// Config is this app's historiaquery.Config: portfolio required.
func Config() historiaquery.Config {
	return historiaquery.Config{
		RequirePortfolio: true,
		ValidPortfolios:  book.PortfolioNames(),
		InflationBase:    InflationBase,
		EnumQualifier:    book.Config.PortfolioEnum,
	}
}

// Adapter satisfies historiaquery.Queries over this app's generated
// *sqlcgen.Queries — the only per-app persistence code the read path needs.
type Adapter struct {
	q   *sqlcgen.Queries
	ref *refdata.Store
}

// NewAdapter wraps a *sqlcgen.Queries.
func NewAdapter(db sqlcgen.DBTX) *Adapter { return &Adapter{q: sqlcgen.New(db), ref: refdata.New(db)} }

// ListRange returns rows for portfolio in [from, to], date-ascending.
func (a *Adapter) ListRange(ctx context.Context, portfolio string, from, to time.Time) ([]historiaquery.Row, error) {
	rows, err := a.q.ListHistoriaRangeByPortfolio(ctx, sqlcgen.ListHistoriaRangeByPortfolioParams{
		Portfolio:      sqlcgen.PortfolioEnum(portfolio),
		SnapshotDate:   pgnum.Date(from),
		SnapshotDate_2: pgnum.Date(to),
	})
	if err != nil {
		return nil, err
	}
	out := make([]historiaquery.Row, 0, len(rows))
	for _, r := range rows {
		decoded, err := decodeRow(r.ID, string(r.Portfolio), r.SnapshotDate, r.TotalDeposits, r.PortfolioValue, r.Xirr, r.PositionValues, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}

// ListAll returns every row for portfolio, date-ascending.
func (a *Adapter) ListAll(ctx context.Context, portfolio string) ([]historiaquery.Row, error) {
	rows, err := a.q.ListHistoriaAllByPortfolio(ctx, sqlcgen.PortfolioEnum(portfolio))
	if err != nil {
		return nil, err
	}
	out := make([]historiaquery.Row, 0, len(rows))
	for _, r := range rows {
		decoded, err := decodeRow(r.ID, string(r.Portfolio), r.SnapshotDate, r.TotalDeposits, r.PortfolioValue, r.Xirr, r.PositionValues, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}

// MomAsc is the CPI index, read through the shared reference-table adapter (task 180 wave 5).
func (a *Adapter) MomAsc(ctx context.Context) (map[historiaquery.YearMonth]decimal.Decimal, error) {
	return a.ref.MomAsc(ctx)
}

// DepositFlows returns (date, amountPln) pairs for portfolio, date-ascending —
// all deposits in this app are PLN, so no FX conversion (mirrors
// HistoriaService.enrich's per-portfolio flows).
func (a *Adapter) DepositFlows(ctx context.Context, portfolio string) ([]historiaquery.CashFlow, error) {
	rows, err := a.q.ListHistoriaDeposits(ctx, sqlcgen.PortfolioEnum(portfolio))
	if err != nil {
		return nil, err
	}
	out := make([]historiaquery.CashFlow, 0, len(rows))
	for _, r := range rows {
		if !r.DepositDate.Valid {
			continue
		}
		amount, err := pgnum.ToDecimal(r.Amount)
		if err != nil {
			continue
		}
		out = append(out, historiaquery.CashFlow{Date: r.DepositDate.Time, Amount: amount})
	}
	return out, nil
}

func decodeRow(id int64, portfolio string, snap pgtype.Date, deposits, value, xirr pgtype.Numeric, positionValues []byte, created pgtype.Timestamptz) (historiaquery.Row, error) {
	if !snap.Valid {
		return historiaquery.Row{}, fmt.Errorf("historia: row %d has NULL snapshot_date", id)
	}
	dep, err := pgnum.ToDecimal(deposits)
	if err != nil {
		return historiaquery.Row{}, fmt.Errorf("historia: decode total_deposits for row %d: %w", id, err)
	}
	val, err := pgnum.ToDecimal(value)
	if err != nil {
		return historiaquery.Row{}, fmt.Errorf("historia: decode portfolio_value for row %d: %w", id, err)
	}
	var xirrPtr *decimal.Decimal
	if xirr.Valid {
		x, err := pgnum.ToDecimal(xirr)
		if err != nil {
			return historiaquery.Row{}, fmt.Errorf("historia: decode xirr for row %d: %w", id, err)
		}
		xirrPtr = &x
	}
	positions, err := historiaquery.DecodePositionValues(positionValues)
	if err != nil {
		return historiaquery.Row{}, fmt.Errorf("historia: decode position_values for row %d: %w", id, err)
	}
	return historiaquery.Row{
		ID: id, SnapshotDate: snap.Time, Portfolio: portfolio,
		TotalDeposits: dep, PortfolioValue: val, Xirr: xirrPtr,
		PositionValues: positions, CreatedAt: created.Time,
	}, nil
}
