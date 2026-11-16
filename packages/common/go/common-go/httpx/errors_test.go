package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteError_AppError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not found", NewNotFoundError("Purchase session 42 not found"), http.StatusNotFound, CodeNotFound},
		{"validation", NewValidationError("ticker", "must not be blank"), http.StatusBadRequest, CodeValidationError},
		{"conflict", NewConflictError(CodeDuplicateEntry, "Resource already exists"), http.StatusConflict, CodeDuplicateEntry},
		{"service unavailable", NewServiceUnavailableError("Market data unavailable"), http.StatusServiceUnavailable, CodeServiceUnavailable},
		{"unauthorized", NewUnauthorizedError("Bearer token missing or invalid"), http.StatusUnauthorized, CodeUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, tt.err)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}

			var body ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if len(body.Errors) != 1 {
				t.Fatalf("expected exactly 1 error detail, got %d", len(body.Errors))
			}
			if body.Errors[0].Code != tt.wantCode {
				t.Fatalf("expected code %q, got %q", tt.wantCode, body.Errors[0].Code)
			}
		})
	}
}

func TestWriteError_UnknownErrorMapsToGenericInternalError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, errors.New("boom: leaked internal detail"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != CodeInternalError {
		t.Fatalf("expected a single INTERNAL_ERROR detail, got %+v", body.Errors)
	}
	if body.Errors[0].Message != "An unexpected error occurred" {
		t.Fatalf("expected the generic message, not the raw error, got %q", body.Errors[0].Message)
	}
}

func TestErrorDetail_FieldOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(ErrorDetail{Code: CodeNotFound, Message: "not found"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["field"]; ok {
		t.Fatalf("expected no 'field' key when Field is empty, got %s", b)
	}
}

// unavailableErr is a domain error that reports the Unavailable() behaviour WriteError maps to
// 503 (task 182 B4: rebalancingcalc.UnavailableError, without an httpx import).
type unavailableErr struct{ unavailable bool }

func (e unavailableErr) Error() string     { return "Price unavailable for ticker: X" }
func (e unavailableErr) Unavailable() bool { return e.unavailable }

func TestWriteError_UnavailableBehaviourMapsTo503(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantMsg    string
	}{
		{"unavailable", unavailableErr{true}, http.StatusServiceUnavailable, CodeServiceUnavailable, "Price unavailable for ticker: X"},
		{"wrapped unavailable", fmt.Errorf("plan: %w", unavailableErr{true}), http.StatusServiceUnavailable, CodeServiceUnavailable, "Price unavailable for ticker: X"},
		{"Unavailable() false stays a generic 500", unavailableErr{false}, http.StatusInternalServerError, CodeInternalError, "An unexpected error occurred"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		WriteError(rec, c.err)
		if rec.Code != c.wantStatus {
			t.Fatalf("%s: status %d, want %d", c.name, rec.Code, c.wantStatus)
		}
		var body ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 {
			t.Fatalf("%s: bad envelope %s (%v)", c.name, rec.Body.String(), err)
		}
		if got := body.Errors[0]; got.Code != c.wantCode || got.Message != c.wantMsg {
			t.Errorf("%s: detail %+v, want code %s message %q", c.name, got, c.wantCode, c.wantMsg)
		}
	}
}
