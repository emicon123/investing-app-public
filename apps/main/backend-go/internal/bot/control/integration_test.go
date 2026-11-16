package control

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestControl_RealPostgresRoundTrip exercises the kill-switch read/flip paths
// against a real postgres:17.10-alpine container, migrated by the same Flyway CLI
// image used in prod — the "never mock the database" gate. Covers: the V65 seed
// row (id=1, enabled), flip with reason, and fail-closed reads. The seed row is
// restored to enabled at the end so co-running suites keep their precondition.
func TestControl_RealPostgresRoundTrip(t *testing.T) {
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

	svc := NewService(sqlcgen.New(pool))

	dto, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// V2__default_reference_data seeds the singleton with trading disabled (safe default).
	if dto.TradingEnabled {
		t.Fatal("expected the seed row to start with trading disabled")
	}
	if svc.IsTradingEnabled(ctx) {
		t.Fatal("expected gate closed on seed")
	}
	enable := true
	if _, err := svc.Update(ctx, &enable, nil); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !svc.IsTradingEnabled(ctx) {
		t.Fatal("expected gate open after enabling")
	}

	paused := false
	reason := "it-pause"
	updated, err := svc.Update(ctx, &paused, &reason)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.TradingEnabled || updated.PausedReason == nil || *updated.PausedReason != reason {
		t.Fatalf("unexpected flipped row: %+v", updated)
	}
	if svc.IsTradingEnabled(ctx) {
		t.Fatal("expected gate closed after pause")
	}
	enabled, gateReason := svc.TradingStatus(ctx)
	if enabled || gateReason == nil || *gateReason != reason {
		t.Fatalf("expected (false, reason) status, got %v/%v", enabled, gateReason)
	}

	resumed := true
	if _, err := svc.Update(ctx, &resumed, nil); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !svc.IsTradingEnabled(ctx) {
		t.Fatal("expected gate open after restore")
	}
}
