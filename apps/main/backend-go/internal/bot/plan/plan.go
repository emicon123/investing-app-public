// Package plan ports BotPlanController + BotPlanService (Phase 5): daily Claude
// plans with scenario ladders.
//
// Write ownership (doc 13 §6.3): bot_daily_plan + bot_plan_scenario are written ONLY
// by Ingest (upsert by plan_date, scenarios replaced atomically). No other Go
// package writes these tables. The watcher (trade package) reads scenarios by
// event_time.
package plan

import (
	"time"

	"github.com/shopspring/decimal"
)

// ScenarioDTO is one ladder rung in service-layer shape.
type ScenarioDTO struct {
	ID               int64
	CalendarEventID  *int64
	EventName        string
	EventTime        time.Time
	ScenarioLabel    string
	TriggerCondition string
	Action           string
	Leverage         *int
	SurpriseMin      *decimal.Decimal
	SurpriseMax      *decimal.Decimal
	Confidence       *decimal.Decimal
	Reasoning        string
}

// PlanDTO is one daily plan with its scenarios (eventTime ASC).
type PlanDTO struct {
	ID            int64
	PlanDate      time.Time
	Status        string
	Summary       *string
	MarketContext map[string]any
	Model         *string
	Scenarios     []ScenarioDTO
}

// IngestScenario is one validated ladder rung of an ingest request.
type IngestScenario struct {
	CalendarEventID  *int64
	EventName        string
	EventTime        time.Time
	ScenarioLabel    string
	TriggerCondition string
	Action           string
	Leverage         *int
	SurpriseMin      *decimal.Decimal
	SurpriseMax      *decimal.Decimal
	Confidence       *decimal.Decimal
	Reasoning        string
}

// IngestInput is a validated plan ingest request.
type IngestInput struct {
	PlanDate      time.Time
	Summary       *string
	MarketContext map[string]any
	Model         *string
	Status        *string
	Scenarios     []IngestScenario
}

// ListResult is one paged plan listing.
type ListResult struct {
	Data  []PlanDTO
	Total int64
	Page  int
	Size  int
}
