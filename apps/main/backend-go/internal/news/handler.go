package news

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/internal/push"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the news endpoints — the
// same thin-Facade convention as the other domains (no business logic here; service -> openapi type
// mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListNews implements GET /news(?date) — the direct port of NewsController.listNews.
// Absent ?date serves the latest date with news; no news at all answers 200 [].
func (h *Handler) ListNews(w http.ResponseWriter, r *http.Request, params openapi.ListNewsParams) {
	var date *time.Time
	if params.Date != nil {
		t := params.Date.Time
		date = &t
	}
	result, err := h.service.List(r.Context(), date)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.NewsItem, 0, len(result.Data))
	for _, d := range result.Data {
		data = append(data, toOpenAPIItem(d))
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.NewsListResponse{Data: data, Meta: newOpenAPIMeta(r.Context())})
}

// ListNewsDates implements GET /news/dates — the direct port of
// NewsController.listDates. Distinct dates, most recent first.
func (h *Handler) ListNewsDates(w http.ResponseWriter, r *http.Request) {
	dates, err := h.service.Dates(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]types.Date, 0, len(dates))
	for _, d := range dates {
		data = append(data, types.Date{Time: d})
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.NewsDatesListResponse{Data: data, Meta: newOpenAPIMeta(r.Context())})
}

// ListNewsAssets implements GET /news/assets (machine-only) — the direct port of
// NewsController.listAssets. Auth (X-Service-Token) is enforced by the router-level
// middleware, not here.
func (h *Handler) ListNewsAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := h.service.Assets(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.NewsAsset, 0, len(assets))
	for _, a := range assets {
		history := make([]openapi.NewsHistoryItem, 0, len(a.RecentHistory))
		for _, hh := range a.RecentHistory {
			history = append(history, openapi.NewsHistoryItem{
				Headline: hh.Headline,
				NewsDate: types.Date{Time: hh.NewsDate},
				Summary:  hh.Summary,
			})
		}
		data = append(data, openapi.NewsAsset{
			AssetName: a.AssetName, RecentHistory: history, Ticker: a.Ticker,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.NewsAssetsListResponse{Data: data, Meta: newOpenAPIMeta(r.Context())})
}

// IngestNews implements POST /news/ingest (machine-only). Answers 200 {data:{upserted},meta};
// validation failures answer 422 per docs/openapi.yaml.
func (h *Handler) IngestNews(w http.ResponseWriter, r *http.Request) {
	items, appErr := decodeIngestBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	upserted, err := h.service.Ingest(r.Context(), items)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.IngestNewsResponse{
		Data: struct {
			Upserted int `json:"upserted"`
		}{Upserted: upserted},
		Meta: newOpenAPIMeta(r.Context()),
	})
}

// decodeIngestBody maps the wire request to service inputs. Presence of the top-level
// items member is checked on the raw map first (explicit null included — a JSON null
// unmarshals into a nil slice, which Service.Ingest then rejects as empty, keeping
// "missing" and "null" both at 422); per-item value rules live in Service.Ingest.
func decodeIngestBody(r *http.Request) ([]IngestItem, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, unprocessable("", "Invalid request body")
	}
	bodyRaw, ok := raw["items"]
	if !ok || string(bodyRaw) == "null" {
		return nil, unprocessable("items", "must not be null")
	}
	var body openapi.IngestNewsRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return nil, unprocessable("", "Invalid request body")
	}
	items := make([]IngestItem, 0, len(body.Items))
	for _, it := range body.Items {
		in := IngestItem{
			Ticker: it.Ticker, AssetName: it.AssetName,
			Headline: it.Headline, Summary: it.Summary,
			NewsDate: it.NewsDate.Time,
		}
		if it.Sentiment != nil {
			s := string(*it.Sentiment)
			in.Sentiment = &s
		}
		if it.Sources != nil {
			for _, s := range *it.Sources {
				in.Sources = append(in.Sources, SourceDTO{Title: s.Title, URL: s.Url})
			}
		}
		items = append(items, in)
	}
	return items, nil
}

// ── Service -> openapi type mapping ──────────────────────────────────────────

func toOpenAPIItem(d ItemDTO) openapi.NewsItem {
	id := d.ID
	createdAt := d.CreatedAt.UTC() // rendered as Z (UTC); same instant, byte-identical text
	updatedAt := d.UpdatedAt.UTC()
	sources := make([]openapi.NewsSource, 0, len(d.Sources))
	for _, s := range d.Sources {
		sources = append(sources, openapi.NewsSource{Title: s.Title, Url: s.URL})
	}
	out := openapi.NewsItem{
		AssetName: d.AssetName, CreatedAt: &createdAt,
		Headline: d.Headline, Id: &id,
		NewsDate: types.Date{Time: d.NewsDate},
		Sources:  &sources, Summary: d.Summary,
		Ticker: d.Ticker, UpdatedAt: &updatedAt,
	}
	if d.Sentiment != nil {
		s := openapi.NewsSentiment(*d.Sentiment)
		out.Sentiment = &s
	}
	return out
}

// newOpenAPIMeta builds the generated openapi.Meta for the response shapes that carry their own
// meta field instead of an httpx envelope; its requestId is the request's X-Request-Id
// (httpx.NewMetaFrom), the id the access and error logs carry (task 182 / F4.2).
func newOpenAPIMeta(ctx context.Context) openapi.Meta {
	m := httpx.NewMetaFrom(ctx)
	id := types.UUID(m.RequestID)
	ts := m.Timestamp
	return openapi.Meta{RequestId: &id, Timestamp: &ts}
}

// NotifyNewsReport implements POST /news/notifications (machine-only, X-Service-Token): the
// morning job's structured outcome becomes the fixed ADR-108 §5 copy, fanned out to every device.
// 200 with the delivery report; 422 on a bad body; 503 PUSH_NOT_CONFIGURED without VAPID keys.
func (h *Handler) NotifyNewsReport(w http.ResponseWriter, r *http.Request) {
	var body openapi.NotifyNewsReportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, unprocessable("", "Invalid request body"))
		return
	}
	rep, err := h.service.NotifyReport(r.Context(), ReportOutcome{
		Outcome: Outcome(body.Outcome), NewsDate: body.NewsDate.Time, ItemCount: body.ItemCount,
	})
	if err != nil {
		httpx.WriteError(w, push.MapError(err))
		return
	}
	push.WriteReport(r.Context(), w, rep)
}
