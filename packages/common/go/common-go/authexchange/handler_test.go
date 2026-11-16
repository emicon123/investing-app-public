package authexchange

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"investing-app/common-go/auth"
	"investing-app/common-go/httpx"
)

// fakeExchanger lets handler tests control auth.Service.Exchange's outcome directly, without a
// real Google ID token (task 152 §1.2 finding 4).
type fakeExchanger struct {
	gotToken string
	resp     auth.TokenExchangeResponse
	err      error
}

func (f *fakeExchanger) Exchange(_ context.Context, googleIDToken string) (auth.TokenExchangeResponse, error) {
	f.gotToken = googleIDToken
	return f.resp, f.err
}

func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) (int, string, string) {
	t.Helper()
	var body struct {
		Errors []struct {
			Code    string `json:"code"`
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not an error envelope: %v\n%s", err, rec.Body.String())
	}
	if len(body.Errors) != 1 {
		t.Fatalf("expected exactly 1 error, got %s", rec.Body.String())
	}
	return rec.Code, body.Errors[0].Code, body.Errors[0].Field
}

func TestExchangeToken_Success_RawBodyNoEnvelope(t *testing.T) {
	fake := &fakeExchanger{resp: auth.TokenExchangeResponse{Token: "signed.jwt.token", ExpiresAt: "2026-10-18T12:00:00Z"}}
	h := NewHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", strings.NewReader(`{"googleIdToken":"raw-google-token"}`))
	rec := httptest.NewRecorder()
	h.ExchangeToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.gotToken != "raw-google-token" {
		t.Fatalf("expected the decoded googleIdToken to be forwarded, got %q", fake.gotToken)
	}

	// Confirm the body is the RAW {token, expiresAt} shape — no data/meta envelope (task 152
	// §1.2 finding 5 / docs/openapi/paths/auth.yaml's unwrapped 200 example).
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	if _, ok := body["data"]; ok {
		t.Fatalf("expected no data/meta envelope, got %s", rec.Body.String())
	}
	if _, ok := body["meta"]; ok {
		t.Fatalf("expected no data/meta envelope, got %s", rec.Body.String())
	}
	var token, expiresAt string
	if err := json.Unmarshal(body["token"], &token); err != nil || token != "signed.jwt.token" {
		t.Fatalf("expected token=signed.jwt.token, got %s", rec.Body.String())
	}
	if err := json.Unmarshal(body["expiresAt"], &expiresAt); err != nil || expiresAt != "2026-10-18T12:00:00Z" {
		t.Fatalf("expected expiresAt=2026-10-18T12:00:00Z, got %s", rec.Body.String())
	}
}

func TestExchangeToken_MalformedBody_Is422(t *testing.T) {
	h := NewHandler(&fakeExchanger{})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", strings.NewReader(`{"googleIdToken":`))
	rec := httptest.NewRecorder()
	h.ExchangeToken(rec, req)

	status, code, _ := decodeErrorBody(t, rec)
	if status != http.StatusUnprocessableEntity || code != "VALIDATION_ERROR" {
		t.Fatalf("expected 422 VALIDATION_ERROR, got %d %s: %s", status, code, rec.Body.String())
	}
}

func TestExchangeToken_ServiceAppError_PropagatesStatusAndCode(t *testing.T) {
	// Every one of Service.Exchange's failure branches (invalid Google token post-FIX-2,
	// email not verified, email not allowlisted) returns an *httpx.AppError{401, AUTH_ERROR}.
	// The handler must forward that through httpx.WriteError unchanged, not swallow it into a
	// generic 500.
	fake := &fakeExchanger{err: httpx.NewAuthError(http.StatusUnauthorized, "Email is not in the allowlist")}
	h := NewHandler(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", strings.NewReader(`{"googleIdToken":"raw-google-token"}`))
	rec := httptest.NewRecorder()
	h.ExchangeToken(rec, req)

	status, code, _ := decodeErrorBody(t, rec)
	if status != http.StatusUnauthorized || code != "AUTH_ERROR" {
		t.Fatalf("expected 401 AUTH_ERROR, got %d %s: %s", status, code, rec.Body.String())
	}
}
