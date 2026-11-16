package calendar

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/common-go/httpx"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	SearchCalendarEvents(ctx context.Context, arg sqlcgen.SearchCalendarEventsParams) ([]sqlcgen.BotCalendarEvent, error)
	CountCalendarEvents(ctx context.Context, arg sqlcgen.CountCalendarEventsParams) (int64, error)
	UpsertCalendarEvent(ctx context.Context, arg sqlcgen.UpsertCalendarEventParams) (int64, error)
	DeleteCalendarEventsBefore(ctx context.Context, cutoff pgtype.Timestamptz) (int64, error)
}

// Service is the calendar service layer: handler -> Service -> sqlc Queries, plus
// the sync orchestrator and the nightly purge.
type Service struct {
	queries       Queries
	orchestrator  *Orchestrator
	retentionDays int
}

// NewService wires queries with the calendar retention window
// (app.bot.execution.calendar-retention-days, default 30). orchestrator is the sync
// orchestrator serving POST /bot/calendar/sync (nil only in unit tests that never
// call Sync).
func NewService(queries Queries, orchestrator *Orchestrator, retentionDays int) *Service {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	return &Service{queries: queries, orchestrator: orchestrator, retentionDays: retentionDays}
}

// List returns calendar events in [from, to) (Europe/Warsaw day bounds), eventTime
// ASC, paged — the direct port of BotCalendarService.list. Nil from defaults to
// now−24h, nil to to from+7d. impact is a comma-separated BotImpact list (blank =
// all); unknown values answer 422.
func (s *Service) List(ctx context.Context, from, to *time.Time, impact, currency string, page, size int) (ListResult, error) {
	if page < 0 {
		return ListResult{}, botutil.Unprocessable("page", "must not be negative")
	}
	if size < 0 {
		return ListResult{}, botutil.Unprocessable("size", "must not be negative")
	}
	fromInst := time.Now().Add(-24 * time.Hour)
	if from != nil {
		fromInst = startOfWarsawDay(*from)
	}
	toInst := fromInst.Add(7 * 24 * time.Hour)
	if to != nil {
		toInst = startOfWarsawDay(*to).Add(24 * time.Hour)
	}
	impacts := []string{}
	if strings.TrimSpace(impact) != "" {
		for _, part := range strings.Split(impact, ",") {
			v := strings.ToUpper(strings.TrimSpace(part))
			if !ValidImpacts[v] {
				return ListResult{}, botutil.Unprocessable("impact", fmt.Sprintf("unknown impact: %s", strings.TrimSpace(part)))
			}
			impacts = append(impacts, v)
		}
	}
	rows, err := s.queries.SearchCalendarEvents(ctx, sqlcgen.SearchCalendarEventsParams{
		EventTime:   pgtype.Timestamptz{Time: fromInst, Valid: true},
		EventTime_2: pgtype.Timestamptz{Time: toInst, Valid: true},
		Limit:       int32(size),
		Offset:      int32(page * size),
		Impacts:     impacts,
		Currency:    strings.TrimSpace(currency),
	})
	if err != nil {
		return ListResult{}, fmt.Errorf("calendar: search: %w", err)
	}
	total, err := s.queries.CountCalendarEvents(ctx, sqlcgen.CountCalendarEventsParams{
		EventTime:   pgtype.Timestamptz{Time: fromInst, Valid: true},
		EventTime_2: pgtype.Timestamptz{Time: toInst, Valid: true},
		Impacts:     impacts,
		Currency:    strings.TrimSpace(currency),
	})
	if err != nil {
		return ListResult{}, fmt.Errorf("calendar: count: %w", err)
	}
	data := make([]EventDTO, 0, len(rows))
	for _, r := range rows {
		data = append(data, toDTO(r))
	}
	return ListResult{Data: data, Total: total, Page: page, Size: size}, nil
}

// Sync runs the full calendar sync FF → TE → BLS-baseline — the direct port of
// BotCalendarSyncOrchestrator.syncAll serving POST /bot/calendar/sync. Never
// parallel, never reordered; fail-soft per source.
func (s *Service) Sync(ctx context.Context) (int, error) {
	if s.orchestrator == nil {
		return 0, &httpx.AppError{Status: 500, Code: httpx.CodeInternalError, Message: "calendar sync not wired"}
	}
	return s.orchestrator.SyncAll(ctx), nil
}

// PurgeOldEvents deletes events older than the retention window — the direct port of
// BotCalendarService.purgeOldEvents (04:00 Warsaw nightly). FKs from
// scenarios/signals/trades are ON DELETE SET NULL, so history rows vanish safely.
func (s *Service) PurgeOldEvents(ctx context.Context) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -s.retentionDays)
	deleted, err := s.queries.DeleteCalendarEventsBefore(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("calendar: purge: %w", err)
	}
	if deleted > 0 {
		slog.Info("bot calendar purge: deleted old events", "deleted", deleted, "retentionDays", s.retentionDays)
	}
	return deleted, nil
}

func toDTO(r sqlcgen.BotCalendarEvent) EventDTO {
	return EventDTO{
		ID: r.ID, EventTime: r.EventTime.Time, Currency: r.Currency,
		Country: r.Country, EventName: r.EventName, Impact: r.Impact,
		Forecast: r.Forecast, Previous: r.Previous, Actual: r.Actual,
		Source: r.Source, ActualSource: r.ActualSource, ExternalID: r.ExternalID,
		CreatedAt: r.CreatedAt.Time,
	}
}

func startOfWarsawDay(t time.Time) time.Time {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		loc = time.UTC
	}
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
