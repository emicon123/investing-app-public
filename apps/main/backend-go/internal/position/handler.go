package position

import (
	"net/http"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the
// asset/subasset endpoints — the same thin-Facade convention as every other domain
// package (no business logic here; service -> wire type mapping only). Responses are
// written with the generated openapi.Asset/Subasset types (compile-time contract
// conformance); request bodies are decoded into local wire structs so a stray legacy key
// (top-level targets, isCompanionRow) can be rejected loudly and target fields can be
// tri-state (httpx.Optional: absent / null / value).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

const defaultPageSize = 20

// ListPositions implements GET /positions. Returns one asset per group (subassets
// nested), not one row per positions row.
func (h *Handler) ListPositions(w http.ResponseWriter, r *http.Request, params openapi.ListPositionsParams) {
	page := 0
	if params.Page != nil {
		page = *params.Page
	}
	size := defaultPageSize
	if params.Size != nil {
		size = *params.Size
	}
	portfolioFilter := ""
	if params.Portfolio != nil {
		portfolioFilter = string(*params.Portfolio)
	}
	items, total, err := h.service.List(r.Context(), portfolioFilter, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.Asset, 0, len(items))
	for _, item := range items {
		data = append(data, toOpenAPIAsset(item))
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(data, httpx.PageInfo{Number: page, Size: size, Total: total}))
}

// GetPosition implements GET /positions/{id} — {id} is a subasset row id (stable
// across the task-156 migration); the response is its parent asset.
func (h *Handler) GetPosition(w http.ResponseWriter, r *http.Request, id int64) {
	dto, err := h.service.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPIAsset(dto)))
}

type wireNewAsset struct {
	Name                       string           `json:"name"`
	Exposure                   string           `json:"exposure"`
	AssetClass                 string           `json:"assetClass"`
	TargetAllocationPct        *decimal.Decimal `json:"targetAllocationPct,omitempty"`
	TargetAllocationPctRiskOff *decimal.Decimal `json:"targetAllocationPctRiskOff,omitempty"`
}

type wireCreatePositionRequest struct {
	Ticker           string  `json:"ticker"`
	Name             string  `json:"name"`
	Portfolio        string  `json:"portfolio"`
	Exchange         string  `json:"exchange"`
	AssetClass       string  `json:"assetClass"`
	Exposure         string  `json:"exposure,omitempty"`
	PriceCurrency    string  `json:"priceCurrency"`
	PurchaseCurrency string  `json:"purchaseCurrency,omitempty"`
	YahooTicker      *string `json:"yahooTicker,omitempty"`
	CompanyBacked    *bool   `json:"companyBacked,omitempty"`
	// Asset linkage (task 156): exactly one of assetId / newAsset is required.
	// Targets live only on the asset — top-level target keys are rejected below.
	AssetID   *int64        `json:"assetId,omitempty"`
	IsPrimary *bool         `json:"isPrimary,omitempty"`
	NewAsset  *wireNewAsset `json:"newAsset,omitempty"`
	// Deprecated companion/target-era fields kept ONLY to fail loudly.
	TargetAllocationPct        *decimal.Decimal `json:"targetAllocationPct,omitempty"`
	TargetAllocationPctRiskOff *decimal.Decimal `json:"targetAllocationPctRiskOff,omitempty"`
	IsCompanionRow             *bool            `json:"isCompanionRow,omitempty"`
	CompanionParentTicker      *string          `json:"companionParentTicker,omitempty"`
}

// CreatePosition implements POST /positions.
func (h *Handler) CreatePosition(w http.ResponseWriter, r *http.Request) {
	var body wireCreatePositionRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if body.TargetAllocationPct != nil || body.TargetAllocationPctRiskOff != nil {
		httpx.WriteError(w, httpx.NewValidationError("targetAllocationPct",
			"targets live on the asset — pass them inside newAsset, or edit via PUT /assets/{id}"))
		return
	}
	if body.IsCompanionRow != nil || body.CompanionParentTicker != nil {
		httpx.WriteError(w, httpx.NewValidationError("isCompanionRow",
			"companion rows no longer exist — pass assetId or newAsset instead"))
		return
	}
	in := CreateInput{
		Ticker: body.Ticker, Name: body.Name, Portfolio: body.Portfolio,
		Exchange: body.Exchange, AssetClass: body.AssetClass,
		PriceCurrency: body.PriceCurrency, YahooTicker: body.YahooTicker, CompanyBacked: body.CompanyBacked,
		AssetID: body.AssetID, IsPrimary: body.IsPrimary,
	}
	if body.Exposure != "" {
		in.Exposure = &body.Exposure
	}
	// PurchaseCurrency is spec-required but the create dialog never collects it —
	// only forward a pointer when a value was actually supplied, so Service.Create's
	// "nil -> default PLN" logic applies (an unconditional "" hits currency_enum
	// with SQLSTATE 22P02 and 500s the whole dialog).
	if body.PurchaseCurrency != "" {
		in.PurchaseCurrency = &body.PurchaseCurrency
	}
	if body.NewAsset != nil {
		in.NewAsset = &NewAssetInput{
			Name: body.NewAsset.Name, Exposure: body.NewAsset.Exposure, AssetClass: body.NewAsset.AssetClass,
			TargetAllocationPct:        body.NewAsset.TargetAllocationPct,
			TargetAllocationPctRiskOff: body.NewAsset.TargetAllocationPctRiskOff,
		}
	}
	dto, err := h.service.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(toOpenAPIAsset(dto)))
}

