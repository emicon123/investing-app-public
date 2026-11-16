package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"investing-app/common-go/httpx"
)

// fakeValidator lets Service.Exchange's tests substitute the Google-token verification step
// (task 152 §1.2 finding 4: exercising the real ValidateGoogleIDToken against live Google JWKS
// is impractical/YAGNI for a single call site). Set on the Service's unexported
// validateGoogleIDToken field directly — a same-package white-box seam, no public API change.
func fakeValidator(claims GoogleClaims, err error) func(ctx context.Context, cfg Config, rawIDToken string) (GoogleClaims, error) {
	return func(_ context.Context, _ Config, _ string) (GoogleClaims, error) {
		return claims, err
	}
}

// assertAuthError fails the test unless err is an *httpx.AppError{401, AUTH_ERROR, message} —
// the shape every one of Exchange's 401 branches must produce (errors.go's three pre-existing
// sentinels, plus FIX 2's newly wrapped ValidateGoogleIDToken error).
func assertAuthError(t *testing.T, err error, wantMessage string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *httpx.AppError, got %T: %v", err, err)
	}
	if appErr.Status != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", appErr.Status)
	}
	if appErr.Code != httpx.CodeAuthError {
		t.Fatalf("expected code AUTH_ERROR, got %s", appErr.Code)
	}
	if appErr.Message != wantMessage {
		t.Fatalf("expected message %q, got %q", wantMessage, appErr.Message)
	}
}

// TestService_Exchange_InvalidGoogleToken_Returns401 covers Exchange's first failure branch —
// FIX 2 (task 152 §1.2 finding 3): an invalid/expired/wrong-aud Google ID token must surface as
// a 401 AUTH_ERROR through httpx.WriteError, not fall through to a generic 500. This exercises
// the exact *httpx.AppError shape ValidateGoogleIDToken now returns post-fix (see
// TestValidateGoogleIDToken_WrapsErrorAs401 in google_test.go for the unit-level proof of the
// fix itself), by simulating what a real failure from that function looks like.
func TestService_Exchange_InvalidGoogleToken_Returns401(t *testing.T) {
	cfg := testConfig()
	svc := NewService(cfg)
	svc.validateGoogleIDToken = fakeValidator(GoogleClaims{}, httpx.NewAuthError(http.StatusUnauthorized, InvalidGoogleTokenMessage))

	_, err := svc.Exchange(context.Background(), "bad-google-token")
	assertAuthError(t, err, InvalidGoogleTokenMessage)
}

// TestService_Exchange_EmailClaimMissing_Returns401 covers Exchange's second failure branch:
// a validated Google token with no "email" claim at all.
func TestService_Exchange_EmailClaimMissing_Returns401(t *testing.T) {
	cfg := testConfig()
	svc := NewService(cfg)
	svc.validateGoogleIDToken = fakeValidator(GoogleClaims{Email: "", EmailVerified: true}, nil)

	_, err := svc.Exchange(context.Background(), "raw-token")
	assertAuthError(t, err, "Google ID token does not contain an email claim")
}

// TestService_Exchange_EmailNotVerified_Returns401 covers Exchange's third failure branch: a
// verified-signature Google token whose email_verified claim is false.
func TestService_Exchange_EmailNotVerified_Returns401(t *testing.T) {
	cfg := testConfig() // allowlist has "Alice@Example.com"
	svc := NewService(cfg)
	svc.validateGoogleIDToken = fakeValidator(GoogleClaims{Email: "alice@example.com", EmailVerified: false}, nil)

	_, err := svc.Exchange(context.Background(), "raw-token")
	assertAuthError(t, err, "Google account email is not verified")
}

// TestService_Exchange_EmailNotAllowlisted_Returns401 covers Exchange's fourth failure branch:
// a verified, email_verified=true Google identity whose email is simply not in the allowlist.
func TestService_Exchange_EmailNotAllowlisted_Returns401(t *testing.T) {
	cfg := testConfig() // allowlist has "Alice@Example.com" only
	svc := NewService(cfg)
	svc.validateGoogleIDToken = fakeValidator(GoogleClaims{Email: "mallory@example.com", EmailVerified: true}, nil)

	_, err := svc.Exchange(context.Background(), "raw-token")
	assertAuthError(t, err, "Email is not in the allowlist")
}

// TestService_Exchange_Allowed_Returns200WithSignedJWT covers Exchange's success path: a
// verified, allowlisted Google identity issues a real signed backend JWT that
// ValidateBackendJWT accepts, with the wire-shape TokenExchangeResponse callers expect.
func TestService_Exchange_Allowed_Returns200WithSignedJWT(t *testing.T) {
	cfg := testConfig() // allowlist has "Alice@Example.com"
	svc := NewService(cfg)
	svc.validateGoogleIDToken = fakeValidator(GoogleClaims{Email: "alice@example.com", EmailVerified: true}, nil)

	resp, err := svc.Exchange(context.Background(), "raw-token")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected a non-empty signed backend JWT")
	}
	if resp.ExpiresAt == "" {
		t.Fatal("expected a non-empty expiresAt")
	}

	claims, err := ValidateBackendJWT(cfg, resp.Token)
	if err != nil {
		t.Fatalf("expected the issued token to validate, got: %v", err)
	}
	if claims.Email != "alice@example.com" {
		t.Fatalf("expected email alice@example.com in the issued token, got %q", claims.Email)
	}
	if claims.Issuer != cfg.JWTIssuer {
		t.Fatalf("expected issuer %q, got %q", cfg.JWTIssuer, claims.Issuer)
	}
}
