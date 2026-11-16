package idempotency_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/idempotency"
	"investing-app/common-go/testsupport"
)

// mainMigrationDir is the real Flyway history of the main app; V210 creates idempotency_keys
// with the DDL that is identical in all three apps' databases (ADR-111).
const mainMigrationDir = "../../../../../apps/main/db/migration"

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestPgStore_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, mainMigrationDir)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	store := idempotency.NewPgStore(pool)

	t.Run("Begin creates once, then returns the in-progress record", func(t *testing.T) {
		rec, created, err := store.Begin(ctx, "k-begin", hashA)
		if err != nil || !created {
			t.Fatalf("first Begin: created=%v err=%v", created, err)
		}
		if rec.RequestHash != hashA || rec.Completed {
			t.Fatalf("first Begin record = %+v", rec)
		}
		rec, created, err = store.Begin(ctx, "k-begin", hashB)
		if err != nil || created {
			t.Fatalf("second Begin: created=%v err=%v", created, err)
		}
		if rec.RequestHash != hashA || rec.Completed || rec.Status != 0 || rec.Body != nil {
			t.Fatalf("second Begin must return the stored in-progress record, got %+v", rec)
		}
	})

	t.Run("Complete stores the response Begin then replays", func(t *testing.T) {
		if _, _, err := store.Begin(ctx, "k-complete", hashA); err != nil {
			t.Fatal(err)
		}
		body := []byte(`{"data":{"id":1}}`)
		if err := store.Complete(ctx, "k-complete", 201, "application/json", body); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		rec, created, err := store.Begin(ctx, "k-complete", hashA)
		if err != nil || created {
			t.Fatalf("Begin after Complete: created=%v err=%v", created, err)
		}
		if !rec.Completed || rec.Status != 201 || rec.ContentType != "application/json" || !bytes.Equal(rec.Body, body) {
			t.Fatalf("completed record = %+v", rec)
		}
	})

	t.Run("Complete without a record is an error", func(t *testing.T) {
		if err := store.Complete(ctx, "k-missing", 200, "application/json", nil); err == nil {
			t.Fatal("expected an error completing a key that has no record")
		}
	})

	t.Run("Forget deletes an in-progress record but never a completed one", func(t *testing.T) {
		if _, _, err := store.Begin(ctx, "k-forget", hashA); err != nil {
			t.Fatal(err)
		}
		if err := store.Forget(ctx, "k-forget"); err != nil {
			t.Fatalf("Forget: %v", err)
		}
		if _, created, err := store.Begin(ctx, "k-forget", hashB); err != nil || !created {
			t.Fatalf("Begin after Forget must create again: created=%v err=%v", created, err)
		}

		if _, _, err := store.Begin(ctx, "k-forget-done", hashA); err != nil {
			t.Fatal(err)
		}
		if err := store.Complete(ctx, "k-forget-done", 200, "application/json", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		if err := store.Forget(ctx, "k-forget-done"); err != nil {
			t.Fatalf("Forget: %v", err)
		}
		if rec, created, err := store.Begin(ctx, "k-forget-done", hashA); err != nil || created || !rec.Completed {
			t.Fatalf("a completed record must survive Forget: created=%v rec=%+v err=%v", created, rec, err)
		}
	})

	t.Run("Purge deletes only records created before the cutoff", func(t *testing.T) {
		for _, k := range []string{"k-old", "k-fresh"} {
			if _, _, err := store.Begin(ctx, k, hashA); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET created_at = NOW() - INTERVAL '25 hours' WHERE idem_key = 'k-old'`); err != nil {
			t.Fatal(err)
		}
		if err := store.Purge(ctx, time.Now().Add(-24*time.Hour)); err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if _, created, err := store.Begin(ctx, "k-old", hashB); err != nil || !created {
			t.Fatalf("a purged key must be creatable again: created=%v err=%v", created, err)
		}
		if _, created, err := store.Begin(ctx, "k-fresh", hashB); err != nil || created {
			t.Fatalf("a fresh key must survive Purge: created=%v err=%v", created, err)
		}
	})

	t.Run("concurrent Begin of one key creates exactly one record", func(t *testing.T) {
		const callers = 10
		var mu sync.Mutex
		createdCount := 0
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range callers {
			wg.Go(func() {
				<-start
				rec, created, err := store.Begin(ctx, "k-race", hashA)
				if err != nil {
					t.Errorf("Begin: %v", err)
					return
				}
				if rec.RequestHash != hashA {
					t.Errorf("Begin returned hash %q", rec.RequestHash)
				}
				if created {
					mu.Lock()
					createdCount++
					mu.Unlock()
				}
			})
		}
		close(start)
		wg.Wait()
		if createdCount != 1 {
			t.Fatalf("expected exactly one created record, got %d", createdCount)
		}
	})
}
