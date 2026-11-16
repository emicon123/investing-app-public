package position

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/rebalancingcalc"
	"investing-app/common-go/assetlifecycle"
	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgerr"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/positioncalc"
)

var hundred = decimal.NewFromInt(100)

// targetSumTolerance is the same tolerance the Step-6 rebalancing invariant uses (formerly
// PROMOTE_INVARIANT_TOLERANCE, ADR-088). Used ONLY by the set-primary Σ targets == 100
// tripwire (task 156) — plain target edits are range-validated only.
var targetSumTolerance = decimal.RequireFromString("0.01")

// Service is the position service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries  Queries
	transact TransactFunc
	prices   PriceSource
}

// NewService wires queries + transaction runner + price source (explicit constructor wiring,
// ADR-094 §2 — no DI framework).
func NewService(queries Queries, transact TransactFunc, prices PriceSource) *Service {
	if transact == nil {
		transact = func(ctx context.Context, fn func(Queries) error) error { return fn(queries) }
	}
	return &Service{queries: queries, transact: transact, prices: prices}
}

// allData is one list scope's assets + subassets, joined in Go.
type allData struct {
	assets []assetInfo
	subs   []decoded
	byID   map[int64]assetInfo
}

// loadAll reads every asset + subasset row (both portfolios — callers filter in Go, the
// established single-user-app convention).
func (s *Service) loadAll(ctx context.Context, q Queries) (allData, error) {
	assetRows, err := q.ListAllAssets(ctx)
	if err != nil {
		return allData{}, fmt.Errorf("position: list assets: %w", err)
	}
	assets, err := decodeAssets(assetRows)
	if err != nil {
		return allData{}, err
	}
	posRows, err := q.ListAllPositions(ctx)
	if err != nil {
		return allData{}, fmt.Errorf("position: list: %w", err)
	}
	subs, err := decodeRows(posRows, assets)
	if err != nil {
		return allData{}, err
	}
	byID := make(map[int64]assetInfo, len(assets))
	for _, a := range assets {
		byID[a.ID] = a
	}
	return allData{assets: assets, subs: subs, byID: byID}, nil
}

// List returns the assets, regrouped by asset (task 156): one AssetDTO per asset, subassets nested,
// primary first. NOTE (a legacy quirk kept as-is from the Java original, not fixed — changing it is
// a behaviour change that needs a decision): when portfolioFilter is empty (both portfolios listed
// together), the targetOverrides computation is keyed off the FILTER parameter, not each row's own
// portfolio — so MAIN assets shown in an unfiltered listing get NO macro de-risk override applied
// even though the SAME assets would get it when the request explicitly filters `?portfolio=MAIN`.
func (s *Service) List(ctx context.Context, portfolioFilter string, page, size int) ([]AssetDTO, int64, error) {
	if page < 0 {
		return nil, 0, httpx.NewValidationError("page", "page must not be negative")
	}
	if size < 1 {
		return nil, 0, httpx.NewValidationError("size", "size must be at least 1")
	}
	data, err := s.loadAll(ctx, s.queries)
	if err != nil {
		return nil, 0, err
	}

	var filtered []assetInfo
	for _, a := range data.assets {
		if portfolioFilter == "" || a.Portfolio == portfolioFilter {
			filtered = append(filtered, a)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return primaryTicker(data.subs, filtered[i]) < primaryTicker(data.subs, filtered[j])
	})
	total := int64(len(filtered))

	start := min(page*size, len(filtered))
	end := min(start+size, len(filtered))
	pageAssets := filtered[start:end]

	portfolioTotals := map[string]decimal.Decimal{}
	portfoliosToCompute := []string{rebalancingcalc.PortfolioMAIN, rebalancingcalc.PortfolioRETIREMENT}
	if portfolioFilter != "" {
		portfoliosToCompute = []string{portfolioFilter}
	}
	for _, p := range portfoliosToCompute {
		if total, ok := rebalancingcalc.TryTotalPortfolioValuePln(ctx, s.queries, s.prices, p); ok {
			portfolioTotals[p] = total
		}
	}

	// Legacy quirk kept as-is (see doc comment above): targetOverrides is keyed off the FILTER, not
	// each asset's own portfolio.
	var targetOverrides map[string]decimal.Decimal
	if portfolioFilter == rebalancingcalc.PortfolioMAIN {
		targetOverrides, err = rebalancingcalc.LoadMainTargetOverrides(ctx, s.queries)
		if err != nil {
			return nil, 0, err
		}
	}

	pricePlnMap, err := s.prices.PricePlnByYahooTicker(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("position: load prices: %w", err)
	}

	out := make([]AssetDTO, 0, len(pageAssets))
	for _, a := range pageAssets {
		overridesForAsset := map[string]decimal.Decimal{}
		if a.Portfolio == rebalancingcalc.PortfolioMAIN {
			overridesForAsset = targetOverrides
		}
		dto, err := s.buildDTO(ctx, a, positioncalc.OrderSubassets(data.subs, a.ID), portfolioTotals, overridesForAsset, pricePlnMap)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, dto)
	}
	return out, total, nil
}

