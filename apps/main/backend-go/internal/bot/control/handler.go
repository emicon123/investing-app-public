package control

import (
	"encoding/json"
	"net/http"

	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for
// BotControlController's endpoints — the same thin-Facade convention as prior
// phases (no business logic here; service -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// GetBotControl implements GET /bot/control (human-only) — the direct port of
// BotControlController.get.
func (h *Handler) GetBotControl(w http.ResponseWriter, r *http.Request) {
	dto, err := h.service.Get(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotControlResponse{
		Data: toOpenAPIControl(dto), Meta: botutil.NewMeta(),
	})
}

// UpdateBotControl implements PATCH /bot/control (human-only) — the direct port of
// BotControlController.update. A missing tradingEnabled answers 422.
func (h *Handler) UpdateBotControl(w http.ResponseWriter, r *http.Request) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		httpx.WriteError(w, botutil.Unprocessable("", "Invalid request body"))
		return
	}
	bodyRaw, _ := json.Marshal(raw)
	var body openapi.UpdateBotControlRequest
	if err := json.Unmarshal(bodyRaw, &body); err != nil {
		httpx.WriteError(w, botutil.Unprocessable("", "Invalid request body"))
		return
	}
	var enabled *bool
	if v, ok := raw["tradingEnabled"]; ok && string(v) != "null" {
		b := body.TradingEnabled
		enabled = &b
	}
	dto, err := h.service.Update(r.Context(), enabled, body.PausedReason)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotControlResponse{
		Data: toOpenAPIControl(dto), Meta: botutil.NewMeta(),
	})
}

func toOpenAPIControl(d ControlDTO) openapi.BotControl {
	return openapi.BotControl{
		PausedReason:   d.PausedReason,
		TradingEnabled: d.TradingEnabled,
		UpdatedAt:      d.UpdatedAt.UTC(),
	}
}
