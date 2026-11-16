package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/common-go/pgnum"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	FindPlanByDate(ctx context.Context, planDate pgtype.Date) (sqlcgen.BotDailyPlan, error)
	LatestPlan(ctx context.Context) (sqlcgen.BotDailyPlan, error)
	ListPlans(ctx context.Context, arg sqlcgen.ListPlansParams) ([]sqlcgen.BotDailyPlan, error)
	CountPlans(ctx context.Context) (int64, error)
	UpsertPlan(ctx context.Context, arg sqlcgen.UpsertPlanParams) (sqlcgen.BotDailyPlan, error)
	DeleteScenariosByPlanID(ctx context.Context, planID int64) error
	InsertScenario(ctx context.Context, arg sqlcgen.InsertScenarioParams) (sqlcgen.BotPlanScenario, error)
	ScenariosByPlanID(ctx context.Context, planID int64) ([]sqlcgen.BotPlanScenario, error)
	ExistingCalendarEventIDs(ctx context.Context, ids []int64) ([]int64, error)
}

// TransactFunc runs fn inside a single DB transaction — ingest's whole persist phase
// (plan upsert, scenario delete, scenario inserts) commits atomically, the direct
// port of BotPlanService.ingest's @Transactional. Production passes a pgx
// Begin/Commit closure; unit tests pass a direct call-through.
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// Service is the plan service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries  Queries
	transact TransactFunc
}

// NewService wires queries with a transaction runner (explicit constructor wiring,
// ADR-094 §2 — no DI framework).
func NewService(queries Queries, transact TransactFunc) *Service {
	if transact == nil {
		transact = func(ctx context.Context, fn func(Queries) error) error { return fn(queries) }
	}
	return &Service{queries: queries, transact: transact}
}

// List returns daily plans with nested scenarios, planDate ASC, paged — the direct
// port of BotPlanService.list.
func (s *Service) List(ctx context.Context, page, size int) (ListResult, error) {
	if page < 0 {
		return ListResult{}, botutil.Unprocessable("page", "must not be negative")
	}
	if size < 0 {
		return ListResult{}, botutil.Unprocessable("size", "must not be negative")
	}
	rows, err := s.queries.ListPlans(ctx, sqlcgen.ListPlansParams{Limit: int32(size), Offset: int32(page * size)})
	if err != nil {
		return ListResult{}, fmt.Errorf("plan: list: %w", err)
	}
	total, err := s.queries.CountPlans(ctx)
	if err != nil {
		return ListResult{}, fmt.Errorf("plan: count: %w", err)
	}
	data := make([]PlanDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := s.loadPlan(ctx, r)
		if err != nil {
			return ListResult{}, err
		}
		data = append(data, dto)
	}
	return ListResult{Data: data, Total: total, Page: page, Size: size}, nil
}

// Latest returns the most recent plan — the direct port of BotPlanService.latest.
// No plans yet answers 404.
func (s *Service) Latest(ctx context.Context) (PlanDTO, error) {
	row, err := s.queries.LatestPlan(ctx)
	if err != nil {
		if isNoRows(err) {
			return PlanDTO{}, botutil.NotFound("No plans yet")
		}
		return PlanDTO{}, fmt.Errorf("plan: latest: %w", err)
	}
	return s.loadPlan(ctx, row)
}

// ByDate returns the plan for one date — the direct port of BotPlanService.getByDate.
// Unknown date answers 404.
func (s *Service) ByDate(ctx context.Context, date time.Time) (PlanDTO, error) {
	row, err := s.queries.FindPlanByDate(ctx, pgtype.Date{Time: date, Valid: true})
	if err != nil {
		if isNoRows(err) {
			return PlanDTO{}, botutil.NotFound(fmt.Sprintf("No plan for %s", date.Format("2006-01-02")))
		}
		return PlanDTO{}, fmt.Errorf("plan: by date: %w", err)
	}
	return s.loadPlan(ctx, row)
}

