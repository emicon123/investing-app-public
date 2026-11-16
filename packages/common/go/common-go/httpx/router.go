package httpx

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// DefaultTimeout bounds every request so one slow upstream call (Yahoo/Binance/OKX/etc.) can't hang
// a worker forever. LongRequestTimeout covers the legitimately long-running POST endpoints that
// loop over live external fetches (GUS SDP sweep, macro fetch fan-out) and already carry a 180s
// proxy_read_timeout on their exact-match nginx locations — chi must allow at least as much, or it
// cancels ctx first and the handler answers 500 while nginx would still have waited (inflation
// /sync took ~34s against a 30s default).
const (
	DefaultTimeout     = 30 * time.Second
	LongRequestTimeout = 180 * time.Second
)

// longRunning reports whether method+path is one of the legitimately long-running POST
// endpoints above. Paths are the full external paths (openapi.HandlerWithOptions mounts
// with BaseURL "/api"), so the match works regardless of auth grouping.
func longRunning(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	switch path {
	case "/api/inflation/sync", "/api/market-data/macro-state/refresh":
		return true
	default:
		return false
	}
}

// scopedTimeout applies LongRequestTimeout to the long-running POST endpoints above and
// DefaultTimeout to everything else. A single global middleware.Timeout cannot express
// this (a per-route With(Timeout) nests INSIDE the global one, so the outer 30s still
// cancels first) — hence the path-conditional here. Reuses chi's tested Timeout
// implementation per request; only the duration is scoped.
func scopedTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeout := DefaultTimeout
		if longRunning(r.Method, r.URL.Path) {
			timeout = LongRequestTimeout
		}
		middleware.Timeout(timeout)(next).ServeHTTP(w, r)
	})
}

// NewRouter returns a chi.Mux pre-wired with the baseline middleware every backend-go service
// needs regardless of which domains it currently owns: request ID propagation (RequestID, first,
// so the access log and every envelope share the id — task 182 / F4.2), panic recovery
// (never crash the process on a handler bug), and a scoped request timeout (30s default,
// 180s for the long-running POST endpoints — see scopedTimeout).
func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(RequestID)
	// No middleware.RealIP (task 181 / F1.4): it overwrites RemoteAddr from X-Forwarded-For /
	// X-Real-IP / True-Client-IP supplied by ANY peer, so a caller that reaches the backend
	// directly could forge its address. Nothing in the backends reads the client IP (the access
	// log does not log it; the rate limit lives in nginx). If an IP is ever needed, trust
	// X-Forwarded-For only when RemoteAddr is the nginx container, never unconditionally.
	r.Use(SlogLogger)
	r.Use(middleware.Recoverer)
	r.Use(scopedTimeout)
	return r
}

// SlogLogger is a chi middleware that logs each request via log/slog instead of chi's default
// stdlib-logger middleware, so backend-go's structured logs (see ADR-094 §2 / doc 13 §2 —
// stdlib log/slog, no new logging dependency) are consistent across every domain package.
func SlogLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		logRequest(r, ww.Status(), time.Since(start))
	})
}
