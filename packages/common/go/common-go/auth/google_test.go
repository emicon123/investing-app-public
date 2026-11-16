package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/api/idtoken"

	"investing-app/common-go/httpx"
)

// TestValidateGoogleIDToken_MalformedToken_Returns401AppError is FIX 2's unit-level proof
// (task 152 §1.2 finding 3): a token idtoken.Validate rejects during structural JWT parsing —
// no dots, not three base64url segments — fails fast with no network call to Google's JWKS
// endpoint (safe to run offline/in CI), and the returned error must be an *httpx.AppError{401,
// AUTH_ERROR}, not the plain fmt.Errorf this function returned before the fix (which
// httpx.WriteError would otherwise map to a generic 500 — a real wire-contract divergence from
// Java's AuthService.exchange(), which always converts this branch to a 401).
//
// Exercising the *valid-signature-but-wrong-aud* or *expired* sub-cases would require a real
// Google-issued ID token and network access to Google's JWKS — impractical for CI (task 152
// §1.2 finding 4); covered instead by a manual burn-in smoke test against a real Google
// Sign-In token before cutover, same rigor Phase 1 used for its own manual price-refresh
// verification.
func TestValidateGoogleIDToken_MalformedToken_Returns401AppError(t *testing.T) {
	cfg := testConfig()

	_, err := ValidateGoogleIDToken(context.Background(), cfg, "not-a-valid-jwt")

	if err == nil {
		t.Fatal("expected an error for a malformed Google ID token")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *httpx.AppError (FIX 2), got %T: %v", err, err)
	}
	if appErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", appErr.Status)
	}
	if appErr.Code != httpx.CodeAuthError {
		t.Fatalf("expected code AUTH_ERROR, got %s", appErr.Code)
	}
}

// The upstream validation error (which check failed, the audience, the token structure) goes to
// the log, never to the 401 body (task 187 B2, the same leak class as F3.7).
func TestValidateGoogleIDToken_RejectionAnswersFixedMessageAndLogsDetail(t *testing.T) {
	logs := captureLogs(t)

	_, err := ValidateGoogleIDToken(context.Background(), testConfig(), "not-a-valid-jwt")

	var appErr *httpx.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *httpx.AppError, got %T: %v", err, err)
	}
	if appErr.Message != InvalidGoogleTokenMessage {
		t.Errorf("message = %q, want exactly %q", appErr.Message, InvalidGoogleTokenMessage)
	}
	// What idtoken.Validate actually said for this input; it must be in the log and nowhere on the wire.
	upstream := validateErrText(t)
	if strings.Contains(appErr.Message, upstream) || strings.Contains(err.Error(), upstream) {
		t.Errorf("error leaks the upstream detail %q: %v", upstream, err)
	}
	if !strings.Contains(logs.String(), "auth: google id token rejected") || !strings.Contains(logs.String(), upstream) {
		t.Errorf("expected the log to carry the upstream detail %q, got: %s", upstream, logs.String())
	}
}

func validateErrText(t *testing.T) string {
	t.Helper()
	_, err := idtoken.Validate(context.Background(), "not-a-valid-jwt", testConfig().GoogleClientID)
	if err == nil {
		t.Fatal("expected idtoken.Validate to reject the malformed token")
	}
	return err.Error()
}