// Ingest upserts a daily plan by planDate and replaces all its scenarios — the
// direct port of BotPlanService.ingest (single transaction). Only "EMPTY" is honored
// literally as status; anything else becomes PUBLISHED. Ladders validate per the
// 422 rules in the spec description; unknown calendarEventIds degrade to null (same
// as Java's findById-or-null).
func (s *Service) Ingest(ctx context.Context, in IngestInput) (PlanDTO, error) {
	if in.PlanDate.IsZero() {
		return PlanDTO{}, botutil.Unprocessable("planDate", "must not be null")
	}
	if err := validateScenarios(in.Scenarios); err != nil {
		return PlanDTO{}, err
	}
	status := "PUBLISHED"
	if in.Status != nil && *in.Status == "EMPTY" {
		status = "EMPTY"
	}
	marketCtx := in.MarketContext
	if marketCtx == nil {
		marketCtx = map[string]any{}
	}
	marketJSON, err := json.Marshal(marketCtx)
	if err != nil {
		return PlanDTO{}, fmt.Errorf("plan: encode marketContext: %w", err)
	}
	var planID int64
	var stored PlanDTO
	err = s.transact(ctx, func(q Queries) error {
		plan, err := q.UpsertPlan(ctx, sqlcgen.UpsertPlanParams{
			PlanDate:      pgtype.Date{Time: in.PlanDate, Valid: true},
			Status:        status,
			Summary:       in.Summary,
			MarketContext: marketJSON,
			Model:         in.Model,
		})
		if err != nil {
			return fmt.Errorf("plan: upsert: %w", err)
		}
		planID = plan.ID
		if err := q.DeleteScenariosByPlanID(ctx, plan.ID); err != nil {
			return fmt.Errorf("plan: replace scenarios: %w", err)
		}
		knownCals := map[int64]bool{}
		if len(in.Scenarios) > 0 {
			ids := make([]int64, 0, len(in.Scenarios))
			for _, sc := range in.Scenarios {
				if sc.CalendarEventID != nil {
					ids = append(ids, *sc.CalendarEventID)
				}
			}
			if len(ids) > 0 {
				existing, err := q.ExistingCalendarEventIDs(ctx, ids)
				if err != nil {
					return fmt.Errorf("plan: resolve calendar ids: %w", err)
				}
				for _, id := range existing {
					knownCals[id] = true
				}
			}
			for _, sc := range in.Scenarios {
				var calID *int64
				if sc.CalendarEventID != nil && knownCals[*sc.CalendarEventID] {
					calID = sc.CalendarEventID
				}
				if _, err := q.InsertScenario(ctx, sqlcgen.InsertScenarioParams{
					PlanID:           plan.ID,
					CalendarEventID:  calID,
					EventName:        sc.EventName,
					EventTime:        pgtype.Timestamptz{Time: sc.EventTime, Valid: true},
					ScenarioLabel:    sc.ScenarioLabel,
					TriggerCondition: sc.TriggerCondition,
					Action:           sc.Action,
					Leverage:         toInt16(sc.Leverage),
					SurpriseMin:      pgnum.FromDecimalPtr(sc.SurpriseMin),
					SurpriseMax:      pgnum.FromDecimalPtr(sc.SurpriseMax),
					Confidence:       pgnum.FromDecimalPtr(sc.Confidence),
					Reasoning:        sc.Reasoning,
				}); err != nil {
					return fmt.Errorf("plan: insert scenario: %w", err)
				}
			}
		}
		stored, err = loadPlanWith(ctx, q, plan)
		return err
	})
	if err != nil {
		return PlanDTO{}, err
	}
	_ = planID
	return stored, nil
}

