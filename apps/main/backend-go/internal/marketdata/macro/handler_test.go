package macro

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"investing-app/backend-go/openapi"
)

// Handler tests assert the wire contract: envelope shapes ({data,meta}), status codes per
// docs/openapi.yaml (422 for m2-manual validation, 400 for benchmark whitelist), and the
// shared VALIDATION_ERROR error envelope — the coexistence bar from the phase task.

func testHandler(fq *fakeQueries, fetcher *Fetcher) *Handler {
	return NewHandler(testService(fq, fetcher))
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return body
}

func TestGetMacroState_EmptyDB_ReturnsDefaultEnvelope(t *testing.T) {
	h := testHandler(newFakeQueries(), emptyFetcher())
	req := httptest.NewRequest(http.MethodGet, "/api/market-data/macro-state", nil)
	rec := httptest.NewRecorder()
	h.GetMacroState(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	data, meta := body["data"], body["meta"]
	if data == nil || meta == nil {
		t.Fatalf("expected {data,meta} envelope, got %s", rec.Body.String())
	}
	var dto openapi.MacroState
	if err := json.Unmarshal(data, &dto); err != nil {
		t.Fatal(err)
	}
	if dto.RiskOff == nil || *dto.RiskOff {
		t.Fatalf("expected riskOff=false default")
	}
	if dto.GlobalM2Stale == nil || !*dto.GlobalM2Stale {
		t.Fatalf("expected globalM2Stale=true default")
	}
	if dto.BlendFactor != nil || dto.Zone != nil {
		t.Fatalf("expected null blend/zone, got %s", string(data))
	}
}

func TestGetBenchmarkHistory_InvalidSymbol_Is400ValidationError(t *testing.T) {
	h := testHandler(newFakeQueries(), emptyFetcher())
	req := httptest.NewRequest(http.MethodGet, "/api/market-data/benchmark-history?symbol=AAPL", nil)
	rec := httptest.NewRecorder()
	h.GetBenchmarkHistory(rec, req, openapi.GetBenchmarkHistoryParams{Symbol: "AAPL"})

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
	if !strings.Contains(errBody.Errors[0].Message, "Unsupported benchmark symbol: AAPL") {
		t.Fatalf("expected Java's message, got %q", errBody.Errors[0].Message)
	}
}

func TestUpsertManualM2_Validation_Is422(t *testing.T) {
	h := testHandler(newFakeQueries(), emptyFetcher())
	cases := []struct {
		name string
		body string
		want string // expected failing field ("" = generic body error)
	}{
		{"missing value", `{"seriesCode":"M2_CN","observationDate":"2026-06-01"}`, "value"},
		{"missing series", `{"observationDate":"2026-06-01","value":1}`, "seriesCode"},
		{"bad series", `{"seriesCode":"M2_US","observationDate":"2026-06-01","value":1}`, "seriesCode"},
		{"malformed json", `{"seriesCode":`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/market-data/macro/m2-manual", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			h.UpsertManualM2(rec, req)
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
		})
	}
}

func TestUpsertManualM2_Valid_PersistsAndReturnsState(t *testing.T) {
	fq := newFakeQueries()
	h := testHandler(fq, emptyFetcher())
	req := httptest.NewRequest(http.MethodPost, "/api/market-data/macro/m2-manual",
		strings.NewReader(`{"seriesCode":"M2_CN","observationDate":"2026-06-01","value":356000000}`))
	rec := httptest.NewRecorder()
	h.UpsertManualM2(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["data"] == nil || body["meta"] == nil {
		t.Fatalf("expected {data,meta} envelope, got %s", rec.Body.String())
	}
	rows, err := fq.ObsBySeriesAsc(req.Context(), SeriesM2CN)
	if err != nil || len(rows) != 1 || rows[0].Source != SourceManual {
		t.Fatalf("expected 1 MANUAL row, got %v,%v", rows, err)
	}
}

func TestGetMacroStateHistory_EnvelopeShape(t *testing.T) {
	h := testHandler(newFakeQueries(), emptyFetcher())
	req := httptest.NewRequest(http.MethodGet, "/api/market-data/macro-state/history", nil)
	rec := httptest.NewRecorder()
	h.GetMacroStateHistory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := decodeBody(t, rec)
	var data []json.RawMessage
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatalf("expected data array, got %s", rec.Body.String())
	}
	if body["meta"] == nil {
		t.Fatalf("expected meta, got %s", rec.Body.String())
	}
}
