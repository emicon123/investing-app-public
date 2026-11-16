package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"investing-app/common-go/httpx"
)

type contextKey int

const emailContextKey contextKey = iota

// RequireAuth is a chi middleware that validates the backend JWT Bearer token (signature, expiry
// and the email allowlist) on every request, in one function.
//
// FIX 1 (task 152 §1.2 finding 2): POST /api/auth/token is exempted inside this same single
// dispatch chokepoint, never a second chain/mount point. Both portfolioB and portfolioC mount their
// generated ServerInterface (including ExchangeToken) under this one middleware (see each app's
// main.go), so one fix here serves both apps. Without this exception, the one endpoint whose entire
// purpose is to obtain a bearer token would itself require one — a real lockout, not hypothetical.
func RequireAuth(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicAuthTokenRoute(r) {
				next.ServeHTTP(w, r)
				return
			}

			token, ok := bearerToken(r)
			if !ok {
				httpx.WriteError(w, httpx.NewUnauthorizedError("Bearer token missing or invalid"))
				return
			}

			claims, err := ValidateBackendJWT(cfg, token)
			if err != nil {
				logTokenRejected(err)
				httpx.WriteError(w, httpx.NewUnauthorizedError(InvalidTokenMessage))
				return
			}

			ctx := context.WithValue(r.Context(), emailContextKey, claims.Email)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// InvalidTokenMessage is the single 401 text for a bearer token that was presented but
// rejected. The validation error (bad signature, expired, wrong issuer, email not allowlisted)
// tells an attacker which check failed and can carry the allowlisted-email probe result, so it
// goes to the log (logTokenRejected), never to the wire (task 181 / F3.7). Clients key off the
// 401 status, not this text.
const InvalidTokenMessage = "Invalid or expired token"

// logTokenRejected records why a presented bearer token was refused.
func logTokenRejected(err error) {
	slog.Info("auth: token rejected", "error", err)
}

// EmailFromContext returns the authenticated caller's email, set by RequireAuth.
func EmailFromContext(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(emailContextKey).(string)
	return email, ok
}

// isPublicAuthTokenRoute reports whether r is POST /api/auth/token — the one route every
// generated ServerInterface's ExchangeToken method answers, and the sole exception to
// RequireAuth's bearer-JWT gate (see the FIX 1 doc comment above). Both portfolioB and portfolioC mount
// their generated routes under BaseURL "/api" (see each app's main.go), so the literal path
// matches smartAuth's own literal-path-matching idiom (backend-go/cmd/server/botauth.go) used
// for root's equivalent exceptions.
func isPublicAuthTokenRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/auth/token"
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
