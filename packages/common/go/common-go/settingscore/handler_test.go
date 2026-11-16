package settingscore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"investing-app/common-go/httpx"
)

// unprocessable is a 422 constructor like the one main passes to NewHandler.
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}

// recordingSettings is a Settings that records what Update received — to show that Handler
// only decodes, and any service (Service, or main's own) can sit behind it.
type recordingSettings struct {
	list       []SettingDTO
	gotKey     string
	gotValue   string
	updateErr  error
	updateCall int
}

func (s *recordingSettings) List(context.Context) ([]SettingDTO, error) { return s.list, nil }

func (s *recordingSettings) Update(_ context.Context, key, value string) (SettingDTO, error) {
	s.updateCall++
	s.gotKey, s.gotValue = key, value
	if s.updateErr != nil {
		return SettingDTO{}, s.updateErr
	}
	return SettingDTO{Key: key, Value: value, UpdatedAt: time.Date(2026, 9, 28, 11, 0, 0, 0, time.FixedZone("CEST", 7200))}, nil
}

func put(h *Handler, key, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.UpdateSetting(rec, httptest.NewRequest(http.MethodPut, "/api/settings/"+key, strings.NewReader(body)), key)
	return rec
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) httpx.ErrorDetail {
	t.Helper()
	var body httpx.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Errors[0]
}

func TestHandler_ListSettings_EnvelopeWithUTCTimestamps(t *testing.T) {
	svc := &recordingSettings{list: []SettingDTO{
		{Key: "b", Value: "2", UpdatedAt: time.Date(2026, 9, 28, 11, 0, 0, 0, time.FixedZone("CEST", 7200))},
		{Key: "a", Value: "1", UpdatedAt: time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)},
	}}
	rec := httptest.NewRecorder()
	NewHandler(svc, httpx.NewValidationError).ListSettings(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	want := `{"data":[{"key":"b","updatedAt":"2026-09-28T09:00:00Z","value":"2"},{"key":"a","updatedAt":"2026-09-28T09:00:00Z","value":"1"}],"meta":`
	if !strings.HasPrefix(body, want) {
		t.Fatalf("body = %s\nwant prefix %s", body, want)
	}
}

func TestHandler_ListSettings_EmptyIsArray(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(&recordingSettings{}, httpx.NewValidationError).ListSettings(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.HasPrefix(rec.Body.String(), `{"data":[],`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandler_UpdateSetting_SingleEnvelope(t *testing.T) {
	svc := &recordingSettings{}
	rec := put(NewHandler(svc, httpx.NewValidationError), "k", `{"value":"42"}`)
	if rec.Code != http.StatusOK || svc.gotKey != "k" || svc.gotValue != "42" {
		t.Fatalf("status %d, service got (%q, %q)", rec.Code, svc.gotKey, svc.gotValue)
	}
	if want := `{"data":{"key":"k","updatedAt":"2026-09-28T09:00:00Z","value":"42"},"meta":`; !strings.HasPrefix(rec.Body.String(), want) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandler_UpdateSetting_NullValueIsEmptyString(t *testing.T) {
	svc := &recordingSettings{}
	if rec := put(NewHandler(svc, httpx.NewValidationError), "k", `{"value":null}`); rec.Code != http.StatusOK || svc.gotValue != "" {
		t.Fatalf("status %d, value %q", rec.Code, svc.gotValue)
	}
}

func TestHandler_UpdateSetting_BodyErrorsUseTheInjectedConstructor(t *testing.T) {
	tests := []struct {
		name, body, field, message string
	}{
		{"malformed", `{`, "", "Invalid request body"},
		{"empty", ``, "", "Invalid request body"},
		{"not an object", `[]`, "", "Invalid request body"},
		{"missing value", `{"other":"1"}`, "value", "must not be null"},
		{"non-string value", `{"value":5}`, "", "Invalid request body"},
	}
	for _, tc := range tests {
		for _, ctor := range []struct {
			name   string
			fn     func(field, message string) *httpx.AppError
			status int
		}{{"main 422", unprocessable, 422}, {"forks 400", httpx.NewValidationError, 400}} {
			t.Run(tc.name+"/"+ctor.name, func(t *testing.T) {
				svc := &recordingSettings{}
				rec := put(NewHandler(svc, ctor.fn), "k", tc.body)
				if rec.Code != ctor.status || svc.updateCall != 0 {
					t.Fatalf("status %d (want %d), service calls %d", rec.Code, ctor.status, svc.updateCall)
				}
				if e := errorOf(t, rec); e.Code != httpx.CodeValidationError || e.Field != tc.field || e.Message != tc.message {
					t.Fatalf("error = %+v", e)
				}
			})
		}
	}
}

func TestHandler_UpdateSetting_ServiceErrorsPassThrough(t *testing.T) {
	svc := NewService(newFakeStore(map[string]string{"k": "1"}), func(key, value string) error {
		return httpx.NewValidationError("", key+" must be greater than 0")
	})
	h := NewHandler(svc, unprocessable)
	if rec := put(h, "missing", `{"value":"1"}`); rec.Code != http.StatusNotFound || errorOf(t, rec).Message != "Setting 'missing' not found" {
		t.Fatalf("unknown key: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := put(h, "k", `{"value":"0"}`); rec.Code != http.StatusBadRequest || errorOf(t, rec).Message != "k must be greater than 0" {
		t.Fatalf("validator: status %d body %s", rec.Code, rec.Body.String())
	}
	broken := &recordingSettings{updateErr: errors.New("db down")}
	if rec := put(NewHandler(broken, unprocessable), "k", `{"value":"1"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("internal error: status %d", rec.Code)
	}
}
