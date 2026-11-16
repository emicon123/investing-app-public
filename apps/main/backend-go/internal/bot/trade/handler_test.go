package trade

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"investing-app/backend-go/openapi"
)

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return body
}

func TestCreateBotTradeHandler_201And422(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()))
	req := httptest.NewRequest(http.MethodPost, "/api/bot/trades",
		strings.NewReader(`{"side":"LONG","quantity":0.01,"entryPrice":60000,"leverage":3}`))
	rec := httptest.NewRecorder()
	h.CreateBotTrade(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeEnvelope(t, rec)
	var data map[string]any
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data["side"] != "LONG" || data["status"] != "OPEN" {
		t.Fatalf("unexpected trade: %s", string(body["data"]))
	}

	for _, payload := range []string{
		`{"quantity":0.01,"entryPrice":60000}`,
		`{"side":"LONG","entryPrice":60000}`,
		`{"side":"MOON","quantity":0.01,"entryPrice":60000}`,
		`{"side":"LONG","quantity":0,"entryPrice":60000}`,
		`{"side":"LONG","quantity":0.01,"entryPrice":60000,"leverage":11}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/bot/trades", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		h.CreateBotTrade(rec, req)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for %s, got %d: %s", payload, rec.Code, rec.Body.String())
		}
	}
}

func TestAddBotTradeTicksHandler_ReturnsList201(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	h := NewHandler(svc)
	if _, err := svc.Create(t.Context(), createInput("LONG", "0.01", "60000", nil)); err != nil {
		t.Fatal(err)
	}
	// The quirk: POST ticks answers 201 with the stored TICKS LIST (not a single).
	req := httptest.NewRequest(http.MethodPost, "/api/bot/trades/1/ticks",
		strings.NewReader(`{"ticks":[{"price":60100,"atr1m":12.5},{"volume":2}]}`))
	rec := httptest.NewRecorder()
	h.AddBotTradeTicks(rec, req, 1)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeEnvelope(t, rec)
	var data []map[string]any
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 ticks, got %s", string(body["data"]))
	}
	// Serialized as atr1m (Java record component name), never atr_1m.
	raw := string(body["data"])
	if !strings.Contains(raw, `"atr1m":12.5`) || strings.Contains(raw, "atr_1m") {
		t.Fatalf("expected atr1m key, got %s", raw)
	}
	// Empty batch → empty list, still 201.
	req = httptest.NewRequest(http.MethodPost, "/api/bot/trades/1/ticks",
		strings.NewReader(`{"ticks":[]}`))
	rec = httptest.NewRecorder()
	h.AddBotTradeTicks(rec, req, 1)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), `"data":null`) {
		t.Fatalf("expected 201 with [] (never null), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPendingScalpAndOpenPositions_ListEnvelopeWithoutPage(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	h := NewHandler(svc)
	if _, err := svc.Create(t.Context(), createInput("LONG", "0.01", "60000", nil)); err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachPlacedAudit(t.Context(), 1, "algo-1", nil, nil); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/bot/trades/pending-scalp", nil)
	rec := httptest.NewRecorder()
	h.ListPendingScalpTrades(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeEnvelope(t, rec)
	if body["page"] != nil {
		t.Fatalf("pending-scalp must be a ListResponse WITHOUT page, got %s", rec.Body.String())
	}
	var data []map[string]any
	if err := json.Unmarshal(body["data"], &data); err != nil || len(data) != 1 {
		t.Fatalf("expected 1 pending row, got %s", rec.Body.String())
	}
}

func TestRequestBotTradeManage_202WithoutDBWrite(t *testing.T) {
	f := newFakeQueries()
	h := NewHandler(testService(f))
	// Unknown id still 202s — the stub performs no lookup and no write.
	req := httptest.NewRequest(http.MethodPost, "/api/bot/trades/999/request-manage", nil)
	rec := httptest.NewRecorder()
	h.RequestBotTradeManage(rec, req, 999)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeEnvelope(t, rec)
	var data map[string]any
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data["tradeId"] != float64(999) || data["status"] != "accepted" {
		t.Fatalf("unexpected ack: %s", string(body["data"]))
	}
	if len(f.trades) != 0 || len(f.actions) != 0 {
		t.Fatal("request-manage must not write anything")
	}
}

func TestGetBotTradeHandler_404(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()))
	req := httptest.NewRequest(http.MethodGet, "/api/bot/trades/999", nil)
	rec := httptest.NewRecorder()
	h.GetBotTrade(rec, req, 999)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListBotTradesHandler_PagedEnriched(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	if _, err := svc.Create(t.Context(), createInput("LONG", "0.01", "60000", nil)); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(svc)
	status := openapi.ListBotTradesParamsStatus("OPEN")
	req := httptest.NewRequest(http.MethodGet, "/api/bot/trades?status=OPEN", nil)
	rec := httptest.NewRecorder()
	h.ListBotTrades(rec, req, openapi.ListBotTradesParams{Status: &status})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeEnvelope(t, rec)
	if body["page"] == nil {
		t.Fatalf("expected paged envelope with page, got %s", rec.Body.String())
	}
	var page struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body["page"], &page); err != nil || page.Total != 1 {
		t.Fatalf("expected page.total=1, got %s", string(body["page"]))
	}
}
