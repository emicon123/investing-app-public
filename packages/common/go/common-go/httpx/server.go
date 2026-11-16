package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Server timeouts (task-174 finding F1.9, D174 §4.6, ADR-110 Decision 4). WriteTimeout must
// outlast the longest handler budget (LongRequestTimeout, the 180 s POSTs) or the server would
// cut a legitimate long response; nginx in front already limits request bodies to 1 MiB.
const (
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverWriteTimeout      = LongRequestTimeout + 10*time.Second
	serverIdleTimeout       = 120 * time.Second
	serverMaxHeaderBytes    = 64 << 10

	// healthPingTimeout bounds the readiness probe's database ping.
	healthPingTimeout = time.Second
)

// NewServer returns an *http.Server with every timeout set (F1.9): ReadHeaderTimeout 5s,
// ReadTimeout 30s, WriteTimeout LongRequestTimeout+10s (190s), IdleTimeout 120s,
// MaxHeaderBytes 64 KiB.
func NewServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}

// HealthHandler answers GET /api/health-go: 200 {"data":{"service":service,"status":"UP"},
// "meta":…} when ping(ctx with 1s timeout) succeeds, otherwise the standard error envelope
// with 503 SERVICE_UNAVAILABLE "database unavailable" (F1.10: a readiness check, not a
// constant). The endpoint stays unauthenticated and outside the OpenAPI contract; the compose
// healthcheck (`backend-go healthcheck`) is its consumer.
func HealthHandler(service string, ping func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthPingTimeout)
		defer cancel()
		if err := ping(ctx); err != nil {
			slog.Warn("health: database ping failed", "service", service, "error", err)
			WriteError(w, NewServiceUnavailableError("database unavailable"))
			return
		}
		WriteJSON(w, http.StatusOK, Single(map[string]string{
			"service": service,
			"status":  "UP",
		}))
	}
}
