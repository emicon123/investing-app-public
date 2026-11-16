// Package calendar ports BotCalendarController + BotCalendarService +
// BotCalendarSyncOrchestrator + ForexFactorySyncService (Phase 5).
//
// Write ownership (doc 13 §6.3): bot_calendar_event is written ONLY by the sync path
// (POST /bot/calendar/sync, ForexFactory source) and the nightly purge. No other Go
// package writes this table.
//
// Sync scope note: the orchestrator runs ForexFactory (fully ported: both feeds,
// 429 backoff, actual-preservation) followed by the TradingEconomics PPI-YoY
// enrichment and the BLS baseline capture as explicit no-op stubs (documented gap —
// those two stay Java-owned until a follow-up ports them; the ordering FF→TE→BLS is
// load-bearing per the orchestrator's contract, so the sequence is preserved even
// while TE/BLS return 0). Never parallel, never reordered.
package calendar

import (
	"time"
)

// EventDTO is one bot_calendar_event row in service-layer shape.
type EventDTO struct {
	ID           int64
	EventTime    time.Time
	Currency     string
	Country      *string
	EventName    string
	Impact       string
	Forecast     *string
	Previous     *string
	Actual       *string
	Source       string
	ActualSource *string
	ExternalID   string
	CreatedAt    time.Time
}

// ListResult is one paged calendar listing.
type ListResult struct {
	Data  []EventDTO
	Total int64
	Page  int
	Size  int
}

// ValidImpacts mirrors the BotImpact enum (V63 CHECK).
var ValidImpacts = map[string]bool{
	"HIGH": true, "MEDIUM": true, "LOW": true, "HOLIDAY": true,
}
