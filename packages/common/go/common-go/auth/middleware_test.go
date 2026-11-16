package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAuth_RejectsMissingBearerToken(t *testing.T) {
	cfg := testConfig()
	handlerCalled := false
	h := RequireAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if handlerCalled {
		t.Fatal("expected the wrapped handler not to be called")
	}
}

func TestRequireAuth_RejectsInvalidToken(t *testing.T) {
	cfg := testConfig()
	h := RequireAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer garbage")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// TestRequireAuth_BypassesAuthTokenRoute proves FIX 1 (task 152 §1.2 finding 2): a genuinely
// unauthenticated request — no Authorization header at all — reaches the wrapped handler for
// POST /api/auth/token specifically, the one route whose entire purpose is to obtain a bearer
// token in the first place. This is the exact lockout scenario the fix prevents: without it,
// the generated ExchangeToken route (mounted inside this same RequireAuth-guarded chi.Group in
// both portfolioB's and portfolioC's main.go) would 401-loop every login attempt.
func TestRequireAuth_BypassesAuthTokenRoute(t *testing.T) {
	cfg := testConfig()
	handlerCalled := false
	h := RequireAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected the bypass to let an unauthenticated POST /api/auth/token through, got %d", rec.Code)
	}
	if !handlerCalled {
		t.Fatal("expected the wrapped handler to be reached without any bearer token")
	}
}

// TestRequireAuth_DoesNotBypassOtherRoutesOrMethods confirms the FIX 1 exception is scoped
// exactly to POST /api/auth/token — every other path, and GET on that same path, still 401s
// without a bearer token.
func TestRequireAuth_DoesNotBypassOtherRoutesOrMethods(t *testing.T) {
	cfg := testConfig()
	h := RequireAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/auth/token"},
		{http.MethodGet, "/api/positions"},
		{http.MethodPost, "/api/settings"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401, got %d", c.method, c.path, rec.Code)
		}
	}
}

func TestRequireAuth_AcceptsValidTokenAndSetsEmailInContext(t *testing.T) {
	cfg := testConfig() // allowlist has "Alice@Example.com"
	token, _, err := IssueBackendJWT(cfg, "alice@example.com")
	if err != nil {
		t.Fatalf("IssueBackendJWT: %v", err)
	}

	var gotEmail string
	h := RequireAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, ok := EmailFromContext(r.Context())
		if !ok {
			t.Fatal("expected email to be present in request context")
		}
		gotEmail = email
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotEmail != "alice@example.com" {
		t.Errorf("expected email alice@example.com in context, got %q", gotEmail)
	}
}
