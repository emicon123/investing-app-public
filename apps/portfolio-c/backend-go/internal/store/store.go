// Package store is portfolioC's one driven adapter for common-go/siblingcore (ADR-113): every
// siblingcore port, implemented over this app's sqlc queries. It is the only place that knows
// portfolioC partitions every table by portfolio_enum (the portfolio argument is bound, rows decode
// their portfolio) and keeps one units column (SingleBucket ↔ units). Errors are returned
// unchanged so pgx sentinels and SQLSTATEs reach siblingcore's pgerr mapping.
package store

import (
	"github.com/jackc/pgx/v5"

	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/deposit"
	"investing-app/common-go/siblingcore/refdata"
	"portfolio-c/backend-go/db/sqlcgen"
)

// Queries binds this app's sqlc queries and the shared reference-table adapter
// (common-go/siblingcore/refdata: price/FX caches) to one connection, pool or transaction.
type Queries struct {
	q   *sqlcgen.Queries
	ref *refdata.Store
}

// New binds the adapter to db (the pool, or a transaction).
func New(db sqlcgen.DBTX) *Queries { return &Queries{q: sqlcgen.New(db), ref: refdata.New(db)} }

// Bind is the pgtx bind function: Bind(tx) == New(tx).
func Bind(tx pgx.Tx) *Queries { return New(tx) }

// SQL is the escape hatch for this app's own policy code (rebalancing Funding, admin) — never
// used by siblingcore.
func (s *Queries) SQL() *sqlcgen.Queries { return s.q }

var (
	_ deposit.Store           = (*Queries)(nil)
	_ siblingcore.PriceSource = (*Queries)(nil)
)
