package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/priceprovider"
	"investing-app/common-go/testsupport"
)

// txProbeOHLC answers like stubOHLC and records, at every fetch, how many sessions of this
// database sit "idle in transaction" — i.e. whether the backfill holds a transaction open across
// the network call (174-remediation §11.6, task 180 step 4.5).
type txProbeOHLC struct {
	stubOHLC
	pool    *pgxpool.Pool
	calls   int
	maxOpen int
}

func (p *txProbeOHLC) FetchOHLC(ctx context.Context, ticker, interval string, p1, p2 int64) ([]priceprovider.OHLCPoint, error) {
	p.calls++
	var open int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND state LIKE 'idle in transaction%'`).Scan(&open); err != nil {
		return nil, err
	}
	p.maxOpen = max(p.maxOpen, open)
	return p.stubOHLC.FetchOHLC(ctx, ticker, interval, p1, p2)
}

func TestBackfill_FetchesPricesBeforeOpeningTheTransaction(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE historia, positions, assets, purchase_lots, position_transfers RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	seedBackfill(t, ctx, pool)

	probe := &txProbeOHLC{pool: pool, series: map[string][]priceprovider.OHLCPoint{
		"YT-AAA": {pt("2026-08-01", 100)}, "YT-CMP": {pt("2026-08-01", 50)}, "YT-BBB": {pt("2026-08-01", 10)},
		"EURPLN=X": {pt("2026-08-01", 4.3)}, "CHFPLN=X": {pt("2026-08-01", 4.6)},
	}}
	result, err := NewService(pool, probe).Backfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if probe.calls == 0 || result.Updated == 0 {
		t.Fatalf("the backfill must fetch and update: calls %d, %+v", probe.calls, result)
	}
	if probe.maxOpen != 0 {
		t.Fatalf("a transaction was open during an OHLC fetch (%d idle-in-transaction sessions)", probe.maxOpen)
	}
}
