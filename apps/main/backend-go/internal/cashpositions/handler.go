package cashpositions

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the cash-positions
// endpoints — the same thin-Facade convention as marketdata.Handler (no business logic here;
// service -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// GetCashPositions implements GET /cash-positions — the direct port of
// CashPositionsController.getLatestSnapshot. Answers 200 with the latest snapshot's rows;
// an empty table answers 200 {data:[],meta} (httpx.List normalizes nil to []), never 404.
func (h *Handler) GetCashPositions(w http.ResponseWriter, r *http.Request) {
	positions, err := h.service.LatestSnapshot(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List(toOpenAPIPositions(positions)))
}

// CreateCashSnapshot implements POST /cash-positions. Answers 201 with the saved snapshot rows.
// Body validation failures answer 422 per docs/openapi.yaml.
//
// NOTE on required fields: positions/recordedAt presence is checked on the raw map first — the
// generated request type cannot distinguish "absent" from zero values (same technique as macro's
// decodeManualM2).
func (h *Handler) CreateCashSnapshot(w http.ResponseWriter, r *http.Request) {
	items, recordedAt, appErr := decodeCreateBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	saved, err := h.service.CreateSnapshot(r.Context(), items, recordedAt)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.List(toOpenAPIPositions(saved)))
}

// createCashPositionsRequest mirrors openapi.CreateCashPositionsRequest with amountPln as a
// decimal (task 182 / F3.4). CreateCashPositionsRequest is not on D174 §8.2's x-go-type list, so
// it is decoded through this hand-written struct instead (A-Q5 fallback).
type createCashPositionsRequest struct {
	Positions []struct {
		Location  string          `json:"location"`
		AmountPln decimal.Decimal `json:"amountPln"`
	} `json:"positions"`
	RecordedAt time.Time `json:"recordedAt"`
}

func decodeCreateBody(r *http.Request) ([]Item, time.Time, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, time.Time{}, unprocessable("", "Invalid request body")
	}
	for _, field := range []string{"positions", "recordedAt"} {
		if _, ok := raw[field]; !ok {
			return nil, time.Time{}, unprocessable(field, "must not be null")
		}
	}
	var body createCashPositionsRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return nil, time.Time{}, unprocessable("", "Invalid request body")
	}
	// An explicit JSON null for positions decodes without error to a nil slice — the
	// service's @NotEmpty port (empty → 422 on "positions") is what rejects it.
	items := make([]Item, 0, len(body.Positions))
	for _, p := range body.Positions {
		items = append(items, Item{Location: p.Location, AmountPln: p.AmountPln})
	}
	return items, body.RecordedAt, nil
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────

func toOpenAPIPosition(d PositionDTO) openapi.CashPosition {
	id := d.ID
	recordedAt := d.RecordedAt.UTC() // rendered as Z (UTC); same instant, byte-identical text
	return openapi.CashPosition{
		Id: &id, Location: d.Location,
		AmountPln:  d.AmountPln.InexactFloat64(),
		RecordedAt: &recordedAt,
	}
}

func toOpenAPIPositions(dtos []PositionDTO) []openapi.CashPosition {
	out := make([]openapi.CashPosition, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, toOpenAPIPosition(d))
	}
	return out
}
