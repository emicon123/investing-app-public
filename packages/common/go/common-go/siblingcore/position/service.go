package position

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/common-go/assetlifecycle"
	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgerr"
	"investing-app/common-go/positioncalc"
	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
)

// Service is the position service.
type Service struct {
	cfg    siblingcore.Config
	store  Store
	tx     func(ctx context.Context, fn func(Store) error) error
	prices siblingcore.PriceSource
}

// NewService wires the service. tx runs every write method (reads included) in one
// transaction; each app passes pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, store.Bind).
// A nil tx calls fn with store directly (unit tests).
func NewService[S Store](cfg siblingcore.Config, store S, tx siblingcore.TxFunc[S], prices siblingcore.PriceSource) *Service {
	run := func(ctx context.Context, fn func(Store) error) error { return fn(store) }
	if tx != nil {
		run = func(ctx context.Context, fn func(Store) error) error {
			return tx(ctx, func(q S) error { return fn(q) })
		}
	}
	return &Service{cfg: cfg, store: store, tx: run, prices: prices}
}

// book is one portfolio's assets + subassets, joined in Go (single-user scale: full reads).
type book struct {
	assets []siblingcore.Asset
	subs   []siblingcore.Subasset
	byID   map[int64]siblingcore.Asset
}

func load(ctx context.Context, q Store, p siblingcore.Portfolio) (book, error) {
	assets, err := q.ListAssets(ctx, p)
	if err != nil {
		return book{}, fmt.Errorf("position: list assets%s: %w", forPortfolio(p), err)
	}
	subs, err := q.ListSubassets(ctx, p)
	if err != nil {
		return book{}, fmt.Errorf("position: list%s: %w", forPortfolio(p), err)
	}
	byID := make(map[int64]siblingcore.Asset, len(assets))
	for _, a := range assets {
		byID[a.ID] = a
	}
	return book{assets: assets, subs: subs, byID: byID}, nil
}

// forPortfolio names a named portfolio in internal error texts (omitted for WholeBook).
func forPortfolio(p siblingcore.Portfolio) string {
	if p == siblingcore.WholeBook {
		return ""
	}
	return " " + string(p)
}

// List returns one AssetDTO per asset of p, sorted by the primary subasset's (sort_order,
// ticker) — an asset without a primary sorts as (0, key) — and paginated over assets.
func (s *Service) List(ctx context.Context, p siblingcore.Portfolio, page, size int) ([]AssetDTO, int64, error) {
	if err := s.cfg.CheckPortfolio("portfolio", p); err != nil {
		return nil, 0, err
	}
	if page < 0 {
		return nil, 0, httpx.NewValidationError("page", "page must not be negative")
	}
	if size < 1 {
		return nil, 0, httpx.NewValidationError("size", "size must be at least 1")
	}
	data, err := load(ctx, s.store, p)
	if err != nil {
		return nil, 0, err
	}
	ordered := append([]siblingcore.Asset(nil), data.assets...)
	sort.SliceStable(ordered, func(i, j int) bool {
		si, ti := sortKey(data.subs, ordered[i])
		sj, tj := sortKey(data.subs, ordered[j])
		if si != sj {
			return si < sj
		}
		return ti < tj
	})
	total := int64(len(ordered))
	start := min(page*size, len(ordered))
	end := min(start+size, len(ordered))

	prices, rates, err := s.priceMaps(ctx)
	if err != nil {
		return nil, 0, err
	}
	bookValue := totalValuePln(data, prices, rates)
	out := make([]AssetDTO, 0, end-start)
	for _, a := range ordered[start:end] {
		out = append(out, buildDTO(a, positioncalc.OrderSubassets(data.subs, a.ID), bookValue, prices, rates))
	}
	return out, total, nil
}

func sortKey(subs []siblingcore.Subasset, a siblingcore.Asset) (int, string) {
	if p := positioncalc.PrimaryOf(subs, a.ID); p != nil {
		return p.SortOrder, p.Ticker
	}
	return 0, a.Key
}

// Get resolves a subasset id to its asset's DTO.
func (s *Service) Get(ctx context.Context, subassetID int64) (AssetDTO, error) {
	sub, err := s.store.GetSubasset(ctx, subassetID)
	if err != nil {
		return AssetDTO{}, notFoundOrErr(subassetID, err)
	}
	data, err := load(ctx, s.store, sub.Portfolio)
	if err != nil {
		return AssetDTO{}, err
	}
	a, ok := data.byID[sub.AssetID]
	if !ok {
		return AssetDTO{}, fmt.Errorf("position: subasset %d references missing asset %d", subassetID, sub.AssetID)
	}
	return s.withComputed(ctx, data, a)
}

func (s *Service) withComputed(ctx context.Context, data book, a siblingcore.Asset) (AssetDTO, error) {
	prices, rates, err := s.priceMaps(ctx)
	if err != nil {
		return AssetDTO{}, err
	}
	return buildDTO(a, positioncalc.OrderSubassets(data.subs, a.ID), totalValuePln(data, prices, rates), prices, rates), nil
}

