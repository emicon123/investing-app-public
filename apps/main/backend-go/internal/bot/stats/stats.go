// Package stats ports BotStatsController (stats/equity/open-positions) +
// BotStatsService (Phase 5): trade aggregates over CLOSED trades.
//
// Read-only: every aggregate is a GROUP BY over bot_trade (never find-all-then-
// compute). No writes in this package. Open positions reuse botutil.EnrichTrade so
// the live-price enrichment is byte-identical with the trade package's list/get.
package stats

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/pgnum"
)

// StatsDTO is the aggregate stats payload in service-layer shape.
type StatsDTO struct {
	TotalTrades    int64
	OpenTrades     int64
	ClosedTrades   int64
	WinRate        *float64
	TotalPnlPln    decimal.Decimal
	TotalPnlPct    *float64 // always nil — reserved, matching Java's literal null
	AvgHoldMinutes *float64
	BestTrade      *openapi.BotTrade
	WorstTrade     *openapi.BotTrade
}

// EquityPoint is one daily cumulative PnL point.
type EquityPoint struct {
	Date             time.Time
	CumulativePnlPln decimal.Decimal
	DailyPnlPln      decimal.Decimal
}

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	BotTradeStats(ctx context.Context) (sqlcgen.BotTradeStatsRow, error)
	BestBotTrade(ctx context.Context) (sqlcgen.BotTrade, error)
	WorstBotTrade(ctx context.Context) (sqlcgen.BotTrade, error)
	BotEquityByDay(ctx context.Context) ([]sqlcgen.BotEquityByDayRow, error)
	OpenBotTrades(ctx context.Context, arg sqlcgen.OpenBotTradesParams) ([]sqlcgen.BotTrade, error)
}

// Service is the stats service layer: handler -> Service -> sqlc Queries + read-only
// BTC price for enrichment.
type Service struct {
	queries Queries
	prices  botutil.BTCPriceSource
	now     func() time.Time
}

// NewService wires queries with the read-only price source (explicit constructor
// wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries, prices botutil.BTCPriceSource) *Service {
	return &Service{queries: queries, prices: prices, now: time.Now}
}

// Stats returns trade aggregates — the direct port of BotStatsService.stats.
// totalPnlPct is always nil (reserved, exactly like Java's literal null). best/worst
// consider only CLOSED trades with non-null pnl; avgHoldMinutes covers CLOSED trades
// with both timestamps.
func (s *Service) Stats(ctx context.Context) (StatsDTO, error) {
	row, err := s.queries.BotTradeStats(ctx)
	if err != nil {
		return StatsDTO{}, fmt.Errorf("stats: totals: %w", err)
	}
	totalPnl, err := pgnum.ToDecimal(row.TotalPnl)
	if err != nil {
		return StatsDTO{}, fmt.Errorf("stats: decode total pnl: %w", err)
	}
	out := StatsDTO{
		TotalTrades: row.Total, OpenTrades: row.OpenTrades,
		ClosedTrades: row.ClosedTrades, TotalPnlPln: totalPnl,
	}
	if row.ClosedTrades > 0 {
		winRate := float64(row.Wins) * 100.0 / float64(row.ClosedTrades)
		// Java: BigDecimal.valueOf(winRate).setScale(2, HALF_UP).
		wr := decimal.NewFromFloat(winRate).Round(2).InexactFloat64()
		out.WinRate = &wr
	}
	if row.AvgHoldMinutes.Valid {
		v, err := pgnum.ToDecimal(row.AvgHoldMinutes)
		if err != nil {
			return StatsDTO{}, fmt.Errorf("stats: decode avg hold: %w", err)
		}
		f := v.InexactFloat64()
		out.AvgHoldMinutes = &f
	}
	currentPrice := s.currentBTC(ctx)
	if best, err := s.queries.BestBotTrade(ctx); err == nil {
		enriched, err := botutil.EnrichTrade(best, currentPrice, s.now())
		if err != nil {
			return StatsDTO{}, fmt.Errorf("stats: enrich best: %w", err)
		}
		out.BestTrade = &enriched
	} else if !isNoRows(err) {
		return StatsDTO{}, fmt.Errorf("stats: best: %w", err)
	}
	if worst, err := s.queries.WorstBotTrade(ctx); err == nil {
		enriched, err := botutil.EnrichTrade(worst, currentPrice, s.now())
		if err != nil {
			return StatsDTO{}, fmt.Errorf("stats: enrich worst: %w", err)
		}
		out.WorstTrade = &enriched
	} else if !isNoRows(err) {
		return StatsDTO{}, fmt.Errorf("stats: worst: %w", err)
	}
	return out, nil
}

// Equity returns the daily cumulative PnL series over CLOSED trades, date ASC,
// grouped by Europe/Warsaw day in SQL — the direct port of BotStatsService.equity.
// The cumulative sum is computed in Go over the ordered day rows.
func (s *Service) Equity(ctx context.Context) ([]EquityPoint, error) {
	rows, err := s.queries.BotEquityByDay(ctx)
	if err != nil {
		return nil, fmt.Errorf("stats: equity: %w", err)
	}
	out := make([]EquityPoint, 0, len(rows))
	cum := decimal.Zero
	for _, r := range rows {
		daily, err := pgnum.ToDecimal(r.DailyPnl)
		if err != nil {
			return nil, fmt.Errorf("stats: decode daily pnl: %w", err)
		}
		cum = cum.Add(daily)
		out = append(out, EquityPoint{
			Date: r.Day.Time, CumulativePnlPln: cum, DailyPnlPln: daily,
		})
	}
	return out, nil
}

// OpenPositions returns OPEN trades with live-price enrichment in the non-paged
// ListResponse envelope (no page member — the byte-for-byte quirk the frontend
// normalizes) — the direct port of BotStatsController.openPositions. page/size
// params are accepted (spec) and applied to the row window, but never echoed.
func (s *Service) OpenPositions(ctx context.Context, page, size int) ([]openapi.BotTrade, error) {
	if page < 0 || size < 0 {
		return nil, botutil.Unprocessable("page", "must not be negative")
	}
	rows, err := s.queries.OpenBotTrades(ctx, sqlcgen.OpenBotTradesParams{
		Limit: int32(size), Offset: int32(page * size),
	})
	if err != nil {
		return nil, fmt.Errorf("stats: open positions: %w", err)
	}
	currentPrice := s.currentBTC(ctx)
	out := make([]openapi.BotTrade, 0, len(rows))
	for _, r := range rows {
		enriched, err := botutil.EnrichTrade(r, currentPrice, s.now())
		if err != nil {
			return nil, fmt.Errorf("stats: enrich open trade %d: %w", r.ID, err)
		}
		out = append(out, enriched)
	}
	return out, nil
}

// currentBTC reads the live BTC-USD price for enrichment; nil when unavailable
// (Java's currentBtcPrice fail-soft — enrichment fields are simply omitted).
func (s *Service) currentBTC(ctx context.Context) *decimal.Decimal {
	if s.prices == nil {
		return nil
	}
	price, err := s.prices.BTCPrice(ctx)
	if err != nil {
		return nil
	}
	return &price
}
