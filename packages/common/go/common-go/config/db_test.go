package config

import (
	"context"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"investing-app/common-go/testsupport"
)

func TestPoolConfig_AppliesBackendPoolPolicy(t *testing.T) {
	cfg, err := PoolConfig("postgres://u:p@h:5432/d")
	if err != nil {
		t.Fatalf("PoolConfig: %v", err)
	}
	if cfg.MaxConns != 10 {
		t.Errorf("MaxConns: expected 10, got %d", cfg.MaxConns)
	}
	if cfg.HealthCheckPeriod != 30*time.Second {
		t.Errorf("HealthCheckPeriod: expected 30s, got %v", cfg.HealthCheckPeriod)
	}
	if cfg.MaxConnIdleTime != 5*time.Minute {
		t.Errorf("MaxConnIdleTime: expected 5m, got %v", cfg.MaxConnIdleTime)
	}
	params := cfg.ConnConfig.RuntimeParams
	if got := params["statement_timeout"]; got != "60s" {
		t.Errorf("statement_timeout: expected 60s, got %q", got)
	}
	if got := params["idle_in_transaction_session_timeout"]; got != "5min" {
		t.Errorf("idle_in_transaction_session_timeout: expected 5min, got %q", got)
	}
}

func TestPoolConfig_RejectsAnUnparsableDSN(t *testing.T) {
	if _, err := PoolConfig("postgres://u:p@h:notaport/d"); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestConnectDB_MissingConfigurationIsAnError(t *testing.T) {
	t.Setenv("DB_URL", "")
	_, err := ConnectDB(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "config: database configuration: ") {
		t.Fatalf("expected a %q error, got %v", "config: database configuration", err)
	}
}

func TestConnectDB_UnreachableDatabaseFailsThePing(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens there now: connection refused

	t.Setenv("DB_URL", "jdbc:postgresql://"+addr+"/d")
	t.Setenv("DB_USERNAME", "u")
	t.Setenv("DB_PASSWORD", "p")
	_, err = ConnectDB(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "config: ping database: ") {
		t.Fatalf("expected a %q error, got %v", "config: ping database", err)
	}
}

// TestConnectDB_RealPostgresAppliesStatementTimeout proves the runtime parameters reach the
// server (never mock the database): the session sees statement_timeout = 60s, which Postgres
// reports as "1min".
func TestConnectDB_RealPostgresAppliesStatementTimeout(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../testsupport/testdata/migrations")
	u, err := url.Parse(pg.ConnString)
	if err != nil {
		t.Fatalf("parse fixture DSN: %v", err)
	}
	password, _ := u.User.Password()
	t.Setenv("DB_URL", "jdbc:postgresql://"+u.Host+u.Path)
	t.Setenv("DB_USERNAME", u.User.Username())
	t.Setenv("DB_PASSWORD", password)

	ctx := context.Background()
	pool, err := ConnectDB(ctx)
	if err != nil {
		t.Fatalf("ConnectDB: %v", err)
	}
	defer pool.Close()

	for setting, want := range map[string]string{
		"statement_timeout":                   "1min",
		"idle_in_transaction_session_timeout": "5min",
	} {
		var got string
		if err := pool.QueryRow(ctx, "SHOW "+setting).Scan(&got); err != nil {
			t.Fatalf("SHOW %s: %v", setting, err)
		}
		if got != want {
			t.Errorf("SHOW %s: expected %q, got %q", setting, want, got)
		}
	}
	if max := pool.Config().MaxConns; max != 10 {
		t.Errorf("pool MaxConns: expected 10, got %d", max)
	}
}
