package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLongRunning_MatchesOnlyTheTwoLongPosts(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/api/inflation/sync", true},
		{http.MethodPost, "/api/market-data/macro-state/refresh", true},
		{http.MethodGet, "/api/inflation/sync", false},
		{http.MethodPost, "/api/inflation", false},
		{http.MethodGet, "/api/inflation", false},
		{http.MethodGet, "/api/market-data/macro-state", false},
		{http.MethodPost, "/api/market-data/macro/m2-manual", false},
		{http.MethodPost, "/api/market-data/prices/refresh", false},
		{http.MethodPost, "/api/market-data/fx-rates/refresh", false},
	}
	for _, c := range cases {
		if got := longRunning(c.method, c.path); got != c.want {
			t.Errorf("longRunning(%s %s) = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}

// deadlineThroughRouter issues one request through NewRouter and reports the ctx deadline
// the handler actually sees — the value chi's Timeout derives from the scoped duration.
func deadlineThroughRouter(t *testing.T, method, path string) time.Duration {
	t.Helper()
	var remaining time.Duration
	var ok bool
	r := NewRouter()
	r.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		dl, hasDL := req.Context().Deadline()
		ok = hasDL
		if hasDL {
			remaining = time.Until(dl)
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s: expected 200, got %d", method, path, rec.Code)
	}
	if !ok {
		t.Fatalf("%s %s: expected a ctx deadline, got none", method, path)
	}
	return remaining
}

func TestScopedTimeout_LongPostsGet180sRestGet30s(t *testing.T) {
	for _, path := range []string{"/api/inflation/sync", "/api/market-data/macro-state/refresh"} {
		got := deadlineThroughRouter(t, http.MethodPost, path)
		if got <= 60*time.Second || got > LongRequestTimeout {
			t.Errorf("POST %s: expected deadline ~180s (remaining in (60s,180s]), got %v", path, got)
		}
	}
	for _, tc := range [][2]string{
		{http.MethodGet, "/api/inflation"},
		{http.MethodPost, "/api/inflation"},
		{http.MethodGet, "/api/market-data/macro-state"},
		{http.MethodPost, "/api/market-data/macro/m2-manual"},
	} {
		got := deadlineThroughRouter(t, tc[0], tc[1])
		if got <= 0 || got > DefaultTimeout {
			t.Errorf("%s %s: expected deadline ~30s (remaining in (0,30s]), got %v", tc[0], tc[1], got)
		}
	}
}

// TestScopedTimeout_SyncSurvivesPast30s is the flip-bloker regression proof without a 30s+
// test runtime: a handler blocked 35s (the real GUS SDP sweep: ~27 lat x 300ms + retry,
// ~34s) must still answer 200 on POST /inflation/sync, while the same handler on a
// normal route is cut by the 30s scope (chi answers 504).
func TestScopedTimeout_SyncSurvivesPast30s(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a 35s slow-handler run")
	}
	block35s := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return // chi cancelled first — Timeout middleware answers 504 itself
		case <-time.After(35 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	})

	r := NewRouter()
	r.Post("/api/inflation/sync", block35s)
	r.Post("/api/inflation", block35s)

	req := httptest.NewRequest(http.MethodPost, "/api/inflation/sync", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/inflation/sync blocked 35s: expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/inflation", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("POST /api/inflation blocked 35s: expected 504 (30s scope intact), got %d", rec.Code)
	}
}

// TestNewRouter_DoesNotRewriteRemoteAddrFromForwardedHeaders pins F1.4: chi's RealIP
// middleware trusts X-Forwarded-For / X-Real-IP from ANY peer, so a client that can reach the
// backend directly could spoof its address. The router must leave RemoteAddr as the socket peer.
func TestNewRouter_DoesNotRewriteRemoteAddrFromForwardedHeaders(t *testing.T) {
	const peer = "10.0.0.7:54321"
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP", "True-Client-IP"} {
		var seen string
		r := NewRouter()
		r.Get("/api/ping", func(w http.ResponseWriter, req *http.Request) {
			seen = req.RemoteAddr
			w.WriteHeader(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
		req.RemoteAddr = peer
		req.Header.Set(header, "1.2.3.4")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", header, rec.Code)
		}
		if seen != peer {
			t.Errorf("%s: RemoteAddr = %q, want the untouched socket peer %q", header, seen, peer)
		}
	}
}
