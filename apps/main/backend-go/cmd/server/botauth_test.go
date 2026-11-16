package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"investing-app/common-go/auth"
)

func testAuthConfig() auth.Config {
	return auth.Config{
		AllowedEmails: []string{"human@example.com"},
		JWTSecret:     "test-secret-with-enough-length-32b",
		JWTIssuer:     "investing-app",
	}
}

func testBearer(t *testing.T) string {
	t.Helper()
	cfg := testAuthConfig()
	claims := jwt.MapClaims{
		"iss":   cfg.JWTIssuer,
		"sub":   "user",
		"email": cfg.AllowedEmails[0],
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func okNext() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

// TestSmartAuth_RoutingMatrix ports SecurityConfig:69-127's matcher as a routing
// table test: machine-only paths take X-Service-Token (cron 401s without it —
// bearer alone is rejected), dual-audience reads take either credential,
// everything else (including prior phases' routes) takes the bearer JWT only.
func TestSmartAuth_RoutingMatrix(t *testing.T) {
	authCfg := testAuthConfig()
	svcCfg := auth.ServiceTokenConfig{Token: "machine-secret"}
	h := smartAuth(authCfg, svcCfg)(okNext())
	bearer := testBearer(t)

	cases := []struct {
		name       string
		method     string
		path       string
		bearer     bool
		service    bool
		wantStatus int
	}{
		// FIX 1 (task 152 §1.2 finding 2): POST /api/auth/token bypasses every credential check —
		// the one endpoint whose entire purpose is to obtain a bearer token in the first place.
		// Neither credential is required or checked; a genuinely unauthenticated request (neither
		// bearer nor service token) must reach the wrapped handler.
		{"auth token, neither credential", "POST", "/api/auth/token", false, false, 204},
		{"auth token, bearer present (still bypassed)", "POST", "/api/auth/token", true, false, 204},
		// Bypass is scoped to POST only — GET on the same path still requires the bearer JWT
		// (falls through to the default branch, same as any other unrecognized route).
		{"auth token, GET requires bearer", "GET", "/api/auth/token", false, false, 401},
		// Machine-only writes: bearer alone 401s, service passes.
		{"news ingest, no creds", "POST", "/api/news/ingest", false, false, 401},
		{"news ingest, bearer only", "POST", "/api/news/ingest", true, false, 401},
		{"news ingest, service", "POST", "/api/news/ingest", false, true, 204},
		{"news assets, bearer only", "GET", "/api/news/assets", true, false, 401},
		{"news assets, service", "GET", "/api/news/assets", false, true, 204},
		// ADR-108 §7: the push trigger is machine-only; a bearer JWT is 401 like /news/ingest.
		{"news notifications, no creds", "POST", "/api/news/notifications", false, false, 401},
		{"news notifications, bearer only", "POST", "/api/news/notifications", true, false, 401},
		{"news notifications, service", "POST", "/api/news/notifications", false, true, 204},
		// The push subscription routes stay on the bearer-JWT branch.
		{"push subscriptions, bearer", "POST", "/api/push/subscriptions", true, false, 204},
		{"push subscriptions, service only", "POST", "/api/push/subscriptions", false, true, 401},
		{"push test, bearer", "POST", "/api/push/test-notifications", true, false, 204},
		{"push config, bearer", "GET", "/api/push/config", true, false, 204},
		{"plan ingest, bearer only", "POST", "/api/bot/plans/ingest", true, false, 401},
		{"plan ingest, service", "POST", "/api/bot/plans/ingest", false, true, 204},
		{"calendar sync, service", "POST", "/api/bot/calendar/sync", false, true, 204},
		{"signals post, service", "POST", "/api/bot/signals", false, true, 204},
		{"trades post, bearer only", "POST", "/api/bot/trades", true, false, 401},
		{"trades post, service", "POST", "/api/bot/trades", false, true, 204},
		{"trade close, service", "PATCH", "/api/bot/trades/1/close", false, true, 204},
		{"scalp claim, bearer only", "POST", "/api/bot/trades/1/scalp-claim", true, false, 401},
		// Dual-audience reads: either credential passes.
		{"calendar read, bearer", "GET", "/api/bot/calendar", true, false, 204},
		{"calendar read, service", "GET", "/api/bot/calendar", false, true, 204},
		{"calendar read, neither", "GET", "/api/bot/calendar", false, false, 401},
		{"trades read, service", "GET", "/api/bot/trades", false, true, 204},
		{"trade actions read, bearer", "GET", "/api/bot/trades/1/actions", true, false, 204},
		{"trade actions read, service", "GET", "/api/bot/trades/1/actions", false, true, 204},
		{"stats read, either", "GET", "/api/bot/stats", false, true, 204},
		{"stats read, bearer", "GET", "/api/bot/stats", true, false, 204},
		// ADR-089: daily-plan.sh reads macro-state with the service token; the settings page reads
		// it with the bearer JWT. Exact path, GET only; the refresh POST stays bearer-only.
		{"macro state read, service", "GET", "/api/market-data/macro-state", false, true, 204},
		{"macro state read, bearer", "GET", "/api/market-data/macro-state", true, false, 204},
		{"macro state read, neither", "GET", "/api/market-data/macro-state", false, false, 401},
		{"macro state history, service only", "GET", "/api/market-data/macro-state/history", false, true, 401},
		{"macro state refresh, service only", "POST", "/api/market-data/macro-state/refresh", false, true, 401},
		// Human-only: service alone 401s, bearer passes.
		{"equity, service only", "GET", "/api/bot/equity", false, true, 401},
		{"equity, bearer", "GET", "/api/bot/equity", true, false, 204},
		{"open positions, service only", "GET", "/api/bot/positions/open", false, true, 401},
		{"control get, bearer", "GET", "/api/bot/control", true, false, 204},
		{"control patch, service only", "PATCH", "/api/bot/control", false, true, 401},
		{"control patch, bearer", "PATCH", "/api/bot/control", true, false, 204},
		{"plans list, bearer", "GET", "/api/bot/plans", true, false, 204},
		{"plans latest, service only", "GET", "/api/bot/plans/latest", false, true, 401},
		{"signals list, bearer", "GET", "/api/bot/signals", true, false, 204},
		// Prior phases unchanged: bearer only.
		{"inflation, bearer", "GET", "/api/inflation", true, false, 204},
		{"inflation, service only", "GET", "/api/inflation", false, true, 401},
		{"inflation, neither", "GET", "/api/inflation", false, false, 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			if c.bearer {
				req.Header.Set("Authorization", "Bearer "+bearer)
			}
			if c.service {
				req.Header.Set(auth.ServiceTokenHeader, "machine-secret")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Fatalf("expected %d, got %d", c.wantStatus, rec.Code)
			}
		})
	}
}
