package store

import (
	"context"

	"github.com/shopspring/decimal"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/position"
	"portfolio-b/backend-go/db/sqlcgen"
	"portfolio-b/backend-go/internal/book"
)

var _ position.Store = (*Queries)(nil)

func (s *Queries) GetAsset(ctx context.Context, id int64) (siblingcore.Asset, error) {
	row, err := s.q.GetAssetByID(ctx, id)
	if err != nil {
		return siblingcore.Asset{}, err
	}
	return assetFromRow(row)
}

func (s *Queries) GetAssetAnyState(ctx context.Context, id int64) (siblingcore.Asset, error) {
	row, err := s.q.GetAssetByIDAnyState(ctx, id)
	if err != nil {
		return siblingcore.Asset{}, err
	}
	return assetFromRow(row)
}

func (s *Queries) GetSubasset(ctx context.Context, id int64) (siblingcore.Subasset, error) {
	row, err := s.q.GetPositionByID(ctx, id)
	if err != nil {
		return siblingcore.Subasset{}, err
	}
	return subassetFromRow(row)
}

// FindAssetByKeyAnyState: assets.key is unique book-wide; the portfolio is ignored.
func (s *Queries) FindAssetByKeyAnyState(ctx context.Context, _ siblingcore.Portfolio, key string) (siblingcore.Asset, error) {
	row, err := s.q.GetAssetByKeyAnyState(ctx, key)
	if err != nil {
		return siblingcore.Asset{}, err
	}
	return assetFromRow(row)
}

// FindSubassetByTickerAnyState: positions.ticker is unique book-wide; the portfolio is ignored.
func (s *Queries) FindSubassetByTickerAnyState(ctx context.Context, _ siblingcore.Portfolio, ticker string) (siblingcore.Subasset, error) {
	row, err := s.q.GetPositionByTickerAnyState(ctx, ticker)
	if err != nil {
		return siblingcore.Subasset{}, err
	}
	return subassetFromRow(row)
}

func (s *Queries) InsertAsset(ctx context.Context, a siblingcore.Asset) (siblingcore.Asset, error) {
	row, err := s.q.InsertAsset(ctx, sqlcgen.InsertAssetParams{
		Key: a.Key, Name: a.Name, Exposure: sqlcgen.ExposureEnum(a.Exposure), AssetClass: sqlcgen.AssetClassEnum(a.AssetClass),
		TargetAllocationPct:        pgnum.FromDecimalPtr(a.TargetAllocationPct),
		TargetAllocationPctRiskOff: pgnum.FromDecimalPtr(a.TargetAllocationPctRiskOff),
	})
	if err != nil {
		return siblingcore.Asset{}, err
	}
	return assetFromRow(row)
}

func (s *Queries) InsertSubasset(ctx context.Context, sub siblingcore.Subasset) (siblingcore.Subasset, error) {
	row, err := s.q.InsertPosition(ctx, insertPositionParams(sub))
	if err != nil {
		return siblingcore.Subasset{}, err
	}
	return subassetFromRow(row)
}

// insertPositionParams: a fresh row holds zero units in every account column.
func insertPositionParams(sub siblingcore.Subasset) sqlcgen.InsertPositionParams {
	zero := pgnum.FromDecimal(decimal.Zero)
	return sqlcgen.InsertPositionParams{
		Ticker: sub.Ticker, Name: sub.Name, Exchange: sqlcgen.ExchangeEnum(sub.Exchange),
		AssetClass: sqlcgen.AssetClassEnum(sub.AssetClass), Exposure: sqlcgen.ExposureEnum(sub.Exposure),
		PriceCurrency: sqlcgen.CurrencyEnum(sub.PriceCurrency),
		UnitsIkeTata:  zero, UnitsIkeMama: zero, UnitsIkzeTata: zero, UnitsIkzeMama: zero, UnitsInne: zero,
		AssetID: sub.AssetID, IsPrimary: sub.IsPrimary, YahooTicker: sub.YahooTicker, SortOrder: int32(sub.SortOrder),
	}
}

