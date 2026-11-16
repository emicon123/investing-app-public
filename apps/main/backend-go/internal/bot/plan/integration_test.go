package plan

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestPlan_RealPostgresRoundTrip exercises the plan ingest/read paths against a
// real postgres:17.10-alpine container, migrated by the same Flyway CLI image used
// in prod — the "never mock the database" gate. Covers: ingest with ladder
// validation persisted (NUMERIC 18,4/3,2 surviving exactly), re-ingest replacing
// ladders atomically, latest/byDate/list reads, and unknown-calendar FK degrading
// to null.
func TestPlan_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE bot_plan_scenario, bot_daily_plan RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	svc := NewService(queries, func(ctx context.Context, fn func(Queries) error) error {
		return fn(queries)
	})
	day := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	lev := 5

	t.Run("ingest persists ladders with exact numerics", func(t *testing.T) {
		dto, err := svc.Ingest(ctx, IngestInput{
			PlanDate: day,
			Scenarios: []IngestScenario{{
				EventName:     "CPI m/m",
				EventTime:     time.Date(2026, 9, 10, 12, 30, 0, 0, time.UTC),
				ScenarioLabel: "BEAT", TriggerCondition: "actual > forecast",
				Action: "LONG", Leverage: &lev,
				SurpriseMin: decimalPtr("0.0500"), SurpriseMax: decimalPtr("0.2000"),
				Confidence: decimalPtr("0.60"), Reasoning: "because",
			}},
		})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if dto.Status != "PUBLISHED" || len(dto.Scenarios) != 1 {
			t.Fatalf("unexpected plan: %+v", dto)
		}
		sc := dto.Scenarios[0]
		if sc.SurpriseMin == nil || sc.SurpriseMin.String() != "0.05" ||
			sc.Confidence == nil || sc.Confidence.String() != "0.6" {
			t.Fatalf("numerics did not survive: %+v", sc)
		}
		var market string
		if err := pool.QueryRow(ctx, `SELECT market_context::text FROM bot_daily_plan`).Scan(&market); err != nil {
			t.Fatal(err)
		}
		if market != "{}" {
			t.Fatalf("expected null marketContext stored as {}, got %s", market)
		}
	})

	t.Run("re-ingest replaces ladders and unknown calendar degrades to null", func(t *testing.T) {
		bogus := int64(987654)
		dto, err := svc.Ingest(ctx, IngestInput{
			PlanDate: day,
			Scenarios: []IngestScenario{{
				CalendarEventID: &bogus,
				EventName:       "NFP",
				EventTime:       time.Date(2026, 9, 10, 12, 30, 0, 0, time.UTC),
				ScenarioLabel:   "INLINE", TriggerCondition: "actual ~= forecast",
				Action: "HOLD", Reasoning: "wait",
			}},
		})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if len(dto.Scenarios) != 1 || dto.Scenarios[0].CalendarEventID != nil {
			t.Fatalf("expected replaced ladder with nulled FK, got %+v", dto.Scenarios)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM bot_plan_scenario`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected atomic replace (1 scenario), got %d", count)
		}
	})

	t.Run("latest byDate list read back", func(t *testing.T) {
		latest, err := svc.Latest(ctx)
		if err != nil {
			t.Fatalf("Latest: %v", err)
		}
		if !latest.PlanDate.Equal(day) {
			t.Fatalf("unexpected latest: %+v", latest.PlanDate)
		}
		byDate, err := svc.ByDate(ctx, day)
		if err != nil {
			t.Fatalf("ByDate: %v", err)
		}
		if len(byDate.Scenarios) != 1 {
			t.Fatalf("unexpected scenarios: %+v", byDate)
		}
		listed, err := svc.List(ctx, 0, 20)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if listed.Total != 1 {
			t.Fatalf("expected total 1, got %+v", listed)
		}
	})

	t.Run("directional ladder without leverage is 422 and writes nothing", func(t *testing.T) {
		bad := IngestScenario{
			EventName:     "CPI m/m",
			EventTime:     time.Date(2026, 9, 11, 12, 30, 0, 0, time.UTC),
			ScenarioLabel: "BEAT", TriggerCondition: "x", Action: "LONG",
			Confidence: decimalPtr("0.5"), Reasoning: "y",
		}
		if _, err := svc.Ingest(ctx, IngestInput{
			PlanDate:  time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
			Scenarios: []IngestScenario{bad},
		}); err == nil {
			t.Fatal("expected 422, got nil")
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM bot_daily_plan`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("rejected ingest must write nothing, plans=%d", count)
		}
	})
}
