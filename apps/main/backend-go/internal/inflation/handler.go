package inflation

import (
	"encoding/json"
	"net/http"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
	"investing-app/common-go/inflationsync"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the inflation endpoints —
// the same thin-Facade convention as marketdata.Handler (no business logic here; service -> openapi
// type mapping only).
type Handler struct {
	service *Service
	sync    *inflationsync.Handler
}

// NewHandler constructs a Handler around service (list/create) and the shared GUS sync.
func NewHandler(service *Service, sync *inflationsync.Service) *Handler {
	return &Handler{service: service, sync: inflationsync.NewHandler(sync)}
}

// ListInflation implements GET /inflation — the direct port of
// InflationController.listInflation. Optional ?year filter, paginated (default size 24),
// newest first.
func (h *Handler) ListInflation(w http.ResponseWriter, r *http.Request, params openapi.ListInflationParams) {
	page, err := h.service.List(r.Context(), params.Year, params.Page, params.Size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(toOpenAPIEntries(page.Items), httpx.PageInfo{
		Number: page.Number, Size: page.Size, Total: page.Total,
	}))
}

// CreateInflationEntry implements POST /inflation. Answers 201, or 409 DUPLICATE_ENTRY when (year,
// month) already exists. Body validation failures answer 422 per docs/openapi.yaml.
//
// NOTE on required fields: the generated request type uses plain int / decimal.Decimal (x-go-type,
// task 182), so "absent" decodes as 0 — presence is checked on the raw map first (same technique as
// macro's decodeManualM2).
func (h *Handler) CreateInflationEntry(w http.ResponseWriter, r *http.Request) {
	year, month, cpi, appErr := decodeCreateBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	dto, err := h.service.Create(r.Context(), year, month, cpi)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(toOpenAPIEntry(dto)))
}

// SyncInflationFromGus implements POST /inflation/sync — the direct port of
// InflationController.syncFromGus; the shared inflationsync.Handler answers it (200 with the
// real-change upsert count, 0 when already in sync).
func (h *Handler) SyncInflationFromGus(w http.ResponseWriter, r *http.Request) {
	h.sync.SyncFromGus(w, r)
}

func decodeCreateBody(r *http.Request) (int, int, decimal.Decimal, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return 0, 0, decimal.Zero, unprocessable("", "Invalid request body")
	}
	for _, field := range []string{"year", "month", "cpiValue"} {
		if _, ok := raw[field]; !ok {
			return 0, 0, decimal.Zero, unprocessable(field, "must not be null")
		}
	}
	var body openapi.CreateInflationRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return 0, 0, decimal.Zero, unprocessable("", "Invalid request body")
	}
	return body.Year, body.Month, body.CpiValue, nil
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────

func toOpenAPIEntry(d EntryDTO) openapi.InflationEntry {
	id := int64(d.ID)
	createdAt := d.CreatedAt.UTC() // rendered as Z (UTC); same instant, byte-identical text
	return openapi.InflationEntry{
		Id: &id, Year: d.Year, Month: d.Month,
		CpiValue: d.Cpi.InexactFloat64(), CreatedAt: &createdAt,
	}
}

func toOpenAPIEntries(dtos []EntryDTO) []openapi.InflationEntry {
	out := make([]openapi.InflationEntry, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, toOpenAPIEntry(d))
	}
	return out
}
