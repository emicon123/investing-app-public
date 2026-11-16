package stats

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

type fakeQueries struct {
	trades []sqlcgen.BotTrade
}

func newFakeQueries() *fakeQueries { return &fakeQueries{} }

func (f *fakeQueries) BotTradeStats(_ context.Context) (sqlcgen.BotTradeStatsRow, error) {
	var total, open, closed, wins int64
	var sum decimal.Decimal
	var holdSum float64
	var holdN int64
	for _, t := range f.trades {
		total++
		switch t.Status {
		case "OPEN":
			open++
		case "CLOSED":
			closed++
			if t.PnlPln.Valid {
				v, _ := pgnum.ToDecimal(t.PnlPln)
				sum = sum.Add(v)
				if v.Cmp(decimal.Zero) > 0 {
					wins++
				}
			}
			if t.OpenedAt.Valid && t.ClosedAt.Valid {
				holdSum += t.ClosedAt.Time.Sub(t.OpenedAt.Time).Minutes()
				holdN++
			}
		}
	}
	row := sqlcgen.BotTradeStatsRow{
		Total: total, OpenTrades: open, ClosedTrades: closed, Wins: wins,
		TotalPnl: pgnum.FromDecimal(sum),
	}
	if holdN > 0 {
		row.AvgHoldMinutes = pgnum.FromDecimal(decimal.NewFromFloat(holdSum / float64(holdN)))
	}
	return row, nil
}

func extremum(f *fakeQueries, best bool) (sqlcgen.BotTrade, error) {
	var candidate *sqlcgen.BotTrade
	for i, t := range f.trades {
		if t.Status != "CLOSED" || !t.PnlPln.Valid {
			continue
		}
		if candidate == nil {
			candidate = &f.trades[i]
			continue
		}
		v, _ := pgnum.ToDecimal(t.PnlPln)
		cv, _ := pgnum.ToDecimal(candidate.PnlPln)
		if (best && v.Cmp(cv) > 0) || (!best && v.Cmp(cv) < 0) {
			candidate = &f.trades[i]
		}
	}
	if candidate == nil {
		return sqlcgen.BotTrade{}, pgx.ErrNoRows
	}
	return *candidate, nil
}

func (f *fakeQueries) BestBotTrade(_ context.Context) (sqlcgen.BotTrade, error) {
	return extremum(f, true)
}

func (f *fakeQueries) WorstBotTrade(_ context.Context) (sqlcgen.BotTrade, error) {
	return extremum(f, false)
}

func (f *fakeQueries) BotEquityByDay(_ context.Context) ([]sqlcgen.BotEquityByDayRow, error) {
	byDay := map[string]decimal.Decimal{}
	order := []string{}
	for _, t := range f.trades {
		if t.Status != "CLOSED" || !t.ClosedAt.Valid {
			continue
		}
		day := t.ClosedAt.Time.Format("2006-01-02")
		if _, ok := byDay[day]; !ok {
			order = append(order, day)
		}
		v, _ := pgnum.ToDecimal(t.PnlPln)
		byDay[day] = byDay[day].Add(v)
	}
	// order is insertion order; tests insert chronologically.
	var out []sqlcgen.BotEquityByDayRow
	for _, d := range order {
		day, _ := time.Parse("2006-01-02", d)
		out = append(out, sqlcgen.BotEquityByDayRow{
			Day:      pgtype.Date{Time: day, Valid: true},
			DailyPnl: pgnum.FromDecimal(byDay[d]),
		})
	}
	return out, nil
}

func (f *fakeQueries) OpenBotTrades(_ context.Context, arg sqlcgen.OpenBotTradesParams) ([]sqlcgen.BotTrade, error) {
	var out []sqlcgen.BotTrade
	for _, t := range f.trades {
		if t.Status == "OPEN" {
			out = append(out, t)
		}
	}
	start := int(arg.Offset)
	if start > len(out) {
		start = len(out)
	}
	end := start + int(arg.Limit)
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], nil
}

func seedTrade(f *fakeQueries, id int64, status, side, pnl string, opened, closed time.Time) {
	qty := pgnum.FromDecimal(decimal.RequireFromString("0.01"))
	entry := pgnum.FromDecimal(decimal.RequireFromString("60000"))
	row := sqlcgen.BotTrade{
		ID: id, Side: side, Status: status, Quantity: qty, Leverage: 3,
		EntryPrice:         entry,
		OpenedAt:           pgtype.Timestamptz{Time: opened, Valid: true},
		CreatedAt:          pgtype.Timestamptz{Time: opened, Valid: true},
		ScalpSessionStatus: "NONE",
	}
	if pnl != "" {
		row.PnlPln = pgnum.FromDecimal(decimal.RequireFromString(pnl))
		row.ClosedAt = pgtype.Timestamptz{Time: closed, Valid: true}
	}
	f.trades = append(f.trades, row)
}

