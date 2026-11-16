package inflationsync

import (
	"net/http"

	"investing-app/common-go/httpx"
)

// Handler answers POST /inflation/sync (operationId syncFromGus) for the apps that mount it:
// portfolioB and portfolioC embed it as their generated ServerInterface's inflation part, main forwards
// its own SyncInflationFromGus to it. No business logic here; service -> wire mapping only.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// SyncFromGus implements POST /inflation/sync. Always answers 200 with the real-change upsert
// count (0 when already in sync — never an error for "nothing to do"), in the shared
// SingleResponse {data: {upserts}, meta} envelope.
func (h *Handler) SyncFromGus(w http.ResponseWriter, r *http.Request) {
	upserts, err := h.service.Sync(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(struct {
		Upserts int `json:"upserts"`
	}{Upserts: upserts}))
}