// wireUpdatePositionRequest decodes every number as a decimal (task 182 / F3.4: no float64 on
// the money input path). A *decimal.Decimal keeps "absent or null leaves the stored value alone".
type wireUpdatePositionRequest struct {
	UnitsIke        *decimal.Decimal `json:"unitsIke,omitempty"`
	UnitsIkze       *decimal.Decimal `json:"unitsIkze,omitempty"`
	UnitsRegularEur *decimal.Decimal `json:"unitsRegularEur,omitempty"`
	UnitsRegularUsd *decimal.Decimal `json:"unitsRegularUsd,omitempty"`
	UnitsRegularPln *decimal.Decimal `json:"unitsRegularPln,omitempty"`
	TotalSpentPln   *decimal.Decimal `json:"totalSpentPln,omitempty"`
	AvgBuyPrice     *decimal.Decimal `json:"avgBuyPrice,omitempty"`
	Name            *string          `json:"name,omitempty"`
	AssetClass      *string          `json:"assetClass,omitempty"`
	YahooTicker     *string          `json:"yahooTicker,omitempty"`
	CompanyBacked   *bool            `json:"companyBacked,omitempty"`
	AssetID         *int64           `json:"assetId,omitempty"` // §13 re-home of a non-primary subasset
	// Deprecated target fields kept ONLY to fail loudly: targets moved to PUT /assets/{id}.
	TargetAllocationPct        *decimal.Decimal `json:"targetAllocationPct,omitempty"`
	TargetAllocationPctRiskOff *decimal.Decimal `json:"targetAllocationPctRiskOff,omitempty"`
}

// UpdatePosition implements PUT /positions/{id} (subasset-owned fields only).
func (h *Handler) UpdatePosition(w http.ResponseWriter, r *http.Request, id int64) {
	var body wireUpdatePositionRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if body.TargetAllocationPct != nil || body.TargetAllocationPctRiskOff != nil {
		httpx.WriteError(w, httpx.NewValidationError("targetAllocationPct",
			"targets live on the asset — edit via PUT /assets/{id}"))
		return
	}
	in := UpdateInput{
		UnitsIke:        body.UnitsIke,
		UnitsIkze:       body.UnitsIkze,
		UnitsRegularEur: body.UnitsRegularEur,
		UnitsRegularUsd: body.UnitsRegularUsd,
		UnitsRegularPln: body.UnitsRegularPln,
		TotalSpentPln:   body.TotalSpentPln,
		AvgBuyPrice:     body.AvgBuyPrice,
		Name:            body.Name,
		AssetClass:      body.AssetClass,
		YahooTicker:     body.YahooTicker,
		CompanyBacked:   body.CompanyBacked,
		AssetID:         body.AssetID,
	}
	dto, err := h.service.Update(r.Context(), id, in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPIAsset(dto)))
}

// DeletePosition implements DELETE /positions/{id}.
func (h *Handler) DeletePosition(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.service.Delete(r.Context(), id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type wireSetPrimaryRequest struct {
	SubassetID int64 `json:"subassetId"`
}

// SetAssetPrimary implements POST /assets/{id}/primary — the task-156 replacement
// for POST /positions/{id}/promote. Pure boolean flip, no cost-basis move.
func (h *Handler) SetAssetPrimary(w http.ResponseWriter, r *http.Request, id int64) {
	var body wireSetPrimaryRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	dto, err := h.service.SetPrimary(r.Context(), id, body.SubassetID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPIAsset(dto)))
}

// wireUpdateAssetRequest decodes targets into httpx.Optional so an explicit JSON null CLEARS a
// target (NULL "no target" != 0 "explicit zero", ADR-053) while an absent key leaves it alone.
type wireUpdateAssetRequest struct {
	TargetAllocationPct        httpx.Optional[decimal.Decimal] `json:"targetAllocationPct"`
	TargetAllocationPctRiskOff httpx.Optional[decimal.Decimal] `json:"targetAllocationPctRiskOff"`
	Name                       *string                         `json:"name,omitempty"`
}

// UpdateAsset implements PUT /assets/{id} — the only writer of asset-level targets.
func (h *Handler) UpdateAsset(w http.ResponseWriter, r *http.Request, id int64) {
	var body wireUpdateAssetRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	dto, err := h.service.UpdateAsset(r.Context(), id, UpdateAssetInput(body))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPIAsset(dto)))
}

