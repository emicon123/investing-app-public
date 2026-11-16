package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/deposit"
	"investing-app/backend-go/internal/push"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
	"investing-app/common-go/idempotency"
	"investing-app/common-go/marketdatacore"
	"investing-app/common-go/testsupport"
)

// Task 181 B4 (F3.3): POST /market-data/fx-rates/refresh and PUT
// /deposits/account-cap-usage/adjustment are in docs/openapi.yaml, so they must be answered by
// the generated ServerInterface (apiServer forwarders), not by hand-mounted chi routes that
// shadow it. generatedOnly mounts nothing but openapi.HandlerWithOptions, so a missing
// forwarder shows up as the Unimplemented 501 instead of being masked by a hand mount.
func generatedOnly(server *apiServer) http.Handler {
	r := chi.NewRouter()
	openapi.HandlerWithOptions(server, openapi.ChiServerOptions{BaseURL: "/api", BaseRouter: r})
	return r
}

func TestGeneratedInterface_RefreshFxRates_Answers204(t *testing.T) {
	h := generatedOnly(&apiServer{marketDataAPI: marketdatacore.NewHandler(newGoldenMarketService())})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/market-data/fx-rates/refresh", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from the generated route, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 must carry no body, got %q", rec.Body.String())
	}
}

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

// One authenticated request per domain (bot routes included) through the real mounted route group
// must reach that domain's handler: not 401 (auth), 404/405 (route missing) or 501 (the fallback).
// The handlers are nil, so a request that reaches one panics on it; the recovering middleware turns
// that into a 599 sentinel.
func TestRouter_EveryDomainReachesItsHandler(t *testing.T) {
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
	mountAuthenticatedRoutes(router, testAuthConfig(), nil, &apiServer{})

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/market-data/prices"},
		{http.MethodGet, "/api/market-data/macro-state"},
		{http.MethodGet, "/api/inflation"},
		{http.MethodGet, "/api/tax-returns"},
		{http.MethodGet, "/api/cash-positions"},
		{http.MethodGet, "/api/settings"},
		{http.MethodGet, "/api/history"},
		{http.MethodGet, "/api/crypto-trades"},
		{http.MethodGet, "/api/market-data/linear-regression?ticker=BTC-USD"},
		{http.MethodGet, "/api/market-data/trade-chart/tickers"},
		{http.MethodGet, "/api/binance-transfers"},
		{http.MethodGet, "/api/news"},
		{http.MethodGet, "/api/push/config"},
		{http.MethodGet, "/api/bot/calendar"},
		{http.MethodGet, "/api/bot/control"},
		{http.MethodGet, "/api/bot/plans/latest"},
		{http.MethodGet, "/api/bot/signals"},
		{http.MethodGet, "/api/bot/trades"},
		{http.MethodGet, "/api/bot/stats"},
		{http.MethodGet, "/api/positions"},
		{http.MethodGet, "/api/deposits"},
		{http.MethodGet, "/api/purchase-sessions/active"},
		{http.MethodPost, "/api/rebalancing-transfers"},
		{http.MethodPost, "/api/auth/token"},
		{http.MethodPost, "/api/admin/historia/backfill-snapshot"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			req.Header.Set("Authorization", "Bearer "+testBearer(t))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			switch rec.Code {
			case http.StatusUnauthorized, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
				t.Fatalf("expected the request to reach its handler, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// The deposit handler is reached (and answers its own 422 for a malformed body) rather than
// the Unimplemented 501; no database is touched before the body is decoded.
func TestGeneratedInterface_UpsertAccountCapAdjustment_ReachesDepositHandler(t *testing.T) {
	h := generatedOnly(&apiServer{depositAPI: deposit.NewHandler(deposit.NewService(nil, nil))})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/deposits/account-cap-usage/adjustment", strings.NewReader("{not json"))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected the deposit handler's 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var body httpx.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 {
		t.Fatalf("expected one error envelope entry, got %s (%v)", rec.Body.String(), err)
	}
	if body.Errors[0].Code != httpx.CodeValidationError {
		t.Errorf("expected %s, got %s", httpx.CodeValidationError, body.Errors[0].Code)
	}
}

// mountedGroup is main's real authenticated route group with only the named handlers wired
// (nil handlers are never called by these tests).
func mountedGroup(store idempotency.Store, md *marketdatacore.Handler, dep *deposit.Handler, pushH *push.Handler) http.Handler {
	router := httpx.NewRouter()
	mountAuthenticatedRoutes(router, testAuthConfig(), store,
		&apiServer{marketDataAPI: md, depositAPI: dep, pushAPI: pushH})
	return router
}

func TestAuthenticatedGroup_RefreshFxRates_Answers204BehindAuth(t *testing.T) {
	h := mountedGroup(nil, marketdatacore.NewHandler(newGoldenMarketService()), nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/market-data/fx-rates/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+testBearer(t))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 with a bearer token, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/market-data/fx-rates/refresh", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", rec.Code)
	}
}

func TestAuthenticatedGroup_UpsertAccountCapAdjustment_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	h := mountedGroup(idempotency.NewPgStore(pool), nil,
		deposit.NewHandler(deposit.NewService(sqlcgen.New(pool), nil)), nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	put := func(bearer bool) (int, []byte) {
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/api/deposits/account-cap-usage/adjustment",
			strings.NewReader(`{"accountType":"IKE","year":2026,"remainingPln":3000.00}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if bearer {
			req.Header.Set("Authorization", "Bearer "+testBearer(t))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}

	if status, _ := put(false); status != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", status)
	}
	status, raw := put(true)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, raw)
	}
	var body struct {
		Data struct {
			AccountType  string  `json:"accountType"`
			Year         int     `json:"year"`
			RemainingPln float64 `json:"remainingPln"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if body.Data.AccountType != "IKE" || body.Data.Year != 2026 || body.Data.RemainingPln != 3000 {
		t.Errorf("expected the recomputed IKE/2026 item with remainingPln 3000, got %s", raw)
	}
}
