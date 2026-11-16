package calendar

import (
	"net/http"
	"time"

	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for
// BotCalendarController's endpoints — the same thin-Facade convention as prior
// phases (no business logic here; service -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListBotCalendar implements GET /bot/calendar (dual-audience read) — the direct
// port of BotCalendarController.list. Answers the paged envelope, eventTime ASC.
func (h *Handler) ListBotCalendar(w http.ResponseWriter, r *http.Request, params openapi.ListBotCalendarParams) {
	page, size, appErr := botutil.PageSize(params.Page, params.Size, 0, 50)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	var from, to *time.Time
	if params.From != nil {
		t := params.From.Time
		from = &t
	}
	if params.To != nil {
		t := params.To.Time
		to = &t
	}
	var impact, currency string
	if params.Impact != nil {
		impact = *params.Impact
	}
	if params.Currency != nil {
		currency = *params.Currency
	}
	result, err := h.service.List(r.Context(), from, to, impact, currency, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.BotCalendarEvent, 0, len(result.Data))
	for _, d := range result.Data {
		data = append(data, toOpenAPIEvent(d))
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotCalendarEventPagedResponse{
		Data: data,
		Meta: botutil.NewMeta(),
		Page: openapi.PageInfo{
			Number: result.Page, Size: result.Size, Total: int(result.Total),
		},
	})
}

// SyncBotCalendar implements POST /bot/calendar/sync (machine-only) — the direct
// port of BotCalendarController.sync. Answers 200 {data:{upserted},meta}.
func (h *Handler) SyncBotCalendar(w http.ResponseWriter, r *http.Request) {
	upserted, err := h.service.Sync(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.BotCalendarSyncResponse{
		Data: openapi.BotCalendarSyncResult{Upserted: upserted},
		Meta: botutil.NewMeta(),
	})
}

func toOpenAPIEvent(d EventDTO) openapi.BotCalendarEvent {
	return openapi.BotCalendarEvent{
		Actual: d.Actual, ActualSource: d.ActualSource, Country: d.Country,
		CreatedAt: d.CreatedAt.UTC(),
		Currency:  d.Currency, EventName: d.EventName,
		EventTime: d.EventTime.UTC(), ExternalId: d.ExternalID,
		Forecast: d.Forecast, Id: d.ID,
		Impact:   openapi.BotCalendarEventImpact(d.Impact),
		Previous: d.Previous, Source: d.Source,
	}
}
