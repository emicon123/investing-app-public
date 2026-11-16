package position

import (
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	scposition "investing-app/common-go/siblingcore/position"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/openapi"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the asset/subasset
// endpoints. Wire fidelity note (see internal/accountcash's doc comment): every response here uses
// a local float64 wire struct instead of the generated models (float32-typed before task 182 added
// `format: double` to the spec).
type Handler struct {
	service *scposition.Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *scposition.Service) *Handler { return &Handler{service: service} }

const defaultPageSizeParam = 20

// ListPositions implements GET /positions?portfolio=... (required query param — this app has
// no unscoped listing). Returns one asset per group (subassets nested), not one row per
// positions row.
func (h *Handler) ListPositions(w http.ResponseWriter, r *http.Request, params openapi.ListPositionsParams) {
	page := 0
	if params.Pageable.Page != nil {
		page = int(*params.Pageable.Page)
	}
	size := defaultPageSizeParam
	if params.Pageable.Size != nil {
		size = int(*params.Pageable.Size)
	}
	portfolio, err := book.Config.ResolvePortfolio("portfolio", string(params.Portfolio))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, total, err := h.service.List(r.Context(), portfolio, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]wireAsset, 0, len(items))
	for _, item := range items {
		data = append(data, toWireAsset(item))
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(data, httpx.PageInfo{Number: page, Size: size, Total: total}))
}

// GetPosition implements GET /positions/{id} — {id} is a subasset row id (stable across the
// task-156 migration); the response is its parent asset.
func (h *Handler) GetPosition(w http.ResponseWriter, r *http.Request, id int64) {
	dto, err := h.service.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toWireAsset(dto)))
}

type wireNewAsset struct {
	Name                       string           `json:"name"`
	Exposure                   string           `json:"exposure"`
	AssetClass                 string           `json:"assetClass"`
	TargetAllocationPct        *decimal.Decimal `json:"targetAllocationPct,omitempty"`
	TargetAllocationPctRiskOff *decimal.Decimal `json:"targetAllocationPctRiskOff,omitempty"`
}

type wireCreatePositionRequest struct {
	Portfolio     string  `json:"portfolio"`
	Ticker        string  `json:"ticker"`
	Name          string  `json:"name"`
	Exchange      string  `json:"exchange"`
	AssetClass    string  `json:"assetClass"`
	Exposure      string  `json:"exposure"`
	PriceCurrency string  `json:"priceCurrency"`
	YahooTicker   *string `json:"yahooTicker,omitempty"`
	SortOrder     *int    `json:"sortOrder,omitempty"`
	// Asset linkage (task 156): exactly one of assetId / newAsset is required. Targets live
	// only on the asset — a top-level targetAllocationPct here is rejected (PUT /assets).
	AssetID   *int64        `json:"assetId,omitempty"`
	IsPrimary *bool         `json:"isPrimary,omitempty"`
	NewAsset  *wireNewAsset `json:"newAsset,omitempty"`
}

// toCreateInput maps the body to the service input (400 for an unknown portfolio; every number
// already a decimal, F3.4).
func (b wireCreatePositionRequest) toCreateInput() (scposition.CreateInput, error) {
	portfolio, err := book.Config.ResolvePortfolio("portfolio", b.Portfolio)
	if err != nil {
		return scposition.CreateInput{}, err
	}
	in := scposition.CreateInput{
		Portfolio: portfolio, Ticker: b.Ticker, Name: b.Name, Exchange: b.Exchange, AssetClass: b.AssetClass,
		Exposure: b.Exposure, PriceCurrency: b.PriceCurrency,
		YahooTicker: b.YahooTicker, SortOrder: b.SortOrder,
		AssetID: b.AssetID, IsPrimary: b.IsPrimary,
	}
	if b.NewAsset != nil {
		in.NewAsset = &scposition.NewAssetInput{
			Name: b.NewAsset.Name, Exposure: b.NewAsset.Exposure, AssetClass: b.NewAsset.AssetClass,
			TargetAllocationPct:        b.NewAsset.TargetAllocationPct,
			TargetAllocationPctRiskOff: b.NewAsset.TargetAllocationPctRiskOff,
		}
	}
	return in, nil
}

// CreatePosition implements POST /positions.
func (h *Handler) CreatePosition(w http.ResponseWriter, r *http.Request) {
	var body wireCreatePositionRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	in, err := body.toCreateInput()
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	dto, err := h.service.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(toWireAsset(dto)))
}

