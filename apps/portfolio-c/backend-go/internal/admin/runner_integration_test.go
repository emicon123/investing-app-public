package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/priceprovider"
	"investing-app/common-go/testsupport"
)

// panickingOHLC blows up on the first history fetch, which Backfill performs inside its
// transaction (after reading the missing rows).
type panickingOHLC struct{}

func (panickingOHLC) FetchOHLC(context.Context, string, string, int64, int64) ([]priceprovider.OHLCPoint, error) {
	panic("OHLC provider blew up")
}

// TestStartBackfillRunner_PanicIsContainedAndRolledBack: a panic inside the one-shot startup
// backfill must not take the process down (lifecycle.Guard, ADR-110), and its transaction must
// be rolled back with the connection back in the pool (pgx.BeginFunc) — against a real,
// Flyway-migrated Postgres, never a mock.
func TestStartBackfillRunner_PanicIsContainedAndRolledBack(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, assets, purchase_lots RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	seedBackfill(t, ctx, pool)
	missingBefore := countMissingPositionValues(t, ctx, pool)

	StartBackfillRunner(ctx, NewService(pool, panickingOHLC{})) // must return, not crash

	if acquired := pool.Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("the panicking backfill's connection was not released: %d still acquired", acquired)
	}
	if after := countMissingPositionValues(t, ctx, pool); after != missingBefore {
		t.Fatalf("the panicking backfill was not rolled back: NULL position_values %d -> %d", missingBefore, after)
	}
}

func countMissingPositionValues(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM historia WHERE position_values IS NULL`).Scan(&n); err != nil {
		t.Fatalf("count NULL position_values: %v", err)
	}
	return n
}
