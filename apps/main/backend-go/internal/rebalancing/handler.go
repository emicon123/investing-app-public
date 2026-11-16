package rebalancing

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface method for
// RebalancingTransferController's endpoint.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// CreateRebalancingTransfer implements POST /rebalancing-transfers.
func (h *Handler) CreateRebalancingTransfer(w http.ResponseWriter, r *http.Request) {
	var body openapi.RebalancingTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Message: "Invalid request body"})
		return
	}
	req := TransferRequest{
		Portfolio: string(body.Portfolio),
		Sold:      toLeg(body.Sold),
	}
	for _, b := range body.Buys {
		req.Buys = append(req.Buys, toLeg(b))
	}
	result, err := h.service.ExecuteTransfer(r.Context(), req)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	resp := openapi.RebalancingTransferResponse{Meta: newOpenAPIMeta(r.Context())}
	resp.Data.AffectedTickers = result.AffectedTickers
	resp.Data.Message = result.Message
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func toLeg(l openapi.RebalancingTransferLeg) Leg {
	leg := Leg{
		Ticker: l.Ticker, AccountType: string(l.AccountType),
		Units: l.Units, PricePln: l.PricePln,
	}
	if l.RegularCurrency != nil {
		c := string(*l.RegularCurrency)
		leg.RegularCurrency = &c
	}
	return leg
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
