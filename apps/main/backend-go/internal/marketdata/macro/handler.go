package macro

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the macro endpoints — the
// same thin-Facade convention as marketdata.Handler (no business logic here; service -> openapi
// type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// GetMacroState implements GET /market-data/macro-state — the direct port of
// MarketDataController.getMacroState. Read-only: latest macro_state row + live settings +
// component freshness; never calls FRED/ECB/BoE/BoJ/chinadata.live.
func (h *Handler) GetMacroState(w http.ResponseWriter, r *http.Request) {
	dto, err := h.service.CurrentState(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPIMacroState(dto)))
}

// GetMacroStateHistory implements GET /market-data/macro-state/history — the direct port of
// MarketDataController.getMacroStateHistory. Every persisted row, asOfDate ascending.
func (h *Handler) GetMacroStateHistory(w http.ResponseWriter, r *http.Request) {
	points, err := h.service.History(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List(toOpenAPIHistoryPoints(points)))
}

// RefreshMacroState implements POST /market-data/macro-state/refresh — the direct port of
// MarketDataController.refreshMacroState. Full fetch + recompute + upsert, returns the fresh
// state. (Nginx gives this exact path a 180s timeout — it does live external fetches.)
func (h *Handler) RefreshMacroState(w http.ResponseWriter, r *http.Request) {
	dto, err := h.service.Refresh(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPIMacroState(dto)))
}

// GetBenchmarkHistory implements GET /market-data/benchmark-history?symbol=... Only the four
// whitelisted symbols; anything else is a 400 VALIDATION_ERROR.
func (h *Handler) GetBenchmarkHistory(w http.ResponseWriter, r *http.Request, params openapi.GetBenchmarkHistoryParams) {
	symbol := string(params.Symbol)
	if !SupportedBenchmarks[symbol] {
		httpx.WriteError(w, httpx.NewValidationError("", "Unsupported benchmark symbol: "+symbol))
		return
	}
	points, err := h.service.BenchmarkHistory(r.Context(), symbol)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List(toOpenAPIBenchmarkPoints(points)))
}

// UpsertManualM2 implements POST /market-data/macro/m2-manual. Upserts one MANUAL observation (no
// recompute) and returns the current state. Body validation failures answer 422 per
// docs/openapi.yaml.
func (h *Handler) UpsertManualM2(w http.ResponseWriter, r *http.Request) {
	series, date, value, appErr := decodeManualM2(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	if err := h.service.UpsertManual(r.Context(), series, date, value); err != nil {
		httpx.WriteError(w, err)
		return
	}
	dto, err := h.service.CurrentState(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPIMacroState(dto)))
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi.yaml documents for this endpoint's validation failures.
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: http.StatusUnprocessableEntity, Code: httpx.CodeValidationError, Field: field, Message: message}
}

// decodeManualM2 parses + validates the m2-manual body: all three fields required
// (ManualM2ObservationRequest), seriesCode ∈ {M2_CN, M2_JP} (the @Pattern rule).
// NOTE: openapi.ManualM2ObservationRequest.Value is a decimal.Decimal (x-go-type, task 182) that
// decodes "absent" as 0 — presence is checked on the raw map first to keep @NotNull semantics.
func decodeManualM2(r *http.Request) (string, time.Time, decimal.Decimal, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return "", time.Time{}, decimal.Zero, unprocessable("", "Invalid request body")
	}
	for _, field := range []string{"seriesCode", "observationDate", "value"} {
		if _, ok := raw[field]; !ok {
			return "", time.Time{}, decimal.Zero, unprocessable(field, "must not be null")
		}
	}
	var body openapi.ManualM2ObservationRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return "", time.Time{}, decimal.Zero, unprocessable("", "Invalid request body")
	}
	series := string(body.SeriesCode)
	if series != SeriesM2CN && series != SeriesM2JP {
		return "", time.Time{}, decimal.Zero, unprocessable("seriesCode", "seriesCode must be M2_CN or M2_JP")
	}
	return series, body.ObservationDate.Time, body.Value, nil
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────────────

