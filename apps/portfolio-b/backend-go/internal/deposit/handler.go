// Package deposit is portfolioB's HTTP adapter for the deposits endpoints; the service is
// common-go/siblingcore/deposit (ADR-113). portfolioB has one book, so every call is WholeBook.
package deposit

import (
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	scdeposit "investing-app/common-go/siblingcore/deposit"
	"investing-app/common-go/siblingcore/wire"
	"portfolio-b/backend-go/openapi"
)

// Handler adapts the siblingcore deposit service to the oapi-codegen ServerInterface methods.
// Wire fidelity note: docs/openapi.yaml's DepositDto/XirrData numbers are bare `number`
// (float32 generated); every response here uses a local float64 shape with the identical JSON
// field names instead, and the request a decimal one.
type Handler struct {
	service *scdeposit.Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *scdeposit.Service) *Handler { return &Handler{service: service} }

const defaultPageSizeParam = 50

// ListDeposits implements GET /deposits.
func (h *Handler) ListDeposits(w http.ResponseWriter, r *http.Request, params openapi.ListDepositsParams) {
	page := 0
	if params.Pageable.Page != nil {
		page = int(*params.Pageable.Page)
	}
	size := defaultPageSizeParam
	if params.Pageable.Size != nil {
		size = int(*params.Pageable.Size)
	}
	var year *int
	if params.Year != nil {
		v := int(*params.Year)
		year = &v
	}
	items, total, err := h.service.List(r.Context(), siblingcore.WholeBook, year, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]wire.Deposit, 0, len(items))
	for _, item := range items {
		data = append(data, wire.FromDeposit(item))
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(data, httpx.PageInfo{Number: page, Size: size, Total: total}))
}

// wireCreateDepositRequest is POST /deposits' body; the amount decodes as a decimal (task 182 /
// F3.4), the wire still takes a JSON number.
type wireCreateDepositRequest struct {
	DepositDate string          `json:"depositDate"`
	Amount      decimal.Decimal `json:"amount"`
	Comment     *string         `json:"comment,omitempty"`
}

// toDeposit maps the body to the service input (400 for a malformed date).
func (b wireCreateDepositRequest) toDeposit() (siblingcore.Deposit, error) {
	depositDate, err := time.Parse("2006-01-02", b.DepositDate)
	if err != nil {
		return siblingcore.Deposit{}, httpx.NewValidationError("depositDate", "must be a valid ISO-8601 date")
	}
	return siblingcore.Deposit{Portfolio: siblingcore.WholeBook, DepositDate: depositDate, Amount: b.Amount, Comment: b.Comment}, nil
}

// CreateDeposit implements POST /deposits.
func (h *Handler) CreateDeposit(w http.ResponseWriter, r *http.Request) {
	var body wireCreateDepositRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	deposit, err := body.toDeposit()
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	created, err := h.service.Create(r.Context(), deposit)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(wire.FromDeposit(created)))
}

// ComputeXirr implements GET /deposits/xirr.
func (h *Handler) ComputeXirr(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ComputeXirr(r.Context(), siblingcore.WholeBook)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(wire.FromXirr(result, time.Now())))
}
