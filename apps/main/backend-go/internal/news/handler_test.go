package news

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"investing-app/backend-go/openapi"
)

// Handler tests assert the wire contract: envelope shapes ({data,meta} list vs.
// {data:{upserted},meta} single), status codes per docs/openapi.yaml (200 list/dates/
// assets/ingest, 422 validation — NOT Java's 400), and UTC-Z instant rendering.

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return body
}

func TestListNews_EmptyIs200EmptyArray(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()))
	req := httptest.NewRequest(http.MethodGet, "/api/news", nil)
	rec := httptest.NewRecorder()
	h.ListNews(rec, req, openapi.ListNewsParams{})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if string(body["data"]) != "[]" {
		t.Fatalf("expected data:[], got %s", rec.Body.String())
	}
	if body["meta"] == nil {
		t.Fatalf("expected meta, got %s", rec.Body.String())
	}
}

func TestIngestNews_Returns200Upserted(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()))
	req := httptest.NewRequest(http.MethodPost, "/api/news/ingest",
		strings.NewReader(`{"items":[{"ticker":"MSTR","assetName":"MicroStrategy","headline":"Adds 3,000 BTC","summary":"Disclosure.","sentiment":"POSITIVE","sources":[{"title":"Watcher Guru","url":"https://watcher.guru/news/x"}],"newsDate":"2026-06-17"}]}`))
	rec := httptest.NewRecorder()
	h.IngestNews(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data struct {
		Upserted int `json:"upserted"`
	}
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data.Upserted != 1 {
		t.Fatalf("expected upserted=1, got %s", string(body["data"]))
	}
}

func TestIngestNews_ValidationIs422(t *testing.T) {
	h := NewHandler(testService(newFakeQueries()))
	cases := []struct {
		name string
		body string
	}{
		{"missing items", `{}`},
		{"null items", `{"items":null}`},
		{"empty items", `{"items":[]}`},
		{"malformed json", `{"items":`},
		{"blank ticker", `{"items":[{"ticker":"","assetName":"A","headline":"H","summary":"S","newsDate":"2026-06-17"}]}`},
		{"bad sentiment", `{"items":[{"ticker":"A","assetName":"A","headline":"H","summary":"S","sentiment":"BULLISH","newsDate":"2026-06-17"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/news/ingest", strings.NewReader(c.body))
			rec := httptest.NewRecorder()
			h.IngestNews(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListNews_ItemWireShape(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	h := NewHandler(svc)
	day := warsawToday()
	if _, err := svc.Ingest(t.Context(), []IngestItem{ingestItem("MSTR", "H", day)}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/news", nil)
	rec := httptest.NewRecorder()
	h.ListNews(rec, req, openapi.ListNewsParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data []map[string]json.RawMessage
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 item, got %s", string(body["data"]))
	}
	item := data[0]
	for _, key := range []string{"ticker", "assetName", "headline", "summary", "newsDate", "createdAt", "updatedAt", "sources"} {
		if item[key] == nil {
			t.Fatalf("expected key %q in item, got %s", key, rec.Body.String())
		}
	}
	// sentiment was absent on ingest → Jackson non_absent parity demands the key be
	// omitted entirely, not rendered as null.
	if _, ok := item["sentiment"]; ok {
		t.Fatalf("expected absent sentiment to be omitted, got %s", rec.Body.String())
	}
}
