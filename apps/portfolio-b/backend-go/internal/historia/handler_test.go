package historia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
	"portfolio-b/backend-go/openapi"
)

// Handler tests assert the wire contract: the {data,meta,page} envelope with the
// HistoriaEntryDto JSON shape (no portfolio field in this app), the returnPct /
// profitPln computed fields, null xirr passthrough, float64 numbers (NOT the
// generated float32 DTO), the size>0?size:50 PageInfo rule, and 400 for a
// negative ?page.

type fakeQueries struct {
	rows []historiaquery.Row
	mom  map[historiaquery.YearMonth]decimal.Decimal
}

func (f *fakeQueries) ListRange(_ context.Context, _ string, from, to time.Time) ([]historiaquery.Row, error) {
	var out []historiaquery.Row
	for _, r := range f.rows {
		if (r.SnapshotDate.Equal(from) || r.SnapshotDate.After(from)) &&
			(r.SnapshotDate.Equal(to) || r.SnapshotDate.Before(to)) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeQueries) ListAll(_ context.Context, _ string) ([]historiaquery.Row, error) {
	return f.rows, nil
}

func (f *fakeQueries) MomAsc(_ context.Context) (map[historiaquery.YearMonth]decimal.Decimal, error) {
	return f.mom, nil
}

func (f *fakeQueries) DepositFlows(_ context.Context, _ string) ([]historiaquery.CashFlow, error) {
	return nil, nil
}

func testService(fq *fakeQueries) *historiaquery.Service {
	return historiaquery.NewService(fq, Config())
}

func seedRow(date, deposits, value string) historiaquery.Row {
	d, _ := time.Parse("2006-01-02", date)
	return historiaquery.Row{
		ID: 7, SnapshotDate: d, Portfolio: "",
		TotalDeposits:  decimal.RequireFromString(deposits),
		PortfolioValue: decimal.RequireFromString(value),
		CreatedAt:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestInflationBase_Pin(t *testing.T) {
	// Binding per-app constant: portfolioB cumulates from 2017-01 (portfolioC uses 2026-07).
	// A silent "unification" of the two bases breaks this test on purpose.
	if InflationBase != (historiaquery.YearMonth{Year: 2017, Month: 1}) {
		t.Fatalf("INFLATION_BASE must stay 2017-01, got %+v", InflationBase)
	}
	if Config().InflationBase != InflationBase {
		t.Fatal("Config must carry InflationBase")
	}
	if Config().RequirePortfolio {
		t.Fatal("portfolioB must not require a portfolio")
	}
}

func TestListHistoria_PagedEnvelopeAndComputedFields(t *testing.T) {
	fq := &fakeQueries{rows: []historiaquery.Row{seedRow("2024-01-01", "1000", "1250")}}
	h := NewHandler(testService(fq))

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
	if d.ReturnPct != 0.25 || d.ProfitPln != 250 {
		t.Fatalf("unexpected computed fields: %+v", d)
	}
	if d.Xirr != nil || d.PositionValues != nil {
		t.Fatalf("expected null xirr/positionValues passthrough, got %+v", d)
	}
	if d.SnapshotDate != "2024-01-01" || d.CreatedAt == "" {
		t.Fatalf("expected date + createdAt, got %+v", d)
	}
	if len(body.Meta) == 0 {
		t.Fatal("expected meta")
	}
	// No portfolio field anywhere in this app's DTO.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, item := range raw["data"].([]any) {
		if _, ok := item.(map[string]any)["portfolio"]; ok {
			t.Fatal("portfolioB DTO must not carry a portfolio field")
		}
	}
}

func TestListHistoria_NegativePageIs400(t *testing.T) {
	h := NewHandler(testService(&fakeQueries{}))
	var page int32 = -1
	req := httptest.NewRequest(http.MethodGet, "/api/history?page=-1", nil)
	rec := httptest.NewRecorder()
	h.ListHistoria(rec, req, openapi.ListHistoriaParams{Page: &page})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListHistoria_SizeFallback(t *testing.T) {
	fq := &fakeQueries{rows: []historiaquery.Row{seedRow("2024-01-01", "1000", "1250")}}
	h := NewHandler(testService(fq))
	var size int32
	req := httptest.NewRequest(http.MethodGet, "/api/history?size=0", nil)
	rec := httptest.NewRecorder()
	h.ListHistoria(rec, req, openapi.ListHistoriaParams{Size: &size})
	var body struct {
		Page struct {
			Size int `json:"size"`
		} `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Page.Size != 50 {
		t.Fatalf("expected PageInfo.size 50 fallback, got %d", body.Page.Size)
	}
}
