package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testConfig() Config {
	return Config{
		AllowedEmails: []string{"Alice@Example.com"},
		JWTSecret:     "test-secret-at-least-32-characters-long",
		JWTExpiryDays: 30,
		JWTIssuer:     "investing-app",
	}
}

func TestIssueAndValidateBackendJWT_RoundTrip(t *testing.T) {
	cfg := testConfig()

	token, expiresAt, err := IssueBackendJWT(cfg, "alice@example.com")
	if err != nil {
		t.Fatalf("IssueBackendJWT: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expected expiry in the future, got %v", expiresAt)
	}

	claims, err := ValidateBackendJWT(cfg, token)
	if err != nil {
		t.Fatalf("ValidateBackendJWT: %v", err)
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %q", claims.Email)
	}
	if claims.Issuer != cfg.JWTIssuer {
		t.Errorf("expected issuer %q, got %q", cfg.JWTIssuer, claims.Issuer)
	}
}

func TestValidateBackendJWT_RejectsWrongIssuer(t *testing.T) {
	cfg := testConfig()
	token, _, err := IssueBackendJWT(cfg, "alice@example.com")
	if err != nil {
		t.Fatalf("IssueBackendJWT: %v", err)
	}

	otherAppCfg := cfg
	otherAppCfg.JWTIssuer = "portfolio-b" // cross-app token reuse must fail
	if _, err := ValidateBackendJWT(otherAppCfg, token); err == nil {
		t.Fatal("expected validation to fail for a token issued under a different app's issuer")
	}
}

func TestValidateBackendJWT_RejectsWrongSecret(t *testing.T) {
	cfg := testConfig()
	token, _, err := IssueBackendJWT(cfg, "alice@example.com")
	if err != nil {
		t.Fatalf("IssueBackendJWT: %v", err)
	}

	tampered := cfg
	tampered.JWTSecret = "a-completely-different-secret-value-32c"
	if _, err := ValidateBackendJWT(tampered, token); err == nil {
		t.Fatal("expected validation to fail for a token signed with a different secret")
	}
}

func TestValidateBackendJWT_RejectsEmailNotInAllowlist(t *testing.T) {
	cfg := testConfig()
	token, _, err := IssueBackendJWT(cfg, "mallory@example.com")
	if err != nil {
		t.Fatalf("IssueBackendJWT: %v", err)
	}

	if _, err := ValidateBackendJWT(cfg, token); err == nil {
		t.Fatal("expected validation to fail for an email not in the allowlist")
	}
}

func TestValidateBackendJWT_AllowlistIsCaseInsensitive(t *testing.T) {
	cfg := testConfig() // allowlist has "Alice@Example.com"
	token, _, err := IssueBackendJWT(cfg, "alice@example.com")
	if err != nil {
		t.Fatalf("IssueBackendJWT: %v", err)
	}

	if _, err := ValidateBackendJWT(cfg, token); err != nil {
		t.Fatalf("expected case-insensitive allowlist match to succeed, got %v", err)
	}
}

func TestValidateBackendJWT_RejectsGarbage(t *testing.T) {
	cfg := testConfig()
	if _, err := ValidateBackendJWT(cfg, "not-a-jwt"); err == nil {
		t.Fatal("expected validation to fail for a malformed token")
	}
}

// TestValidateBackendJWT_RejectsTokenWithoutExp pins F1.11: a correctly signed, correctly
// issued token with no exp claim would otherwise be valid forever.
func TestValidateBackendJWT_RejectsTokenWithoutExp(t *testing.T) {
	cfg := testConfig()
	noExp := signClaims(t, cfg.JWTSecret, jwt.MapClaims{
		"iss":   cfg.JWTIssuer,
		"sub":   "alice@example.com",
		"email": "alice@example.com",
		"iat":   time.Now().Unix(),
	})
	if _, err := ValidateBackendJWT(cfg, noExp); err == nil {
		t.Fatal("expected validation to fail for a signed token without an exp claim")
	}

	h := RequireAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+noExp)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a token without exp, got %d", rec.Code)
	}
}
