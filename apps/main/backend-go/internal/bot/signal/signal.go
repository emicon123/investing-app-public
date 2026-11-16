// Package signal ports BotSignalController + BotSignalService (Phase 5): the
// per-event decision record (one bot_signal per evaluated event).
//
// Write ownership (doc 13 §6.3): bot_signal is written ONLY here (POST /bot/signals
// machine-only, plus the watcher-internal path which reuses Create). executed flips
// via MarkExecuted. No other Go package writes this table.
package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/botutil"
)

// SignalDTO is one bot_signal row in service-layer shape.
type SignalDTO struct {
	ID              int64
	CalendarEventID *int64
	PlanScenarioID  *int64
	EventName       string
	Actual          string
	Forecast        *string
	Previous        *string
	SurpriseDesc    *string
	Decision        string
	Executed        bool
	RawPayload      map[string]any
	CreatedAt       time.Time
}

// CreateInput is a validated signal record request.
type CreateInput struct {
	CalendarEventID *int64
	PlanScenarioID  *int64
	EventName       string
	Actual          string
	Forecast        *string
	Previous        *string
	SurpriseDesc    *string
	Decision        string
	RawPayload      map[string]any
}

// ListResult is one paged signal listing.
type ListResult struct {
	Data  []SignalDTO
	Total int64
	Page  int
	Size  int
}

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	InsertSignal(ctx context.Context, arg sqlcgen.InsertSignalParams) (sqlcgen.BotSignal, error)
	MarkSignalExecuted(ctx context.Context, id int64) (sqlcgen.BotSignal, error)
	ListSignals(ctx context.Context, arg sqlcgen.ListSignalsParams) ([]sqlcgen.BotSignal, error)
	CountSignals(ctx context.Context, eventName string) (int64, error)
}

// Service is the signal service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries Queries
}

// NewService wires queries (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries) *Service { return &Service{queries: queries} }

// Create records a signal (executed=false) — the direct port of
// BotSignalService.create. calendarEventId/planScenarioId resolve-or-null exactly
// like Java (unknown ids are tolerated, not rejected).
func (s *Service) Create(ctx context.Context, in CreateInput) (SignalDTO, error) {
	if strings.TrimSpace(in.EventName) == "" {
		return SignalDTO{}, botutil.Unprocessable("eventName", "must not be blank")
	}
	if strings.TrimSpace(in.Actual) == "" {
		return SignalDTO{}, botutil.Unprocessable("actual", "must not be blank")
	}
	switch strings.ToUpper(strings.TrimSpace(in.Decision)) {
	case "LONG", "SHORT", "HOLD":
	default:
		return SignalDTO{}, botutil.Unprocessable("decision",
			fmt.Sprintf("Unknown decision: %s", in.Decision))
	}
	var raw []byte
	if in.RawPayload != nil {
		var err error
		raw, err = json.Marshal(in.RawPayload)
		if err != nil {
			return SignalDTO{}, fmt.Errorf("signal: encode rawPayload: %w", err)
		}
	}
	row, err := s.queries.InsertSignal(ctx, sqlcgen.InsertSignalParams{
		CalendarEventID: in.CalendarEventID,
		PlanScenarioID:  in.PlanScenarioID,
		EventName:       strings.TrimSpace(in.EventName),
		Actual:          in.Actual,
		Forecast:        in.Forecast,
		Previous:        in.Previous,
		SurpriseDesc:    in.SurpriseDesc,
		Decision:        strings.ToUpper(strings.TrimSpace(in.Decision)),
		RawPayload:      raw,
	})
	if err != nil {
		return SignalDTO{}, fmt.Errorf("signal: create: %w", err)
	}
	return toDTO(row)
}

// MarkExecuted flips one signal to executed=true — the direct port of
// BotSignalService.markExecuted. Unknown id answers 404.
func (s *Service) MarkExecuted(ctx context.Context, id int64) (SignalDTO, error) {
	row, err := s.queries.MarkSignalExecuted(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return SignalDTO{}, botutil.NotFound("Signal not found")
		}
		return SignalDTO{}, fmt.Errorf("signal: mark executed: %w", err)
	}
	return toDTO(row)
}

// List returns signals newest-first, optionally filtered by event-name substring
// (case-insensitive) — the direct port of BotSignalService.list.
func (s *Service) List(ctx context.Context, eventName string, page, size int) (ListResult, error) {
	if page < 0 {
		return ListResult{}, botutil.Unprocessable("page", "must not be negative")
	}
	if size < 0 {
		return ListResult{}, botutil.Unprocessable("size", "must not be negative")
	}
	rows, err := s.queries.ListSignals(ctx, sqlcgen.ListSignalsParams{
		EventName: eventName, Limit: int32(size), Offset: int32(page * size),
	})
	if err != nil {
		return ListResult{}, fmt.Errorf("signal: list: %w", err)
	}
	total, err := s.queries.CountSignals(ctx, eventName)
	if err != nil {
		return ListResult{}, fmt.Errorf("signal: count: %w", err)
	}
	data := make([]SignalDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			return ListResult{}, fmt.Errorf("signal: decode row %d: %w", r.ID, err)
		}
		data = append(data, dto)
	}
	return ListResult{Data: data, Total: total, Page: page, Size: size}, nil
}

func toDTO(row sqlcgen.BotSignal) (SignalDTO, error) {
	out := SignalDTO{
		ID: row.ID, CalendarEventID: row.CalendarEventID,
		PlanScenarioID: row.PlanScenarioID, EventName: row.EventName,
		Actual: row.Actual, Forecast: row.Forecast, Previous: row.Previous,
		SurpriseDesc: row.SurpriseDesc, Decision: row.Decision,
		Executed: row.Executed, CreatedAt: row.CreatedAt.Time,
	}
	if len(row.RawPayload) > 0 {
		var m map[string]any
		if err := json.Unmarshal(row.RawPayload, &m); err != nil {
			return SignalDTO{}, err
		}
		out.RawPayload = m
	}
	return out, nil
}

func isNoRows(err error) bool {
	return err != nil && (err == pgx.ErrNoRows || strings.Contains(err.Error(), "no rows"))
}