// primaryTicker resolves an asset's visible sort/display ticker: its primary subasset's
// ticker, falling back to the asset key (never on committed data — every asset has exactly
// one primary).
func primaryTicker(subs []decoded, a assetInfo) string {
	if p := positioncalc.PrimaryOf(subs, a.ID); p != nil {
		return p.Ticker
	}
	return a.Key
}

// Get resolves a subasset id to its parent asset's DTO (row ids are stable across the task
// 156 migration — asset_id was added, row ids unchanged). Direct port of getPosition's
// contract, regrouped: the response is the whole asset, not the single row.
func (s *Service) Get(ctx context.Context, id int64) (AssetDTO, error) {
	row, err := s.queries.GetPositionByID(ctx, id)
	if err != nil {
		return AssetDTO{}, notFoundOrErr(id, err)
	}
	data, err := s.loadAll(ctx, s.queries)
	if err != nil {
		return AssetDTO{}, err
	}
	a, ok := data.byID[row.AssetID]
	if !ok {
		return AssetDTO{}, fmt.Errorf("position: subasset %d references missing asset %d", id, row.AssetID)
	}
	return s.getWithComputed(ctx, data, a)
}

func (s *Service) getWithComputed(ctx context.Context, data allData, a assetInfo) (AssetDTO, error) {
	portfolioTotals := map[string]decimal.Decimal{}
	if total, ok := rebalancingcalc.TryTotalPortfolioValuePln(ctx, s.queries, s.prices, a.Portfolio); ok {
		portfolioTotals[a.Portfolio] = total
	}
	var targetOverrides map[string]decimal.Decimal
	var err error
	if a.Portfolio == rebalancingcalc.PortfolioMAIN {
		targetOverrides, err = rebalancingcalc.LoadMainTargetOverrides(ctx, s.queries)
		if err != nil {
			return AssetDTO{}, err
		}
	}
	pricePlnMap, err := s.prices.PricePlnByYahooTicker(ctx)
	if err != nil {
		return AssetDTO{}, fmt.Errorf("position: load prices: %w", err)
	}
	return s.buildDTO(ctx, a, positioncalc.OrderSubassets(data.subs, a.ID), portfolioTotals, targetOverrides, pricePlnMap)
}

// Delete soft-deletes one subasset row (task 173, ADR-109: rows are never hard-deleted, so
// history never blocks or loses its link). Primary-with-live-siblings is a 409 ("set a new
// primary first" — no silent leadership change, cf. ADR-045's manual-judgment philosophy);
// the last live subasset soft-deletes the whole asset in the same statement.
func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.transact(ctx, func(q Queries) error {
		row, err := q.GetPositionByID(ctx, id)
		if err != nil {
			return notFoundOrErr(id, err)
		}
		data, err := s.loadAll(ctx, q)
		if err != nil {
			return err
		}
		switch assetlifecycle.PlanSubassetDelete(row.IsPrimary, len(positioncalc.OrderSubassets(data.subs, row.AssetID))) {
		case assetlifecycle.DeleteConflict:
			return httpx.NewConflictError(httpx.CodeConflict, "asset still has other subassets — set a new primary first")
		case assetlifecycle.DeleteSoftDeleteAsset:
			if err := q.SoftDeleteAsset(ctx, row.AssetID); err != nil {
				return fmt.Errorf("position: delete asset %d: %w", row.AssetID, err)
			}
		default:
			if err := q.SoftDeletePosition(ctx, id); err != nil {
				return fmt.Errorf("position: delete %d: %w", id, err)
			}
		}
		return nil
	})
}

