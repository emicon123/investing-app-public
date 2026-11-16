package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"investing-app/common-go/settingscore"
)

// Handler tests assert the wire contract: envelope shapes ({data,meta} / {data,meta} list),
// status codes per this app's checked-in docs/openapi.yaml (404 unknown key, 400 validation —
// NOT root's own Phase 2 kernel's 422), and the shared VALIDATION_ERROR/NOT_FOUND envelopes.

// fakeStore is an in-memory stand-in for settingscore.Store — handler-level tests only need the
// HTTP mapping; the real Postgres round-trip is covered by integration_test.go.
type fakeStore struct {
	rows map[string]settingscore.Row
	now  time.Time
}

func newFakeStore() *fakeStore {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	return &fakeStore{
		now: now,
		rows: map[string]settingscore.Row{
			KeyFixedMonthlyDepositDefault: {Key: KeyFixedMonthlyDepositDefault, Value: "400", UpdatedAt: now},
		},
	}
}

func (f *fakeStore) Get(_ context.Context, key string) (settingscore.Row, error) {
	r, ok := f.rows[key]
	if !ok {
		return settingscore.Row{}, settingscore.ErrNotFound
	}
	return r, nil
}

func (f *fakeStore) List(_ context.Context) ([]settingscore.Row, error) {
	out := make([]settingscore.Row, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) Update(_ context.Context, key, value string) (settingscore.Row, error) {
	r, ok := f.rows[key]
	if !ok {
		return settingscore.Row{}, settingscore.ErrNotFound
	}
	f.now = f.now.Add(time.Second)
	r.Value = value
	r.UpdatedAt = f.now
	f.rows[key] = r
	return r, nil
}

func testService() *settingscore.Service { return settingscore.NewService(newFakeStore(), Validate) }

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
	if len(data) != 1 || body["meta"] == nil {
		t.Fatalf("expected 1 setting + meta, got %s", rec.Body.String())
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
	req := httptest.NewRequest(http.MethodPut, "/api/settings/"+KeyFixedMonthlyDepositDefault,
		strings.NewReader(`{"value":"450"}`))
	rec := httptest.NewRecorder()
	h.UpdateSetting(rec, req, KeyFixedMonthlyDepositDefault)

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
	if data.Key != KeyFixedMonthlyDepositDefault || data.Value != "450" {
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

func TestUpdateSetting_Validation_Is400(t *testing.T) {
	h := NewHandler(testService())
	cases := []struct {
		name string
		key  string
		body string
		want string // expected failing field ("" = generic body error)
	}{
		{"missing value", KeyFixedMonthlyDepositDefault, `{}`, "value"},
		{"malformed json", KeyFixedMonthlyDepositDefault, `{"value":`, ""},
		{"bad decimal", KeyFixedMonthlyDepositDefault, `{"value":"abc"}`, ""},
		{"zero rejected", KeyFixedMonthlyDepositDefault, `{"value":"0"}`, ""},
		{"negative rejected", KeyFixedMonthlyDepositDefault, `{"value":"-5"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings/"+c.key, strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			h.UpdateSetting(rec, req, c.key)
			status, code, field := decodeError(t, rec)
			if status != http.StatusBadRequest || code != "VALIDATION_ERROR" {
				t.Fatalf("expected 400 VALIDATION_ERROR, got %d %s: %s", status, code, rec.Body.String())
			}
			if c.want != "" && field != c.want {
				t.Fatalf("expected failing field %q, got %q: %s", c.want, field, rec.Body.String())
			}
		})
	}
}
