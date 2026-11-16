package signal

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestSignal_RealPostgresRoundTrip exercises the signal record/list/mark paths
// against a real postgres:17.10-alpine container, migrated by the same Flyway CLI
// image used in prod — the "never mock the database" gate. Covers: create with
// executed=false and JSONB raw_payload surviving, newest-first listing with the
// case-insensitive filter, and the executed flip.
func TestSignal_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE bot_signal RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	svc := NewService(sqlcgen.New(pool))

	created, err := svc.Create(ctx, CreateInput{
		EventName: "CPI m/m", Actual: "0.4%", Forecast: strPtr("0.3%"),
		SurpriseDesc: strPtr("surprise=0.1 matched=BEAT"), Decision: "LONG",
		RawPayload: map[string]any{"source": "watcher"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Executed {
		t.Fatal("expected executed=false on record")
	}
	if created.RawPayload == nil || created.RawPayload["source"] != "watcher" {
		t.Fatalf("raw_payload did not survive: %+v", created.RawPayload)
	}

	if _, err := svc.Create(ctx, CreateInput{EventName: "NFP", Actual: "213K", Decision: "HOLD"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	listed, err := svc.List(ctx, "cpi", 0, 20)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listed.Total != 1 || listed.Data[0].EventName != "CPI m/m" {
		t.Fatalf("expected filtered single row, got %+v", listed)
	}

	marked, err := svc.MarkExecuted(ctx, created.ID)
	if err != nil {
		t.Fatalf("MarkExecuted: %v", err)
	}
	if !marked.Executed {
		t.Fatal("expected executed=true after mark")
	}

	if _, err := svc.MarkExecuted(ctx, 999999); err == nil {
		t.Fatal("expected 404 for unknown signal, got nil")
	}
}

func strPtr(s string) *string { return &s }
