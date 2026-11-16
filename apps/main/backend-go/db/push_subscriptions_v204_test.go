package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"investing-app/common-go/testsupport"
)

// Task 172 / ADR-108 §4: V204 creates push_subscriptions. Proves on the real Flyway history that
// the table rejects a non-https endpoint (check constraint) and a duplicate endpoint (unique
// constraint), and that the shared set_updated_at() trigger bumps updated_at on UPDATE.
// Endpoints here are fake hosts; no real subscription or key material.
func TestV204PushSubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}
	pg := testsupport.StartMigratedPostgres(t, "../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	insert := func(endpoint string) error {
		_, err := conn.Exec(ctx, `INSERT INTO push_subscriptions (user_email, endpoint, p256dh, auth, device_label)
			VALUES ('owner@example.com', $1, 'p256dh-test', 'auth-test', 'Android · Chrome')`, endpoint)
		return err
	}
	pgCode := func(err error) string {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
			return pgErr.Code
		}
		return ""
	}

	t.Run("rejects http endpoint", func(t *testing.T) {
		if code := pgCode(insert("http://fcm.googleapis.com/fcm/send/x")); code != "23514" {
			t.Fatalf("expected check violation 23514, got %q", code)
		}
	})

	t.Run("rejects duplicate endpoint", func(t *testing.T) {
		if err := insert("https://fcm.googleapis.com/fcm/send/dup"); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		if code := pgCode(insert("https://fcm.googleapis.com/fcm/send/dup")); code != "23505" {
			t.Fatalf("expected unique violation 23505, got %q", code)
		}
	})

	t.Run("trigger bumps updated_at", func(t *testing.T) {
		// INSERT does not fire the BEFORE UPDATE trigger, so an old updated_at sticks here.
		if _, err := conn.Exec(ctx, `INSERT INTO push_subscriptions (user_email, endpoint, p256dh, auth, device_label, updated_at)
			VALUES ('owner@example.com', 'https://fcm.googleapis.com/fcm/send/old', 'p', 'a', 'x', TIMESTAMPTZ '2020-01-01Z')`); err != nil {
			t.Fatal(err)
		}
		var bumped bool
		if err := conn.QueryRow(ctx, `UPDATE push_subscriptions SET device_label = 'iOS · Safari'
			WHERE endpoint = 'https://fcm.googleapis.com/fcm/send/old'
			RETURNING updated_at > TIMESTAMPTZ '2025-01-01Z'`).Scan(&bumped); err != nil {
			t.Fatal(err)
		}
		if !bumped {
			t.Fatal("expected set_updated_at() trigger to refresh updated_at")
		}
	})
}
