package signal

import (
	"encoding/json"
	"net/http"

	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for
// BotSignalController's endpoints — the same thin-Facade convention as prior
// phases (no business logic here; service -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListBotSignals implements GET /bot/signals (human-only) — the direct port of
// BotSignalController.list. Newest first, paged.
func (h *Handler) ListBotSignals(w http.ResponseWriter, r *http.Request, params openapi.ListBotSignalsParams) {
	page, size, appErr := botutil.PageSize(params.Page, params.Size, 0, 20)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	var eventName string
	if params.EventName != nil {
		eventName = *params.EventName
	}
	result, err := h.service.List(r.Context(), eventName, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.BotSignal, 0, len(result.Data))
	for _, d := range result.Data {
		data = append(data, toOpenAPISignal(d))
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotSignalPagedResponse{
		Data: data, Meta: botutil.NewMeta(),
		Page: openapi.PageInfo{Number: result.Page, Size: result.Size, Total: int(result.Total)},
	})
}

// CreateBotSignal implements POST /bot/signals (machine-only) — the direct port of
// BotSignalController.create. Answers 201 with executed=false; validation failures
// answer 422 per docs/openapi.yaml (NOT Java's 400).
func (h *Handler) CreateBotSignal(w http.ResponseWriter, r *http.Request) {
	in, appErr := decodeCreateBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	dto, err := h.service.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, openapi.BotSignalSingleResponse{
		Data: toOpenAPISignal(dto), Meta: botutil.NewMeta(),
	})
}

// decodeCreateBody maps the wire request to service inputs. Presence of the required
// members is checked on the raw map first (explicit null included); value rules live
// in Service.Create.
func decodeCreateBody(r *http.Request) (CreateInput, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return CreateInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	for _, field := range []string{"eventName", "actual", "decision"} {
		if v, ok := raw[field]; !ok || string(v) == "null" {
			return CreateInput{}, botutil.Unprocessable(field, "must not be null")
		}
	}
	var body openapi.CreateBotSignalRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return CreateInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	in := CreateInput{
		CalendarEventID: body.CalendarEventId, PlanScenarioID: body.PlanScenarioId,
		EventName: body.EventName, Actual: body.Actual,
		Forecast: body.Forecast, Previous: body.Previous,
		SurpriseDesc: body.SurpriseDesc, Decision: string(body.Decision),
	}
	if body.RawPayload != nil {
		in.RawPayload = *body.RawPayload
	}
	return in, nil
}

func toOpenAPISignal(d SignalDTO) openapi.BotSignal {
	out := openapi.BotSignal{
		Actual: d.Actual, CalendarEventId: d.CalendarEventID,
		CreatedAt: d.CreatedAt.UTC(), Decision: openapi.BotSignalDecision(d.Decision),
		EventName: d.EventName, Executed: d.Executed,
		Forecast: d.Forecast, Id: d.ID, PlanScenarioId: d.PlanScenarioID,
		Previous: d.Previous, SurpriseDesc: d.SurpriseDesc,
	}
	if d.RawPayload != nil {
		m := map[string]any(d.RawPayload)
		out.RawPayload = &m
	}
	return out
}
