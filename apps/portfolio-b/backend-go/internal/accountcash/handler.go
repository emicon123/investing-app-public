package accountcash

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/accountcash"
	"investing-app/common-go/httpx"
)

// Handler adapts the shared accountcash.Service to this app's generated
// ServerInterface methods for GET /account-cash, PUT /account-cash and
// POST /account-cash/deduct — the same thin-Facade convention as
// internal/inflation (no business logic here; request -> service -> wire only).
//
// The responses are written RAW with httpx.WriteJSON — deliberately NOT wrapped in httpx.Single:
// the endpoints return the bare AccountCashDto, and the OpenAPI bundle shows no SingleResponse
// wrapper on these three endpoints.
//
// Wire fidelity note: the handler writes its own float64 wire struct with the identical JSON shape
// instead of the generated openapi.AccountCashDto (which was float32-typed before task 182 added
// `format: double` to the spec).
type Handler struct {
	service *accountcash.Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *accountcash.Service) *Handler { return &Handler{service: service} }

// Get implements GET /account-cash.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	snap, err := h.service.GetCurrent(r.Context(), "")
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWire(snap))
}

// Update implements PUT /account-cash.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var body wireCashAmounts
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, httpx.NewValidationError("", "malformed JSON request body"))
		return
	}
	vals, err := checked(body.vals())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	snap, err := h.service.UpdateManual(r.Context(), "", vals)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWire(snap))
}

// Deduct implements POST /account-cash/deduct.
func (h *Handler) Deduct(w http.ResponseWriter, r *http.Request) {
	var body wireCashAmounts
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, httpx.NewValidationError("", "malformed JSON request body"))
		return
	}
	vals, err := checked(body.vals())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	snap, err := h.service.Deduct(r.Context(), "", vals)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWire(snap))
}

// wireCashAmounts is the body of PUT /account-cash and POST /account-cash/deduct. Amounts decode
// as decimals (task 182 / F3.4; the wire still takes JSON numbers); an absent or null amount is
// zero, as it was with the former *float64 fields.
type wireCashAmounts struct {
	IkeTata  decimal.Decimal `json:"ikeTata"`
	IkeMama  decimal.Decimal `json:"ikeMama"`
	IkzeTata decimal.Decimal `json:"ikzeTata"`
	IkzeMama decimal.Decimal `json:"ikzeMama"`
}

func (b wireCashAmounts) vals() map[string]decimal.Decimal {
	return map[string]decimal.Decimal{
		KeyIkeTata:  b.IkeTata,
		KeyIkeMama:  b.IkeMama,
		KeyIkzeTata: b.IkzeTata,
		KeyIkzeMama: b.IkzeMama,
	}
}

func checked(vals map[string]decimal.Decimal) (map[string]decimal.Decimal, error) {
	if err := accountcash.CheckNonNegative(vals); err != nil {
		return nil, err
	}
	return vals, nil
}

// ── Snapshot -> wire mapping (AccountCashDto JSON shape, float64 numbers) ──

type wireCash struct {
	IkeTata      float64   `json:"ikeTata"`
	IkeMama      float64   `json:"ikeMama"`
	IkzeTata     float64   `json:"ikzeTata"`
	IkzeMama     float64   `json:"ikzeMama"`
	UpdatedAt    time.Time `json:"updatedAt"`
	UpdateReason string    `json:"updateReason"`
}

func toWire(snap accountcash.Snapshot) wireCash {
	return wireCash{
		IkeTata:      snap.Balances[KeyIkeTata].InexactFloat64(),
		IkeMama:      snap.Balances[KeyIkeMama].InexactFloat64(),
		IkzeTata:     snap.Balances[KeyIkzeTata].InexactFloat64(),
		IkzeMama:     snap.Balances[KeyIkzeMama].InexactFloat64(),
		UpdatedAt:    snap.UpdatedAt.UTC(),
		UpdateReason: snap.UpdateReason,
	}
}
