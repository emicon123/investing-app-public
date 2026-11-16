package httpx

import (
	"log/slog"
	"net/http"
	"time"
)

func logRequest(r *http.Request, status int, dur time.Duration) {
	slog.Info("http_request",
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration_ms", dur.Milliseconds(),
		"request_id", RequestIDFrom(r.Context()).String(),
	)
}