// Create adds one subasset row: either under an existing asset (assetId set) or under a
// brand-new asset (assetId omitted + inline newAsset carrying the targets — targets never
// live on the subasset row). Row-level defaults mirror the old createPosition path
// (purchaseCurrency->PLN, companyBacked derived from assetClass when omitted).
//
// Task 173 (ADR-109, design §6): re-adding a soft-deleted natural key RESTORES the old row
// (same id, units, cost, history) instead of inserting; the shared assetlifecycle kernel
// decides, this method looks up and executes. On restore the required request fields
// overwrite and the optional ones keep their old value when omitted (raw request pointers).
func (s *Service) Create(ctx context.Context, in CreateInput) (AssetDTO, error) {
	isPrimary, err := validateCreate(in)
	if err != nil {
		return AssetDTO{}, err
	}

	var createdAssetID int64
	err = s.transact(ctx, func(q Queries) error {
		st, target, err := s.loadCreateState(ctx, q, in, isPrimary)
		if err != nil {
			return err
		}
		plan, err := assetlifecycle.PlanCreate(st)
		if err != nil {
			return s.createPlanError(in, err)
		}
		createdAssetID, err = s.applyCreate(ctx, q, in, target, plan)
		return err
	})
	if err != nil {
		return AssetDTO{}, err
	}
	// Re-read the asset for the response — mirrors the old create path's "base DTO,
	// nothing computed yet" contract.
	data, err := s.loadAll(ctx, s.queries)
	if err != nil {
		return AssetDTO{}, err
	}
	a, ok := data.byID[createdAssetID]
	if !ok {
		return AssetDTO{}, fmt.Errorf("position: created asset %d not found after create", createdAssetID)
	}
	return toDTOBase(a, positioncalc.OrderSubassets(data.subs, a.ID)), nil
}

// validateCreate checks the request shape (no database, so it answers before the transaction
// opens and takes the money-write lock) and returns whether the new subasset is primary: a new
// asset's first subasset always is, an attached one only when asked.
func validateCreate(in CreateInput) (isPrimary bool, err error) {
	if in.AssetID == nil && in.NewAsset == nil {
		return false, httpx.NewValidationError("assetId", "either assetId or newAsset is required")
	}
	if in.AssetID != nil && in.NewAsset != nil {
		return false, httpx.NewValidationError("newAsset", "newAsset must be omitted when assetId is set")
	}
	if in.NewAsset != nil {
		if err := validateNewAsset(in.NewAsset); err != nil {
			return false, err
		}
	}
	isPrimary = in.AssetID == nil // a new asset's first subasset is trivially primary
	if in.IsPrimary != nil {
		if in.AssetID == nil && !*in.IsPrimary {
			return false, httpx.NewValidationError("isPrimary", "a new asset's first subasset is always primary")
		}
		isPrimary = *in.IsPrimary
	}
	return isPrimary, nil
}

func validateNewAsset(n *NewAssetInput) error {
	if err := decx.ValidatePercentRange("targetAllocationPct", n.TargetAllocationPct); err != nil {
		return err
	}
	if err := decx.ValidatePercentRange("targetAllocationPctRiskOff", n.TargetAllocationPctRiskOff); err != nil {
		return err
	}
	if n.Name == "" || n.Exposure == "" || n.AssetClass == "" {
		return httpx.NewValidationError("newAsset", "name, exposure and assetClass are required")
	}
	return nil
}

// loadCreateState is the read phase inside the transaction: the natural-key row in any state,
// then either the asset to attach to (returned as target; the zero Asset when absent, which the
// kernel answers with ErrAssetNotFound) or the asset already holding the new key.
func (s *Service) loadCreateState(ctx context.Context, q Queries, in CreateInput, isPrimary bool) (assetlifecycle.CreateInput, sqlcgen.Asset, error) {
	st := assetlifecycle.CreateInput{NewAsset: in.NewAsset != nil, WantPrimary: isPrimary}
	if err := s.loadPositionState(ctx, q, in, &st); err != nil {
		return st, sqlcgen.Asset{}, err
	}
	if in.AssetID == nil {
		return st, sqlcgen.Asset{}, s.loadKeyAssetState(ctx, q, in, &st)
	}
	target, err := s.loadTargetAssetState(ctx, q, in, isPrimary, &st)
	return st, target, err
}

