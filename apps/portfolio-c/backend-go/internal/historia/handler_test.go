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
	"portfolio-c/backend-go/openapi"
)

// Handler tests assert the wire contract: the {data,meta,page} envelope with the
// HistoriaEntryDto JSON shape INCLUDING the extra portfolio field, float64 numbers
// (NOT the generated float32 DTO), the size>0?size:50 PageInfo rule, and 400 for a
// missing/unknown ?portfolio or a negative ?page.

type fakeQueries struct {
	rows []historiaquery.Row
	mom  map[historiaquery.YearMonth]decimal.Decimal
}

func (f *fakeQueries) ListRange(_ context.Context, portfolio string, from, to time.Time) ([]historiaquery.Row, error) {
	var out []historiaquery.Row
	for _, r := range f.rows {
		if r.Portfolio != portfolio {
			continue
		}
		if (r.SnapshotDate.Equal(from) || r.SnapshotDate.After(from)) &&
			(r.SnapshotDate.Equal(to) || r.SnapshotDate.Before(to)) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeQueries) ListAll(_ context.Context, portfolio string) ([]historiaquery.Row, error) {
	var out []historiaquery.Row
	for _, r := range f.rows {
		if r.Portfolio == portfolio {
			out = append(out, r)
		}
	}
	return out, nil
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

func seedRow(date, portfolio, deposits, value string) historiaquery.Row {
	d, _ := time.Parse("2006-01-02", date)
	return historiaquery.Row{
		ID: 7, SnapshotDate: d, Portfolio: portfolio,
		TotalDeposits:  decimal.RequireFromString(deposits),
		PortfolioValue: decimal.RequireFromString(value),
		CreatedAt:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestInflationBase_Pin(t *testing.T) {
	// Binding per-app constant: portfolioC cumulates from 2026-07 (portfolioB uses 2017-01).
	// A silent "unification" of the two bases breaks this test on purpose.
	if InflationBase != (historiaquery.YearMonth{Year: 2026, Month: 7}) {
		t.Fatalf("INFLATION_BASE must stay 2026-07, got %+v", InflationBase)
	}
	if Config().InflationBase != InflationBase {
		t.Fatal("Config must carry InflationBase")
	}
	if !Config().RequirePortfolio {
		t.Fatal("portfolioC must require a portfolio")
	}
}

func TestListHistoria_PortfolioFieldAndComputedFields(t *testing.T) {
	fq := &fakeQueries{rows: []historiaquery.Row{seedRow("2024-01-01", "VARIABLE", "1000", "1250")}}
	h := NewHandler(testService(fq))

	req := httptest.NewRequest(http.MethodGet, "/api/history?portfolio=VARIABLE", nil)
	rec := httptest.NewRecorder()
	h.ListHistoria(rec, req, openapi.ListHistoriaParams{Portfolio: "VARIABLE"})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			Portfolio    string  `json:"portfolio"`
			ReturnPct    float64 `json:"returnPct"`
			ProfitPln    float64 `json:"profitPln"`
			SnapshotDate string  `json:"snapshotDate"`
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
	if len(body.Data) != 1 || body.Page.Total != 1 || body.Page.Size != 50 {
		t.Fatalf("unexpected page: %s", rec.Body.String())
	}
	d := body.Data[0]
	if d.Portfolio != "VARIABLE" {
		t.Fatalf("expected the extra portfolio field, got %+v", d)
	}
	if d.ReturnPct != 0.25 || d.ProfitPln != 250 {
		t.Fatalf("unexpected computed fields: %+v", d)
	}
}

func TestListHistoria_MissingOrUnknownPortfolioIs400(t *testing.T) {
	fq := &fakeQueries{rows: []historiaquery.Row{seedRow("2024-01-01", "VARIABLE", "1000", "1250")}}
	h := NewHandler(testService(fq))

	// Missing portfolio.
	rec := httptest.NewRecorder()
	h.ListHistoria(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil), openapi.ListHistoriaParams{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing portfolio: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// Unknown portfolio.
	rec = httptest.NewRecorder()
	h.ListHistoria(rec, httptest.NewRequest(http.MethodGet, "/api/history?portfolio=BOGUS", nil),
		openapi.ListHistoriaParams{Portfolio: "BOGUS"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown portfolio: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// Lowercase accepted (case-insensitive, like Spring's enum conversion).
	rec = httptest.NewRecorder()
	h.ListHistoria(rec, httptest.NewRequest(http.MethodGet, "/api/history?portfolio=fixed", nil),
		openapi.ListHistoriaParams{Portfolio: "fixed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("lowercase portfolio: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListHistoria_NegativePageIs400(t *testing.T) {
	h := NewHandler(testService(&fakeQueries{}))
	var page int32 = -1
	rec := httptest.NewRecorder()
	h.ListHistoria(rec, httptest.NewRequest(http.MethodGet, "/api/history?portfolio=VARIABLE&page=-1", nil),
		openapi.ListHistoriaParams{Portfolio: "VARIABLE", Page: &page})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
