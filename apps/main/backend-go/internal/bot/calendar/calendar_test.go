package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/openapi"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as
// prior phases (business logic table-tested here; real Postgres covered by
// integration_test.go).
type fakeQueries struct {
	rows   []sqlcgen.BotCalendarEvent
	nextID int64
}

func newFakeQueries() *fakeQueries { return &fakeQueries{nextID: 1} }

func (f *fakeQueries) SearchCalendarEvents(_ context.Context, arg sqlcgen.SearchCalendarEventsParams) ([]sqlcgen.BotCalendarEvent, error) {
	want := map[string]bool{}
	for _, v := range arg.Impacts {
		want[v] = true
	}
	var out []sqlcgen.BotCalendarEvent
	for _, r := range f.rows {
		if r.EventTime.Time.Before(arg.EventTime.Time) || !r.EventTime.Time.Before(arg.EventTime_2.Time) {
			continue
		}
		if len(want) > 0 && !want[r.Impact] {
			continue
		}
		if arg.Currency != "" && r.Currency != arg.Currency {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventTime.Time.Before(out[j].EventTime.Time) })
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

func (f *fakeQueries) CountCalendarEvents(_ context.Context, arg sqlcgen.CountCalendarEventsParams) (int64, error) {
	rows, _ := f.SearchCalendarEvents(context.Background(), sqlcgen.SearchCalendarEventsParams{
		EventTime: arg.EventTime, EventTime_2: arg.EventTime_2,
		Limit: 1 << 30, Impacts: arg.Impacts, Currency: arg.Currency,
	})
	return int64(len(rows)), nil
}

func (f *fakeQueries) UpsertCalendarEvent(_ context.Context, arg sqlcgen.UpsertCalendarEventParams) (int64, error) {
	for i, r := range f.rows {
		if r.ExternalID == arg.ExternalID {
			f.rows[i].EventTime = arg.EventTime
			f.rows[i].Currency = arg.Currency
			f.rows[i].Country = arg.Country
			f.rows[i].EventName = arg.EventName
			f.rows[i].Impact = arg.Impact
			f.rows[i].Forecast = arg.Forecast
			f.rows[i].Previous = arg.Previous
			// ADR-093 §1b preservation rule, mirrored from SQL.
			if arg.Actual != nil {
				f.rows[i].Actual = arg.Actual
			}
			f.rows[i].Source = arg.Source
			return r.ID, nil
		}
	}
	id := f.nextID
	f.nextID++
	f.rows = append(f.rows, sqlcgen.BotCalendarEvent{
		ID: id, EventTime: arg.EventTime, Currency: arg.Currency, Country: arg.Country,
		EventName: arg.EventName, Impact: arg.Impact, Forecast: arg.Forecast,
		Previous: arg.Previous, Actual: arg.Actual, Source: arg.Source,
		ExternalID: arg.ExternalID,
		CreatedAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	})
	return id, nil
}

func (f *fakeQueries) DeleteCalendarEventsBefore(_ context.Context, cutoff pgtype.Timestamptz) (int64, error) {
	var kept []sqlcgen.BotCalendarEvent
	var deleted int64
	for _, r := range f.rows {
		if r.EventTime.Time.Before(cutoff.Time) {
			deleted++
			continue
		}
		kept = append(kept, r)
	}
	f.rows = kept
	return deleted, nil
}

func testService(f *fakeQueries) *Service {
	return NewService(f, NewOrchestrator(NewForexSource(f, "", "")), 30)
}

func seedEvent(f *fakeQueries, name, currency, impact string, at time.Time) {
	f.rows = append(f.rows, sqlcgen.BotCalendarEvent{
		ID: f.nextID, EventTime: pgtype.Timestamptz{Time: at, Valid: true},
		Currency: currency, EventName: name, Impact: impact, Source: "FAIRECONOMY",
		ExternalID: fmt.Sprintf("ext-%d", f.nextID),
		CreatedAt:  pgtype.Timestamptz{Time: at, Valid: true},
	})
	f.nextID++
}

func TestList_FiltersAndPaging(t *testing.T) {
	f := newFakeQueries()
	now := time.Now().UTC()
	seedEvent(f, "CPI m/m", "USD", "HIGH", now.Add(2*time.Hour))
	seedEvent(f, "ECB Rate", "EUR", "LOW", now.Add(3*time.Hour))
	svc := testService(f)
	ctx := context.Background()

	res, err := svc.List(ctx, nil, nil, "", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || len(res.Data) != 2 {
		t.Fatalf("expected 2 rows, got %+v", res)
	}
	if res.Data[0].EventName != "CPI m/m" {
		t.Fatalf("expected eventTime ASC, got %+v", res.Data)
	}

	res, err = svc.List(ctx, nil, nil, "HIGH", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Data[0].Currency != "USD" {
		t.Fatalf("expected HIGH-only, got %+v", res)
	}

	res, err = svc.List(ctx, nil, nil, "", "EUR", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Fatalf("expected EUR-only, got %+v", res)
	}

	if _, err := svc.List(ctx, nil, nil, "CRITICAL", "", 0, 50); err == nil {
		t.Fatal("expected 422 for unknown impact, got nil")
	}

	res, err = svc.List(ctx, nil, nil, "", "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || len(res.Data) != 1 || res.Data[0].EventName != "ECB Rate" {
		t.Fatalf("expected second page row, got %+v", res)
	}
}

func TestPurgeOldEvents(t *testing.T) {
	f := newFakeQueries()
	now := time.Now().UTC()
	seedEvent(f, "Old", "USD", "HIGH", now.AddDate(0, 0, -31))
	seedEvent(f, "Fresh", "USD", "HIGH", now.AddDate(0, 0, -5))
	svc := testService(f)
	deleted, err := svc.PurgeOldEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 || len(f.rows) != 1 || f.rows[0].EventName != "Fresh" {
		t.Fatalf("expected only the fresh row kept, got %d/%+v", deleted, f.rows)
	}
}

func TestParseFeedTime(t *testing.T) {
	// ISO-offset form parses directly.
	if got := parseFeedTime("2026-09-10T12:30:00+00:00", ""); got.IsZero() {
		t.Fatal("expected ISO datetime to parse")
	}
	// FF display form is America/New_York wall time: 12:30 EDT = 16:30Z (September).
	got := parseFeedTime("09-10-2026", "12:30")
	if got.Hour() != 16 || got.Minute() != 30 {
		t.Fatalf("expected 16:30Z, got %s", got.Format(time.RFC3339))
	}
	// All Day / Tentative collapse to midnight ET.
	midnight := parseFeedTime("09-10-2026", "All Day")
	if midnight.Hour() != 4 {
		t.Fatalf("expected 04:00Z (midnight EDT), got %s", midnight.Format(time.RFC3339))
	}
	if !parseFeedTime("", "12:30").IsZero() || !parseFeedTime("garbage", "xx").IsZero() {
		t.Fatal("expected unparseable inputs to yield zero time (row skipped)")
	}
}

func TestForexSync_UpsertsBothFeeds(t *testing.T) {
	feed := `[{"title":"CPI m/m","currency":"USD","country":"US","impact":"HIGH",` +
		`"date":"2026-09-10T12:30:00+00:00","time":"12:30","forecast":"0.3%","previous":"0.2%","actual":""}]`
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()

	f := newFakeQueries()
	src := NewForexSource(f, srv.URL+"/this", srv.URL+"/next")
	// Each feed counts its own upsert (Java sums the two counts the same way);
	// overlapping ranges dedupe to ONE row by content external_id.
	if n := src.Sync(context.Background()); n != 2 {
		t.Fatalf("expected 2 upserts (1 per feed), got %d", n)
	}
	if hits != 2 {
		t.Fatalf("expected both feeds fetched independently, hits=%d", hits)
	}
	if len(f.rows) != 1 || f.rows[0].Impact != "HIGH" || f.rows[0].Source != "FAIRECONOMY" {
		t.Fatalf("unexpected rows: %+v", f.rows)
	}
	// Re-sync is idempotent (same external_id) and must not blank a known actual.
	actual := "0.4%"
	f.rows[0].Actual = &actual
	if n := src.Sync(context.Background()); n != 2 {
		t.Fatalf("expected re-sync to count both upserts, got %d", n)
	}
	if len(f.rows) != 1 || *f.rows[0].Actual != "0.4%" {
		t.Fatalf("ADR-093 §1b violated: empty feed actual blanked a known value: %+v", f.rows)
	}
}

func TestForexSync_RateLimitSetsBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := newFakeQueries()
	src := NewForexSource(f, srv.URL, srv.URL)
	if n := src.Sync(context.Background()); n != 0 {
		t.Fatalf("expected 0 on 429, got %d", n)
	}
	if time.Until(src.BackoffUntil()) < 5*time.Second {
		t.Fatal("expected backoff deadline ~7s out")
	}
	if len(f.rows) != 0 {
		t.Fatalf("expected no rows on 429, got %+v", f.rows)
	}
}

func TestListBotCalendar_WireShape(t *testing.T) {
	f := newFakeQueries()
	seedEvent(f, "CPI m/m", "USD", "HIGH", time.Now().UTC().Add(2*time.Hour))
	h := NewHandler(testService(f))
	req := httptest.NewRequest(http.MethodGet, "/api/bot/calendar", nil)
	rec := httptest.NewRecorder()
	h.ListBotCalendar(rec, req, openapi.ListBotCalendarParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
		Page map[string]any   `json:"page"`
		Meta map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0]["eventName"] != "CPI m/m" {
		t.Fatalf("unexpected data: %s", rec.Body.String())
	}
	if body.Page["total"] != float64(1) || body.Meta["requestId"] == nil {
		t.Fatalf("expected page.total=1 + meta, got %s", rec.Body.String())
	}
}
