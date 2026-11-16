package binancetransfers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the binance-transfers
// endpoints — the same thin-Facade convention as the other domains (no business logic here; service
// -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListBinanceTransfers implements GET /binance-transfers — the direct port of
// BinanceTransfersController.listBinanceTransfers. When ?year is supplied, data
// holds only that year's rows; yearlySums always covers all years (year DESC).
func (h *Handler) ListBinanceTransfers(w http.ResponseWriter, r *http.Request, params openapi.ListBinanceTransfersParams) {
	result, err := h.service.List(r.Context(), params.Year)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOpenAPIList(result, newOpenAPIMeta(r.Context())))
}

// CreateBinanceTransfer implements POST /binance-transfers. Answers 201 with the {data, meta}
// envelope. Body validation failures answer 422 per docs/openapi.yaml.
//
// NOTE on numerics: the generated request type carries decimal.Decimal (x-go-type, task 182), and
// an absent amountPln decodes as 0 — presence is checked on the raw map first and value rules live
// in Service.Create (same split as crypto-trades' decodeCreateBody + service range checks).
func (h *Handler) CreateBinanceTransfer(w http.ResponseWriter, r *http.Request) {
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
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(toOpenAPITransfer(dto)))
}

// DeleteBinanceTransfer implements DELETE /binance-transfers/{id} — the direct port
// of BinanceTransfersController.deleteBinanceTransfer. Answers 204 with no body;
// unknown id answers 404.
func (h *Handler) DeleteBinanceTransfer(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.service.Delete(r.Context(), id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requiredCreateFields mirrors CreateBinanceTransferRequest's @NotNull members —
// transferDate and amountPln must be present (explicit null included: a JSON null
// unmarshals into the zero value, which Service.Create then rejects on value
// rules, keeping "missing" and "null" both at 422 with the field named).
var requiredCreateFields = []string{"transferDate", "amountPln"}

func decodeCreateBody(r *http.Request) (CreateInput, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return CreateInput{}, unprocessable("", "Invalid request body")
	}
	for _, field := range requiredCreateFields {
		if _, ok := raw[field]; !ok {
			return CreateInput{}, unprocessable(field, "must not be null")
		}
	}
	var body openapi.CreateBinanceTransferRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return CreateInput{}, unprocessable("", "Invalid request body")
	}
	in := CreateInput{
		TransferDate: body.TransferDate.Time,
		AmountPln:    body.AmountPln,
		AmountNative: body.AmountNative,
		Comment:      body.Comment,
	}
	if body.NativeCurrency != nil {
		s := string(*body.NativeCurrency)
		in.NativeCurrency = &s
	}
	return in, nil
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────

func toOpenAPITransfer(d TransferDTO) openapi.BinanceTransfer {
	transferDate := types.Date{Time: d.TransferDate}
	createdAt := d.CreatedAt.UTC() // rendered as Z (UTC); same instant, byte-identical text
	amountPln := d.AmountPln.InexactFloat64()
	source := openapi.BinanceTransferSource(d.Source)
	out := openapi.BinanceTransfer{
		Id: &d.ID, TransferDate: transferDate, AmountPln: amountPln,
		Comment: d.Comment, Source: &source,
		SessionId: d.SessionID, CreatedAt: &createdAt,
	}
	if d.AmountNative != nil {
		v := d.AmountNative.InexactFloat64()
		out.AmountNative = &v
	}
	if d.NativeCurrency != nil {
		c := openapi.Currency(*d.NativeCurrency)
		out.NativeCurrency = &c
	}
	return out
}

func toOpenAPIList(res ListResult, meta openapi.Meta) openapi.BinanceTransfersListResponse {
	data := make([]openapi.BinanceTransfer, 0, len(res.Data))
	for _, d := range res.Data {
		data = append(data, toOpenAPITransfer(d))
	}
	sums := make([]openapi.BinanceTransferYearlySum, 0, len(res.YearlySums))
	for _, s := range res.YearlySums {
		sums = append(sums, openapi.BinanceTransferYearlySum{
			Year: s.Year, TotalPln: s.TotalPln.InexactFloat64(),
		})
	}
	return openapi.BinanceTransfersListResponse{
		Data: data, YearlySums: sums, Meta: meta,
	}
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