func TestStats_Aggregates(t *testing.T) {
	f := newFakeQueries()
	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	seedTrade(f, 1, "CLOSED", "LONG", "30", base, base.Add(30*time.Minute))
	seedTrade(f, 2, "CLOSED", "SHORT", "-10", base, base.Add(90*time.Minute))
	seedTrade(f, 3, "OPEN", "LONG", "", base, time.Time{})
	svc := NewService(f, nil)

	stats, err := svc.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalTrades != 3 || stats.OpenTrades != 1 || stats.ClosedTrades != 2 {
		t.Fatalf("unexpected counts: %+v", stats)
	}
	if stats.TotalPnlPln.String() != "20" {
		t.Fatalf("expected total pnl 20, got %s", stats.TotalPnlPln)
	}
	// 1 win / 2 closed = 50.00.
	if stats.WinRate == nil || *stats.WinRate != 50 {
		t.Fatalf("expected winRate 50, got %v", stats.WinRate)
	}
	// totalPnlPct is ALWAYS null (reserved) — the binding quirk.
	if stats.TotalPnlPct != nil {
		t.Fatalf("expected totalPnlPct null, got %v", *stats.TotalPnlPct)
	}
	// avg hold = (30+90)/2 = 60 minutes.
	if stats.AvgHoldMinutes == nil || *stats.AvgHoldMinutes != 60 {
		t.Fatalf("expected avgHold 60, got %v", stats.AvgHoldMinutes)
	}
	if stats.BestTrade == nil || stats.BestTrade.PnlPln == nil || *stats.BestTrade.PnlPln != 30 {
		t.Fatalf("expected best pnl 30, got %+v", stats.BestTrade)
	}
	if stats.WorstTrade == nil || stats.WorstTrade.PnlPln == nil || *stats.WorstTrade.PnlPln != -10 {
		t.Fatalf("expected worst pnl -10, got %+v", stats.WorstTrade)
	}
}

func TestStats_EmptyIsZeroed(t *testing.T) {
	stats, err := NewService(newFakeQueries(), nil).Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalTrades != 0 || stats.WinRate != nil || stats.AvgHoldMinutes != nil ||
		stats.BestTrade != nil || stats.WorstTrade != nil || stats.TotalPnlPct != nil {
		t.Fatalf("expected zeroed stats with nulls, got %+v", stats)
	}
	if !stats.TotalPnlPln.IsZero() {
		t.Fatalf("expected zero total pnl, got %s", stats.TotalPnlPln)
	}
}

func TestEquity_CumulativeAscending(t *testing.T) {
	f := newFakeQueries()
	d1 := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	seedTrade(f, 1, "CLOSED", "LONG", "30", d1, d1.Add(time.Hour))
	seedTrade(f, 2, "CLOSED", "SHORT", "-10", d2, d2.Add(time.Hour))
	seedTrade(f, 3, "OPEN", "LONG", "", d2, time.Time{})
	points, err := NewService(f, nil).Equity(context.Background())
	if err != nil {
		t.Fatalf("Equity: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 points (OPEN excluded), got %+v", points)
	}
	if points[0].DailyPnlPln.String() != "30" || points[0].CumulativePnlPln.String() != "30" {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].DailyPnlPln.String() != "-10" || points[1].CumulativePnlPln.String() != "20" {
		t.Fatalf("unexpected second point: %+v", points[1])
	}
}

func TestOpenPositions_NonPagedEnvelopeQuirk(t *testing.T) {
	f := newFakeQueries()
	base := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	seedTrade(f, 1, "OPEN", "LONG", "", base, time.Time{})
	seedTrade(f, 2, "CLOSED", "LONG", "5", base, base.Add(time.Hour))
	svc := NewService(f, nil)

	trades, err := svc.OpenPositions(context.Background(), 0, 20)
	if err != nil {
		t.Fatalf("OpenPositions: %v", err)
	}
	if len(trades) != 1 || trades[0].Status != "OPEN" {
		t.Fatalf("expected only the OPEN row, got %+v", trades)
	}
	// scalpSessionStatus is always emitted (DB NOT NULL DEFAULT 'NONE'), even NONE.
	if trades[0].ScalpSessionStatus == nil || *trades[0].ScalpSessionStatus != "NONE" {
		t.Fatalf("expected scalpSessionStatus NONE emitted, got %+v", trades[0].ScalpSessionStatus)
	}
}
