package config

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Backend pool policy (task-174 finding F3.6, D174 §4.5, ADR-110 Decision 3). pgxpool's default
// of max(4, NumCPU) connections is shared by HTTP, every scheduler and the 180 s long requests;
// 3 apps × 10 stays well inside the shared Postgres's default max_connections of 100.
const (
	poolMaxConns          = 10
	poolHealthCheckPeriod = 30 * time.Second // drop dead connections after a db-shared restart
	poolMaxConnIdleTime   = 5 * time.Minute
	// statementTimeout bounds any single statement, including the ADR-111 advisory-lock wait.
	statementTimeout = "60s"
	// idleInTxTimeout kills a leaked open transaction server-side. 5 min, not less: the siblings'
	// admin backfill still fetches Yahoo OHLC inside its transaction (D174 §11 item 6).
	idleInTxTimeout = "5min"
	pingTimeout     = 5 * time.Second
)

// PoolConfig parses dsn and applies the backend pool policy (F3.6): MaxConns 10,
// HealthCheckPeriod 30s, MaxConnIdleTime 5m, and the server-side runtime parameters
// statement_timeout=60s and idle_in_transaction_session_timeout=5min. Pure; unit-tested.
func PoolConfig(dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = poolMaxConns
	cfg.HealthCheckPeriod = poolHealthCheckPeriod
	cfg.MaxConnIdleTime = poolMaxConnIdleTime
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = statementTimeout
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = idleInTxTimeout
	return cfg, nil
}

// ConnectDB builds the pool from PostgresDSN() + PoolConfig and pings it (5s). Errors are
// wrapped as "config: database configuration: …", "config: connect to database: …",
// "config: ping database: …". It never exits; main returns the error from run().
func ConnectDB(ctx context.Context) (*pgxpool.Pool, error) {
	dsn, err := PostgresDSN()
	if err != nil {
		return nil, fmt.Errorf("config: database configuration: %w", err)
	}
	cfg, err := PoolConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("config: database configuration: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("config: connect to database: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("config: ping database: %w", err)
	}
	return pool, nil
}
