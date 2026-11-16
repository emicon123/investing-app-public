package auth

import (
	"context"
	"log/slog"
	"net/http"

	"google.golang.org/api/idtoken"

	"investing-app/common-go/httpx"
)

// InvalidGoogleTokenMessage is the single 401 text for a Google ID token idtoken.Validate
// refused. The upstream error says which check failed (signature, audience, expiry, structure),
// so it goes to the log, never to the wire (task 187 B2, the same pattern as InvalidTokenMessage,
// F3.7). Clients key off the 401 status, not this text.
const InvalidGoogleTokenMessage = "Invalid Google ID token"

// GoogleClaims is the subset of the verified Google ID token payload AuthService.exchange()
// actually reads: the email claim and its verification flag.
type GoogleClaims struct {
	Email         string
	EmailVerified bool
}

// ValidateGoogleIDToken verifies an inbound Google ID token's signature (RS256, against Google's
// own published JWKS, fetched and cached internally by the official idtoken client — no third-party
// fork per ADR-094 §2), standard claims (exp/iat), and the `aud` claim against cfg.GoogleClientID.
//
// FIX 2 (task 152 §1.2 finding 3): the returned error is an *httpx.AppError (401), not a plain
// fmt.Errorf — Service.Exchange's first failure branch propagates this error unchanged, and
// httpx.WriteError only maps *AppError to a controlled status; anything else falls through to a
// generic 500. An invalid token is a client error, so this wrap is required for the wire contract
// (401), not an enhancement.
func ValidateGoogleIDToken(ctx context.Context, cfg Config, rawIDToken string) (GoogleClaims, error) {
	payload, err := idtoken.Validate(ctx, rawIDToken, cfg.GoogleClientID)
	if err != nil {
		slog.Info("auth: google id token rejected", "error", err)
		return GoogleClaims{}, httpx.NewAuthError(http.StatusUnauthorized, InvalidGoogleTokenMessage)
	}

	email, _ := payload.Claims["email"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)

	return GoogleClaims{Email: email, EmailVerified: verified}, nil
}
