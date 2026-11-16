// Package positiontransfer is portfolioB's HTTP adapter for POST /position-transfers, the manual
// "Sprzedaj" sell-assist (task 151 Finding 1); the service is common-go/siblingcore/transfer
// (ADR-113). Every leg names its account type (book's buckets); a missing or unknown one is a 400
// before any write (task 180 D1).
package positiontransfer

import (
	"net/http"

	"investing-app/common-go/httpx"
	sctransfer "investing-app/common-go/siblingcore/transfer"
	"investing-app/common-go/siblingcore/wire"
	"portfolio-b/backend-go/internal/book"
)

// Handler adapts the siblingcore transfer service to the ServerInterface's Execute method.
type Handler struct {
	service *sctransfer.Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *sctransfer.Service) *Handler { return &Handler{service: service} }

// Execute implements POST /position-transfers.
func (h *Handler) Execute(w http.ResponseWriter, r *http.Request) {
	var body wire.TransferRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	req, err := body.ToRequest(book.Config)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.ExecuteTransfer(r.Context(), req)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(wire.FromTransfer(result, book.Config)))
}
