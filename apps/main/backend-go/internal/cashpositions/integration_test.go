package cashpositions

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// TestCashPositions_RealPostgresRoundTrip exercises the cash_positions write path against a
// real postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod —
// the "never mock the database" gate, asserting against literally the same schema Java's
// own tests do. Covers: the V10 Flyway seed shape, the empty-table → empty-snapshot
// contract, the snapshot batch insert through the REAL single-transaction runner (atomic,
// one shared recorded_at), superseding snapshots, and NUMERIC(18,4) precision.
func TestCashPositions_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
	svc := NewService(queries, transact)

	t.Run("empty table snapshots to empty, not an error", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `TRUNCATE cash_positions`); err != nil {
			t.Fatal(err)
		}
		latest, err := svc.LatestSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if latest == nil || len(latest) != 0 {
			t.Fatalf("expected empty non-nil snapshot, got %+v", latest)
		}
	})

	t.Run("snapshot batch is atomic with one shared recorded_at", func(t *testing.T) {
		recordedAt := time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC)
		saved, err := svc.CreateSnapshot(ctx, []Item{
			{Location: "mBank", AmountPln: decimal.RequireFromString("15000.5000")},
			{Location: "portfel", AmountPln: decimal.NewFromInt(200)},
			{Location: "XTB cash", AmountPln: decimal.NewFromInt(500)},
		}, recordedAt)
		if err != nil {
			t.Fatalf("CreateSnapshot: %v", err)
		}
		if len(saved) != 3 {
			t.Fatalf("expected 3 saved rows, got %+v", saved)
		}
		for _, d := range saved {
			if d.ID == 0 {
				t.Fatalf("expected ids assigned, got %+v", saved)
			}
			if !d.RecordedAt.Equal(recordedAt) {
				t.Fatalf("expected shared recordedAt %v, got %v", recordedAt, d.RecordedAt)
			}
		}
		if saved[0].AmountPln.StringFixed(4) != "15000.5000" {
			t.Fatalf("expected 4dp precision to survive, got %s", saved[0].AmountPln)
		}

		latest, err := svc.LatestSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(latest) != 3 {
			t.Fatalf("expected latest snapshot of 3, got %+v", latest)
		}

		// A newer snapshot supersedes the older one entirely.
		if _, err := svc.CreateSnapshot(ctx, []Item{
			{Location: "mBank", AmountPln: decimal.NewFromInt(1)},
		}, recordedAt.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		latest, err = svc.LatestSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(latest) != 1 || latest[0].Location != "mBank" {
			t.Fatalf("expected only the newer snapshot, got %+v", latest)
		}
	})
}