func (s *Service) priceMaps(ctx context.Context) (prices, rates map[string]decimal.Decimal, err error) {
	prices, err = s.prices.PricePlnByYahooTicker(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("position: load prices: %w", err)
	}
	rates, err = s.prices.AllRatesToPln(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("position: load fx rates: %w", err)
	}
	return prices, rates, nil
}

// totalValuePln is the book's value (non-primaries fold into their asset's primary); an
// unpriced row contributes zero, never an error.
func totalValuePln(data book, prices, rates map[string]decimal.Decimal) decimal.Decimal {
	return rebalancecalc.TotalPortfolioValuePln(rebalancecalc.ComputeValues(siblingcore.PositionInputs(data.assets, data.subs), rates, prices))
}

// Delete soft-deletes one subasset (ADR-109: history keeps its rows). A primary with live
// siblings is a 409; the last live subasset soft-deletes its whole asset.
func (s *Service) Delete(ctx context.Context, subassetID int64) error {
	return s.tx(ctx, func(q Store) error {
		sub, err := q.GetSubasset(ctx, subassetID)
		if err != nil {
			return notFoundOrErr(subassetID, err)
		}
		data, err := load(ctx, q, sub.Portfolio)
		if err != nil {
			return err
		}
		switch assetlifecycle.PlanSubassetDelete(sub.IsPrimary, len(positioncalc.OrderSubassets(data.subs, sub.AssetID))) {
		case assetlifecycle.DeleteConflict:
			return httpx.NewConflictError(httpx.CodeConflict, "asset still has other subassets — set a new primary first")
		case assetlifecycle.DeleteSoftDeleteAsset:
			if err := q.SoftDeleteAsset(ctx, sub.AssetID); err != nil {
				return fmt.Errorf("position: delete asset %d: %w", sub.AssetID, err)
			}
		default:
			if err := q.SoftDeleteSubasset(ctx, subassetID); err != nil {
				return fmt.Errorf("position: delete %d: %w", subassetID, err)
			}
		}
		return nil
	})
}

// DeleteAsset soft-deletes a whole asset and its live subassets (a deleted or unknown asset is a
// 404).
func (s *Service) DeleteAsset(ctx context.Context, assetID int64) error {
	return s.tx(ctx, func(q Store) error {
		if _, err := q.GetAsset(ctx, assetID); err != nil {
			return assetNotFoundOrErr(assetID, err)
		}
		if err := q.SoftDeleteAsset(ctx, assetID); err != nil {
			return fmt.Errorf("position: delete asset %d: %w", assetID, err)
		}
		return nil
	})
}

// UpdateAsset edits an asset's targets and name — the only writer of targets. No Σ == 100 guard
// here (moving weight from one asset to another takes two edits); set-primary keeps the tripwire.
func (s *Service) UpdateAsset(ctx context.Context, assetID int64, in UpdateAssetInput) (AssetDTO, error) {
	if err := decx.ValidatePercentRange("targetAllocationPct", in.TargetAllocationPct.Value); err != nil {
		return AssetDTO{}, err
	}
	if err := decx.ValidatePercentRange("targetAllocationPct", in.TargetAllocationPctRiskOff.Value); err != nil {
		return AssetDTO{}, err
	}
	var saved siblingcore.Asset
	err := s.tx(ctx, func(q Store) error {
		a, err := q.GetAsset(ctx, assetID)
		if err != nil {
			return assetNotFoundOrErr(assetID, err)
		}
		if in.TargetAllocationPct.Set {
			a.TargetAllocationPct = in.TargetAllocationPct.Value // nil (explicit null) clears
		}
		if in.TargetAllocationPctRiskOff.Set {
			a.TargetAllocationPctRiskOff = in.TargetAllocationPctRiskOff.Value
		}
		if in.Name != nil {
			a.Name = *in.Name
		}
		saved, err = q.UpdateAssetFields(ctx, a)
		if err != nil {
			return fmt.Errorf("position: update asset %d: %w", assetID, err)
		}
		return nil
	})
	if err != nil {
		return AssetDTO{}, err
	}
	data, err := load(ctx, s.store, saved.Portfolio)
	if err != nil {
		return AssetDTO{}, err
	}
	return s.withComputed(ctx, data, saved)
}

func notFoundOrErr(id int64, err error) error {
	return pgerr.NotFoundOr(err, fmt.Sprintf("Position with id %d not found", id), fmt.Sprintf("position: read %d", id))
}

func assetNotFoundOrErr(id int64, err error) error {
	return pgerr.NotFoundOr(err, fmt.Sprintf("Asset with id %d not found", id), fmt.Sprintf("position: read asset %d", id))
}

// isNoRows reports a missing row (pgx.ErrNoRows, possibly wrapped).
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
