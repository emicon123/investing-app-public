// Package store is portfolioB's one driven adapter for common-go/siblingcore (ADR-113): every
// siblingcore port, implemented over this app's sqlc queries. It is the only place that knows
// portfolioB has no portfolio column (the portfolio argument is ignored, rows decode as WholeBook)
// and five units columns (book's buckets ↔ units_ike_tata … units_inne). Errors are returned
// unchanged so pgx sentinels and SQLSTATEs reach siblingcore's pgerr mapping.
package store

import (
	"github.com/jackc/pgx/v5"

	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/deposit"
	"investing-app/common-go/siblingcore/refdata"
	"portfolio-b/backend-go/db/sqlcgen"
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
