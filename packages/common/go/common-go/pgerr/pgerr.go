// Package pgerr classifies pgx/Postgres errors (SQLSTATE codes, no-rows) and maps them to the
// httpx error envelope, keeping Postgres-specific error inspection out of common-go/httpx (which
// stays free of any DB import) and out of each domain service, which would otherwise hand-roll
// the same errors.As(*pgconn.PgError) / errors.Is(pgx.ErrNoRows) checks — as
// inflation/service.go and taxreturns/service.go both did independently before this package
// existed.
package pgerr

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"investing-app/common-go/httpx"
)

// sqlStateUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation (23505).
const sqlStateUniqueViolation = "23505"

// IsUniqueViolation reports whether err is (or wraps) a pgconn.PgError with SQLSTATE 23505 — a
// unique-constraint violation, regardless of which named constraint fired.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == sqlStateUniqueViolation
}

// NotFoundOr maps a single-row read error: pgx.ErrNoRows (or an error wrapping it) becomes a 404
// NOT_FOUND with the caller's wire message notFound; anything else is wrapped as "op: err" so
// errors.Is/As still reach the cause. The message text stays in each domain package (the per-app
// notFoundOrErr wrappers); only the mechanism is shared.
func NotFoundOr(err error, notFound, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NewNotFoundError(notFound)
	}
	return fmt.Errorf("%s: %w", op, err)
}
