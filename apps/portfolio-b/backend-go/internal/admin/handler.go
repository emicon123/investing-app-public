package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	scsnapshot "investing-app/common-go/siblingcore/snapshot"
)

// Handler adapts Service to the generated ServerInterface method for
// POST /admin/history/backfill-position-values — the same thin-Facade convention
// as the other domains (no business logic here).
//
// The response is written RAW with httpx.WriteJSON — deliberately NOT wrapped in httpx.Single: the
// endpoint returns a bare map ({"updated","skipped","warnings"}), with no envelope.
//
// historiaSnapshotter is a second, unrelated ops entry point added by ADR-097 —
// the historia-snapshot backfill (POST /admin/historia/backfill-snapshot), placed
// on this same Handler/package rather than a new one per that ADR's Decision §2
// ("added to the existing internal/admin package/handler ... as a second
// method — no new package needed").
type Handler struct {
	service  *Service
	historia *scsnapshot.Snapshotter
}

// NewHandler constructs a Handler around service and the historia snapshotter
// (ADR-097's second ops entry point on this package).
func NewHandler(service *Service, historiaSnapshotter *scsnapshot.Snapshotter) *Handler {
	return &Handler{service: service, historia: historiaSnapshotter}
}

// Backfill implements POST /admin/history/backfill-position-values — the direct
// port of AdminController.backfill.
func (h *Handler) Backfill(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Backfill(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	warnings := result.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"updated":  result.Updated,
		"skipped":  result.Skipped,
		"warnings": warnings,
	})
}

// backfillSnapshotRequest is POST /admin/historia/backfill-snapshot's body —
// {"date":"2026-09-18"}. ADR-097 Decision §2: this route is deliberately NOT part
// of docs/openapi.yaml — a one-shot internal ops entry point, called directly
// against the container (docker compose exec / container-internal port), never
// proxied through Nginx and never linked from any UI.
type backfillSnapshotRequest struct {
	Date string `json:"date"`
}

// BackfillSnapshot implements POST /admin/historia/backfill-snapshot (ADR-097
// Decision §2's chosen recovery mechanism for a missed nightly snapshot, e.g. the
// 2026-09-18 gap this ADR documents). This app has a single portfolio (no
// portfolio dimension), so one call backfills the one snapshot row for date.
// Callers are responsible for ADR-097 Decision §3's mandatory trade-window
// sanity check before calling this endpoint — it is an ops runbook step, not
// enforced here.
func (h *Handler) BackfillSnapshot(w http.ResponseWriter, r *http.Request) {
	var body backfillSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, httpx.NewValidationError("date", "Invalid request body"))
		return
	}
	date, err := time.Parse("2006-01-02", body.Date)
	if err != nil {
		httpx.WriteError(w, httpx.NewValidationError("date", "date must be formatted YYYY-MM-DD"))
		return
	}
	if err := h.historia.BackfillSnapshot(r.Context(), siblingcore.WholeBook, date); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"date":   body.Date,
		"result": "ok",
	})
}
