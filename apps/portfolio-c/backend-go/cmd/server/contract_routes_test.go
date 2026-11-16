package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"

	"investing-app/common-go/auth"
	"portfolio-c/backend-go/openapi"
)

// Every operation of the generated ServerInterface must be served by one of the embedded handlers:
// none may fall through to the openapi.Unimplemented 501 default. apiServer promotes handler
// methods by name, so a handler whose method is renamed or whose signature drifts from the
// generated one would otherwise silently turn its route into a 501. The handlers are nil here: a
// served operation panics on its nil handler (recovered), an unserved one answers 501.
func TestEveryOperationIsServedByAHandler(t *testing.T) {
	iface := reflect.TypeFor[openapi.ServerInterface]()
	server := reflect.ValueOf(&apiServer{})
	for op := range iface.Methods() {
		args := []reflect.Value{
			reflect.ValueOf(httptest.NewRecorder()),
			reflect.ValueOf(httptest.NewRequest(http.MethodGet, "/", nil)),
		}
		for j := 2; j < op.Type.NumIn(); j++ {
			args = append(args, reflect.Zero(op.Type.In(j)))
		}
		rec := args[0].Interface().(*httptest.ResponseRecorder)
		func() {
			defer func() { _ = recover() }() // a nil handler dereference: the operation is served
			server.MethodByName(op.Name).Call(args)
		}()
		if rec.Code == http.StatusNotImplemented {
			t.Errorf("operation %s is not served by any handler (fell through to the 501 default)", op.Name)
		}
	}
}

// One authenticated request per domain through the real mounted route group must reach that
// domain's handler: not 401 (auth), 404/405 (route missing) or 501 (the fallback). The handlers are
// nil, so a request that reaches one panics on it; the recovering middleware turns that into a 599
// sentinel.
func TestRouter_EveryDomainReachesItsHandler(t *testing.T) {
	authCfg := auth.Config{
		AllowedEmails: []string{"owner@example.com"},
		JWTSecret:     "test-secret-with-enough-length-32b",
		JWTExpiryDays: 1,
		JWTIssuer:     jwtIssuer,
	}
	bearer, _, err := auth.IssueBackendJWT(authCfg, authCfg.AllowedEmails[0])
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recover() != nil {
					w.WriteHeader(599)
				}
			}()
			next.ServeHTTP(w, r)
		})
	})
	mountAuthenticatedRoutes(router, authCfg, nil, &apiServer{})

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/settings"},
		{http.MethodGet, "/api/account-cash/VARIABLE"},
		{http.MethodGet, "/api/history?portfolio=VARIABLE"},
		{http.MethodGet, "/api/deposits/xirr?portfolio=VARIABLE"},
		{http.MethodGet, "/api/market-data/prices"},
		{http.MethodGet, "/api/purchases/history?portfolio=VARIABLE"},
		{http.MethodPost, "/api/positions"},
		{http.MethodPost, "/api/deposits"},
		{http.MethodPost, "/api/position-transfers"},
		{http.MethodPost, "/api/inflation/sync"},
		{http.MethodPost, "/api/auth/token"},
		{http.MethodPost, "/api/admin/history/backfill-position-values"},
		{http.MethodPost, "/api/admin/historia/backfill-snapshot"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			req.Header.Set("Authorization", "Bearer "+bearer)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			switch rec.Code {
			case http.StatusUnauthorized, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
				t.Fatalf("expected the request to reach its handler, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