// loadPositionState records the row (live or soft-deleted) the request's natural key points at,
// with its asset.
func (s *Service) loadPositionState(ctx context.Context, q Queries, in CreateInput, st *assetlifecycle.CreateInput) error {
	pos, err := q.GetPositionByNaturalKeyAnyState(ctx, sqlcgen.GetPositionByNaturalKeyAnyStateParams{
		Ticker: in.Ticker, Exchange: sqlcgen.ExchangeEnum(in.Exchange), Portfolio: sqlcgen.PortfolioEnum(in.Portfolio),
	})
	switch {
	case err == nil:
		st.Position = &assetlifecycle.RowState{ID: pos.ID, AssetID: pos.AssetID, Deleted: pos.DeletedAt.Valid, IsPrimary: pos.IsPrimary}
		pa, err := q.GetAssetByIDAnyState(ctx, pos.AssetID)
		if err != nil {
			return fmt.Errorf("position: read asset %d: %w", pos.AssetID, err)
		}
		st.PositionAsset = &assetlifecycle.AssetState{ID: pa.ID, Deleted: pa.DeletedAt.Valid}
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("position: read %s: %w", in.Ticker, err)
	}
	return nil
}

// loadTargetAssetState reads the live asset a subasset is attached to (the portfolio must match;
// whether it already has a live primary is only looked up when a primary is wanted).
func (s *Service) loadTargetAssetState(ctx context.Context, q Queries, in CreateInput, isPrimary bool, st *assetlifecycle.CreateInput) (sqlcgen.Asset, error) {
	target, err := q.GetAssetByID(ctx, *in.AssetID)
	switch {
	case err == nil:
		if string(target.Portfolio) != in.Portfolio {
			return target, httpx.NewValidationError("portfolio", "must match the asset's portfolio")
		}
		hasPrimary := false
		if isPrimary {
			data, err := s.loadAll(ctx, q)
			if err != nil {
				return target, err
			}
			hasPrimary = positioncalc.PrimaryOf(data.subs, target.ID) != nil
		}
		st.TargetAsset = &assetlifecycle.AssetState{ID: target.ID, HasLivePrimary: hasPrimary}
	case !errors.Is(err, pgx.ErrNoRows):
		return target, fmt.Errorf("position: read asset %d: %w", *in.AssetID, err)
	}
	return target, nil
}

// loadKeyAssetState records the asset (live or soft-deleted) already holding the new asset's key.
func (s *Service) loadKeyAssetState(ctx context.Context, q Queries, in CreateInput, st *assetlifecycle.CreateInput) error {
	ka, err := q.GetAssetByKeyAnyState(ctx, sqlcgen.GetAssetByKeyAnyStateParams{Key: in.Ticker, Portfolio: sqlcgen.PortfolioEnum(in.Portfolio)})
	switch {
	case err == nil:
		st.KeyAsset = &assetlifecycle.AssetState{ID: ka.ID, Deleted: ka.DeletedAt.Valid}
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("position: read asset key %s: %w", in.Ticker, err)
	}
	return nil
}

// applyCreate executes the kernel's plan in its order — the asset (insert or restore), the
// primary-flag clear, the subasset (restore or insert) — and returns the asset id.
func (s *Service) applyCreate(ctx context.Context, q Queries, in CreateInput, target sqlcgen.Asset, plan assetlifecycle.CreatePlan) (int64, error) {
	assetID, err := s.applyCreateAsset(ctx, q, in, plan)
	if err != nil {
		return 0, err
	}
	if plan.ClearPrimaries {
		if err := q.ClearAssetPrimary(ctx, assetID); err != nil {
			return 0, fmt.Errorf("position: clear primary of asset %d: %w", assetID, err)
		}
	}
	if plan.Subasset == assetlifecycle.SubassetRestore {
		if _, err := q.RestorePosition(ctx, restorePositionParams(in, plan, assetID)); err != nil {
			return 0, fmt.Errorf("position: restore %d: %w", plan.SubassetID, err)
		}
		return assetID, nil
	}
	return assetID, s.insertSubasset(ctx, q, in, target, assetID, plan.IsPrimary)
}

