package taxreturns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
)

// Handler tests assert the wire contract: envelope shapes ({data,meta} / {data,meta,page}),
// status codes per docs/openapi.yaml (201 create, 409 duplicate, 422 validation — NOT
// Java's 400), and the shared error envelopes.

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return body
}

func seedService() *Service {
	svc := NewService(newFakeQueries())
	for _, y := range []int{2023, 2024} {
		if _, err := svc.Create(context.Background(), y,
			decimal.NewFromInt(int64(y*1000)), decimal.NewFromInt(190), decimal.NewFromInt(19)); err != nil {
			panic(err)
		}
	}
	return svc
}

func TestListTaxReturns_PagedEnvelope(t *testing.T) {
	h := NewHandler(seedService())
	req := httptest.NewRequest(http.MethodGet, "/api/tax-returns", nil)
	rec := httptest.NewRecorder()
	h.ListTaxReturns(rec, req, openapi.ListTaxReturnsParams{})

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
	if page.Number != 0 || page.Size != 20 || page.Total != 2 {
		t.Fatalf("expected default page 0/20 total 2, got %+v", page)
	}
	var first struct {
		Year      int    `json:"year"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(data[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.Year != 2024 || first.CreatedAt == "" || first.UpdatedAt == "" {
		t.Fatalf("expected newest-first with audit timestamps, got %s", string(data[0]))
	}
}

func TestCreateTaxReturn_Returns201(t *testing.T) {
	h := NewHandler(NewService(newFakeQueries()))
	req := httptest.NewRequest(http.MethodPost, "/api/tax-returns",
		strings.NewReader(`{"year":2025,"grossGain":12000,"taxPaid":2280,"effectiveRatePct":19}`))
	rec := httptest.NewRecorder()
	h.CreateTaxReturn(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["data"] == nil || body["meta"] == nil {
		t.Fatalf("expected {data,meta} envelope, got %s", rec.Body.String())
	}
}

func TestCreateTaxReturn_DuplicateIs409(t *testing.T) {
	h := NewHandler(seedService())
	req := httptest.NewRequest(http.MethodPost, "/api/tax-returns",
		strings.NewReader(`{"year":2024,"grossGain":1,"taxPaid":1,"effectiveRatePct":1}`))
	rec := httptest.NewRecorder()
	h.CreateTaxReturn(rec, req)

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

func TestCreateTaxReturn_ValidationIs422(t *testing.T) {
	h := NewHandler(NewService(newFakeQueries()))
	cases := []struct {
		name string
		body string
		want string // expected failing field ("" = generic body error)
	}{
		{"missing taxPaid", `{"year":2024,"grossGain":1,"effectiveRatePct":1}`, "taxPaid"},
		{"malformed json", `{"year":`, ""},
		{"year out of range", `{"year":2020,"grossGain":1,"taxPaid":1,"effectiveRatePct":1}`, "year"},
		{"negative taxPaid", `{"year":2024,"grossGain":1,"taxPaid":-1,"effectiveRatePct":1}`, "taxPaid"},
		{"rate over 100", `{"year":2024,"grossGain":1,"taxPaid":1,"effectiveRatePct":101}`, "effectiveRatePct"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/tax-returns", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			h.CreateTaxReturn(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
			}
			var errBody struct {
				Errors []struct {
					Code  string `json:"code"`
					Field string `json:"field"`
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
