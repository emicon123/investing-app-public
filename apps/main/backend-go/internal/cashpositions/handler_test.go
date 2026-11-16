package cashpositions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Handler tests assert the wire contract: envelope shapes ({data,meta} list), status codes
// per docs/openapi.yaml (201 snapshot, 200 latest — including 200 [] when empty — 422
// validation, NOT Java's 400), and the shared error envelopes.

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return body
}

func TestGetCashPositions_EmptyIs200EmptyArray(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()))
	req := httptest.NewRequest(http.MethodGet, "/api/cash-positions", nil)
	rec := httptest.NewRecorder()
	h.GetCashPositions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	// data must be [] (empty array), never null — the frontend's `cashData?.data ?? []`
	// tolerates both, but Java's Jackson emits [] and Go must match byte-for-byte.
	if string(body["data"]) != "[]" {
		t.Fatalf("expected data:[], got %s", rec.Body.String())
	}
	if body["meta"] == nil {
		t.Fatalf("expected meta, got %s", rec.Body.String())
	}
}

func TestCreateCashSnapshot_Returns201List(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()))
	req := httptest.NewRequest(http.MethodPost, "/api/cash-positions",
		strings.NewReader(`{"positions":[{"location":"mBank","amountPln":15000},{"location":"portfel","amountPln":200}],"recordedAt":"2026-06-08T08:00:00Z"}`))
	rec := httptest.NewRecorder()
	h.CreateCashSnapshot(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data []struct {
		Location   string  `json:"location"`
		AmountPln  float64 `json:"amountPln"`
		RecordedAt string  `json:"recordedAt"`
	}
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 || data[0].Location != "mBank" || data[0].RecordedAt == "" {
		t.Fatalf("unexpected data: %s", string(body["data"]))
	}
	if body["meta"] == nil {
		t.Fatalf("expected meta, got %s", rec.Body.String())
	}
}

func TestCreateCashSnapshot_ValidationIs422(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()))
	cases := []struct {
		name string
		body string
		want string // expected failing field prefix ("" = generic body error)
	}{
		{"missing positions", `{"recordedAt":"2026-06-08T08:00:00Z"}`, "positions"},
		{"missing recordedAt", `{"positions":[{"location":"mBank","amountPln":1}]}`, "recordedAt"},
		{"null positions", `{"positions":null,"recordedAt":"2026-06-08T08:00:00Z"}`, "positions"},
		{"malformed json", `{"positions":`, ""},
		{"negative amount", `{"positions":[{"location":"mBank","amountPln":-1}],"recordedAt":"2026-06-08T08:00:00Z"}`, "positions[0].amountPln"},
		{"blank location", `{"positions":[{"location":"  ","amountPln":1}],"recordedAt":"2026-06-08T08:00:00Z"}`, "positions[0].location"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/cash-positions", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			h.CreateCashSnapshot(rec, req)
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
