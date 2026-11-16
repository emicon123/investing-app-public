package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBackfillSnapshot_MalformedBodyIs400 and TestBackfillSnapshot_InvalidDateIs400
// pin the request-validation branches of ADR-097's admin.Handler.BackfillSnapshot
// (POST /admin/historia/backfill-snapshot) — both fail before ever touching
// h.historia, so no database is needed here (the underlying
// historia.Snapshotter.BackfillSnapshot behavior itself is covered by
// internal/historia's own real-Postgres integration test).
func TestBackfillSnapshot_MalformedBodyIs400(t *testing.T) {
	h := NewHandler(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/historia/backfill-snapshot",
		strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	h.BackfillSnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBackfillSnapshot_InvalidDateIs400(t *testing.T) {
	h := NewHandler(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/historia/backfill-snapshot",
		strings.NewReader(`{"date":"not-a-date"}`))
	rec := httptest.NewRecorder()
	h.BackfillSnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
