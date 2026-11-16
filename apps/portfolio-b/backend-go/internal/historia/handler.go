package historia

import (
	"net/http"
	"time"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/httpx"
	"portfolio-b/backend-go/openapi"
)

// Handler adapts the shared historiaquery.Service to this app's generated ServerInterface method
// for GET /history — the same thin-Facade convention as internal/inflation (no business logic here;
// service -> wire mapping only).
//
// Wire fidelity note: the handler writes its own float64 wire struct with the identical JSON shape
// instead of the generated openapi.HistoriaEntryDto (which was float32-typed before task 182 added
// `format: double` to the spec) — same field names, full precision for NUMERIC(15,2) portfolio
// values.
type Handler struct {
	service *historiaquery.Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *historiaquery.Service) *Handler { return &Handler{service: service} }

// ListHistoria implements GET /history — the direct port of
// HistoriaController.listHistoria: paginated daily snapshots with response-time
// computed returnPct/profitPln and best-effort inflation enrichment. Answers 200,
// or 400 VALIDATION_ERROR for a negative ?page. Never writes.
func (h *Handler) ListHistoria(w http.ResponseWriter, r *http.Request, params openapi.ListHistoriaParams) {
	page, err := h.service.List(r.Context(), historiaquery.ListParams{
		From: openapiDateToTime(params.From),
		To:   openapiDateToTime(params.To),
		Page: int32ToInt(params.Page),
		Size: int32ToInt(params.Size),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(toWireEntries(page.Items), httpx.PageInfo{
		Number: page.Number, Size: page.Size, Total: page.Total,
	}))
}

func openapiDateToTime(d *types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time
	return &t
}

func int32ToInt(v *int32) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

// ── Service -> wire mapping (HistoriaEntryDto JSON shape, float64 numbers) ──

type wireEntry struct {
	ID                        int64               `json:"id"`
	SnapshotDate              types.Date          `json:"snapshotDate"`
	TotalDeposits             float64             `json:"totalDeposits"`
	PortfolioValue            float64             `json:"portfolioValue"`
	Xirr                      *float64            `json:"xirr,omitempty"`
	ReturnPct                 float64             `json:"returnPct"`
	ProfitPln                 float64             `json:"profitPln"`
	PositionValues            *map[string]float64 `json:"positionValues,omitempty"`
	CreatedAt                 time.Time           `json:"createdAt"`
	InflationAdjustedDeposits *float64            `json:"inflationAdjustedDeposits,omitempty"`
	RealReturnPct             *float64            `json:"realReturnPct,omitempty"`
	AnnualizedInflationPct    *float64            `json:"annualizedInflationPct,omitempty"`
}

func toWireEntry(d historiaquery.EntryDTO) wireEntry {
	e := wireEntry{
		ID:             d.ID,
		SnapshotDate:   types.Date{Time: d.SnapshotDate},
		TotalDeposits:  d.TotalDeposits.InexactFloat64(),
		PortfolioValue: d.PortfolioValue.InexactFloat64(),
		ReturnPct:      d.ReturnPct.InexactFloat64(),
		ProfitPln:      d.ProfitPln.InexactFloat64(),
		CreatedAt:      d.CreatedAt.UTC(),
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

func toWireEntries(dtos []historiaquery.EntryDTO) []wireEntry {
	out := make([]wireEntry, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, toWireEntry(d))
	}
	return out
}
