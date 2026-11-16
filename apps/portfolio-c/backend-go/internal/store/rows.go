package store

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore"
	"portfolio-c/backend-go/db/sqlcgen"
)

// enum binds a siblingcore portfolio to the portfolio_enum column.
func enum(p siblingcore.Portfolio) sqlcgen.PortfolioEnum { return sqlcgen.PortfolioEnum(p) }

// unitsOf is the bucket ↔ column mapping: the single bucket is the units column.
func unitsOf(v decimal.Decimal) siblingcore.Units {
	return siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{siblingcore.SingleBucket: v})
}

func assetFromRow(row sqlcgen.Asset) (siblingcore.Asset, error) {
	target, err := pgnum.ToDecimalPtr(row.TargetAllocationPct)
	if err != nil {
		return siblingcore.Asset{}, fmt.Errorf("store: decode assets.target_allocation_pct for %s: %w", row.Key, err)
	}
	riskOff, err := pgnum.ToDecimalPtr(row.TargetAllocationPctRiskOff)
	if err != nil {
		return siblingcore.Asset{}, fmt.Errorf("store: decode assets.target_allocation_pct_risk_off for %s: %w", row.Key, err)
	}
	return siblingcore.Asset{
		ID: row.ID, Portfolio: siblingcore.Portfolio(row.Portfolio), Key: row.Key, Name: row.Name,
		Exposure: string(row.Exposure), AssetClass: string(row.AssetClass),
		TargetAllocationPct: target, TargetAllocationPctRiskOff: riskOff,
		Deleted: row.DeletedAt.Valid, CreatedAt: timeOf(row.CreatedAt), UpdatedAt: timeOf(row.UpdatedAt),
	}, nil
}

func assetsFromRows(rows []sqlcgen.Asset) ([]siblingcore.Asset, error) {
	out := make([]siblingcore.Asset, 0, len(rows))
	for _, r := range rows {
		a, err := assetFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func subassetFromRow(row sqlcgen.Position) (siblingcore.Subasset, error) {
	units, err := pgnum.ToDecimal(row.Units)
	if err != nil {
		return siblingcore.Subasset{}, fmt.Errorf("store: decode units for %s: %w", row.Ticker, err)
	}
	cost, err := pgnum.ToDecimalPtr(row.TotalSpentPln)
	if err != nil {
		return siblingcore.Subasset{}, fmt.Errorf("store: decode total_spent_pln for %s: %w", row.Ticker, err)
	}
	avg, err := pgnum.ToDecimalPtr(row.AvgBuyPrice)
	if err != nil {
		return siblingcore.Subasset{}, fmt.Errorf("store: decode avg_buy_price for %s: %w", row.Ticker, err)
	}
	return siblingcore.Subasset{
		ID: row.ID, AssetID: row.AssetID, Portfolio: siblingcore.Portfolio(row.Portfolio), IsPrimary: row.IsPrimary,
		Ticker: row.Ticker, Name: row.Name, Exchange: string(row.Exchange), AssetClass: string(row.AssetClass),
		Exposure: string(row.Exposure), PriceCurrency: string(row.PriceCurrency),
		Holding:     siblingcore.Holding{Units: unitsOf(units), TotalSpentPln: cost},
		AvgBuyPrice: avg, GroupName: row.GroupName, YahooTicker: row.YahooTicker, SortOrder: int(row.SortOrder),
		Deleted: row.DeletedAt.Valid, CreatedAt: timeOf(row.CreatedAt), UpdatedAt: timeOf(row.UpdatedAt),
	}, nil
}

func subassetsFromRows(rows []sqlcgen.Position) ([]siblingcore.Subasset, error) {
	out := make([]siblingcore.Subasset, 0, len(rows))
	for _, r := range rows {
		s, err := subassetFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func depositFromRow(row sqlcgen.Deposit) (siblingcore.Deposit, error) {
	amount, err := pgnum.ToDecimal(row.Amount)
	if err != nil {
		return siblingcore.Deposit{}, fmt.Errorf("store: decode deposit amount: %w", err)
	}
	return siblingcore.Deposit{
		ID: row.ID, Portfolio: siblingcore.Portfolio(row.Portfolio), DepositDate: row.DepositDate.Time, Amount: amount,
		Comment: row.Comment, CreatedAt: timeOf(row.CreatedAt),
	}, nil
}

func depositsFromRows(rows []sqlcgen.Deposit) ([]siblingcore.Deposit, error) {
	out := make([]siblingcore.Deposit, 0, len(rows))
	for _, r := range rows {
		d, err := depositFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func timeOf(t pgtype.Timestamptz) time.Time {
	if t.Valid {
		return t.Time
	}
	return time.Time{}
}

func year32(year *int) *int32 {
	if year == nil {
		return nil
	}
	v := int32(*year)
	return &v
}
