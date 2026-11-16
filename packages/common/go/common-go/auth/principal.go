package auth

import (
	"context"
	"net/http"
)

type principalContextKey struct{}

// withPrincipal records the authenticated principal (human email or machine
// service identity) on the request context for audit use downstream.
func withPrincipal(ctx context.Context, principal string) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext returns the authenticated principal recorded by the auth
// middleware — the caller's email for bearer JWTs, ServicePrincipal for machine
// tokens. It ports BotScalpSessionService.launcherIdentity's SecurityContext read
// for the durable SCALP_SESSION_* audit rows.
func PrincipalFromContext(ctx context.Context) (string, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(string)
	return principal, ok
}

// RequireAuthWithPrincipal is RequireAuth that additionally records the caller's
// email as the context principal for audit use.
func RequireAuthWithPrincipal(cfg Config) func(http.Handler) http.Handler {
	requireAuth := RequireAuth(cfg)
	return func(next http.Handler) http.Handler {
		return requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if email, ok := EmailFromContext(r.Context()); ok {
				r = r.WithContext(withPrincipal(r.Context(), email))
			}
			next.ServeHTTP(w, r)
		}))
	}
}

// RequireServiceTokenWithPrincipal is RequireServiceToken that additionally records
// ServicePrincipal as the context principal for audit use.
func RequireServiceTokenWithPrincipal(cfg ServiceTokenConfig) func(http.Handler) http.Handler {
	requireService := RequireServiceToken(cfg)
	return func(next http.Handler) http.Handler {
		return requireService(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), ServicePrincipal)))
		}))
	}
}
