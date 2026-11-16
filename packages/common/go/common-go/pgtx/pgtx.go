// Package pgtx is the one transaction runner for server code (ADR-110 Decision 2, D174 §2.1):
// Template Method as a Go higher-order function. Every service keeps depending on its own
// `TransactFunc func(ctx context.Context, fn func(Queries) error) error`; the composition root
// builds that value with Runner instead of a hand-rolled begin/commit closure.
//
// Runner is built on pgx.BeginFunc, which defers the rollback: a panic inside fn rolls the
// transaction back and returns the connection to the pool before the panic propagates, so a
// panicking request can no longer leave a connection "idle in transaction" holding row locks
// (task-174 finding F2.5).
package pgtx

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Beginner is anything that can open a transaction: *pgxpool.Pool, *pgx.Conn, or a pgx.Tx
// (which opens a savepoint). pgx.BeginFunc accepts the same shape.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Runner returns a transaction runner with the exact shape of every per-package
// `TransactFunc func(ctx context.Context, fn func(Queries) error) error`, so the result is
// assignable to those named types without conversion.
//
// Each call opens one transaction on db, binds it with bind (typically
// `func(tx pgx.Tx) pkg.Queries { return queries.WithTx(tx) }`, or the method value
// `queries.WithTx` when Q is the concrete *sqlcgen.Queries), runs fn, and commits iff fn
// returned nil; fn's error is returned unchanged. Built on pgx.BeginFunc: the rollback is
// deferred, so a panic inside fn rolls back and releases the connection before the panic
// propagates (F2.5).
func Runner[Q any](db Beginner, bind func(pgx.Tx) Q) func(ctx context.Context, fn func(Q) error) error {
	return func(ctx context.Context, fn func(Q) error) error {
		return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			return fn(bind(tx))
		})
	}
}

// MoneyWriteLockKey is the advisory-lock key every money-writing transaction takes first
// (ADR-111). Advisory locks are scoped to the current database, so the same constant is
// correct in all three apps' databases.
const MoneyWriteLockKey int64 = 0x494e56_4d4f4e // "INV" "MON"

// SerializedRunner is Runner plus, as the first statement of every transaction,
// `SELECT pg_advisory_xact_lock($1)` with lockKey. Two transactions built from runners with
// the same key never overlap: the second waits (bounded by ctx and the pool's
// statement_timeout) until the first commits or rolls back, then reads the committed state
// (READ COMMITTED takes a fresh snapshot per statement). The lock is released automatically
// at commit/rollback. Use it for every transaction that reads and then writes positions,
// deposits, transfer records or purchase lots (ADR-111 Decision 1).
//
// When ctx expires while waiting, pgx cancels the lock query and closes the connection, so a
// waiter never lingers on the lock and fn never runs without it.
func SerializedRunner[Q any](db Beginner, lockKey int64, bind func(pgx.Tx) Q) func(ctx context.Context, fn func(Q) error) error {
	return func(ctx context.Context, fn func(Q) error) error {
		return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey); err != nil {
				return fmt.Errorf("pgtx: acquire money-write lock: %w", err)
			}
			return fn(bind(tx))
		})
	}
}
