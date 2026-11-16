package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
)

type fakeQueries struct {
	row sqlcgen.BotControl
}

func newFakeQueries(enabled bool) *fakeQueries {
	return &fakeQueries{row: sqlcgen.BotControl{
		ID: 1, TradingEnabled: enabled,
		UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}}
}

func (f *fakeQueries) GetBotControl(_ context.Context) (sqlcgen.BotControl, error) {
	return f.row, nil
}

func (f *fakeQueries) UpdateBotControl(_ context.Context, arg sqlcgen.UpdateBotControlParams) (sqlcgen.BotControl, error) {
	f.row.TradingEnabled = arg.TradingEnabled
	f.row.PausedReason = arg.PausedReason
	f.row.UpdatedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	return f.row, nil
}

func TestGet_ReturnsSeedRow(t *testing.T) {
	dto, err := NewService(newFakeQueries(true)).Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !dto.TradingEnabled || dto.PausedReason != nil {
		t.Fatalf("unexpected control: %+v", dto)
	}
}

func TestUpdate_FlipsSwitchWithReason(t *testing.T) {
	f := newFakeQueries(true)
	svc := NewService(f)
	enabled := false
	reason := "manual pause"
	dto, err := svc.Update(context.Background(), &enabled, &reason)
	if err != nil {
		t.Fatal(err)
	}
	if dto.TradingEnabled || dto.PausedReason == nil || *dto.PausedReason != reason {
		t.Fatalf("unexpected control: %+v", dto)
	}
	if f.row.TradingEnabled {
		t.Fatal("expected stored row flipped")
	}
}

func TestUpdate_NilEnabledIs422(t *testing.T) {
	if _, err := NewService(newFakeQueries(true)).Update(context.Background(), nil, nil); err == nil {
		t.Fatal("expected 422 for missing tradingEnabled, got nil")
	}
}

func TestIsTradingEnabled_FailsClosed(t *testing.T) {
	if !NewService(newFakeQueries(true)).IsTradingEnabled(context.Background()) {
		t.Fatal("expected true when enabled")
	}
	if NewService(newFakeQueries(false)).IsTradingEnabled(context.Background()) {
		t.Fatal("expected false when paused")
	}
}

func TestUpdateBotControlHandler_MissingEnabledIs422(t *testing.T) {
	h := NewHandler(NewService(newFakeQueries(true)))
	req := httptest.NewRequest(http.MethodPatch, "/api/bot/control",
		strings.NewReader(`{"pausedReason":"x"}`))
	rec := httptest.NewRecorder()
	h.UpdateBotControl(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateBotControlHandler_WireShape(t *testing.T) {
	h := NewHandler(NewService(newFakeQueries(true)))
	req := httptest.NewRequest(http.MethodPatch, "/api/bot/control",
		strings.NewReader(`{"tradingEnabled":false,"pausedReason":"storm"}`))
	rec := httptest.NewRecorder()
	h.UpdateBotControl(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data map[string]any `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["tradingEnabled"] != false || body.Data["pausedReason"] != "storm" {
		t.Fatalf("unexpected data: %s", rec.Body.String())
	}
}