func (s *Queries) RestoreAsset(ctx context.Context, r position.AssetRestore) (siblingcore.Asset, error) {
	row, err := s.q.RestoreAsset(ctx, sqlcgen.RestoreAssetParams{
		ID: r.ID, Name: r.Name, Exposure: sqlcgen.ExposureEnum(r.Exposure), AssetClass: sqlcgen.AssetClassEnum(r.AssetClass),
		TargetAllocationPct:        pgnum.FromDecimalPtr(r.TargetAllocationPct),
		TargetAllocationPctRiskOff: pgnum.FromDecimalPtr(r.TargetAllocationPctRiskOff),
	})
	if err != nil {
		return siblingcore.Asset{}, err
	}
	return assetFromRow(row)
}

func (s *Queries) RestoreSubasset(ctx context.Context, r position.SubassetRestore) (siblingcore.Subasset, error) {
	p := sqlcgen.RestorePositionParams{
		ID: r.ID, AssetID: r.AssetID, IsPrimary: r.IsPrimary, Name: r.Name,
		AssetClass: sqlcgen.AssetClassEnum(r.AssetClass), PriceCurrency: sqlcgen.CurrencyEnum(r.PriceCurrency),
		YahooTicker: r.YahooTicker,
	}
	if r.Exposure != nil {
		e := sqlcgen.ExposureEnum(*r.Exposure)
		p.Exposure = &e
	}
	if r.SortOrder != nil {
		so := int32(*r.SortOrder)
		p.SortOrder = &so
	}
	row, err := s.q.RestorePosition(ctx, p)
	if err != nil {
		return siblingcore.Subasset{}, err
	}
	return subassetFromRow(row)
}

func (s *Queries) UpdateSubassetFields(ctx context.Context, sub siblingcore.Subasset) error {
	_, err := s.q.UpdatePositionFields(ctx, updatePositionFieldsParams(sub))
	return err
}

// updatePositionFieldsParams is the full-row overwrite: each account column from its bucket.
func updatePositionFieldsParams(sub siblingcore.Subasset) sqlcgen.UpdatePositionFieldsParams {
	u := sub.Holding.Units
	return sqlcgen.UpdatePositionFieldsParams{
		ID:           sub.ID,
		UnitsIkeTata: pgnum.FromDecimal(u.Of(book.IkeTata)), UnitsIkeMama: pgnum.FromDecimal(u.Of(book.IkeMama)),
		UnitsIkzeTata: pgnum.FromDecimal(u.Of(book.IkzeTata)), UnitsIkzeMama: pgnum.FromDecimal(u.Of(book.IkzeMama)),
		UnitsInne:     pgnum.FromDecimal(u.Of(book.Inne)),
		TotalSpentPln: pgnum.FromDecimalPtr(sub.Holding.TotalSpentPln), AvgBuyPrice: pgnum.FromDecimalPtr(sub.AvgBuyPrice),
		Name: sub.Name, GroupName: sub.GroupName, AssetClass: sqlcgen.AssetClassEnum(sub.AssetClass),
		YahooTicker: sub.YahooTicker, SortOrder: int32(sub.SortOrder), AssetID: sub.AssetID,
	}
}

func (s *Queries) UpdateAssetFields(ctx context.Context, a siblingcore.Asset) (siblingcore.Asset, error) {
	row, err := s.q.UpdateAssetFields(ctx, sqlcgen.UpdateAssetFieldsParams{
		ID: a.ID, Name: a.Name,
		TargetAllocationPct:        pgnum.FromDecimalPtr(a.TargetAllocationPct),
		TargetAllocationPctRiskOff: pgnum.FromDecimalPtr(a.TargetAllocationPctRiskOff),
	})
	if err != nil {
		return siblingcore.Asset{}, err
	}
	return assetFromRow(row)
}

func (s *Queries) ClearAssetPrimary(ctx context.Context, assetID int64) error {
	return s.q.ClearAssetPrimary(ctx, assetID)
}

// SetSubassetPrimary returns pgx.ErrNoRows when subassetID is not a live subasset of assetID.
func (s *Queries) SetSubassetPrimary(ctx context.Context, assetID, subassetID int64) error {
	_, err := s.q.SetSubassetPrimary(ctx, sqlcgen.SetSubassetPrimaryParams{ID: subassetID, AssetID: assetID})
	return err
}

func (s *Queries) SoftDeleteSubasset(ctx context.Context, id int64) error {
	return s.q.SoftDeletePosition(ctx, id)
}

func (s *Queries) SoftDeleteAsset(ctx context.Context, assetID int64) error {
	return s.q.SoftDeleteAsset(ctx, assetID)
}
