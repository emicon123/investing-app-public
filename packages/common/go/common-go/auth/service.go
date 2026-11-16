package auth

import (
	"context"
	"time"
)

// TokenExchangeResponse is the direct equivalent of pl.shared.dto.auth.TokenExchangeResponse.
type TokenExchangeResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

// Service exchanges a Google ID token for a backend JWT (POST /auth/token; ported from the Java
// AuthService, ADR-094 / doc 13 §6.4).
type Service struct {
	cfg Config

	// validateGoogleIDToken defaults to the package-level ValidateGoogleIDToken and exists as a
	// field (not a hardcoded call) purely so this package's own tests can substitute a fake
	// Google-token validator — testing the real function against live Google JWKS is
	// impractical/YAGNI for a single call site (task 152 §1.2 finding 4). Unexported: no public
	// API surface added, only a same-package white-box test seam.
	validateGoogleIDToken func(ctx context.Context, cfg Config, rawIDToken string) (GoogleClaims, error)
}

// NewService constructs an auth Service for the given app's Config.
func NewService(cfg Config) *Service {
	return &Service{cfg: cfg, validateGoogleIDToken: ValidateGoogleIDToken}
}

// Exchange validates a Google ID token and, if the caller's email is allowlisted, issues a
// backend JWT — the direct equivalent of AuthService.exchange(String googleIdToken).
func (s *Service) Exchange(ctx context.Context, googleIDToken string) (TokenExchangeResponse, error) {
	claims, err := s.validateGoogleIDToken(ctx, s.cfg, googleIDToken)
	if err != nil {
		return TokenExchangeResponse{}, err
	}
	if claims.Email == "" {
		return TokenExchangeResponse{}, errEmailClaimMissing
	}
	if !claims.EmailVerified {
		return TokenExchangeResponse{}, errEmailNotVerified
	}
	if !s.cfg.IsAllowed(claims.Email) {
		return TokenExchangeResponse{}, errEmailNotAllowed
	}

	token, expiresAt, err := IssueBackendJWT(s.cfg, claims.Email)
	if err != nil {
		return TokenExchangeResponse{}, err
	}
	return TokenExchangeResponse{
		Token:     token,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}
