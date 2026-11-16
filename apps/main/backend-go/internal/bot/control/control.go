// Package control ports BotControlController + BotControlService (Phase 5): the
// human-operated kill switch (V65 bot_control, single row id=1) that blocks NEW trade
// entry while paused without ever blocking management of an open position.
//
// Write ownership (doc 13 §6.3): bot_control is written ONLY by Update here. No other
// Go package writes this table. Reads (IsTradingEnabled) gate trade entry in the
// trade package and live execution — same assertTradingEnabled semantics as Java.
package control

import (
	"context"
	"fmt"
	"time"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/botutil"
)

// ControlDTO is the kill-switch row in service-layer shape.
type ControlDTO struct {
	TradingEnabled bool
	PausedReason   *string
	UpdatedAt      time.Time
}

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	GetBotControl(ctx context.Context) (sqlcgen.BotControl, error)
	UpdateBotControl(ctx context.Context, arg sqlcgen.UpdateBotControlParams) (sqlcgen.BotControl, error)
}

// Service is the control service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries Queries
}

// NewService wires queries (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries) *Service { return &Service{queries: queries} }

// Get reads the kill-switch row — the direct port of BotControlService.get. A missing
// V65 seed row answers 500 (operator misconfiguration, never a user error).
func (s *Service) Get(ctx context.Context) (ControlDTO, error) {
	row, err := s.queries.GetBotControl(ctx)
	if err != nil {
		return ControlDTO{}, fmt.Errorf("control: missing bot_control seed row: %w", err)
	}
	return toDTO(row), nil
}

// Update flips the kill switch — the direct port of BotControlService.update.
// tradingEnabled is required (nil → 422; Java's @NotNull would 400, the spec binds
// Go to 422 per the phase precedent).
func (s *Service) Update(ctx context.Context, tradingEnabled *bool, pausedReason *string) (ControlDTO, error) {
	if tradingEnabled == nil {
		return ControlDTO{}, botutil.Unprocessable("tradingEnabled", "must not be null")
	}
	row, err := s.queries.UpdateBotControl(ctx, sqlcgen.UpdateBotControlParams{
		TradingEnabled: *tradingEnabled,
		PausedReason:   pausedReason,
	})
	if err != nil {
		return ControlDTO{}, fmt.Errorf("control: update: %w", err)
	}
	return toDTO(row), nil
}

// IsTradingEnabled reports the kill-switch state for entry gates (trade create, live
// execution). Missing row fails closed (false) — a paused-by-default posture on
// operator misconfiguration.
func (s *Service) IsTradingEnabled(ctx context.Context) bool {
	enabled, _ := s.TradingStatus(ctx)
	return enabled
}

// TradingStatus reports (enabled, pausedReason) for the trade entry choke point.
// A missing seed row fails closed as (false, nil) — entry refuses with 409 rather
// than leaking operator misconfiguration to the caller.
func (s *Service) TradingStatus(ctx context.Context) (bool, *string) {
	row, err := s.queries.GetBotControl(ctx)
	if err != nil {
		return false, nil
	}
	return row.TradingEnabled, row.PausedReason
}

func toDTO(row sqlcgen.BotControl) ControlDTO {
	return ControlDTO{
		TradingEnabled: row.TradingEnabled,
		PausedReason:   row.PausedReason,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}