// applyCreateAsset inserts or restores the asset the plan names (neither when attaching) and
// returns its id.
func (s *Service) applyCreateAsset(ctx context.Context, q Queries, in CreateInput, plan assetlifecycle.CreatePlan) (int64, error) {
	switch plan.Asset {
	case assetlifecycle.AssetInsert:
		created, err := q.InsertAsset(ctx, sqlcgen.InsertAssetParams{
			Portfolio: sqlcgen.PortfolioEnum(in.Portfolio), Key: in.Ticker, Name: in.NewAsset.Name,
			Exposure: sqlcgen.ExposureEnum(in.NewAsset.Exposure), AssetClass: sqlcgen.AssetClassEnum(in.NewAsset.AssetClass),
			TargetAllocationPct:        pgnum.FromDecimalPtr(in.NewAsset.TargetAllocationPct),
			TargetAllocationPctRiskOff: pgnum.FromDecimalPtr(in.NewAsset.TargetAllocationPctRiskOff),
		})
		if err != nil {
			if pgerr.IsUniqueViolation(err) {
				return 0, httpx.NewConflictError(httpx.CodeDuplicateEntry, "Resource already exists")
			}
			return 0, fmt.Errorf("position: create asset: %w", err)
		}
		return created.ID, nil
	case assetlifecycle.AssetRestore:
		if _, err := q.RestoreAsset(ctx, sqlcgen.RestoreAssetParams{
			ID: plan.AssetID, Name: in.NewAsset.Name,
			Exposure: sqlcgen.ExposureEnum(in.NewAsset.Exposure), AssetClass: sqlcgen.AssetClassEnum(in.NewAsset.AssetClass),
			TargetAllocationPct:        pgnum.FromDecimalPtr(in.NewAsset.TargetAllocationPct),
			TargetAllocationPctRiskOff: pgnum.FromDecimalPtr(in.NewAsset.TargetAllocationPctRiskOff),
		}); err != nil {
			return 0, fmt.Errorf("position: restore asset %d: %w", plan.AssetID, err)
		}
	}
	return plan.AssetID, nil
}

// createPlanError maps the kernel's sentinel errors onto the existing wire errors.
func (s *Service) createPlanError(in CreateInput, err error) error {
	switch {
	case errors.Is(err, assetlifecycle.ErrDuplicate):
		return httpx.NewConflictError(httpx.CodeDuplicateEntry, "Resource already exists")
	case errors.Is(err, assetlifecycle.ErrAssetNotFound):
		return httpx.NewNotFoundError(fmt.Sprintf("Asset with id %d not found", *in.AssetID))
	case errors.Is(err, assetlifecycle.ErrPrimaryTaken):
		return httpx.NewConflictError(httpx.CodeConflict, "asset already has a primary subasset")
	}
	return err
}

