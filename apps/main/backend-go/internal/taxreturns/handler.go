package taxreturns

import (
	"encoding/json"
	"net/http"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the tax-returns endpoints
// — the same thin-Facade convention as marketdata.Handler (no business logic here; service ->
// openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListTaxReturns implements GET /tax-returns — the direct port of
// TaxReturnsController.listTaxReturns. Paginated (default size 20), newest year first.
func (h *Handler) ListTaxReturns(w http.ResponseWriter, r *http.Request, params openapi.ListTaxReturnsParams) {
	page, err := h.service.List(r.Context(), params.Page, params.Size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(toOpenAPIReturns(page.Items), httpx.PageInfo{
		Number: page.Number, Size: page.Size, Total: page.Total,
	}))
}

// CreateTaxReturn implements POST /tax-returns. Answers 201, or 409 DUPLICATE_ENTRY when the year
// already exists. Body validation failures answer 422 per docs/openapi.yaml.
//
// NOTE on required fields: the generated request type uses plain int / decimal.Decimal (x-go-type,
// task 182), so "absent" decodes as 0 — presence is checked on the raw map first (same technique as
// macro's decodeManualM2).
func (h *Handler) CreateTaxReturn(w http.ResponseWriter, r *http.Request) {
	year, gross, tax, rate, appErr := decodeCreateBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	dto, err := h.service.Create(r.Context(), year, gross, tax, rate)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(toOpenAPIReturn(dto)))
}

func decodeCreateBody(r *http.Request) (int, decimal.Decimal, decimal.Decimal, decimal.Decimal, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return 0, decimal.Zero, decimal.Zero, decimal.Zero, unprocessable("", "Invalid request body")
	}
	for _, field := range []string{"year", "grossGain", "taxPaid", "effectiveRatePct"} {
		if _, ok := raw[field]; !ok {
			return 0, decimal.Zero, decimal.Zero, decimal.Zero, unprocessable(field, "must not be null")
		}
	}
	var body openapi.CreateTaxReturnRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return 0, decimal.Zero, decimal.Zero, decimal.Zero, unprocessable("", "Invalid request body")
	}
	return body.Year, body.GrossGain, body.TaxPaid, body.EffectiveRatePct, nil
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────

func toOpenAPIReturn(d ReturnDTO) openapi.TaxReturn {
	id := d.ID
	createdAt := d.CreatedAt.UTC() // rendered as Z (UTC); same instant, byte-identical text
	updatedAt := d.UpdatedAt.UTC()
	return openapi.TaxReturn{
		Id: &id, Year: d.Year,
		GrossGain:        d.GrossGain.InexactFloat64(),
		TaxPaid:          d.TaxPaid.InexactFloat64(),
		EffectiveRatePct: d.EffectiveRatePct.InexactFloat64(),
		CreatedAt:        &createdAt, UpdatedAt: &updatedAt,
	}
}

func toOpenAPIReturns(dtos []ReturnDTO) []openapi.TaxReturn {
	out := make([]openapi.TaxReturn, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, toOpenAPIReturn(d))
	}
	return out
}
