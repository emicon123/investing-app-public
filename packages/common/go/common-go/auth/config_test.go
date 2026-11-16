package auth

import (
	"strings"
	"testing"
)

func TestLoadConfig_ScansNumberedAllowedEmailVars(t *testing.T) {
	t.Setenv("ALLOWED_EMAIL_1", "one@example.com")
	t.Setenv("ALLOWED_EMAIL_2", "two@example.com")
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-characters-long")

	cfg, err := LoadConfig("investing-app")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.AllowedEmails) != 2 {
		t.Fatalf("expected 2 allowed emails, got %v", cfg.AllowedEmails)
	}
	if !cfg.IsAllowed("ONE@EXAMPLE.COM") {
		t.Error("expected case-insensitive match on ALLOWED_EMAIL_1")
	}
	if !cfg.IsAllowed("two@example.com") {
		t.Error("expected exact match on ALLOWED_EMAIL_2")
	}
	if cfg.IsAllowed("three@example.com") {
		t.Error("expected an unlisted email to be rejected")
	}
	if cfg.JWTExpiryDays != 30 {
		t.Errorf("expected default JWTExpiryDays 30, got %d", cfg.JWTExpiryDays)
	}
}

func TestLoadConfig_RequiresJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if _, err := LoadConfig("investing-app"); err == nil {
		t.Fatal("expected an error when JWT_SECRET is unset")
	}
}

func TestLoadConfig_RequiresIssuer(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-characters-long")
	if _, err := LoadConfig(""); err == nil {
		t.Fatal("expected an error when jwtIssuer is empty")
	}
}

// TestLoadConfig_EnforcesMinimumJWTSecretLength pins F1.11: an HS256 key shorter than the
// 32-byte hash output is brute-forceable and must stop the process at startup.
func TestLoadConfig_EnforcesMinimumJWTSecretLength(t *testing.T) {
	cases := []struct {
		name    string
		secret  string
		wantErr string
	}{
		{"31 bytes is rejected", strings.Repeat("s", 31), "auth: JWT_SECRET must be at least 32 bytes"},
		{"32 bytes is accepted", strings.Repeat("s", 32), ""},
		{"longer is accepted", strings.Repeat("s", 64), ""},
		{"empty keeps its own error", "", "auth: JWT_SECRET is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", c.secret)
			_, err := LoadConfig("investing-app")
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || err.Error() != c.wantErr {
				t.Fatalf("expected error %q, got %v", c.wantErr, err)
			}
		})
	}
}
