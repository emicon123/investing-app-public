package settings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/testsupport"
)

// TestSettings_RealPostgresRoundTrip exercises the settings write path against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod —
// the "never mock the database" gate, asserting against literally the same schema Java's
// own tests do. Covers: the Flyway-seeded live key set (exactly the 4 registry Defs —
// V37 deleted the cut-factor/sink keys), update round-trip, the set_updated_at() trigger
// (Go never sends updated_at), the strict-404 IKE/IKZE contract, and bool
// malformed→false against real rows.
func TestSettings_RealPostgresRoundTrip(t *testing.T) {
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
	svc := NewService(queries)

	t.Run("flyway seeds exactly the 4 registered keys", func(t *testing.T) {
		dtos, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(dtos) != len(AllDefs) {
			keys := []string{}
			for _, d := range dtos {
				keys = append(keys, d.Key+"="+d.Value)
			}
			t.Fatalf("expected exactly the %d registered keys, got %v", len(AllDefs), keys)
		}
		seen := map[string]bool{}
		for _, d := range dtos {
			seen[d.Key] = true
		}
		for _, d := range AllDefs {
			if !seen[d.Key] {
				t.Fatalf("expected registered key %s in Flyway seed", d.Key)
			}
		}
	})

	t.Run("update round-trip moves value and trigger-updated_at", func(t *testing.T) {
		var before time.Time
		for _, d := range mustList(t, svc, ctx) {
			if d.Key == DefIkeAnnualLimitPln.Key {
				before = d.UpdatedAt
			}
		}
		if before.IsZero() {
			t.Fatal("expected seeded IKE key in list")
		}
		time.Sleep(5 * time.Millisecond) // separate NOW() readings deterministically

		dto, err := svc.Update(ctx, DefIkeAnnualLimitPln.Key, "24000.00")
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if dto.Value != "24000.00" {
			t.Fatalf("expected stored 24000.00, got %q", dto.Value)
		}
		// updated_at comes from the set_updated_at() trigger — Go never sends it
		// (the UPDATE statement sets only value); it must have moved forward.
		if !dto.UpdatedAt.After(before) {
			t.Fatalf("expected trigger to move updated_at past %v, got %v", before, dto.UpdatedAt)
		}

		// Restore the Flyway seed for the subtests below.
		if _, err := svc.Update(ctx, DefIkeAnnualLimitPln.Key, "23472.00"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("registry strict reads against real rows", func(t *testing.T) {
		ike, err := Get(ctx, queries, DefIkeAnnualLimitPln, decimal.Zero, true)
		if err != nil || !ike.Equal(decimal.RequireFromString("23472.00")) {
			t.Fatalf("expected strict IKE 23472.00, got %v,%v", ike, err)
		}
		if _, err := Get(ctx, queries, DefIkzeAnnualLimitPln, decimal.Zero, true); err != nil {
			t.Fatalf("expected strict IKZE to parse, got %v", err)
		}

		// Missing key in strict mode → 404.
		if _, err := pool.Exec(ctx, `DELETE FROM settings WHERE key = 'ikze_annual_limit_pln'`); err != nil {
			t.Fatal(err)
		}
		_, err = Get(ctx, queries, DefIkzeAnnualLimitPln, decimal.Zero, true)
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != 404 {
			t.Fatalf("expected strict-404 for deleted IKZE key, got %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO settings (key, value) VALUES ('ikze_annual_limit_pln', '9388.00')`); err != nil {
			t.Fatal(err)
		}

		// Malformed bool in the real table → false (parseBoolean parity, not the fallback).
		if _, err := pool.Exec(ctx, `UPDATE settings SET value = 'yes' WHERE key = 'rebalancing.macro_derisk_enabled'`); err != nil {
			t.Fatal(err)
		}
		enabled, err := Get(ctx, queries, DefMacroDeriskEnabled, MacroDeriskEnabledFallback, false)
		if err != nil || enabled {
			t.Fatalf("expected malformed bool → false, got %v,%v", enabled, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE settings SET value = 'true' WHERE key = 'rebalancing.macro_derisk_enabled'`); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unknown key update is 404, not 422", func(t *testing.T) {
		_, err := svc.Update(ctx, "no.such.key", "x")
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != 404 {
			t.Fatalf("expected 404, got %v", err)
		}
	})
}

func mustList(t *testing.T, svc *Service, ctx context.Context) []SettingDTO {
	t.Helper()
	dtos, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return dtos
}
