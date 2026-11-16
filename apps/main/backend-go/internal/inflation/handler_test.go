package inflation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/inflationsync"
	"investing-app/common-go/pgnum"
)

// Handler tests assert the wire contract: envelope shapes ({data,meta} / {data,meta,page}),
// status codes per docs/openapi.yaml (201 create, 409 duplicate, 422 validation — NOT
// Java's 400), and the shared error envelopes.

func seedFake(t *testing.T, fq *fakeQueries) {
	t.Helper()
	ctx := context.Background()
	for _, ym := range [][2]int32{{2024, 1}, {2024, 2}} {
		if _, err := fq.CreateInflationEntry(ctx, sqlcgen.CreateInflationEntryParams{
			Year: ym[0], Month: ym[1], CpiValue: pgnum.FromDecimal(decimal.NewFromInt(100)),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return body
}

func TestListInflation_PagedEnvelope(t *testing.T) {
	fq := newFakeQueries()
	seedFake(t, fq)
	h := NewHandler(testService(fq), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/inflation", nil)
	rec := httptest.NewRecorder()
	h.ListInflation(rec, req, openapi.ListInflationParams{})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data []json.RawMessage
	if err := json.Unmarshal(body["data"], &data); err != nil || len(data) != 2 {
		t.Fatalf("expected 2 data items, got %s", rec.Body.String())
	}
	var page struct {
		Number int   `json:"number"`
		Size   int   `json:"size"`
		Total  int64 `json:"total"`
	}
	if err := json.Unmarshal(body["page"], &page); err != nil {
		t.Fatalf("expected page object, got %s", rec.Body.String())
	}
	if page.Number != 0 || page.Size != 24 || page.Total != 2 {
		t.Fatalf("expected default page 0/24 total 2, got %+v", page)
	}
	if body["meta"] == nil {
		t.Fatalf("expected meta, got %s", rec.Body.String())
	}
	var first struct {
		Year      int     `json:"year"`
		Month     int     `json:"month"`
		CpiValue  float64 `json:"cpiValue"`
		CreatedAt string  `json:"createdAt"`
	}
	if err := json.Unmarshal(data[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.Year != 2024 || first.Month != 2 || first.CreatedAt == "" {
		t.Fatalf("expected newest-first with createdAt, got %s", string(data[0]))
	}
}

func TestCreateInflationEntry_Returns201(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/inflation",
		strings.NewReader(`{"year":2024,"month":3,"cpiValue":2.5}`))
	rec := httptest.NewRecorder()
	h.CreateInflationEntry(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["data"] == nil || body["meta"] == nil {
		t.Fatalf("expected {data,meta} envelope, got %s", rec.Body.String())
	}
	var data struct {
		CpiValue float64 `json:"cpiValue"`
	}
	if err := json.Unmarshal(body["data"], &data); err != nil || data.CpiValue != 2.5 {
		t.Fatalf("expected cpiValue 2.5, got %s", string(body["data"]))
	}
}

func TestCreateInflationEntry_DuplicateIs409(t *testing.T) {
	fq := newFakeQueries()
	seedFake(t, fq)
	h := NewHandler(testService(fq), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/inflation",
		strings.NewReader(`{"year":2024,"month":1,"cpiValue":2.5}`))
	rec := httptest.NewRecorder()
	h.CreateInflationEntry(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if len(errBody.Errors) != 1 || errBody.Errors[0].Code != "DUPLICATE_ENTRY" {
		t.Fatalf("expected DUPLICATE_ENTRY, got %s", rec.Body.String())
	}
}

func TestCreateInflationEntry_ValidationIs422(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()), nil)
	cases := []struct {
		name string
		body string
		want string // expected failing field ("" = generic body error)
	}{
		{"missing cpiValue", `{"year":2024,"month":3}`, "cpiValue"},
		{"missing year", `{"month":3,"cpiValue":2.5}`, "year"},
		{"malformed json", `{"year":`, ""},
		{"month out of range", `{"year":2024,"month":13,"cpiValue":2.5}`, "month"},
		{"year out of range", `{"year":1999,"month":3,"cpiValue":2.5}`, "year"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/inflation", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			h.CreateInflationEntry(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
			}
			var errBody struct {
				Errors []struct {
					Code    string `json:"code"`
					Field   string `json:"field"`
					Message string `json:"message"`
				} `json:"errors"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
				t.Fatal(err)
			}
			if len(errBody.Errors) != 1 || errBody.Errors[0].Code != "VALIDATION_ERROR" {
				t.Fatalf("expected single VALIDATION_ERROR, got %s", rec.Body.String())
			}
			if c.want != "" && errBody.Errors[0].Field != c.want {
				t.Fatalf("expected failing field %q, got %q: %s", c.want, errBody.Errors[0].Field, rec.Body.String())
			}
		})
	}
}

// fakeCore is an in-memory inflationsync.Queries for the POST /inflation/sync wire test
// (the real Adapter is covered by adapter_test.go against Postgres).
type fakeCore map[[2]int]decimal.Decimal

func (f fakeCore) GetCpi(_ context.Context, year, month int) (decimal.Decimal, error) {
	v, ok := f[[2]int{year, month}]
	if !ok {
		return decimal.Zero, inflationsync.ErrNotFound
	}
	return v, nil
}

func (f fakeCore) CreateEntry(_ context.Context, year, month int, cpi decimal.Decimal) error {
	f[[2]int{year, month}] = cpi
	return nil
}

func (f fakeCore) UpdateCpi(_ context.Context, year, month int, cpi decimal.Decimal) error {
	f[[2]int{year, month}] = cpi
	return nil
}

func (f fakeCore) ListAsc(context.Context) ([]inflationsync.YearMonth, error) { return nil, nil }

func TestSyncInflationFromGus_ReturnsUpsertCount(t *testing.T) {
	core := fakeCore{{2024, 1}: decimal.NewFromInt(100), {2024, 2}: decimal.NewFromInt(100)}
	sync := inflationsync.NewService(core, func(ctx context.Context, fn func(inflationsync.Queries) error) error {
		return fn(core)
	}, stubGus{[]inflationsync.MomEntry{
		{Year: 2024, Month: 1, Cpi: decimal.NewFromInt(100)},   // unchanged
		{Year: 2024, Month: 3, Cpi: decimal.NewFromFloat(2.5)}, // new
	}})
	h := NewHandler(testService(newFakeQueries()), sync)

	req := httptest.NewRequest(http.MethodPost, "/api/inflation/sync", nil)
	rec := httptest.NewRecorder()
	h.SyncInflationFromGus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data struct {
		Upserts int `json:"upserts"`
	}
	if err := json.Unmarshal(body["data"], &data); err != nil || data.Upserts != 1 {
		t.Fatalf("expected {upserts:1}, got %s", string(body["data"]))
	}
	if body["meta"] == nil {
		t.Fatalf("expected meta, got %s", rec.Body.String())
	}
}
