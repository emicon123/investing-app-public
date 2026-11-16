package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// BackendClaims is the claim set of the backend-issued JWT: subject + issuer + issued/expires-at
// (all standard, from RegisteredClaims) plus one custom claim — "email" (duplicated onto the
// subject too, a wire-contract detail kept from the original implementation).
type BackendClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// IssueBackendJWT signs an HS256 backend JWT, the direct equivalent of AuthService.exchange()'s
// jwtEncoder.encode(...) call. Returns the signed token string and its expiry instant.
func IssueBackendJWT(cfg Config, email string) (token string, expiresAt time.Time, err error) {
	now := time.Now().UTC()
	expiresAt = now.AddDate(0, 0, cfg.JWTExpiryDays)

	claims := BackendClaims{
		Email:     email,
		Subject:   email,
		Issuer:    cfg.JWTIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign backend jwt: %w", err)
	}
	return signed, expiresAt, nil
}

// ValidateBackendJWT parses and validates a backend-issued JWT: signature (HS256, shared secret),
// issuer, and standard expiry/not-before checks. An exp claim is mandatory
// (jwt.WithExpirationRequired): golang-jwt treats a missing exp as "never expires", which would
// make any signed token without one immortal (task 181 / F1.11). It additionally enforces the
// email-allowlist check on every request — one function does signature validation and the allowlist
// check (the Java original split these across a JwtDecoder and a Converter).
func ValidateBackendJWT(cfg Config, tokenString string) (*BackendClaims, error) {
	claims := &BackendClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Method)
		}
		return []byte(cfg.JWTSecret), nil
	}, jwt.WithIssuer(cfg.JWTIssuer), jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, fmt.Errorf("auth: invalid token: %w", err)
	}

	if claims.Email == "" {
		return nil, fmt.Errorf("auth: no email claim in token")
	}
	if !cfg.IsAllowed(claims.Email) {
		return nil, fmt.Errorf("auth: email not in allowlist: %s", claims.Email)
	}
	return claims, nil
}
