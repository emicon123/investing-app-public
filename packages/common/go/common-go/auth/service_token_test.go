package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestServiceToken_Valid(t *testing.T) {
	cfg := ServiceTokenConfig{Token: "secret"}
	if !cfg.Valid("secret") {
		t.Fatal("expected exact token to validate")
	}
	if cfg.Valid("SECRET") {
		t.Fatal("comparison must be case-sensitive")
	}
	if cfg.Valid("") {
		t.Fatal("empty provided must not validate")
	}
	if (ServiceTokenConfig{}).Valid("secret") {
		t.Fatal("blank expected token must fail closed, never open")
	}
}

func TestRequireServiceToken(t *testing.T) {
	cfg := ServiceTokenConfig{Token: "secret"}
	h := RequireServiceToken(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/news/ingest", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/news/ingest", nil)
	req.Header.Set(ServiceTokenHeader, "wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/news/ingest", nil)
	req.Header.Set(ServiceTokenHeader, "secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with valid token, got %d: %s", rec.Code, rec.Body.String())
	}
}

func bearerFor(t *testing.T, cfg Config) string {
	t.Helper()
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

func TestRequireAuthOrServiceToken(t *testing.T) {
	authCfg := Config{
		AllowedEmails: []string{"human@example.com"},
		JWTSecret:     "test-secret-with-enough-length-32b",
		JWTIssuer:     "investing-app",
	}
	svcCfg := ServiceTokenConfig{Token: "machine-secret"}
	h := RequireAuthOrServiceToken(authCfg, svcCfg)(okHandler())

	cases := []struct {
		name       string
		setup      func(r *http.Request)
		wantStatus int
	}{
		{"valid bearer passes", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+bearerFor(t, authCfg))
		}, http.StatusNoContent},
		{"valid service token passes", func(r *http.Request) {
			r.Header.Set(ServiceTokenHeader, "machine-secret")
		}, http.StatusNoContent},
		{"neither credential is 401", func(r *http.Request) {}, http.StatusUnauthorized},
		{"bad bearer without service token is 401", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer garbage")
		}, http.StatusUnauthorized},
		{"bad bearer with valid service token passes", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer garbage")
			r.Header.Set(ServiceTokenHeader, "machine-secret")
		}, http.StatusNoContent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/bot/calendar", nil)
			c.setup(req)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Fatalf("expected %d, got %d: %s", c.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPrincipalFromContext(t *testing.T) {
	authCfg := Config{
		AllowedEmails: []string{"human@example.com"},
		JWTSecret:     "test-secret-with-enough-length-32b",
		JWTIssuer:     "investing-app",
	}
	svcCfg := ServiceTokenConfig{Token: "machine-secret"}

	seen := ""
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	// Service-token calls record the machine identity for the SCALP_SESSION_*
	// audit rows (BotScalpSessionService.launcherIdentity port).
	req := httptest.NewRequest(http.MethodPost, "/api/bot/trades/1/scalp-claim", nil)
	req.Header.Set(ServiceTokenHeader, "machine-secret")
	RequireAuthOrServiceToken(authCfg, svcCfg)(probe).ServeHTTP(httptest.NewRecorder(), req)
	if seen != ServicePrincipal {
		t.Fatalf("expected %q principal, got %q", ServicePrincipal, seen)
	}

	// Bearer calls record the human email.
	seen = ""
	req = httptest.NewRequest(http.MethodGet, "/api/bot/calendar", nil)
	req.Header.Set("Authorization", "Bearer "+bearerFor(t, authCfg))
	RequireAuthOrServiceToken(authCfg, svcCfg)(probe).ServeHTTP(httptest.NewRecorder(), req)
	if seen != "human@example.com" {
		t.Fatalf("expected email principal, got %q", seen)
	}
}
