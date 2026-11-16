package accountcash

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/accountcash"
	"investing-app/common-go/httpx"
	"portfolio-c/backend-go/openapi"
)

// Handler adapts the shared accountcash.Service to this app's generated
// ServerInterface methods for GET/PUT /account-cash/{portfolio} and
// POST /account-cash/{portfolio}/deduct — the same thin-Facade convention as
// internal/inflation (no business logic here; request -> service -> wire only).
//
// The responses are written RAW with httpx.WriteJSON — deliberately NOT wrapped in httpx.Single:
// the endpoints return the bare AccountCashDto, and the OpenAPI bundle shows no SingleResponse
// wrapper on these three endpoints.
//
// Wire fidelity note: the handler writes its own float64 wire struct with the identical JSON shape
// (including the extra portfolio field) instead of the generated openapi.AccountCashDto (balancePln
// was float32-typed before task 182 added `format: double` to the spec).
type Handler struct {
	service *accountcash.Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *accountcash.Service) *Handler { return &Handler{service: service} }

// Get implements GET /account-cash/{portfolio} — the direct port of
// AccountCashController.get.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request, portfolio openapi.GetParamsPortfolio) {
	key, err := ValidPortfolio(string(portfolio))
	if err != nil {
		httpx.WriteError(w, httpx.NewValidationError("portfolio", err.Error()))
		return
	}
	snap, err := h.service.GetCurrent(r.Context(), key)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWire(key, snap))
}

// Update implements PUT /account-cash/{portfolio} — the direct port of
// AccountCashController.update.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request, portfolio openapi.UpdateParamsPortfolio) {
	key, err := ValidPortfolio(string(portfolio))
	if err != nil {
		httpx.WriteError(w, httpx.NewValidationError("portfolio", err.Error()))
		return
	}
	vals, err := decodeBody(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	snap, err := h.service.UpdateManual(r.Context(), key, vals)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWire(key, snap))
}

// Deduct implements POST /account-cash/{portfolio}/deduct — the direct port of
// AccountCashController.deduct.
func (h *Handler) Deduct(w http.ResponseWriter, r *http.Request, portfolio openapi.DeductParamsPortfolio) {
	key, err := ValidPortfolio(string(portfolio))
	if err != nil {
		httpx.WriteError(w, httpx.NewValidationError("portfolio", err.Error()))
		return
	}
	vals, err := decodeBody(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	snap, err := h.service.Deduct(r.Context(), key, vals)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWire(key, snap))
}

// decodeBody reads {"balancePln": ...} with @NotNull parity: a missing (or null)
// balancePln answers 400 — oapi-codegen would decode it as 0, so presence is
// checked on the raw map first. A negative amount answers 400 (@DecimalMin). The value is
// parsed straight from the JSON number as a decimal (task 182 / F3.4); a string, bool, object
// or array is 400 "must be a number".
func decodeBody(r *http.Request) (map[string]decimal.Decimal, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, httpx.NewValidationError("", "malformed JSON request body")
	}
	rawVal, ok := raw[BalancePln]
	if !ok || string(rawVal) == "null" {
		return nil, httpx.NewValidationError(BalancePln, "must not be null")
	}
	// rawVal is valid JSON, so only a number parses (a quoted value keeps its quotes and fails).
	v, err := decimal.NewFromString(string(rawVal))
	if err != nil {
		return nil, httpx.NewValidationError(BalancePln, "must be a number")
	}
	vals := map[string]decimal.Decimal{BalancePln: v}
	if err := accountcash.CheckNonNegative(vals); err != nil {
		return nil, err
	}
	return vals, nil
}

// ── Snapshot -> wire mapping (AccountCashDto JSON shape + portfolio, float64) ──

type wireCash struct {
	Portfolio    string    `json:"portfolio"`
	BalancePln   float64   `json:"balancePln"`
	UpdatedAt    time.Time `json:"updatedAt"`
	UpdateReason string    `json:"updateReason"`
}

func toWire(portfolio string, snap accountcash.Snapshot) wireCash {
	return wireCash{
		Portfolio:    portfolio,
		BalancePln:   snap.Balances[BalancePln].InexactFloat64(),
		UpdatedAt:    snap.UpdatedAt.UTC(),
		UpdateReason: snap.UpdateReason,
	}
}
