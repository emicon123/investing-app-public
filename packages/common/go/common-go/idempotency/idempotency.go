// Package idempotency implements the optional Idempotency-Key header on money-moving requests
// (ADR-111 Decision 2, D174 §2.5 / §5.2–§5.5; IETF draft-ietf-httpapi-idempotency-key-header).
//
// Middleware is a Decorator over any write handler: a request that carries the header is run
// at most once per key, and a retry after a lost response gets the stored 2xx answer replayed
// instead of writing again. Requests without the header pass through untouched. Records are
// kept by a Store (port); NewPgStore is the one implementation, over each app's
// `idempotency_keys` table.
package idempotency

import (
	"context"
	"time"
)

// Header is the request header name (IETF draft-ietf-httpapi-idempotency-key-header).
const Header = "Idempotency-Key"

// ReplayedHeader marks a response that was replayed from a stored record.
const ReplayedHeader = "Idempotent-Replayed"

// Store persists one record per key. Implementations must make Begin atomic with respect to
// concurrent callers using the same key.
type Store interface {
	// Begin inserts an IN_PROGRESS record. If the key already exists it returns the existing
	// record and created=false.
	Begin(ctx context.Context, key, requestHash string) (rec Record, created bool, err error)
	// Complete marks the record COMPLETED with the response to replay.
	Complete(ctx context.Context, key string, status int, contentType string, body []byte) error
	// Forget deletes the record (the request was rejected without effect, so a retry may run).
	Forget(ctx context.Context, key string) error
	// Purge deletes records created before cutoff.
	Purge(ctx context.Context, cutoff time.Time) error
}

// Record is one stored key: the request hash it was first used with and, once COMPLETED, the
// response to replay.
type Record struct {
	RequestHash string
	Completed   bool
	Status      int
	ContentType string
	Body        []byte
}
