package main

import (
	"net/http"
	"strings"

	"investing-app/common-go/auth"
)

// smartAuth is the Go port of SecurityConfig's three filter chains (botServiceToken
// FilterChain :69-104, newsServiceTokenFilterChain :117-127, JWT resource-server
// chain :131-156), collapsed into one path-based dispatcher because Go has no
// ordered-chain matcher:
//
//   - Machine-only paths (nightly host cron + bot scripts, no Google identity):
//     POST /news/ingest, GET /news/assets, POST /news/notifications, POST /bot/plans/ingest, POST
//     /bot/calendar/sync, POST /bot/signals, and every POST/PATCH/PUT under
//     /bot/trades/* — X-Service-Token only, else 401 (cron 401s without it).
//   - Dual-audience reads (frontend JWT OR host-script token): GET /bot/calendar,
//     GET /bot/trades[/**], GET /bot/stats — either credential.
//   - Everything else (all prior phases' routes, bot/equity, bot/positions/open,
//     bot/control, bot/plans reads, bot/signals reads): backend-JWT bearer only —
//     byte-identical behavior to the RequireAuth middleware used before Phase 5.
//
// The winning principal (email or "news-ingest-service") is recorded on the context
// for the durable SCALP_SESSION_* audit rows.
func smartAuth(authCfg auth.Config, svcCfg auth.ServiceTokenConfig) func(http.Handler) http.Handler {
	requireAuth := auth.RequireAuthWithPrincipal(authCfg)
	requireService := auth.RequireServiceTokenWithPrincipal(svcCfg)
	requireEither := auth.RequireAuthOrServiceToken(authCfg, svcCfg)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case isPublicAuthToken(r):
				next.ServeHTTP(w, r)
			case isMachineOnly(r):
				requireService(next).ServeHTTP(w, r)
			case isDualRead(r):
				requireEither(next).ServeHTTP(w, r)
			default:
				requireAuth(next).ServeHTTP(w, r)
			}
		})
	}
}

// isPublicAuthToken ports SecurityConfig's `.requestMatchers("/auth/token").permitAll()` line
// (task 152 §1.2 finding 2 / FIX 1), living inside this same single dispatcher chokepoint
// rather than a second chain/mount point — POST /api/auth/token is the one endpoint whose
// entire purpose is to obtain a bearer token in the first place, so it cannot itself require
// one. Left unhandled, the generated ExchangeToken route (mounted inside this same
// smartAuth-guarded group, see mountAuthenticatedRoutes) would be unreachable by the logged-out
// caller it exists to serve — a real lockout, not hypothetical.
func isPublicAuthToken(r *http.Request) bool {
	return r.URL.Path == "/api/auth/token" && r.Method == http.MethodPost
}

// isMachineOnly ports SecurityConfig's machine-write matcher plus the news chain's
// two unconditional paths: these never accept a bearer JWT, exactly like Java
// (the bot write branch claims regardless of token presence; the news chain claims
// both its paths unconditionally).
func isMachineOnly(r *http.Request) bool {
	path, method := r.URL.Path, r.Method
	if path == "/api/news/ingest" && method == http.MethodPost {
		return true
	}
	if path == "/api/news/assets" && method == http.MethodGet {
		return true
	}
	// ADR-108 §7: the morning job's push trigger takes the same service token as /news/ingest.
	if path == "/api/news/notifications" && method == http.MethodPost {
		return true
	}
	if path == "/api/bot/plans/ingest" && method == http.MethodPost {
		return true
	}
	if path == "/api/bot/calendar/sync" && method == http.MethodPost {
		return true
	}
	if path == "/api/bot/signals" && method == http.MethodPost {
		return true
	}
	if path == "/api/bot/trades" || strings.HasPrefix(path, "/api/bot/trades/") {
		return method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut
	}
	return false
}

// isDualRead ports the bot chain's read branch (plus the one macro-state read ADR-089 names): GET
// reads on paths the frontend ALSO calls with a bearer JWT, claimed for service-token auth only when such a
// request arrives — the Either middleware reproduces the fall-through by trying
// bearer first.
func isDualRead(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := r.URL.Path
	return path == "/api/bot/calendar" ||
		path == "/api/bot/trades" ||
		strings.HasPrefix(path, "/api/bot/trades/") ||
		path == "/api/bot/stats" ||
		// ADR-089: daily-plan.sh reads macro-state with the service token (the settings page
		// reads it with the bearer JWT). Exact path only; history and refresh stay bearer-only.
		path == "/api/market-data/macro-state"
}
