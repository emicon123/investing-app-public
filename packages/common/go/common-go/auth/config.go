// Package auth is the direct Go port of pl.shared.config.{SecurityConfig,SecurityProperties,
// GoogleJwtAuthenticationConverter} and pl.shared.service.AuthService: backend-issued HS256 JWT
// validation, Google RS256 ID token verification, and the email-allowlist check that gates both.
package auth

import (
	"fmt"
	"os"
	"strings"
)

// MinJWTSecretBytes is the shortest HS256 signing secret LoadConfig accepts: RFC 7518 §3.2
// requires a key at least as long as the 256-bit hash output, and a shorter one is
// brute-forceable offline from any issued token (task 181 / F1.11).
const MinJWTSecretBytes = 32

// Config holds the security settings (ALLOWED_EMAIL_*, GOOGLE_CLIENT_ID, JWT_SECRET; the Java
// SecurityProperties equivalent). JWTIssuer is deliberately NOT read from an env var — it is a
// literal per-app value (investing-app / portfolio-b / portfolio-c), never overridden via the
// environment, so each backend-go's main.go passes its own app's literal issuer string when
// constructing Config.
type Config struct {
	AllowedEmails  []string
	GoogleClientID string
	JWTSecret      string
	JWTExpiryDays  int
	JWTIssuer      string
}

// LoadConfig builds a Config from environment variables (infra/compose.yml, infra/.env.example):
// GOOGLE_CLIENT_ID, JWT_SECRET, and ALLOWED_EMAIL_1, ALLOWED_EMAIL_2, ... (a numbered-variable
// convention, not a single comma-separated list). jwtIssuer is supplied by the caller (see the
// Config doc comment above), not read from the environment. jwtExpiryDays defaults to 30.
func LoadConfig(jwtIssuer string) (Config, error) {
	cfg := Config{
		AllowedEmails:  scanAllowedEmails(),
		GoogleClientID: os.Getenv("GOOGLE_CLIENT_ID"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		JWTExpiryDays:  30,
		JWTIssuer:      jwtIssuer,
	}
	if v := os.Getenv("JWT_EXPIRY_DAYS"); v != "" {
		var days int
		if _, err := fmt.Sscanf(v, "%d", &days); err == nil && days > 0 {
			cfg.JWTExpiryDays = days
		}
	}
	if cfg.JWTSecret == "" {
		return cfg, fmt.Errorf("auth: JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < MinJWTSecretBytes {
		return cfg, fmt.Errorf("auth: JWT_SECRET must be at least %d bytes", MinJWTSecretBytes)
	}
	if jwtIssuer == "" {
		return cfg, fmt.Errorf("auth: jwtIssuer is required")
	}
	return cfg, nil
}

// scanAllowedEmails reads ALLOWED_EMAIL_1, ALLOWED_EMAIL_2, ... stopping once 20 consecutive
// numbered variables are unset, so adding a 3rd/4th allowed email needs no code change (still
// ALLOWED_EMAIL_N).
func scanAllowedEmails() []string {
	var emails []string
	misses := 0
	for i := 1; misses < 20; i++ {
		v := strings.TrimSpace(os.Getenv(fmt.Sprintf("ALLOWED_EMAIL_%d", i)))
		if v == "" {
			misses++
			continue
		}
		misses = 0
		emails = append(emails, v)
	}
	return emails
}

// IsAllowed reports whether email is present in the allowlist, case-insensitively — matching
// GoogleJwtAuthenticationConverter's `allowedEmail.equalsIgnoreCase(email)`.
func (c Config) IsAllowed(email string) bool {
	for _, allowed := range c.AllowedEmails {
		if strings.EqualFold(allowed, email) {
			return true
		}
	}
	return false
}
