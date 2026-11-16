package plan

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as
// prior phases (business logic table-tested here; real Postgres covered by
// integration_test.go).
type fakeQueries struct {
	plans     []sqlcgen.BotDailyPlan
	scenarios map[int64][]sqlcgen.BotPlanScenario
	nextID    int64
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{scenarios: map[int64][]sqlcgen.BotPlanScenario{}, nextID: 1}
}

func (f *fakeQueries) FindPlanByDate(_ context.Context, planDate pgtype.Date) (sqlcgen.BotDailyPlan, error) {
	for _, p := range f.plans {
		if p.PlanDate.Time.Equal(planDate.Time) {
			return p, nil
		}
	}
	return sqlcgen.BotDailyPlan{}, pgx.ErrNoRows
}

func (f *fakeQueries) LatestPlan(_ context.Context) (sqlcgen.BotDailyPlan, error) {
	if len(f.plans) == 0 {
		return sqlcgen.BotDailyPlan{}, pgx.ErrNoRows
	}
	latest := f.plans[0]
	for _, p := range f.plans[1:] {
		if p.PlanDate.Time.After(latest.PlanDate.Time) {
			latest = p
		}
	}
	return latest, nil
}

func (f *fakeQueries) ListPlans(_ context.Context, arg sqlcgen.ListPlansParams) ([]sqlcgen.BotDailyPlan, error) {
	rows := append([]sqlcgen.BotDailyPlan{}, f.plans...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].PlanDate.Time.Before(rows[j].PlanDate.Time) })
	start := int(arg.Offset)
	if start > len(rows) {
		start = len(rows)
	}
	end := start + int(arg.Limit)
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end], nil
}

func (f *fakeQueries) CountPlans(_ context.Context) (int64, error) {
	return int64(len(f.plans)), nil
}

func (f *fakeQueries) UpsertPlan(_ context.Context, arg sqlcgen.UpsertPlanParams) (sqlcgen.BotDailyPlan, error) {
	for i, p := range f.plans {
		if p.PlanDate.Time.Equal(arg.PlanDate.Time) {
			f.plans[i].Status = arg.Status
			f.plans[i].Summary = arg.Summary
			f.plans[i].MarketContext = arg.MarketContext
			f.plans[i].Model = arg.Model
			return f.plans[i], nil
		}
	}
	id := f.nextID
	f.nextID++
	p := sqlcgen.BotDailyPlan{
		ID: id, PlanDate: arg.PlanDate, Status: arg.Status, Summary: arg.Summary,
		MarketContext: arg.MarketContext, Model: arg.Model,
	}
	f.plans = append(f.plans, p)
	return p, nil
}

func (f *fakeQueries) DeleteScenariosByPlanID(_ context.Context, planID int64) error {
	f.scenarios[planID] = nil
	return nil
}

func (f *fakeQueries) InsertScenario(_ context.Context, arg sqlcgen.InsertScenarioParams) (sqlcgen.BotPlanScenario, error) {
	id := f.nextID
	f.nextID++
	sc := sqlcgen.BotPlanScenario{
		ID: id, PlanID: arg.PlanID, CalendarEventID: arg.CalendarEventID,
		EventName: arg.EventName, EventTime: arg.EventTime, ScenarioLabel: arg.ScenarioLabel,
		TriggerCondition: arg.TriggerCondition, Action: arg.Action, Leverage: arg.Leverage,
		Reasoning: arg.Reasoning, SurpriseMin: arg.SurpriseMin, SurpriseMax: arg.SurpriseMax,
		Confidence: arg.Confidence,
		CreatedAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	f.scenarios[arg.PlanID] = append(f.scenarios[arg.PlanID], sc)
	return sc, nil
}

func (f *fakeQueries) ScenariosByPlanID(_ context.Context, planID int64) ([]sqlcgen.BotPlanScenario, error) {
	rows := append([]sqlcgen.BotPlanScenario{}, f.scenarios[planID]...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].EventTime.Time.Before(rows[j].EventTime.Time) })
	return rows, nil
}

func (f *fakeQueries) ExistingCalendarEventIDs(_ context.Context, ids []int64) ([]int64, error) {
	return nil, nil
}

func testService(f *fakeQueries) *Service { return NewService(f, nil) }

