package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"investing-app/backend-go/internal/deposit"
	"investing-app/backend-go/internal/push"
	"investing-app/common-go/httpx"
	"investing-app/common-go/testsupport"
)

// TestRequestID_OneIDInHeaderEnvelopeAndLog (task 182 B2, F4.2): through main's real router and
// authenticated group, one request's X-Request-Id header, envelope meta.requestId and access-log
// request_id are the same UUID — for a generated openapi.Meta response (newOpenAPIMeta(ctx), the
// inbound id reused) and for an httpx error envelope (WriteError, a generated id). No database:
// push config and the malformed-body 422 are answered before any query.
func TestRequestID_OneIDInHeaderEnvelopeAndLog(t *testing.T) {
	h := mountedGroup(nil, nil, deposit.NewHandler(deposit.NewService(nil, nil)),
		push.NewHandler(push.NewService(nil, nil, push.Config{}, nil)))
	bearer := "Bearer " + testBearer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/push/config", nil)
	req.Header.Set("Authorization", bearer)
	req.Header.Set(httpx.RequestIDHeader, uuid.NewString())
	testsupport.AssertOneRequestID(t, h, req, http.StatusOK)

	req = httptest.NewRequest(http.MethodPut, "/api/deposits/account-cap-usage/adjustment", strings.NewReader("{not json"))
	req.Header.Set("Authorization", bearer)
	testsupport.AssertOneRequestID(t, h, req, http.StatusUnprocessableEntity)
}
