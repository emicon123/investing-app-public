package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestCalendar_RealPostgresRoundTrip exercises the calendar read/sync/purge paths
// against a real postgres:17.10-alpine container, migrated by the same Flyway CLI
// image used in prod — the "never mock the database" gate. Covers: FF sync upsert
// with content external_id idempotency, SQL-level actual preservation (ADR-093
// §1b), list filtering/paging over real rows, and the 30-day purge boundary.
func TestCalendar_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE bot_calendar_event RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	feed := `[{"title":"CPI m/m","currency":"USD","country":"US","impact":"HIGH",` +
		`"date":"2026-09-10T12:30:00+00:00","time":"12:30","forecast":"0.3%","previous":"0.2%","actual":""}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	defer srv.Close()

	queries := sqlcgen.New(pool)
	svc := NewService(queries, NewOrchestrator(NewForexSource(queries, srv.URL, srv.URL)), 30)

	t.Run("sync upserts then dedupes by content external_id", func(t *testing.T) {
		if n := svc.orchestrator.SyncAll(ctx); n != 2 {
			t.Fatalf("expected 2 (1 per feed), got %d", n)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM bot_calendar_event`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected 1 idempotent row, got %d", count)
		}
		var externalID string
		if err := pool.QueryRow(ctx, `SELECT external_id FROM bot_calendar_event`).Scan(&externalID); err != nil {
			t.Fatal(err)
		}
		if len(externalID) != 32 {
			t.Fatalf("expected 32-hex content hash, got %q", externalID)
		}
	})

	t.Run("feed actual never blanks a known value", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE bot_calendar_event SET actual = '0.4', actual_source = 'BLS' `); err != nil {
			t.Fatal(err)
		}
		svc.orchestrator.SyncAll(ctx)
		var actual, source string
		if err := pool.QueryRow(ctx, `SELECT actual, actual_source FROM bot_calendar_event`).Scan(&actual, &source); err != nil {
			t.Fatal(err)
		}
		if actual != "0.4" || source != "BLS" {
			t.Fatalf("ADR-093 §1b violated at SQL level: actual=%q source=%q", actual, source)
		}
	})

	t.Run("list filters and purges by real rows", func(t *testing.T) {
		from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
		res, err := svc.List(ctx, &from, &to, "HIGH", "", 0, 50)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if res.Total != 1 || res.Data[0].EventName != "CPI m/m" {
			t.Fatalf("unexpected list: %+v", res)
		}
		res, err = svc.List(ctx, &from, &to, "LOW", "", 0, 50)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if res.Total != 0 {
			t.Fatalf("expected LOW filter to exclude, got %+v", res)
		}
		// The fixture's fixed 2026-09-10 date ages past the 30-day window, so make the row fresh
		// relative to NOW() before asserting the purge keeps it.
		if _, err := pool.Exec(ctx, `UPDATE bot_calendar_event SET event_time = NOW() - INTERVAL '1 day'`); err != nil {
			t.Fatal(err)
		}
		deleted, err := svc.PurgeOldEvents(ctx)
		if err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if deleted != 0 {
			t.Fatalf("expected fresh row kept, deleted=%d", deleted)
		}
		if _, err := pool.Exec(ctx, `UPDATE bot_calendar_event SET event_time = NOW() - INTERVAL '31 days'`); err != nil {
			t.Fatal(err)
		}
		deleted, err = svc.PurgeOldEvents(ctx)
		if err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if deleted != 1 {
			t.Fatalf("expected 31-day-old row purged, deleted=%d", deleted)
		}
	})
}