func validScenario(label, action string, lev *int) IngestScenario {
	return IngestScenario{
		EventName: "CPI m/m", EventTime: time.Date(2026, 9, 10, 12, 30, 0, 0, time.UTC),
		ScenarioLabel: label, TriggerCondition: "actual > forecast",
		Action: action, Leverage: lev,
		SurpriseMin: decimalPtr("0.1"), SurpriseMax: decimalPtr("0.5"),
		Confidence: decimalPtr("0.6"), Reasoning: "because",
	}
}

func decimalPtr(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

func intPtr(v int) *int { return &v }

func TestIngest_UpsertAndReplace(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	day := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	dto, err := svc.Ingest(ctx, IngestInput{
		PlanDate: day, Scenarios: []IngestScenario{validScenario("BEAT", "LONG", intPtr(3))},
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if dto.Status != "PUBLISHED" || len(dto.Scenarios) != 1 || *dto.Scenarios[0].Leverage != 3 {
		t.Fatalf("unexpected plan: %+v", dto)
	}

	// Re-ingest replaces ladders and honors EMPTY literally.
	empty := "EMPTY"
	dto, err = svc.Ingest(ctx, IngestInput{
		PlanDate: day, Status: &empty,
		Scenarios: []IngestScenario{validScenario("INLINE", "HOLD", nil)},
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if dto.Status != "EMPTY" || len(dto.Scenarios) != 1 || dto.Scenarios[0].Action != "HOLD" {
		t.Fatalf("expected replaced EMPTY plan, got %+v", dto)
	}
	if len(f.plans) != 1 {
		t.Fatalf("expected one plan row after re-ingest, got %d", len(f.plans))
	}

	// Anything else becomes PUBLISHED.
	other := "DRAFT"
	dto, err = svc.Ingest(ctx, IngestInput{PlanDate: day, Status: &other})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if dto.Status != "PUBLISHED" {
		t.Fatalf("expected PUBLISHED, got %+v", dto)
	}
}

func TestIngest_Validation422(t *testing.T) {
	svc := testService(newFakeQueries())
	ctx := context.Background()
	day := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		scenarios []IngestScenario
	}{
		{"directional without confidence", []IngestScenario{func() IngestScenario {
			s := validScenario("BEAT", "LONG", intPtr(3))
			s.Confidence = nil
			return s
		}()}},
		{"directional without leverage", []IngestScenario{func() IngestScenario {
			s := validScenario("BEAT", "SHORT", nil)
			return s
		}()}},
		{"directional leverage out of range", []IngestScenario{func() IngestScenario {
			s := validScenario("BEAT", "LONG", intPtr(11))
			return s
		}()}},
		{"HOLD with leverage", []IngestScenario{func() IngestScenario {
			s := validScenario("INLINE", "HOLD", intPtr(2))
			return s
		}()}},
		{"surpriseMin above surpriseMax", []IngestScenario{func() IngestScenario {
			s := validScenario("BEAT", "LONG", intPtr(3))
			s.SurpriseMin = decimalPtr("0.9")
			s.SurpriseMax = decimalPtr("0.1")
			return s
		}()}},
		{"unknown action", []IngestScenario{func() IngestScenario {
			s := validScenario("BEAT", "MOON", intPtr(3))
			return s
		}()}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.Ingest(ctx, IngestInput{PlanDate: day, Scenarios: c.scenarios}); err == nil {
				t.Fatal("expected 422, got nil")
			}
		})
	}
}

func TestLatestAndByDate(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	if _, err := svc.Latest(ctx); err == nil {
		t.Fatal("expected 404 with no plans, got nil")
	}
	d1 := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	if _, err := svc.Ingest(ctx, IngestInput{PlanDate: d1}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ingest(ctx, IngestInput{PlanDate: d2}); err != nil {
		t.Fatal(err)
	}
	latest, err := svc.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !latest.PlanDate.Equal(d2) {
		t.Fatalf("expected latest %v, got %v", d2, latest.PlanDate)
	}
	byDate, err := svc.ByDate(ctx, d1)
	if err != nil {
		t.Fatal(err)
	}
	if !byDate.PlanDate.Equal(d1) {
		t.Fatalf("expected %v, got %v", d1, byDate.PlanDate)
	}
	if _, err := svc.ByDate(ctx, d1.AddDate(0, 0, -5)); err == nil {
		t.Fatal("expected 404 for unknown date, got nil")
	}
}
