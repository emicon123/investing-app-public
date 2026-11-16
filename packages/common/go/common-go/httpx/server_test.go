package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewServer_SetsEveryTimeout(t *testing.T) {
	h := http.NotFoundHandler()
	srv := NewServer(":8091", h)

	if srv.Addr != ":8091" {
		t.Errorf("Addr: expected :8091, got %q", srv.Addr)
	}
	if srv.Handler == nil {
		t.Error("Handler: expected the given handler, got nil")
	}
	for name, c := range map[string]struct{ got, want time.Duration }{
		"ReadHeaderTimeout": {srv.ReadHeaderTimeout, 5 * time.Second},
		"ReadTimeout":       {srv.ReadTimeout, 30 * time.Second},
		"WriteTimeout":      {srv.WriteTimeout, 190 * time.Second},
		"IdleTimeout":       {srv.IdleTimeout, 120 * time.Second},
	} {
		if c.got != c.want {
			t.Errorf("%s: expected %v, got %v", name, c.want, c.got)
		}
	}
	if srv.WriteTimeout != LongRequestTimeout+10*time.Second {
		t.Errorf("WriteTimeout must outlast LongRequestTimeout by 10s, got %v", srv.WriteTimeout)
	}
	if srv.MaxHeaderBytes != 64<<10 {
		t.Errorf("MaxHeaderBytes: expected 64 KiB, got %d", srv.MaxHeaderBytes)
	}
}

func TestHealthHandler_UpWhenPingSucceeds(t *testing.T) {
	var deadline time.Duration
	ping := func(ctx context.Context) error {
		d, ok := ctx.Deadline()
		if !ok {
			t.Error("ping context has no deadline")
		}
		deadline = time.Until(d)
		return nil
	}
	rec := httptest.NewRecorder()
	HealthHandler("investing-backend-go", ping)(rec, httptest.NewRequest(http.MethodGet, "/api/health-go", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if deadline <= 0 || deadline > time.Second {
		t.Errorf("ping deadline must be at most 1s away, got %v", deadline)
	}
	var body struct {
		Data map[string]string `json:"data"`
		Meta *Meta             `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]string{"service": "investing-backend-go", "status": "UP"}
	if len(body.Data) != len(want) {
		t.Errorf("data must hold exactly service+status (no phase), got %v", body.Data)
	}
	for k, v := range want {
		if body.Data[k] != v {
			t.Errorf("data.%s: expected %q, got %q", k, v, body.Data[k])
		}
	}
	if body.Meta == nil || body.Meta.Timestamp.IsZero() {
		t.Errorf("expected a meta envelope, got %s", rec.Body.String())
	}
}

func TestHealthHandler_503WhenPingFails(t *testing.T) {
	ping := func(context.Context) error { return errors.New("dial tcp: connection refused") }
	rec := httptest.NewRecorder()
	HealthHandler("portfolio-b-backend-go", ping)(rec, httptest.NewRequest(http.MethodGet, "/api/health-go", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Errors) != 1 || body.Errors[0].Code != CodeServiceUnavailable || body.Errors[0].Message != "database unavailable" {
		t.Fatalf("expected one SERVICE_UNAVAILABLE \"database unavailable\" error, got %+v", body.Errors)
	}
}
