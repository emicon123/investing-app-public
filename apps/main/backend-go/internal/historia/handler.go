package historia

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface method for the history endpoint — the
// same thin-Facade convention as inflation.Handler (no business logic here; service -> openapi type
// mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListHistoria implements GET /history: paginated daily snapshots with response-time computed
// returnPct/profitPln and best-effort inflation enrichment. Answers 200, or 400 VALIDATION_ERROR
// for an unknown ?portfolio or a negative ?page. Never writes.
func (h *Handler) ListHistoria(w http.ResponseWriter, r *http.Request, params openapi.ListHistoriaParams) {
	var portfolio *string
	if params.Portfolio != nil {
		s := string(*params.Portfolio)
		portfolio = &s
	}
	page, err := h.service.List(r.Context(), ListParams{
		Portfolio: portfolio,
		From:      openapiDateToTime(params.From),
		To:        openapiDateToTime(params.To),
		Page:      params.Page,
		Size:      params.Size,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(toOpenAPIEntries(page.Items), httpx.PageInfo{
		Number: page.Number, Size: page.Size, Total: page.Total,
	}))
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
// 2026-09-18 gap this ADR documents). Loops internally over trackedPortfolios
// (MAIN, RETIREMENT) — one call backfills both, no portfolio field in the request.
// A single portfolio's failure is captured per-portfolio in the response rather
// than aborting the other, mirroring StartScheduler's own per-portfolio fail-soft
// loop. Callers are responsible for ADR-097 Decision §3's mandatory trade-window
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
	results := make(map[string]string, len(trackedPortfolios))
	for _, portfolio := range trackedPortfolios {
		if err := h.service.BackfillSnapshot(r.Context(), portfolio, date); err != nil {
			results[portfolio] = "error: " + err.Error()
			continue
		}
		results[portfolio] = "ok"
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"date":    body.Date,
		"results": results,
	})
}

func openapiDateToTime(d *types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time
	return &t
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────

func toOpenAPIEntry(d EntryDTO) openapi.HistoriaEntry {
	id := d.ID
	portfolio := openapi.Portfolio(d.Portfolio)
	snapshotDate := types.Date{Time: d.SnapshotDate}
	totalDeposits := d.TotalDeposits.InexactFloat64()
	portfolioValue := d.PortfolioValue.InexactFloat64()
	returnPct := d.ReturnPct.InexactFloat64()
	profitPln := d.ProfitPln.InexactFloat64()
	createdAt := d.CreatedAt.UTC() // rendered as Z (UTC); same instant, byte-identical text
	e := openapi.HistoriaEntry{
		Id: &id, SnapshotDate: &snapshotDate, Portfolio: &portfolio,
		TotalDeposits: &totalDeposits, PortfolioValue: &portfolioValue,
		ReturnPct: &returnPct, ProfitPln: &profitPln, CreatedAt: &createdAt,
	}
	if d.Xirr != nil {
		xirr := d.Xirr.InexactFloat64()
		e.Xirr = &xirr
	}
	if d.PositionValues != nil {
		m := make(map[string]float64, len(d.PositionValues))
		for k, v := range d.PositionValues {
			m[k] = v.InexactFloat64()
		}
		e.PositionValues = &m
	}
	if d.InflationAdjustedDeposits != nil {
		v := d.InflationAdjustedDeposits.InexactFloat64()
		e.InflationAdjustedDeposits = &v
	}
	if d.RealReturnPct != nil {
		v := d.RealReturnPct.InexactFloat64()
		e.RealReturnPct = &v
	}
	if d.AnnualizedInflationPct != nil {
		v := d.AnnualizedInflationPct.InexactFloat64()
		e.AnnualizedInflationPct = &v
	}
	return e
}

func toOpenAPIEntries(dtos []EntryDTO) []openapi.HistoriaEntry {
	out := make([]openapi.HistoriaEntry, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, toOpenAPIEntry(d))
	}
	return out
}
