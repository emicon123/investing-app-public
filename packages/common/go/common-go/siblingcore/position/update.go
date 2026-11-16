package position

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
)

// Update edits one subasset's own fields (nil = unchanged); targets live on the asset. The
// read-modify-write runs in one serialised transaction (ADR-111) so the full-row overwrite starts
// from the state the previous money write committed.
func (s *Service) Update(ctx context.Context, subassetID int64, in UpdateInput) (AssetDTO, error) {
	if err := s.validateUnitsPatch(in.Units); err != nil {
		return AssetDTO{}, err
	}
	if err := s.tx(ctx, func(q Store) error { return applyUpdate(ctx, q, subassetID, in) }); err != nil {
		return AssetDTO{}, err
	}
	return s.Get(ctx, subassetID)
}

// validateUnitsPatch: every patched bucket is configured and non-negative, checked in config
// order (the 400 names that bucket's request field).
func (s *Service) validateUnitsPatch(units map[siblingcore.Bucket]decimal.Decimal) error {
	for b := range units {
		if err := s.cfg.CheckBucket("units", b); err != nil {
			return err
		}
	}
	for _, spec := range s.cfg.Buckets {
		if v, ok := units[spec.Bucket]; ok && v.Sign() < 0 {
			return httpx.NewValidationError(spec.Field, "must not be negative")
		}
	}
	return nil
}

func applyUpdate(ctx context.Context, q Store, subassetID int64, in UpdateInput) error {
	cur, err := q.GetSubasset(ctx, subassetID)
	if err != nil {
		return notFoundOrErr(subassetID, err)
	}
	data, err := load(ctx, q, cur.Portfolio)
	if err != nil {
		return err
	}
	var p *siblingcore.Subasset
	for i := range data.subs {
		if data.subs[i].ID == subassetID {
			p = &data.subs[i]
			break
		}
	}
	if p == nil {
		return notFoundOrErr(subassetID, pgx.ErrNoRows)
	}

	// Re-home (task 156 §13): every check runs before the single write, so a rejected edit
	// writes nothing. Order: same-as-current -> primary guard -> existence -> portfolio.
	if in.AssetID != nil && *in.AssetID != p.AssetID {
		if p.IsPrimary {
			return httpx.NewConflictError(httpx.CodeConflict,
				"a primary subasset cannot be moved to another asset — set another primary first, or delete and re-create it under the target asset")
		}
		target, err := q.GetAsset(ctx, *in.AssetID)
		if err != nil {
			return assetNotFoundOrErr(*in.AssetID, err)
		}
		if target.Portfolio != p.Portfolio {
			return httpx.NewValidationError("assetId", "asset belongs to a different portfolio")
		}
		p.AssetID = *in.AssetID
	}

	for b, v := range in.Units {
		p.Holding.Units = p.Holding.Units.With(b, v)
	}
	if in.TotalSpentPln != nil {
		p.Holding.TotalSpentPln = in.TotalSpentPln
	}
	if in.AvgBuyPrice != nil {
		p.AvgBuyPrice = in.AvgBuyPrice
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.GroupName != nil {
		p.GroupName = in.GroupName
	}
	if in.AssetClass != nil {
		p.AssetClass = *in.AssetClass
	}
	if in.YahooTicker != nil {
		p.YahooTicker = in.YahooTicker
	}
	if in.SortOrder != nil {
		p.SortOrder = *in.SortOrder
	}
	if err := q.UpdateSubassetFields(ctx, *p); err != nil {
		return fmt.Errorf("position: update %d: %w", subassetID, err)
	}
	return nil
}
