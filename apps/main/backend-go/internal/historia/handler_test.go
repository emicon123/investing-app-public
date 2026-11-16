package historia

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
)

// Handler tests assert the wire contract: the {data,meta,page} envelope, the
// returnPct/profitPln computed names (NOT zwrot/zysk), null xirr passthrough, and
// the 400 VALIDATION_ERROR for an unknown ?portfolio.

func TestListHistoria_PagedEnvelopeAndComputedFields(t *testing.T) {
	fq := newFakeQueries()
	seedHistoria(t, fq, "2024-01-01", "MAIN", "1000", "1250")
	h := NewHandler(testService(fq, nil))

	req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	rec := httptest.NewRecorder()
	h.ListHistoria(rec, req, openapi.ListHistoriaParams{})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			ID             int64               `json:"id"`
			SnapshotDate   string              `json:"snapshotDate"`
			Portfolio      string              `json:"portfolio"`
			TotalDeposits  float64             `json:"totalDeposits"`
			PortfolioValue float64             `json:"portfolioValue"`
			Xirr           *float64            `json:"xirr"`
			ReturnPct      float64             `json:"returnPct"`
			ProfitPln      float64             `json:"profitPln"`
			PositionValues *map[string]float64 `json:"positionValues"`
			CreatedAt      string              `json:"createdAt"`
		} `json:"data"`
		Page struct {
			Number int   `json:"number"`
			Size   int   `json:"size"`
			Total  int64 `json:"total"`
		} `json:"page"`
		Meta json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the paged envelope: %v\n%s", err, rec.Body.String())
	}
	if len(body.Data) != 1 || body.Page.Total != 1 || body.Page.Number != 0 || body.Page.Size != 50 {
		t.Fatalf("unexpected page: %s", rec.Body.String())
	}
	d := body.Data[0]
	if d.ReturnPct != 0.25 || d.ProfitPln != 250 || d.Portfolio != "MAIN" {
		t.Fatalf("unexpected computed fields: %+v", d)
	}
	if d.Xirr != nil {
		t.Fatalf("expected null xirr passthrough, got %v", *d.Xirr)
	}
	if d.PositionValues != nil {
		t.Fatalf("expected null positionValues for legacy row, got %v", *d.PositionValues)
	}
	if d.SnapshotDate != "2024-01-01" || d.CreatedAt == "" {
		t.Fatalf("expected date + createdAt, got %+v", d)
	}
	if len(body.Meta) == 0 {
		t.Fatal("expected meta")
	}
}

func TestListHistoria_InvalidPortfolioIs400(t *testing.T) {
	h := NewHandler(testService(newFakeQueries(), nil))
	p := openapi.ListHistoriaParamsPortfolio("BOGUS")
	req := httptest.NewRequest(http.MethodGet, "/api/history?portfolio=BOGUS", nil)
	rec := httptest.NewRecorder()
	h.ListHistoria(rec, req, openapi.ListHistoriaParams{Portfolio: &p})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if len(errBody.Errors) != 1 || errBody.Errors[0].Code != "VALIDATION_ERROR" {
		t.Fatalf("expected single VALIDATION_ERROR, got %s", rec.Body.String())
	}
}

func TestListHistoria_QueryParamsReachService(t *testing.T) {
	fq := newFakeQueries()
	seedHistoria(t, fq, "2024-01-01", "MAIN", "1000", "1100")
	seedHistoria(t, fq, "2024-06-01", "MAIN", "1000", "1150")
	h := NewHandler(testService(fq, nil))

	from := types.Date{Time: mustDate("2024-05-01")}
	to := types.Date{Time: mustDate("2024-07-01")}
	size := 1
	req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	rec := httptest.NewRecorder()
	h.ListHistoria(rec, req, openapi.ListHistoriaParams{From: &from, To: &to, Size: &size})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			SnapshotDate string `json:"snapshotDate"`
		} `json:"data"`
		Page struct {
			Size  int   `json:"size"`
			Total int64 `json:"total"`
		} `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Page.Total != 1 || len(body.Data) != 1 || body.Data[0].SnapshotDate != "2024-06-01" {
		t.Fatalf("expected the ranged row only, got %s", rec.Body.String())
	}
}

// TestBackfillSnapshot_BackfillsBothTrackedPortfolios pins ADR-097 Decision §2's
// endpoint shape: one call with {"date":...} and no portfolio field backfills
// every tracked portfolio (MAIN, RETIREMENT) independently.
func TestBackfillSnapshot_BackfillsBothTrackedPortfolios(t *testing.T) {
	fq := newFakeQueries()
	yahoo := "VUA.WA"
	seedPosition(fq, "MAIN", "VUA", "VUA", &yahoo, "USD",
		map[string]string{"ike": "0", "ikze": "0", "eur": "0", "usd": "10", "pln": "0"}, true)
	fq.prices["VUA.WA"] = decimal.RequireFromString("400")
	h := NewHandler(testService(fq, nil))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/historia/backfill-snapshot",
		strings.NewReader(`{"date":"2026-09-18"}`))
	rec := httptest.NewRecorder()
	h.BackfillSnapshot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Date    string            `json:"date"`
		Results map[string]string `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Date != "2026-09-18" {
		t.Fatalf("expected echoed date 2026-09-18, got %s", body.Date)
	}
	if body.Results["MAIN"] != "ok" || body.Results["RETIREMENT"] != "ok" {
		t.Fatalf("expected both tracked portfolios ok, got %+v", body.Results)
	}
	if len(fq.upserts) != 2 {
		t.Fatalf("expected 2 upserts (MAIN + RETIREMENT), got %d", len(fq.upserts))
	}
	for _, up := range fq.upserts {
		if got := dateKey(up.SnapshotDate.Time); got != "2026-09-18" {
			t.Fatalf("expected snapshot_date 2026-09-18, got %s", got)
		}
	}
}

func TestBackfillSnapshot_InvalidDateIs400(t *testing.T) {
	h := NewHandler(testService(newFakeQueries(), nil))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/historia/backfill-snapshot",
		strings.NewReader(`{"date":"not-a-date"}`))
	rec := httptest.NewRecorder()
	h.BackfillSnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackfillSnapshot_MalformedBodyIs400(t *testing.T) {
	h := NewHandler(testService(newFakeQueries(), nil))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/historia/backfill-snapshot",
		strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	h.BackfillSnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
