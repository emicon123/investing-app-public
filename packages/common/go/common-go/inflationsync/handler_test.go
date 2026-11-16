package inflationsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

// TestHandler_SyncFromGus_WireShape pins the wire contract of POST /inflation/sync for every app
// that mounts it: 200 + SingleResponse {data: {upserts}, meta: {requestId, timestamp}}. It
// replaces the identical per-app handler tests of portfolioB and portfolioC (task 187 B5e).
func TestHandler_SyncFromGus_WireShape(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []MomEntry
		want    int
	}{
		{"one new month", []MomEntry{{Year: 2024, Month: 3, Cpi: decimal.RequireFromString("2.5")}}, 1},
		{"already in sync answers 0, not an error", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(testService(newFakeQueries(), tc.entries))

			rec := httptest.NewRecorder()
			h.SyncFromGus(rec, httptest.NewRequest(http.MethodPost, "/api/inflation/sync", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Data map[string]int `json:"data"`
				Meta struct {
					RequestID string `json:"requestId"`
					Timestamp string `json:"timestamp"`
				} `json:"meta"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not valid JSON: %v\n%s", err, rec.Body.String())
			}
			if got, ok := body.Data["upserts"]; !ok || got != tc.want || len(body.Data) != 1 {
				t.Fatalf("expected data = {upserts: %d}, got %v", tc.want, body.Data)
			}
			if body.Meta.RequestID == "" || body.Meta.Timestamp == "" {
				t.Fatalf("expected meta.{requestId,timestamp} present, got %+v", body.Meta)
			}
		})
	}
}

func TestHandler_SyncFromGus_ServiceErrorIsAnErrorEnvelope(t *testing.T) {
	failing := NewService(newFakeQueries(), func(context.Context, func(Queries) error) error {
		return errors.New("db down")
	}, stubGus{[]MomEntry{{Year: 2024, Month: 3, Cpi: decimal.RequireFromString("2.5")}}})

	rec := httptest.NewRecorder()
	NewHandler(failing).SyncFromGus(rec, httptest.NewRequest(http.MethodPost, "/api/inflation/sync", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !json.Valid([]byte(body)) || !strings.Contains(body, `"errors"`) || strings.Contains(body, "db down") {
		t.Fatalf("expected a generic error envelope without the cause, got %s", body)
	}
}