// wireUpdatePositionRequest decodes every number as a decimal (task 182 / F3.4); a
// *decimal.Decimal keeps "absent or null leaves the stored value alone".
type wireUpdatePositionRequest struct {
	Units         *decimal.Decimal `json:"units,omitempty"`
	TotalSpentPln *decimal.Decimal `json:"totalSpentPln,omitempty"`
	AvgBuyPrice   *decimal.Decimal `json:"avgBuyPrice,omitempty"`
	Name          *string          `json:"name,omitempty"`
	GroupName     *string          `json:"groupName,omitempty"`
	AssetClass    *string          `json:"assetClass,omitempty"`
	YahooTicker   *string          `json:"yahooTicker,omitempty"`
	SortOrder     *int             `json:"sortOrder,omitempty"`
	AssetID       *int64           `json:"assetId,omitempty"`
}

// toUpdateInput maps the body to the service input.
func (b wireUpdatePositionRequest) toUpdateInput() scposition.UpdateInput {
	return scposition.UpdateInput{
		Units:         unitsPatch(b.Units),
		TotalSpentPln: b.TotalSpentPln,
		AvgBuyPrice:   b.AvgBuyPrice,
		Name:          b.Name,
		GroupName:     b.GroupName,
		AssetClass:    b.AssetClass,
		YahooTicker:   b.YahooTicker,
		SortOrder:     b.SortOrder,
		AssetID:       b.AssetID,
	}
}

// UpdatePosition implements PUT /positions/{id} (subasset-owned fields only).
func (h *Handler) UpdatePosition(w http.ResponseWriter, r *http.Request, id int64) {
	var body wireUpdatePositionRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	dto, err := h.service.Update(r.Context(), id, body.toUpdateInput())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toWireAsset(dto)))
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

// SetAssetPrimary implements POST /assets/{id}/primary — the task-156 replacement for
// POST /positions/{id}/promote. Pure boolean flip, no cost-basis move.
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
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toWireAsset(dto)))
}

// wireUpdateAssetRequest decodes targets into httpx.Optional so an explicit JSON null CLEARS
// the target while an absent key leaves it alone (a plain pointer cannot tell the two apart).
// Decoding straight into decimal.Decimal also keeps the exact digits the client sent.
type wireUpdateAssetRequest struct {
	TargetAllocationPct        httpx.Optional[decimal.Decimal] `json:"targetAllocationPct"`
	TargetAllocationPctRiskOff httpx.Optional[decimal.Decimal] `json:"targetAllocationPctRiskOff"`
	Name                       *string                         `json:"name"`
}

// UpdateAsset implements PUT /assets/{id} — the only writer of asset-level targets.
func (h *Handler) UpdateAsset(w http.ResponseWriter, r *http.Request, id int64) {
	var body wireUpdateAssetRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	dto, err := h.service.UpdateAsset(r.Context(), id, scposition.UpdateAssetInput{
		TargetAllocationPct:        body.TargetAllocationPct,
		TargetAllocationPctRiskOff: body.TargetAllocationPctRiskOff,
		Name:                       body.Name,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toWireAsset(dto)))
}