func toOpenAPIMacroState(d StateDTO) openapi.MacroState {
	out := openapi.MacroState{
		RiskOff:       new(d.RiskOff),
		DeriskEnabled: new(d.DeriskEnabled),
		GlobalM2Stale: new(d.GlobalM2Stale),
	}
	if d.AsOfDate != nil {
		out.AsOfDate = &types.Date{Time: *d.AsOfDate}
	}
	if d.BlendFactor != nil {
		out.BlendFactor = new(decimalToFloat(*d.BlendFactor))
	}
	if d.HasZone {
		out.Zone = macroZonePtr(d.Zone)
	}
	if d.CompositeZ != nil {
		out.CompositeZ = new(decimalToFloat(*d.CompositeZ))
	}
	if d.SmoothedZ != nil {
		out.SmoothedZ = new(decimalToFloat(*d.SmoothedZ))
	}
	if d.NetLiqLatest != nil {
		out.NetLiqLatestUsdMln = new(decimalToFloat(*d.NetLiqLatest))
	}
	if d.NetLiqYoY != nil {
		out.NetLiqYoy = new(decimalToFloat(*d.NetLiqYoY))
	}
	if d.NetLiqYoYAccel != nil {
		out.NetLiqYoyAccel = new(decimalToFloat(*d.NetLiqYoYAccel))
	}
	out.NetLiqRiskOff = d.NetLiqRiskOff
	if d.GlobalM2Latest != nil {
		out.GlobalM2LatestUsdMln = new(decimalToFloat(*d.GlobalM2Latest))
	}
	if d.GlobalM2YoY != nil {
		out.GlobalM2Yoy = new(decimalToFloat(*d.GlobalM2YoY))
	}
	if d.GlobalM2YoYAccel != nil {
		out.GlobalM2YoyAccel = new(decimalToFloat(*d.GlobalM2YoYAccel))
	}
	out.GlobalM2RiskOff = d.GlobalM2RiskOff
	if d.Components != nil {
		components := make([]openapi.MacroComponentStatus, 0, len(d.Components))
		for _, c := range d.Components {
			components = append(components, toOpenAPIComponent(c))
		}
		out.Components = &components
	}
	if d.ComputedAt != nil {
		utc := d.ComputedAt.UTC() // rendered as Z (UTC); same instant, byte-identical text
		out.ComputedAt = &utc
	}
	return out
}

func toOpenAPIComponent(c ComponentStatus) openapi.MacroComponentStatus {
	series := c.SeriesCode
	source := openapi.MacroComponentStatusSource(c.Source)
	date := types.Date{Time: c.LatestObservationDate}
	value := decimalToFloat(c.LatestValue)
	stale := c.Stale
	return openapi.MacroComponentStatus{
		SeriesCode:            &series,
		Source:                &source,
		LatestObservationDate: &date,
		LatestValue:           &value,
		Stale:                 &stale,
	}
}

func toOpenAPIHistoryPoints(points []HistoryPoint) []openapi.MacroStateHistoryPoint {
	out := make([]openapi.MacroStateHistoryPoint, 0, len(points))
	for _, p := range points {
		date := types.Date{Time: p.AsOfDate}
		hp := openapi.MacroStateHistoryPoint{AsOfDate: &date}
		if p.BlendFactor != nil {
			hp.BlendFactor = new(decimalToFloat(*p.BlendFactor))
		}
		if p.HasZone {
			zone := openapi.MacroStateHistoryPointZone(p.Zone)
			hp.Zone = &zone
		}
		out = append(out, hp)
	}
	return out
}

func toOpenAPIBenchmarkPoints(points []BenchmarkPoint) []openapi.BenchmarkHistoryPoint {
	out := make([]openapi.BenchmarkHistoryPoint, 0, len(points))
	for _, p := range points {
		date := types.Date{Time: p.Date}
		source := openapi.PriceSource(p.Source)
		out = append(out, openapi.BenchmarkHistoryPoint{
			Symbol:      p.Symbol,
			Date:        date,
			Close:       p.Close,
			PriceSource: &source,
		})
	}
	return out
}

func macroZonePtr(zone string) *openapi.MacroStateZone {
	z := openapi.MacroStateZone(zone)
	return &z
}

// decimalToFloat converts to the nearest float64.
func decimalToFloat(d decimal.Decimal) float64 { return d.InexactFloat64() }
