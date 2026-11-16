package plan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/openapi"
)

// stubServer exposes only the plan routes through the real oapi-codegen chi
// wiring, so routing-shape quirks are tested against the same router production
// uses rather than asserted by reasoning.
type stubServer struct {
	openapi.Unimplemented
	plans *Handler
}

func (s *stubServer) ListBotPlans(w http.ResponseWriter, r *http.Request, params openapi.ListBotPlansParams) {
	s.plans.ListBotPlans(w, r, params)
}

func (s *stubServer) GetLatestBotPlan(w http.ResponseWriter, r *http.Request) {
	s.plans.GetLatestBotPlan(w, r)
}

func (s *stubServer) GetBotPlanByDate(w http.ResponseWriter, r *http.Request, date types.Date) {
	s.plans.GetBotPlanByDate(w, r, date)
}

func (s *stubServer) IngestBotPlan(w http.ResponseWriter, r *http.Request) {
	s.plans.IngestBotPlan(w, r)
}

// TestPlansLatestRouteBeatsDateParam pins the binding quirk: GET /bot/plans/latest
// must reach GetLatestBotPlan (static route wins in chi), never GetBotPlanByDate
// with date="latest" (which would fail date binding). Regression guard for the
// oapi-codegen route registration order.
func TestPlansLatestRouteBeatsDateParam(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	day := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	if _, err := svc.Ingest(ctx, IngestInput{PlanDate: day}); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	openapi.HandlerWithOptions(&stubServer{plans: NewHandler(svc)}, openapi.ChiServerOptions{
		BaseURL:    "/api",
		BaseRouter: router,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/bot/plans/latest", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected latest to route to GetLatestBotPlan (200), got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/bot/plans/2026-09-10", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected date route to reach GetBotPlanByDate (200), got %d: %s", rec.Code, rec.Body.String())
	}
}