// DeleteAsset implements DELETE /assets/{id} — subassets cascade via the FK.
func (h *Handler) DeleteAsset(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.service.DeleteAsset(r.Context(), id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Response mapping (service DTO -> generated openapi types) ────────────────

func toOpenAPIAsset(d AssetDTO) openapi.Asset {
	subs := make([]openapi.Subasset, 0, len(d.Subassets))
	for _, s := range d.Subassets {
		subs = append(subs, toOpenAPISubasset(s))
	}
	id := d.ID
	createdAt, updatedAt := d.CreatedAt.UTC(), d.UpdatedAt.UTC()
	out := openapi.Asset{
		Id: &id, Key: d.Key, Name: d.Name, Portfolio: openapi.Portfolio(d.Portfolio),
		Exposure: d.Exposure, AssetClass: openapi.AssetClass(d.AssetClass),
		TargetAllocationPct:        decx.ToFloatPtr(d.TargetAllocationPct),
		TargetAllocationPctRiskOff: decx.ToFloatPtr(d.TargetAllocationPctRiskOff),
		Computed:                   toOpenAPIComputed(d.Computed),
		Subassets:                  &subs,
	}
	if !d.CreatedAt.IsZero() {
		out.CreatedAt = &createdAt
	}
	if !d.UpdatedAt.IsZero() {
		out.UpdatedAt = &updatedAt
	}
	return out
}

func toOpenAPISubasset(s SubassetDTO) openapi.Subasset {
	id, assetID, isPrimary, backed := s.ID, s.AssetID, s.IsPrimary, s.CompanyBacked
	class := openapi.AssetClass(s.AssetClass)
	eff := decx.ToFloat(s.EffectiveUnits)
	ike, ikze := decx.ToFloat(s.UnitsIke), decx.ToFloat(s.UnitsIkze)
	eur, usd, pln := decx.ToFloat(s.UnitsRegularEur), decx.ToFloat(s.UnitsRegularUsd), decx.ToFloat(s.UnitsRegularPln)
	return openapi.Subasset{
		Id: &id, AssetId: &assetID, IsPrimary: &isPrimary,
		Ticker: s.Ticker, Name: s.Name, Portfolio: openapi.Portfolio(s.Portfolio), Exchange: openapi.Exchange(s.Exchange), AssetClass: &class,
		PriceCurrency: openapi.Currency(s.PriceCurrency), PurchaseCurrency: openapi.Currency(s.PurchaseCurrency),
		UnitsIke: &ike, UnitsIkze: &ikze, UnitsRegularEur: &eur, UnitsRegularUsd: &usd, UnitsRegularPln: &pln,
		EffectiveUnits: &eff,
		PricePln:       decx.ToFloatPtr(s.PricePln), PriceNative: decx.ToFloatPtr(s.PriceNative), ValuePln: decx.ToFloatPtr(s.ValuePln),
		TotalSpentPln: decx.ToFloatPtr(s.TotalSpentPln), AvgBuyPrice: decx.ToFloatPtr(s.AvgBuyPrice),
		YahooTicker: s.YahooTicker, CompanyBacked: &backed,
	}
}

func toOpenAPIComputed(c ComputedFields) *openapi.PositionComputed {
	return &openapi.PositionComputed{
		UnitsTotal: decx.ToFloatPtr(c.UnitsTotal), PricePln: decx.ToFloatPtr(c.PricePln),
		PriceNative: decx.ToFloatPtr(c.PriceNative), ValuePln: decx.ToFloatPtr(c.ValuePln),
		CurrentPct:                   decx.ToFloatPtr(c.CurrentPct),
		EffectiveTargetAllocationPct: decx.ToFloatPtr(c.EffectiveTargetAllocationPct),
		DeviationPct:                 decx.ToFloatPtr(c.DeviationPct),
		ProfitPct:                    decx.ToFloatPtr(c.ProfitPct), AvgBuyPricePln: decx.ToFloatPtr(c.AvgBuyPricePln),
		AvgBuyPriceNative: decx.ToFloatPtr(c.AvgBuyPriceNative), EquivalentUnits: decx.ToFloatPtr(c.EquivalentUnits),
		CostlessSubassets: costlessToWire(c.CostlessSubassets),
	}
}

// costlessToWire maps the costless-subassets list to the wire's optional `*[]string`: none -> nil,
// so the field is omitted (never `[]`), as the contract requires.
func costlessToWire(tickers []string) *[]string {
	if len(tickers) == 0 {
		return nil
	}
	return &tickers
}
