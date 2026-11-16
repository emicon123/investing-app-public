package store

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore"
	"portfolio-b/backend-go/db/sqlcgen"
	"portfolio-b/backend-go/internal/book"
)

// unitColumns is the bucket ↔ column mapping: book.Config's buckets in config order, each with
// its positions column name (for error texts) and its row accessor.
var unitColumns = []struct {
	bucket siblingcore.Bucket
	column string
	of     func(sqlcgen.Position) pgtype.Numeric
}{
	{book.IkeTata, "units_ike_tata", func(r sqlcgen.Position) pgtype.Numeric { return r.UnitsIkeTata }},
	{book.IkeMama, "units_ike_mama", func(r sqlcgen.Position) pgtype.Numeric { return r.UnitsIkeMama }},
	{book.IkzeTata, "units_ikze_tata", func(r sqlcgen.Position) pgtype.Numeric { return r.UnitsIkzeTata }},
	{book.IkzeMama, "units_ikze_mama", func(r sqlcgen.Position) pgtype.Numeric { return r.UnitsIkzeMama }},
	{book.Inne, "units_inne", func(r sqlcgen.Position) pgtype.Numeric { return r.UnitsInne }},
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
		ID: row.ID, Portfolio: siblingcore.WholeBook, Key: row.Key, Name: row.Name,
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
	amounts := make(map[siblingcore.Bucket]decimal.Decimal, len(unitColumns))
	for _, c := range unitColumns {
		v, err := pgnum.ToDecimal(c.of(row))
		if err != nil {
			return siblingcore.Subasset{}, fmt.Errorf("store: decode %s for %s: %w", c.column, row.Ticker, err)
		}
		amounts[c.bucket] = v
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
		ID: row.ID, AssetID: row.AssetID, Portfolio: siblingcore.WholeBook, IsPrimary: row.IsPrimary,
		Ticker: row.Ticker, Name: row.Name, Exchange: string(row.Exchange), AssetClass: string(row.AssetClass),
		Exposure: string(row.Exposure), PriceCurrency: string(row.PriceCurrency),
		Holding:     siblingcore.Holding{Units: siblingcore.NewUnits(amounts), TotalSpentPln: cost},
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
		ID: row.ID, Portfolio: siblingcore.WholeBook, DepositDate: row.DepositDate.Time, Amount: amount,
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