// validateScenarios ports BotPlanService.validateScenarios: directional legs require
// confidence + leverage 1–10, HOLD legs must carry null leverage, surpriseMin must
// not exceed surpriseMax. Every violation answers 422 per docs/openapi.yaml (NOT
// Java's 400/UNPROCESSABLE_CONTENT split — the spec documents a single 422).
func validateScenarios(scenarios []IngestScenario) error {
	for _, sc := range scenarios {
		if strings.TrimSpace(sc.EventName) == "" {
			return botutil.Unprocessable("eventName", "must not be blank")
		}
		if sc.EventTime.IsZero() {
			return botutil.Unprocessable("eventTime", "must not be null")
		}
		if strings.TrimSpace(sc.ScenarioLabel) == "" {
			return botutil.Unprocessable("scenarioLabel", "must not be blank")
		}
		if strings.TrimSpace(sc.TriggerCondition) == "" {
			return botutil.Unprocessable("triggerCondition", "must not be blank")
		}
		if strings.TrimSpace(sc.Reasoning) == "" {
			return botutil.Unprocessable("reasoning", "must not be blank")
		}
		var action string
		switch strings.ToUpper(strings.TrimSpace(sc.Action)) {
		case "LONG", "SHORT", "HOLD":
			action = strings.ToUpper(strings.TrimSpace(sc.Action))
		default:
			return botutil.Unprocessable("action", fmt.Sprintf("Unknown action: %s", sc.Action))
		}
		directional := action == "LONG" || action == "SHORT"
		if directional && sc.Confidence == nil {
			return botutil.Unprocessable("confidence",
				fmt.Sprintf("Directional scenario '%s' requires confidence", sc.ScenarioLabel))
		}
		if directional && (sc.Leverage == nil || *sc.Leverage < 1 || *sc.Leverage > 10) {
			return botutil.Unprocessable("leverage",
				fmt.Sprintf("Directional scenario '%s' requires leverage 1-10", sc.ScenarioLabel))
		}
		if action == "HOLD" && sc.Leverage != nil {
			return botutil.Unprocessable("leverage",
				fmt.Sprintf("HOLD scenario '%s' must carry null leverage", sc.ScenarioLabel))
		}
		if sc.SurpriseMin != nil && sc.SurpriseMax != nil && sc.SurpriseMin.Cmp(*sc.SurpriseMax) > 0 {
			return botutil.Unprocessable("surpriseMin",
				fmt.Sprintf("Scenario '%s' has surpriseMin > surpriseMax", sc.ScenarioLabel))
		}
	}
	return nil
}

func (s *Service) loadPlan(ctx context.Context, row sqlcgen.BotDailyPlan) (PlanDTO, error) {
	return loadPlanWith(ctx, s.queries, row)
}

func loadPlanWith(ctx context.Context, q Queries, row sqlcgen.BotDailyPlan) (PlanDTO, error) {
	scenarios, err := q.ScenariosByPlanID(ctx, row.ID)
	if err != nil {
		return PlanDTO{}, fmt.Errorf("plan: scenarios: %w", err)
	}
	out := PlanDTO{
		ID: row.ID, PlanDate: row.PlanDate.Time, Status: row.Status,
		Summary: row.Summary, Model: row.Model,
		Scenarios: make([]ScenarioDTO, 0, len(scenarios)),
	}
	if len(row.MarketContext) > 0 {
		var m map[string]any
		if err := json.Unmarshal(row.MarketContext, &m); err != nil {
			return PlanDTO{}, fmt.Errorf("plan: decode marketContext: %w", err)
		}
		out.MarketContext = m
	}
	for _, sc := range scenarios {
		dto, err := toScenarioDTO(sc)
		if err != nil {
			return PlanDTO{}, err
		}
		out.Scenarios = append(out.Scenarios, dto)
	}
	return out, nil
}

func toScenarioDTO(sc sqlcgen.BotPlanScenario) (ScenarioDTO, error) {
	out := ScenarioDTO{
		ID: sc.ID, CalendarEventID: sc.CalendarEventID,
		EventName: sc.EventName, EventTime: sc.EventTime.Time,
		ScenarioLabel: sc.ScenarioLabel, TriggerCondition: sc.TriggerCondition,
		Action: sc.Action, Reasoning: sc.Reasoning,
	}
	if sc.Leverage != nil {
		v := int(*sc.Leverage)
		out.Leverage = &v
	}
	if sc.SurpriseMin.Valid {
		v, err := pgnum.ToDecimal(sc.SurpriseMin)
		if err != nil {
			return ScenarioDTO{}, err
		}
		out.SurpriseMin = &v
	}
	if sc.SurpriseMax.Valid {
		v, err := pgnum.ToDecimal(sc.SurpriseMax)
		if err != nil {
			return ScenarioDTO{}, err
		}
		out.SurpriseMax = &v
	}
	if sc.Confidence.Valid {
		v, err := pgnum.ToDecimal(sc.Confidence)
		if err != nil {
			return ScenarioDTO{}, err
		}
		out.Confidence = &v
	}
	return out, nil
}

func toInt16(v *int) *int16 {
	if v == nil {
		return nil
	}
	out := int16(*v)
	return &out
}

func isNoRows(err error) bool {
	return err != nil && (err == pgx.ErrNoRows || strings.Contains(err.Error(), "no rows"))
}
