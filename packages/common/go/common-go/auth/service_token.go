package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"

	"investing-app/common-go/httpx"
)

// ServiceTokenHeader is the direct equivalent of ServiceTokenAuthFilter.HEADER_NAME —
// the static-secret header the nightly host cron jobs (news research, bot daily-plan,
// scalp launcher) present instead of a Google-JWT user identity (ADR-041, ADR-089).
const ServiceTokenHeader = "X-Service-Token"

// ServiceTokenConfig holds the single shared machine secret. One token guards both the
// news machine endpoints and every bot machine-write path server-side —
// infra/.env.example documents BOT_INGEST_TOKEN as a client-side alias only ("There is
// no separate token validated server-side today"), so Go reads NEWS_INGEST_TOKEN exactly
// like ServiceTokenAuthAdapter reads app.news.ingest-token.
type ServiceTokenConfig struct {
	Token string
}

// LoadServiceTokenConfig builds a ServiceTokenConfig from NEWS_INGEST_TOKEN. An empty
// token is legal here (dev shells without the secret) — Valid then rejects everything,
// mirroring the adapter's blank-expected early return, so machine paths fail closed
// rather than fail open.
func LoadServiceTokenConfig() ServiceTokenConfig {
	return ServiceTokenConfig{Token: os.Getenv("NEWS_INGEST_TOKEN")}
}

// Valid reports whether provided equals the configured secret using a constant-time
// comparison, mirroring ServiceTokenAuthAdapter.adapt's MessageDigest.isEqual. A blank
// expected token never validates.
func (c ServiceTokenConfig) Valid(provided string) bool {
	if provided == "" || c.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(c.Token)) == 1
}

// ServicePrincipal is the context principal recorded for machine-authenticated calls —
// the direct equivalent of the adapter's "news-ingest-service" principal.
const ServicePrincipal = "news-ingest-service"

// RequireServiceToken is a chi middleware accepting only requests carrying a valid
// X-Service-Token — the direct equivalent of the newsServiceTokenFilterChain /
// botServiceTokenFilterChain machine-only branches in SecurityConfig. Failures answer
// 401 with the AUTH_ERROR code and the exact message ServiceTokenAuthFilter writes
// ("Missing or invalid X-Service-Token"); the filter's raw body carries no meta
// envelope, but every Go error goes through the shared ErrorResponse envelope so the
// frontend sees one error shape — status code and message match byte-for-byte.
func RequireServiceToken(cfg ServiceTokenConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Valid(r.Header.Get(ServiceTokenHeader)) {
				httpx.WriteError(w, httpx.NewUnauthorizedError("Missing or invalid X-Service-Token"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuthOrServiceToken is a chi middleware accepting EITHER a valid backend-JWT
// bearer token OR a valid X-Service-Token — the direct equivalent of SecurityConfig's
// dual-audience bot read branch (botServiceTokenFilterChain's isBotReadPath &&
// hasServiceToken matcher falling through to the JWT chain otherwise). Bearer is tried
// first so human callers keep the exact RequireAuth behavior; the service token is the
// fallback for machine callers with no Google identity. The winning principal is
// recorded on the context (email or ServicePrincipal) for audit use; a double failure
// answers 401 with the message of the chain the caller appeared to target (service
// message when a service token was presented, bearer message otherwise).
func RequireAuthOrServiceToken(authCfg Config, svcCfg ServiceTokenConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var bearerErr error
			if token, ok := bearerToken(r); ok {
				claims, err := ValidateBackendJWT(authCfg, token)
				if err == nil {
					ctx := context.WithValue(r.Context(), emailContextKey, claims.Email)
					next.ServeHTTP(w, r.WithContext(withPrincipal(ctx, claims.Email)))
					return
				}
				bearerErr = err
			}
			provided := r.Header.Get(ServiceTokenHeader)
			if svcCfg.Valid(provided) {
				next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), ServicePrincipal)))
				return
			}
			if bearerErr != nil {
				logTokenRejected(bearerErr)
			}
			if provided != "" {
				httpx.WriteError(w, httpx.NewUnauthorizedError("Missing or invalid X-Service-Token"))
				return
			}
			httpx.WriteError(w, httpx.NewUnauthorizedError("Bearer token missing or invalid"))
		})
	}
}