// insertSubasset is the fresh-row path with the request defaults (purchaseCurrency -> PLN,
// companyBacked from assetClass, exposure: explicit > new asset's > attached asset's).
func (s *Service) insertSubasset(ctx context.Context, q Queries, in CreateInput, target sqlcgen.Asset, assetID int64, isPrimary bool) error {
	purchaseCurrency := "PLN"
	if in.PurchaseCurrency != nil {
		purchaseCurrency = *in.PurchaseCurrency
	}
	companyBacked := in.AssetClass == "STOCK" || in.AssetClass == "CRYPTO"
	if in.CompanyBacked != nil {
		companyBacked = *in.CompanyBacked
	}
	exposure := string(target.Exposure)
	if in.Exposure != nil {
		exposure = *in.Exposure
	} else if in.NewAsset != nil {
		exposure = in.NewAsset.Exposure
	}
	_, err := q.InsertPosition(ctx, sqlcgen.InsertPositionParams{
		Ticker: in.Ticker, Name: in.Name,
		Portfolio: sqlcgen.PortfolioEnum(in.Portfolio), Exchange: sqlcgen.ExchangeEnum(in.Exchange),
		AssetClass: sqlcgen.AssetClassEnum(in.AssetClass), Exposure: sqlcgen.ExposureEnum(exposure),
		PriceCurrency: sqlcgen.CurrencyEnum(in.PriceCurrency), PurchaseCurrency: sqlcgen.CurrencyEnum(purchaseCurrency),
		UnitsIke:        pgnum.FromDecimal(decimal.Zero),
		UnitsIkze:       pgnum.FromDecimal(decimal.Zero),
		UnitsRegularEur: pgnum.FromDecimal(decimal.Zero),
		UnitsRegularUsd: pgnum.FromDecimal(decimal.Zero),
		UnitsRegularPln: pgnum.FromDecimal(decimal.Zero),
		AssetID:         assetID,
		IsPrimary:       isPrimary,
		YahooTicker:     in.YahooTicker,
		CompanyBacked:   companyBacked,
	})
	if err != nil {
		if pgerr.IsUniqueViolation(err) {
			return httpx.NewConflictError(httpx.CodeDuplicateEntry, "Resource already exists")
		}
		return fmt.Errorf("position: create: %w", err)
	}
	return nil
}

// restorePositionParams maps a create request onto RestorePosition (design §6.1): required
// fields overwrite, optional ones pass the RAW request pointer (nil keeps the old setting).
func restorePositionParams(in CreateInput, plan assetlifecycle.CreatePlan, assetID int64) sqlcgen.RestorePositionParams {
	p := sqlcgen.RestorePositionParams{
		ID: plan.SubassetID, AssetID: assetID, IsPrimary: plan.IsPrimary,
		Name: in.Name, AssetClass: sqlcgen.AssetClassEnum(in.AssetClass), PriceCurrency: sqlcgen.CurrencyEnum(in.PriceCurrency),
		YahooTicker: in.YahooTicker, CompanyBacked: in.CompanyBacked,
	}
	if in.Exposure != nil {
		e := sqlcgen.ExposureEnum(*in.Exposure)
		p.Exposure = &e
	}
	if in.PurchaseCurrency != nil {
		c := sqlcgen.CurrencyEnum(*in.PurchaseCurrency)
		p.PurchaseCurrency = &c
	}
	return p
}

// Update edits one subasset row's own fields — every field optional, nil means "leave
// unchanged". Targets are not accepted here (handler 400s them to PUT /assets/{id}).
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (AssetDTO, error) {
	unitFields := []struct {
		name string
		v    *decimal.Decimal
	}{
		{"unitsIke", in.UnitsIke}, {"unitsIkze", in.UnitsIkze}, {"unitsRegularEur", in.UnitsRegularEur},
		{"unitsRegularUsd", in.UnitsRegularUsd}, {"unitsRegularPln", in.UnitsRegularPln},
	}
	for _, f := range unitFields {
		if f.v != nil && f.v.Sign() < 0 {
			return AssetDTO{}, httpx.NewValidationError(f.name, "must not be negative")
		}
	}

	if err := s.transact(ctx, func(q Queries) error { return s.applyUpdate(ctx, q, id, in) }); err != nil {
		return AssetDTO{}, err
	}
	return s.Get(ctx, id)
}

