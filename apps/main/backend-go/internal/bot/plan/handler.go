package plan

import (
	"encoding/json"
	"net/http"

	"github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for
// BotPlanController's endpoints — the same thin-Facade convention as prior phases
// (no business logic here; service -> openapi type mapping only).
//
// Routing note (binding quirk): GET /bot/plans/latest is registered as a static
// route and GET /bot/plans/{date} as a param route — chi gives the static route
// precedence, so "latest" never binds as a date. Covered by a handler-level test.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListBotPlans implements GET /bot/plans (human-only) — the direct port of
// BotPlanController.list. planDate ASC, paged.
func (h *Handler) ListBotPlans(w http.ResponseWriter, r *http.Request, params openapi.ListBotPlansParams) {
	page, size, appErr := botutil.PageSize(params.Page, params.Size, 0, 20)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	result, err := h.service.List(r.Context(), page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.BotDailyPlan, 0, len(result.Data))
	for _, d := range result.Data {
		data = append(data, toOpenAPIPlan(d))
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotDailyPlanPagedResponse{
		Data: data, Meta: botutil.NewMeta(),
		Page: openapi.PageInfo{Number: result.Page, Size: result.Size, Total: int(result.Total)},
	})
}

// GetLatestBotPlan implements GET /bot/plans/latest (human-only) — the direct port
// of BotPlanController.latest. No plans yet answers 404.
func (h *Handler) GetLatestBotPlan(w http.ResponseWriter, r *http.Request) {
	dto, err := h.service.Latest(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotDailyPlanSingleResponse{
		Data: toOpenAPIPlan(dto), Meta: botutil.NewMeta(),
	})
}

// GetBotPlanByDate implements GET /bot/plans/{date} (human-only) — the direct port
// of BotPlanController.byDate. Unknown date answers 404.
func (h *Handler) GetBotPlanByDate(w http.ResponseWriter, r *http.Request, date types.Date) {
	dto, err := h.service.ByDate(r.Context(), date.Time)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotDailyPlanSingleResponse{
		Data: toOpenAPIPlan(dto), Meta: botutil.NewMeta(),
	})
}

// IngestBotPlan implements POST /bot/plans/ingest (machine-only) — the direct port
// of BotPlanController.ingest. Answers 200 with the stored plan; ladder violations
// answer 422 per docs/openapi.yaml (NOT Java's 400).
func (h *Handler) IngestBotPlan(w http.ResponseWriter, r *http.Request) {
	in, appErr := decodeIngestBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	dto, err := h.service.Ingest(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotDailyPlanSingleResponse{
		Data: toOpenAPIPlan(dto), Meta: botutil.NewMeta(),
	})
}

// decodeIngestBody maps the wire request to service inputs. planDate presence is
// checked on the raw map (explicit null included); per-scenario value rules live in
// Service.Ingest. Numbers arrive as float64 and convert to decimal exactly for the
// 4dp/2dp bound columns.
func decodeIngestBody(r *http.Request) (IngestInput, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return IngestInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	if v, ok := raw["planDate"]; !ok || string(v) == "null" {
		return IngestInput{}, botutil.Unprocessable("planDate", "must not be null")
	}
	var body openapi.IngestBotPlanRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return IngestInput{}, botutil.Unprocessable("", "Invalid request body")
	}
	in := IngestInput{
		PlanDate: body.PlanDate.Time,
		Summary:  body.Summary, Model: body.Model, Status: body.Status,
	}
	if body.MarketContext != nil {
		in.MarketContext = *body.MarketContext
	}
	if body.Scenarios != nil {
		for _, sc := range *body.Scenarios {
			out := IngestScenario{
				CalendarEventID: sc.CalendarEventId, EventName: sc.EventName,
				EventTime: sc.EventTime, ScenarioLabel: sc.ScenarioLabel,
				TriggerCondition: sc.TriggerCondition, Action: string(sc.Action),
				Reasoning: sc.Reasoning,
			}
			if sc.Leverage != nil {
				v := *sc.Leverage
				out.Leverage = &v
			}
			if sc.SurpriseMin != nil {
				v := decimal.NewFromFloat(*sc.SurpriseMin)
				out.SurpriseMin = &v
			}
			if sc.SurpriseMax != nil {
				v := decimal.NewFromFloat(*sc.SurpriseMax)
				out.SurpriseMax = &v
			}
			if sc.Confidence != nil {
				v := decimal.NewFromFloat(*sc.Confidence)
				out.Confidence = &v
			}
			in.Scenarios = append(in.Scenarios, out)
		}
	}
	return in, nil
}

func toOpenAPIPlan(d PlanDTO) openapi.BotDailyPlan {
	scenarios := make([]openapi.BotPlanScenario, 0, len(d.Scenarios))
	for _, sc := range d.Scenarios {
		out := openapi.BotPlanScenario{
			Action:          openapi.BotPlanScenarioAction(sc.Action),
			CalendarEventId: sc.CalendarEventID,
			EventName:       sc.EventName, EventTime: sc.EventTime.UTC(), Id: sc.ID,
			Reasoning:        sc.Reasoning,
			ScenarioLabel:    openapi.BotPlanScenarioScenarioLabel(sc.ScenarioLabel),
			TriggerCondition: sc.TriggerCondition,
		}
		if sc.Leverage != nil {
			v := *sc.Leverage
			out.Leverage = &v
		}
		if sc.SurpriseMin != nil {
			v := sc.SurpriseMin.InexactFloat64()
			out.SurpriseMin = &v
		}
		if sc.SurpriseMax != nil {
			v := sc.SurpriseMax.InexactFloat64()
			out.SurpriseMax = &v
		}
		if sc.Confidence != nil {
			v := sc.Confidence.InexactFloat64()
			out.Confidence = &v
		}
		scenarios = append(scenarios, out)
	}
	out := openapi.BotDailyPlan{
		Id: d.ID, PlanDate: types.Date{Time: d.PlanDate},
		Status:    openapi.BotDailyPlanStatus(d.Status),
		Scenarios: scenarios, Summary: d.Summary, Model: d.Model,
	}
	if d.MarketContext != nil {
		m := map[string]any(d.MarketContext)
		out.MarketContext = &m
	}
	return out
}
