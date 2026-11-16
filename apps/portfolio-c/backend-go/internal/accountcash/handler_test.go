package accountcash

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/accountcash"
	"portfolio-c/backend-go/openapi"
)

// Handler tests assert the cash wire contract: RAW AccountCashDto (no {data,meta}
// envelope — cash answers bez koperty) WITH the extra portfolio field, float64
// numbers, MANUAL / POST_PURCHASE reasons, floor-zero deduct, 400 on missing /
// negative balancePln and on an unknown {portfolio}, 404 on a missing row.

type fakeStore struct {
	snaps map[string]accountcash.Snapshot
	err   error
}

func (f *fakeStore) Load(_ context.Context, key string) (accountcash.Snapshot, error) {
	if f.err != nil {
		return accountcash.Snapshot{}, f.err
	}
	return f.snaps[key], nil
}

func (f *fakeStore) Save(_ context.Context, key string, snap accountcash.Snapshot) error {
	f.snaps[key] = snap
	return nil
}

func testService() (*accountcash.Service, *fakeStore) {
	st := &fakeStore{snaps: map[string]accountcash.Snapshot{
		"VARIABLE": {Balances: map[string]decimal.Decimal{BalancePln: decimal.RequireFromString("1500")},
			UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
	}}
	return accountcash.NewService(st, NotFoundMessage, Keys...), st
}

func decodeCash(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
	}
	if _, ok := body["data"]; ok {
		t.Fatalf("cash must answer WITHOUT the envelope, got: %s", rec.Body.String())
	}
	return body
}

func TestGet_RawDtoWithPortfolio(t *testing.T) {
	svc, _ := testService()
	h := NewHandler(svc)

	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/account-cash/VARIABLE", nil), openapi.GetParamsPortfolio("VARIABLE"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeCash(t, rec)
	if body["portfolio"] != "VARIABLE" || body["balancePln"] != 1500.0 {
		t.Fatalf("unexpected body: %v", body)
	}
	for _, k := range []string{"portfolio", "balancePln", "updatedAt", "updateReason"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("expected key %s in %v", k, body)
		}
	}
}

func TestGet_UnknownPortfolioIs400(t *testing.T) {
	svc, _ := testService()
	h := NewHandler(svc)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/account-cash/BOGUS", nil), openapi.GetParamsPortfolio("BOGUS"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdate_ManualReason(t *testing.T) {
	svc, _ := testService()
	h := NewHandler(svc)

	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPut, "/api/account-cash/VARIABLE",
		strings.NewReader(`{"balancePln":2000}`)), openapi.UpdateParamsPortfolio("VARIABLE"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeCash(t, rec)
	if body["balancePln"] != 2000.0 || body["updateReason"] != "MANUAL" || body["portfolio"] != "VARIABLE" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestDeduct_FloorsAtZero(t *testing.T) {
	svc, _ := testService()
	h := NewHandler(svc)

	rec := httptest.NewRecorder()
	h.Deduct(rec, httptest.NewRequest(http.MethodPost, "/api/account-cash/VARIABLE/deduct",
		strings.NewReader(`{"balancePln":2000}`)), openapi.DeductParamsPortfolio("VARIABLE"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeCash(t, rec)
	if body["balancePln"] != 0.0 || body["updateReason"] != "POST_PURCHASE" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestUpdate_MissingOrNegativeIs400(t *testing.T) {
	svc, _ := testService()
	h := NewHandler(svc)

	for _, payload := range []string{`{}`, `{"balancePln":null}`, `{"balancePln":-1}`} {
		rec := httptest.NewRecorder()
		h.Update(rec, httptest.NewRequest(http.MethodPut, "/api/account-cash/VARIABLE",
			strings.NewReader(payload)), openapi.UpdateParamsPortfolio("VARIABLE"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", payload, rec.Code, rec.Body.String())
		}
	}
}

func TestGet_MissingRowIs404(t *testing.T) {
	svc := accountcash.NewService(&fakeStore{err: accountcash.ErrNotFound}, NotFoundMessage, Keys...)
	h := NewHandler(svc)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/account-cash/FIXED", nil), openapi.GetParamsPortfolio("FIXED"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdate_BalanceReachesTheStoreExactly pins task 182 B5 (F3.4): balancePln decodes as a
// decimal (a typed PLN amount and 18 significant digits are stored as sent), and anything but a
// JSON number is still 400 "must be a number" — a quoted number included.
func TestUpdate_BalanceReachesTheStoreExactly(t *testing.T) {
	svc, st := testService()
	h := NewHandler(svc)
	for _, amount := range []string{"1234567.8912", "0.123456789012345678"} {
		rec := httptest.NewRecorder()
		h.Update(rec, httptest.NewRequest(http.MethodPut, "/api/account-cash/VARIABLE",
			strings.NewReader(`{"balancePln":`+amount+`}`)), openapi.UpdateParamsPortfolio("VARIABLE"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", amount, rec.Code, rec.Body.String())
		}
		if got := st.snaps["VARIABLE"].Balances[BalancePln].String(); got != amount {
			t.Errorf("stored %s, want exactly %s", got, amount)
		}
	}
	for _, payload := range []string{`{"balancePln":"5"}`, `{"balancePln":true}`, `{"balancePln":[1]}`} {
		rec := httptest.NewRecorder()
		h.Update(rec, httptest.NewRequest(http.MethodPut, "/api/account-cash/VARIABLE",
			strings.NewReader(payload)), openapi.UpdateParamsPortfolio("VARIABLE"))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "must be a number") {
			t.Errorf("%s: expected 400 must be a number, got %d: %s", payload, rec.Code, rec.Body.String())
		}
	}
}
