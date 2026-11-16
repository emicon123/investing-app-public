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
)

// Handler tests assert the cash wire contract: RAW AccountCashDto (no {data,meta}
// envelope — cash answers bez koperty), float64 numbers, MANUAL / POST_PURCHASE
// reasons, floor-zero deduct, 400 on negative amounts, and 404 on a missing row.

type fakeStore struct {
	snap accountcash.Snapshot
	err  error
}

func (f *fakeStore) Load(_ context.Context, _ string) (accountcash.Snapshot, error) {
	if f.err != nil {
		return accountcash.Snapshot{}, f.err
	}
	return f.snap, nil
}

func (f *fakeStore) Save(_ context.Context, _ string, snap accountcash.Snapshot) error {
	f.snap = snap
	return nil
}

func testService(seed map[string]decimal.Decimal) (*accountcash.Service, *fakeStore) {
	st := &fakeStore{snap: accountcash.Snapshot{Balances: seed, UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}}
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
	if _, ok := body["meta"]; ok {
		t.Fatalf("cash must answer WITHOUT the envelope, got: %s", rec.Body.String())
	}
	return body
}

func TestGet_RawDto(t *testing.T) {
	svc, _ := testService(map[string]decimal.Decimal{
		KeyIkeTata: decimal.RequireFromString("1234.56"), KeyIkeMama: decimal.RequireFromString("2500"),
		KeyIkzeTata: decimal.RequireFromString("5700"), KeyIkzeMama: decimal.RequireFromString("5700"),
	})
	h := NewHandler(svc)

	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/account-cash", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeCash(t, rec)
	if body["ikeTata"] != 1234.56 {
		t.Fatalf("unexpected ikeTata: %v", body)
	}
	for _, k := range []string{"ikeTata", "ikeMama", "ikzeTata", "ikzeMama", "updatedAt", "updateReason"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("expected key %s in %v", k, body)
		}
	}
}

func TestUpdate_ManualReason(t *testing.T) {
	svc, _ := testService(map[string]decimal.Decimal{})
	h := NewHandler(svc)

	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPut, "/api/account-cash",
		strings.NewReader(`{"ikeTata":100,"ikeMama":200,"ikzeTata":300,"ikzeMama":400}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeCash(t, rec)
	if body["ikeTata"] != 100.0 || body["updateReason"] != "MANUAL" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestDeduct_FloorsAtZero(t *testing.T) {
	svc, _ := testService(map[string]decimal.Decimal{
		KeyIkeTata: decimal.RequireFromString("50"), KeyIkeMama: decimal.RequireFromString("200"),
	})
	h := NewHandler(svc)

	rec := httptest.NewRecorder()
	h.Deduct(rec, httptest.NewRequest(http.MethodPost, "/api/account-cash/deduct",
		strings.NewReader(`{"ikeTata":80,"ikeMama":30}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeCash(t, rec)
	if body["ikeTata"] != 0.0 || body["ikeMama"] != 170.0 || body["updateReason"] != "POST_PURCHASE" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestUpdate_NegativeIs400(t *testing.T) {
	svc, _ := testService(map[string]decimal.Decimal{})
	h := NewHandler(svc)

	for _, path := range []string{"/api/account-cash", "/api/account-cash/deduct"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"ikeTata":-1}`))
		if path == "/api/account-cash" {
			h.Update(rec, req)
		} else {
			h.Deduct(rec, req)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestGet_MissingRowIs404(t *testing.T) {
	svc := accountcash.NewService(&fakeStore{err: accountcash.ErrNotFound}, NotFoundMessage, Keys...)
	h := NewHandler(svc)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/account-cash", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateAndDeduct_AmountsReachTheStoreExactly pins task 182 B5 (F3.4): amounts decode as
// decimals, so a typed PLN amount and 18 significant digits are stored as sent; an absent or
// null amount is still zero (update) / deducts nothing.
func TestUpdateAndDeduct_AmountsReachTheStoreExactly(t *testing.T) {
	svc, st := testService(map[string]decimal.Decimal{})
	h := NewHandler(svc)
	rec := httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPut, "/api/account-cash",
		strings.NewReader(`{"ikeTata":1234567.8912,"ikeMama":0.123456789012345678,"ikzeTata":null}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	want := map[string]string{KeyIkeTata: "1234567.8912", KeyIkeMama: "0.123456789012345678", KeyIkzeTata: "0", KeyIkzeMama: "0"}
	for k, v := range want {
		if got := st.snap.Balances[k].String(); got != v {
			t.Errorf("update: %s stored %s, want %s", k, got, v)
		}
	}

	rec = httptest.NewRecorder()
	h.Deduct(rec, httptest.NewRequest(http.MethodPost, "/api/account-cash/deduct",
		strings.NewReader(`{"ikeTata":0.0012,"ikeMama":0.000000000000000001}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("deduct: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := st.snap.Balances[KeyIkeTata].String(); got != "1234567.89" {
		t.Errorf("deduct: ikeTata %s, want 1234567.89", got)
	}
	if got := st.snap.Balances[KeyIkeMama].String(); got != "0.123456789012345677" {
		t.Errorf("deduct: ikeMama %s, want 0.123456789012345677", got)
	}
}
