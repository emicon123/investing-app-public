// Package trade ports BotTradeController + BotTradeService + BotScalpSessionService +
// BotTradeActionService + BotTradeTickService + LiveExecutionService +
// BotPositionSizingService + EventReleaseWatcherService + OkxTradingClient (Phase 5):
// the full OKX macro-calendar trading path.
//
// Write ownership (doc 13 §6.3): bot_trade, bot_trade_action, bot_trade_tick are
// written ONLY here. Read-only dependencies: bot_control (kill switch),
// price_cache BTC-USD (live enrichment) + fx_rate_cache USD→PLN (sizing audit
// column) via Phase-1 providers, bot_signal/bot_plan_scenario/bot_calendar_event
// (watcher snapshots). Zero writes outside bot_*.
//
// SAFETY (ADR-090, Architect binding — money-critical):
//   - BOT_LIVE_EXECUTION_ENABLED gates LiveExecutionService.Execute as its FIRST
//     instruction; default OFF. Blocked → zero OKX calls, outcome
//     SKIPPED_FLAG_DISABLED, warn log with action/leverage/confidence. Signals are
//     still recorded (tracking is never gated).
//   - BOT_SCHEDULER_ENABLED gates the 1s release watcher + 04:00 purge; default OFF
//     (shadow mode — Java is the only actor until the flip).
//   - Kill switch (bot_control.trading_enabled) gates entry (create → 409 +
//     SKIPPED_PAUSED); close/actions/ticks/scalp paths intentionally bypass it.
//   - Sizing veto: leverage×stopDistance ≤ 30% (reduce leverage first, else skip).
//   - STALE_SKIP 45s, cluster-wait 20s, DUPLICATE 60s + max-1-OPEN — all inside the
//     create choke point / watcher, mirroring Java exactly.
//   - OKX call sites exist ONLY in LiveExecutionService via the OKXClient interface
//     (equity/leverage/ctVal/placeMarket/SL-algo/closeMarket+verify-flat).
//   - NOT ported (pre-existing Java debt, ADR-090 §5): pnl_pln quote-currency
//     labeling; dead countWinning/Closed/deleteByCreatedAtBefore (no callers).
//
// Audit (doc 13 §5 smell #4 — no formal Command type): a func(ctx) error plus the
// AuditTrail helper below (AppendNote/RecordAction) is the durable trail — trade
// okx_*/margin/confidence/notes columns, one signal per evaluated event, SYSTEM/
// SCALP_* action rows, SL_PLACE_FAILED_EMERGENCY_CLOSED marker.
package trade

import (
	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
)

// CreateInput is a validated trade-open request.
type CreateInput struct {
	SignalID        *int64
	CalendarEventID *int64
	Side            string
	Quantity        decimal.Decimal
	QuantitySet     bool
	Leverage        *int
	EntryPrice      decimal.Decimal
	EntryPriceSet   bool
	OkxOrderID      *string
	Notes           *string
}

// CloseInput is a validated trade-close request.
type CloseInput struct {
	ExitPrice    decimal.Decimal
	ExitPriceSet bool
	PnlPln       *decimal.Decimal
	PnlPct       *decimal.Decimal
}

// ActionInput is a validated trade-action append request.
type ActionInput struct {
	ActionType string
	Price      *decimal.Decimal
	Quantity   *decimal.Decimal
	Reasoning  *string
	Source     *string
}

// TickInput is one validated scalp tick.
type TickInput struct {
	Price          *decimal.Decimal
	Volume         *decimal.Decimal
	Cvd            *decimal.Decimal
	Atr1m          *decimal.Decimal
	ObImbalance    *decimal.Decimal
	FundingRate    *decimal.Decimal
	LongShortRatio *decimal.Decimal
	OpenInterest   *decimal.Decimal
}

// ListResult is one paged trade listing (enriched, wire-ready).
type ListResult struct {
	Data  []openapi.BotTrade
	Total int64
	Page  int
	Size  int
}

// ValidActionTypes mirrors the bot_trade_action CHECK (V63 + V70 widening).
var ValidActionTypes = map[string]bool{
	"SET_SL": true, "SET_TP": true, "TRAIL_SL": true, "PARTIAL_CLOSE": true,
	"FULL_CLOSE": true, "SCALP_SESSION_STARTED": true, "SCALP_SESSION_FINISHED": true,
}

// ValidActionSources mirrors the source CHECK (V63 + V70 SYSTEM widening).
var ValidActionSources = map[string]bool{
	"BOT": true, "CLAUDE": true, "SYSTEM": true,
}
