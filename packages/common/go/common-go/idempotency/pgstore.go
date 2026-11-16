package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// statusCompleted is the `status` value of a record holding a response to replay.
const statusCompleted = "COMPLETED"

// beginAttempts bounds Begin's insert-or-read loop: the existing record can vanish between the
// conflicting INSERT and the SELECT (a concurrent Forget after a 4xx, or a Purge), in which
// case the key is free again and the INSERT is retried.
const beginAttempts = 3

// pgStore is the Store over the per-app `idempotency_keys` table (ADR-111 DDL). Hand-written
// pgx SQL: the table is common-go's, not any app's sqlc package.
type pgStore struct {
	db *pgxpool.Pool
}

// NewPgStore returns the Store over the per-app `idempotency_keys` table (ADR-111 DDL).
func NewPgStore(db *pgxpool.Pool) Store {
	return &pgStore{db: db}
}

// Begin inserts the key; INSERT … ON CONFLICT DO NOTHING makes it atomic for concurrent
// callers (exactly one sees created=true). On conflict it reads the existing record.
func (s *pgStore) Begin(ctx context.Context, key, requestHash string) (Record, bool, error) {
	for range beginAttempts {
		var id int64
		err := s.db.QueryRow(ctx, `
			INSERT INTO idempotency_keys (idem_key, request_hash) VALUES ($1, $2)
			ON CONFLICT (idem_key) DO NOTHING
			RETURNING id`, key, requestHash).Scan(&id)
		if err == nil {
			return Record{RequestHash: requestHash}, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Record{}, false, fmt.Errorf("idempotency: insert key: %w", err)
		}
		rec, found, err := s.read(ctx, key)
		if err != nil {
			return Record{}, false, err
		}
		if found {
			return rec, false, nil
		}
	}
	return Record{}, false, fmt.Errorf("idempotency: key kept disappearing after %d attempts", beginAttempts)
}

func (s *pgStore) read(ctx context.Context, key string) (Record, bool, error) {
	var (
		rec         Record
		status      string
		code        *int16
		contentType *string
	)
	err := s.db.QueryRow(ctx, `
		SELECT request_hash, status, response_code, content_type, response_body
		FROM idempotency_keys WHERE idem_key = $1`, key).
		Scan(&rec.RequestHash, &status, &code, &contentType, &rec.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("idempotency: read key: %w", err)
	}
	rec.Completed = status == statusCompleted
	if code != nil {
		rec.Status = int(*code)
	}
	if contentType != nil {
		rec.ContentType = *contentType
	}
	return rec, true, nil
}

// Complete stores the response to replay. An empty content type is stored as NULL.
func (s *pgStore) Complete(ctx context.Context, key string, status int, contentType string, body []byte) error {
	var ct *string
	if contentType != "" {
		ct = &contentType
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE idempotency_keys
		SET status = 'COMPLETED', response_code = $2, content_type = $3, response_body = $4, completed_at = NOW()
		WHERE idem_key = $1`, key, int16(status), ct, body)
	if err != nil {
		return fmt.Errorf("idempotency: complete key: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("idempotency: complete key: no record")
	}
	return nil
}

// Forget deletes an IN_PROGRESS record; a COMPLETED one is never deleted (its response must
// stay replayable).
func (s *pgStore) Forget(ctx context.Context, key string) error {
	if _, err := s.db.Exec(ctx,
		`DELETE FROM idempotency_keys WHERE idem_key = $1 AND status = 'IN_PROGRESS'`, key); err != nil {
		return fmt.Errorf("idempotency: forget key: %w", err)
	}
	return nil
}

// Purge deletes every record created before cutoff (the 24 h retention, ADR-111).
func (s *pgStore) Purge(ctx context.Context, cutoff time.Time) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM idempotency_keys WHERE created_at < $1`, cutoff); err != nil {
		return fmt.Errorf("idempotency: purge: %w", err)
	}
	return nil
}
