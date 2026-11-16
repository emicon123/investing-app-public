package signal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/openapi"
)

type fakeQueries struct {
	rows   []sqlcgen.BotSignal
	nextID int64
}

func newFakeQueries() *fakeQueries { return &fakeQueries{nextID: 1} }

func (f *fakeQueries) InsertSignal(_ context.Context, arg sqlcgen.InsertSignalParams) (sqlcgen.BotSignal, error) {
	id := f.nextID
	f.nextID++
	row := sqlcgen.BotSignal{
		ID: id, CalendarEventID: arg.CalendarEventID, PlanScenarioID: arg.PlanScenarioID,
		EventName: arg.EventName, Actual: arg.Actual, Forecast: arg.Forecast,
		Previous: arg.Previous, SurpriseDesc: arg.SurpriseDesc, Decision: arg.Decision,
		Executed: false, RawPayload: arg.RawPayload,
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	f.rows = append(f.rows, row)
	return row, nil
}

func (f *fakeQueries) MarkSignalExecuted(_ context.Context, id int64) (sqlcgen.BotSignal, error) {
	for i, r := range f.rows {
		if r.ID == id {
			f.rows[i].Executed = true
			return f.rows[i], nil
		}
	}
	return sqlcgen.BotSignal{}, pgx.ErrNoRows
}

func (f *fakeQueries) ListSignals(_ context.Context, arg sqlcgen.ListSignalsParams) ([]sqlcgen.BotSignal, error) {
	var out []sqlcgen.BotSignal
	for _, r := range f.rows {
		if arg.EventName != "" && !strings.Contains(strings.ToLower(r.EventName), strings.ToLower(arg.EventName)) {
			continue
		}
		out = append(out, r)
	}
	// createdAt DESC — insertion order is chronological in the fake.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	start := int(arg.Offset)
	if start > len(out) {
		start = len(out)
	}
	end := start + int(arg.Limit)
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], nil
}

func (f *fakeQueries) CountSignals(_ context.Context, eventName string) (int64, error) {
	rows, _ := f.ListSignals(context.Background(), sqlcgen.ListSignalsParams{EventName: eventName, Limit: 1 << 30})
	return int64(len(rows)), nil
}

func TestCreate_ExecutedFalse(t *testing.T) {
	svc := NewService(newFakeQueries())
	dto, err := svc.Create(context.Background(), CreateInput{
		EventName: "CPI m/m", Actual: "0.4%", Decision: "LONG",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dto.Executed {
		t.Fatal("expected executed=false on record")
	}
	if dto.Decision != "LONG" {
		t.Fatalf("unexpected decision: %+v", dto)
	}
}

func TestCreate_Validation(t *testing.T) {
	svc := NewService(newFakeQueries())
	ctx := context.Background()
	for _, in := range []CreateInput{
		{Actual: "0.4%", Decision: "LONG"},
		{EventName: "CPI", Decision: "LONG"},
		{EventName: "CPI", Actual: "0.4%", Decision: "MOON"},
	} {
		if _, err := svc.Create(ctx, in); err == nil {
			t.Fatalf("expected validation error for %+v, got nil", in)
		}
	}
}

func TestMarkExecuted_404WhenUnknown(t *testing.T) {
	svc := NewService(newFakeQueries())
	if _, err := svc.MarkExecuted(context.Background(), 999); err == nil {
		t.Fatal("expected 404, got nil")
	}
	dto, err := svc.Create(context.Background(), CreateInput{EventName: "CPI", Actual: "0.4%", Decision: "HOLD"})
	if err != nil {
		t.Fatal(err)
	}
	marked, err := svc.MarkExecuted(context.Background(), dto.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !marked.Executed {
		t.Fatal("expected executed=true after mark")
	}
}

func TestCreateBotSignalHandler_201And422(t *testing.T) {
	h := NewHandler(NewService(newFakeQueries()))
	req := httptest.NewRequest(http.MethodPost, "/api/bot/signals",
		strings.NewReader(`{"eventName":"CPI m/m","actual":"0.4%","decision":"LONG"}`))
	rec := httptest.NewRecorder()
	h.CreateBotSignal(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["executed"] != false {
		t.Fatalf("expected executed=false, got %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/bot/signals",
		strings.NewReader(`{"eventName":"CPI m/m","decision":"LONG"}`))
	rec = httptest.NewRecorder()
	h.CreateBotSignal(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing actual, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListBotSignalsHandler_Paged(t *testing.T) {
	f := newFakeQueries()
	svc := NewService(f)
	ctx := context.Background()
	if _, err := svc.Create(ctx, CreateInput{EventName: "CPI m/m", Actual: "0.4%", Decision: "LONG"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateInput{EventName: "NFP", Actual: "213K", Decision: "HOLD"}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(svc)
	eventName := "cpi"
	req := httptest.NewRequest(http.MethodGet, "/api/bot/signals", nil)
	rec := httptest.NewRecorder()
	h.ListBotSignals(rec, req, openapi.ListBotSignalsParams{EventName: &eventName})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
		Page map[string]any   `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0]["eventName"] != "CPI m/m" {
		t.Fatalf("expected case-insensitive filter to match CPI, got %s", rec.Body.String())
	}
}
