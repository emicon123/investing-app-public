package stats

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestStats_RealPostgresRoundTrip exercises the GROUP BY aggregates against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in
// prod — the "never mock the database" gate. Covers: totals/win-rate/avg-hold over
// CLOSED rows, best/worst selection, Warsaw-day equity grouping with cumulative
// sums, and the always-null totalPnlPct quirk.
func TestStats_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE bot_trade, bot_trade_action, bot_trade_tick RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	// Two CLOSED trades on different Warsaw days + one OPEN. Closed_at chosen so
	// both fall on distinct Europe/Warsaw calendar days regardless of DST.
	if _, err := pool.Exec(ctx, `INSERT INTO bot_trade
		(side, status, quantity, leverage, entry_price, exit_price, pnl_pln, opened_at, closed_at)
		VALUES
		('LONG', 'CLOSED', 0.01, 3, 60000, 61000, 30, '2026-09-10T10:00:00Z', '2026-09-10T10:30:00Z'),
		('SHORT', 'CLOSED', 0.02, 2, 60000, 60500, -20, '2026-09-11T10:00:00Z', '2026-09-11T11:30:00Z'),
		('LONG', 'OPEN', 0.01, 5, 60000, NULL, NULL, NOW(), NULL)`); err != nil {
		t.Fatalf("seed trades: %v", err)
	}

	svc := NewService(sqlcgen.New(pool), nil)

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalTrades != 3 || stats.OpenTrades != 1 || stats.ClosedTrades != 2 {
		t.Fatalf("unexpected counts: %+v", stats)
	}
	if stats.TotalPnlPln.String() != "10" {
		t.Fatalf("expected total pnl 10, got %s", stats.TotalPnlPln)
	}
	if stats.WinRate == nil || *stats.WinRate != 50 {
		t.Fatalf("expected winRate 50, got %v", stats.WinRate)
	}
	if stats.TotalPnlPct != nil {
		t.Fatalf("expected totalPnlPct always null, got %v", *stats.TotalPnlPct)
	}
	if stats.AvgHoldMinutes == nil || *stats.AvgHoldMinutes != 60 {
		t.Fatalf("expected avgHold 60 ((30+90)/2), got %v", stats.AvgHoldMinutes)
	}
	if stats.BestTrade == nil || stats.BestTrade.PnlPln == nil || *stats.BestTrade.PnlPln != 30 {
		t.Fatalf("expected best 30, got %+v", stats.BestTrade)
	}
	if stats.WorstTrade == nil || stats.WorstTrade.PnlPln == nil || *stats.WorstTrade.PnlPln != -20 {
		t.Fatalf("expected worst -20, got %+v", stats.WorstTrade)
	}

	points, err := svc.Equity(ctx)
	if err != nil {
		t.Fatalf("Equity: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 Warsaw-day points, got %+v", points)
	}
	if points[0].DailyPnlPln.String() != "30" || points[0].CumulativePnlPln.String() != "30" ||
		points[1].DailyPnlPln.String() != "-20" || points[1].CumulativePnlPln.String() != "10" {
		t.Fatalf("unexpected equity series: %+v", points)
	}

	open, err := svc.OpenPositions(ctx, 0, 20)
	if err != nil {
		t.Fatalf("OpenPositions: %v", err)
	}
	if len(open) != 1 || open[0].Status != "OPEN" {
		t.Fatalf("expected only the OPEN row, got %+v", open)
	}
}