// applyUpdate is Update's read-modify-write, run inside one serialised transaction (ADR-111):
// the full-row overwrite below must start from the state the previous money write committed.
func (s *Service) applyUpdate(ctx context.Context, q Queries, id int64, in UpdateInput) error {
	row, err := q.GetPositionByID(ctx, id)
	if err != nil {
		return notFoundOrErr(id, err)
	}
	data, err := s.loadAll(ctx, q)
	if err != nil {
		return err
	}
	keyByID := make(map[int64]string, len(data.assets))
	for _, a := range data.assets {
		keyByID[a.ID] = a.Key
	}
	p, err := decodeRow(row, keyByID)
	if err != nil {
		return err
	}

	// §13 re-home. Guard clauses, in order: same-as-current/absent (ignored) -> primary (409) ->
	// target exists (404) -> same portfolio (400, field assetId). All run before the single
	// UpdatePositionFields write, so a rejected edit writes nothing. The write is a full-row
	// overwrite, so newAssetID MUST default to the current asset (writing 0 would fail the FK).
	newAssetID := p.AssetID
	if in.AssetID != nil && *in.AssetID != p.AssetID {
		if p.IsPrimary {
			return httpx.NewConflictError(httpx.CodeConflict,
				"a primary subasset cannot be re-homed — set another primary first, or delete and re-create it under the target asset")
		}
		target, err := q.GetAssetByID(ctx, *in.AssetID)
		if err != nil {
			return assetNotFoundOrErr(*in.AssetID, err)
		}
		if string(target.Portfolio) != p.Portfolio {
			return httpx.NewValidationError("assetId", "asset belongs to a different portfolio")
		}
		newAssetID = target.ID
	}

	if in.UnitsIke != nil {
		p.UnitsIke = *in.UnitsIke
	}
	if in.UnitsIkze != nil {
		p.UnitsIkze = *in.UnitsIkze
	}
	if in.UnitsRegularEur != nil {
		p.UnitsRegularEur = *in.UnitsRegularEur
	}
	if in.UnitsRegularUsd != nil {
		p.UnitsRegularUsd = *in.UnitsRegularUsd
	}
	if in.UnitsRegularPln != nil {
		p.UnitsRegularPln = *in.UnitsRegularPln
	}
	if in.TotalSpentPln != nil {
		p.TotalSpentPln = in.TotalSpentPln
	}
	if in.AvgBuyPrice != nil {
		p.AvgBuyPrice = in.AvgBuyPrice
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.AssetClass != nil {
		p.AssetClass = *in.AssetClass
	}
	if in.YahooTicker != nil {
		p.YahooTicker = in.YahooTicker
	}
	if in.CompanyBacked != nil {
		p.CompanyBacked = *in.CompanyBacked
	}

	if _, err := q.UpdatePositionFields(ctx, sqlcgen.UpdatePositionFieldsParams{
		ID:              id,
		UnitsIke:        pgnum.FromDecimal(p.UnitsIke),
		UnitsIkze:       pgnum.FromDecimal(p.UnitsIkze),
		UnitsRegularEur: pgnum.FromDecimal(p.UnitsRegularEur),
		UnitsRegularUsd: pgnum.FromDecimal(p.UnitsRegularUsd),
		UnitsRegularPln: pgnum.FromDecimal(p.UnitsRegularPln),
		TotalSpentPln:   pgnum.FromDecimalPtr(p.TotalSpentPln),
		AvgBuyPrice:     pgnum.FromDecimalPtr(p.AvgBuyPrice),
		Name:            p.Name,
		AssetClass:      sqlcgen.AssetClassEnum(p.AssetClass),
		YahooTicker:     p.YahooTicker,
		CompanyBacked:   p.CompanyBacked,
		AssetID:         newAssetID,
	}); err != nil {
		return fmt.Errorf("position: update %d: %w", id, err)
	}
	return nil
}

// UpdateAsset edits asset-level fields (targets + name) — the only writer of targets. Each
// target is tri-state (httpx.Optional): absent leaves the stored value alone, an explicit JSON
// null CLEARS it (NULL = no target, distinct from 0 = explicit zero target, ADR-053), a value
// sets it (range 0..100 only). There is deliberately NO Σ targets == 100 guard here: partial
// sums must stay editable (the UI's Σ indicator relies on it); Σ == 100 is enforced only as
// the set-primary tripwire (guardTargetSum).
func (s *Service) UpdateAsset(ctx context.Context, id int64, in UpdateAssetInput) (AssetDTO, error) {
	if err := decx.ValidatePercentRange("targetAllocationPct", in.TargetAllocationPct.Value); err != nil {
		return AssetDTO{}, err
	}
	if err := decx.ValidatePercentRange("targetAllocationPctRiskOff", in.TargetAllocationPctRiskOff.Value); err != nil {
		return AssetDTO{}, err
	}
	var updated sqlcgen.Asset
	err := s.transact(ctx, func(q Queries) error {
		assetRow, err := q.GetAssetByID(ctx, id)
		if err != nil {
			return assetNotFoundOrErr(id, err)
		}
		a, err := decodeAsset(assetRow)
		if err != nil {
			return err
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
		updated, err = q.UpdateAssetFields(ctx, sqlcgen.UpdateAssetFieldsParams{
			ID:                         id,
			TargetAllocationPct:        pgnum.FromDecimalPtr(a.TargetAllocationPct),
			TargetAllocationPctRiskOff: pgnum.FromDecimalPtr(a.TargetAllocationPctRiskOff),
			Name:                       a.Name,
		})
		if err != nil {
			return fmt.Errorf("position: update asset %d: %w", id, err)
		}
		return nil
	})
	if err != nil {
		return AssetDTO{}, err
	}
	saved, err := decodeAsset(updated)
	if err != nil {
		return AssetDTO{}, err
	}
	data, err := s.loadAll(ctx, s.queries)
	if err != nil {
		return AssetDTO{}, err
	}
	return s.getWithComputed(ctx, data, saved)
}

// DeleteAsset soft-deletes a whole asset and its live subassets in one statement (task 173):
// history keeps every row; a deleted or unknown asset answers 404.
func (s *Service) DeleteAsset(ctx context.Context, id int64) error {
	return s.transact(ctx, func(q Queries) error {
		if _, err := q.GetAssetByID(ctx, id); err != nil {
			return assetNotFoundOrErr(id, err)
		}
		if err := q.SoftDeleteAsset(ctx, id); err != nil {
			return fmt.Errorf("position: delete asset %d: %w", id, err)
		}
		return nil
	})
}

// guardTargetSum is the Σ targets == 100 defensive tripwire (the rebalancing Step-6
// precondition) — used ONLY by SetPrimary, inside its transaction, so a violation rolls the
// flip back. Targets never move on set-primary, so it passes by construction on a complete
// book; it fails when an in-progress target edit (which is deliberately not Σ-guarded) left
// the book off 100. MAIN checks both regime tables (risk-on and COALESCE(riskOff, riskOn) —
// promote's own two-sum check, ADR-088); RETIREMENT has no risk-off table.
func (s *Service) guardTargetSum(ctx context.Context, q Queries, portfolio string) error {
	assetRows, err := q.ListAllAssets(ctx)
	if err != nil {
		return fmt.Errorf("position: list assets: %w", err)
	}
	sum := decimal.Zero
	sumRiskOff := decimal.Zero
	for _, r := range assetRows {
		if string(r.Portfolio) != portfolio {
			continue
		}
		v, err := pgnum.ToDecimalPtr(r.TargetAllocationPct)
		if err != nil {
			return fmt.Errorf("position: decode asset target for %s: %w", r.Key, err)
		}
		if v != nil {
			sum = sum.Add(*v)
		}
		vr, err := pgnum.ToDecimalPtr(r.TargetAllocationPctRiskOff)
		if err != nil {
			return fmt.Errorf("position: decode asset risk-off target for %s: %w", r.Key, err)
		}
		if vr != nil {
			sumRiskOff = sumRiskOff.Add(*vr)
		} else if v != nil {
			sumRiskOff = sumRiskOff.Add(*v)
		}
	}
	if sum.Sub(hundred).Abs().Cmp(targetSumTolerance) > 0 {
		return httpx.NewValidationError("", fmt.Sprintf(
			"Invariant violated: portfolio %s's asset targetAllocationPct would sum to %s (expected 100)",
			portfolio, sum.StringFixed(4)))
	}
	if portfolio == rebalancingcalc.PortfolioMAIN && sumRiskOff.Sub(hundred).Abs().Cmp(targetSumTolerance) > 0 {
		return httpx.NewValidationError("", fmt.Sprintf(
			"Invariant violated: portfolio %s's asset COALESCE(targetAllocationPctRiskOff, targetAllocationPct) would sum to %s (expected 100)",
			portfolio, sumRiskOff.StringFixed(4)))
	}
	return nil
}

func notFoundOrErr(id int64, err error) error {
	return pgerr.NotFoundOr(err, fmt.Sprintf("Position with id %d not found", id), fmt.Sprintf("position: read %d", id))
}

func assetNotFoundOrErr(id int64, err error) error {
	return pgerr.NotFoundOr(err, fmt.Sprintf("Asset with id %d not found", id), fmt.Sprintf("position: read asset %d", id))
}
