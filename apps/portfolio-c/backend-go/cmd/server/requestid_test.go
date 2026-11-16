package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"investing-app/common-go/auth"
	"investing-app/common-go/httpx"
	"investing-app/common-go/settingscore"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/settings"
)

// TestRequestID_OneIDInHeaderEnvelopeAndLog (task 182 B2, F4.2): through this app's real router
// and authenticated group, one request's X-Request-Id header, envelope meta.requestId and
// access-log request_id are the same UUID — for an httpx.List envelope (the inbound id reused)
// and for a WriteError envelope (a generated id). The settings store is the golden fake, no DB.
func TestRequestID_OneIDInHeaderEnvelopeAndLog(t *testing.T) {
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
	server := &apiServer{settingsAPI: settings.NewHandler(settingscore.NewService(newGoldenSettingsStore(), settings.Validate))}
	router := httpx.NewRouter()
	mountAuthenticatedRoutes(router, authCfg, nil, server)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set(httpx.RequestIDHeader, uuid.NewString())
	testsupport.AssertOneRequestID(t, router, req, http.StatusOK)

	req = httptest.NewRequest(http.MethodPut, "/api/settings/no_such_key", strings.NewReader(`{"value":"1"}`))
	req.Header.Set("Authorization", "Bearer "+bearer)
	testsupport.AssertOneRequestID(t, router, req, http.StatusNotFound)
}