// DeleteAsset implements DELETE /assets/{id} — subassets cascade via the FK.
func (h *Handler) DeleteAsset(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.service.DeleteAsset(r.Context(), id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Wire types ──────────────────────────────────────────────────────────────

type wireAsset struct {
	ID                         int64          `json:"id"`
	Portfolio                  string         `json:"portfolio"`
	Key                        string         `json:"key"`
	Name                       string         `json:"name"`
	Exposure                   string         `json:"exposure"`
	AssetClass                 string         `json:"assetClass"`
	TargetAllocationPct        *float64       `json:"targetAllocationPct,omitempty"`
	TargetAllocationPctRiskOff *float64       `json:"targetAllocationPctRiskOff,omitempty"`
	Computed                   wireComputed   `json:"computed"`
	Subassets                  []wireSubasset `json:"subassets,omitempty"`
	CreatedAt                  time.Time      `json:"createdAt"`
	UpdatedAt                  time.Time      `json:"updatedAt"`
}

type wireComputed struct {
	UnitsTotal     *float64 `json:"unitsTotal,omitempty"`
	PricePln       *float64 `json:"pricePln,omitempty"`
	PriceNative    *float64 `json:"priceNative,omitempty"`
	ValuePln       *float64 `json:"valuePln,omitempty"`
	CurrentPct     *float64 `json:"currentPct,omitempty"`
	DeviationPct   *float64 `json:"deviationPct,omitempty"`
	ProfitPct      *float64 `json:"profitPct,omitempty"`
	AvgBuyPricePln *float64 `json:"avgBuyPricePln,omitempty"`
	// Task 161c-1: the entry price in the primary's price currency at the current fx, the asset's
	// holdings in units of the primary, and the tickers of priced subassets holding units with no
	// recorded cost. All optional; nil / empty are omitted from the JSON.
	AvgBuyPriceNative *float64 `json:"avgBuyPriceNative,omitempty"`
	EquivalentUnits   *float64 `json:"equivalentUnits,omitempty"`
	CostlessSubassets []string `json:"costlessSubassets,omitempty"`
}

type wireSubasset struct {
	ID             int64    `json:"id"`
	AssetID        int64    `json:"assetId"`
	IsPrimary      bool     `json:"isPrimary"`
	Ticker         string   `json:"ticker"`
	Name           string   `json:"name"`
	Exchange       string   `json:"exchange"`
	YahooTicker    *string  `json:"yahooTicker,omitempty"`
	PriceCurrency  string   `json:"priceCurrency"`
	Units          float64  `json:"units"`
	EffectiveUnits float64  `json:"effectiveUnits"`
	PricePln       *float64 `json:"pricePln,omitempty"`
	PriceNative    *float64 `json:"priceNative,omitempty"`
	ValuePln       *float64 `json:"valuePln,omitempty"`
	GroupName      *string  `json:"groupName,omitempty"`
	SortOrder      int      `json:"sortOrder"`
}

func toWireAsset(d scposition.AssetDTO) wireAsset {
	subs := make([]wireSubasset, 0, len(d.Subassets))
	for _, s := range d.Subassets {
		subs = append(subs, wireSubasset{
			ID: s.ID, AssetID: s.AssetID, IsPrimary: s.IsPrimary,
			Ticker: s.Ticker, Name: s.Name, Exchange: s.Exchange,
			YahooTicker: s.YahooTicker, PriceCurrency: s.PriceCurrency,
			Units: decx.ToFloat(s.Units.Of(siblingcore.SingleBucket)), EffectiveUnits: decx.ToFloat(s.EffectiveUnits),
			PricePln: decx.ToFloatPtr(s.PricePln), PriceNative: decx.ToFloatPtr(s.PriceNative), ValuePln: decx.ToFloatPtr(s.ValuePln),
			GroupName: s.GroupName, SortOrder: s.SortOrder,
		})
	}
	return wireAsset{
		ID: d.ID, Portfolio: string(d.Portfolio), Key: d.Key, Name: d.Name, Exposure: d.Exposure,
		AssetClass:                 d.AssetClass,
		TargetAllocationPct:        decx.ToFloatPtr(d.TargetAllocationPct),
		TargetAllocationPctRiskOff: decx.ToFloatPtr(d.TargetAllocationPctRiskOff),
		Computed: wireComputed{
			UnitsTotal: decx.ToFloatPtr(d.Computed.UnitsTotal), PricePln: decx.ToFloatPtr(d.Computed.PricePln),
			PriceNative: decx.ToFloatPtr(d.Computed.PriceNative), ValuePln: decx.ToFloatPtr(d.Computed.ValuePln),
			CurrentPct: decx.ToFloatPtr(d.Computed.CurrentPct), DeviationPct: decx.ToFloatPtr(d.Computed.DeviationPct),
			ProfitPct: decx.ToFloatPtr(d.Computed.ProfitPct), AvgBuyPricePln: decx.ToFloatPtr(d.Computed.AvgBuyPricePln),
			AvgBuyPriceNative: decx.ToFloatPtr(d.Computed.AvgBuyPriceNative), EquivalentUnits: decx.ToFloatPtr(d.Computed.EquivalentUnits),
			CostlessSubassets: d.Computed.CostlessSubassets,
		},
		Subassets: subs, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
}

// unitsPatch is UpdateInput.Units: the single bucket when `units` is present.
func unitsPatch(units *decimal.Decimal) map[siblingcore.Bucket]decimal.Decimal {
	if units == nil {
		return nil
	}
	return map[siblingcore.Bucket]decimal.Decimal{siblingcore.SingleBucket: *units}
}
