package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Handler tests assert the wire contract: envelope shapes ({data,meta} / {data,meta} list),
// status codes per docs/openapi.yaml (404 unknown key, 422 validation — NOT Java's 400),
// and the shared VALIDATION_ERROR/NOT_FOUND envelopes.

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return body
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (int, string, string) {
	t.Helper()
	var errBody struct {
		Errors []struct {
			Code    string `json:"code"`
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("response is not an error envelope: %v\n%s", err, rec.Body.String())
	}
	if len(errBody.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %s", rec.Body.String())
	}
	return rec.Code, errBody.Errors[0].Code, errBody.Errors[0].Field
}

func TestListSettings_EnvelopeShape(t *testing.T) {
	h := NewHandler(testService())
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	h.ListSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data []json.RawMessage
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatalf("expected data array, got %s", rec.Body.String())
	}
	if len(data) != 4 || body["meta"] == nil {
		t.Fatalf("expected 4 settings + meta, got %s", rec.Body.String())
	}
	var first struct {
		Key       string `json:"key"`
		Value     string `json:"value"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(data[0], &first); err != nil || first.Key == "" || first.UpdatedAt == "" {
		t.Fatalf("expected {key,value,updatedAt} items, got %s", rec.Body.String())
	}
}

func TestUpdateSetting_Valid_Returns200Single(t *testing.T) {
	h := NewHandler(testService())
	req := httptest.NewRequest(http.MethodPut, "/api/settings/"+DefMacroDeriskEnabled.Key,
		strings.NewReader(`{"value":"false"}`))
	rec := httptest.NewRecorder()
	h.UpdateSetting(rec, req, DefMacroDeriskEnabled.Key)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["data"] == nil || body["meta"] == nil {
		t.Fatalf("expected {data,meta} envelope, got %s", rec.Body.String())
	}
	var data struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data.Key != DefMacroDeriskEnabled.Key || data.Value != "false" {
		t.Fatalf("unexpected data: %s", string(body["data"]))
	}
}

func TestUpdateSetting_UnknownKey_Is404(t *testing.T) {
	h := NewHandler(testService())
	req := httptest.NewRequest(http.MethodPut, "/api/settings/unknown.key",
		strings.NewReader(`{"value":"test"}`))
	rec := httptest.NewRecorder()
	h.UpdateSetting(rec, req, "unknown.key")

	status, code, _ := decodeError(t, rec)
	if status != http.StatusNotFound || code != "NOT_FOUND" {
		t.Fatalf("expected 404 NOT_FOUND, got %d %s: %s", status, code, rec.Body.String())
	}
}

func TestUpdateSetting_Validation_Is422(t *testing.T) {
	h := NewHandler(testService())
	cases := []struct {
		name string
		key  string
		body string
		want string // expected failing field ("" = generic body error)
	}{
		{"missing value", DefMacroDeriskEnabled.Key, `{}`, "value"},
		{"malformed json", DefMacroDeriskEnabled.Key, `{"value":`, ""},
		{"bad bool", DefMacroDeriskEnabled.Key, `{"value":"yes"}`, "value"},
		{"bad decimal", DefRetirementMonthlyContributionPln.Key, `{"value":"abc"}`, "value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings/"+c.key, strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			h.UpdateSetting(rec, req, c.key)
			status, code, field := decodeError(t, rec)
			if status != http.StatusUnprocessableEntity || code != "VALIDATION_ERROR" {
				t.Fatalf("expected 422 VALIDATION_ERROR, got %d %s: %s", status, code, rec.Body.String())
			}
			if c.want != "" && field != c.want {
				t.Fatalf("expected failing field %q, got %q: %s", c.want, field, rec.Body.String())
			}
		})
	}
}
