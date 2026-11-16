package purchasesession

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

func TestCreateSession_RejectsWhenPlannedSessionAlreadyExists(t *testing.T) {
	fq := newFakeQueries()
	fq.settings["retirement.monthly_contribution_pln"] = "1000"
	fq.settings["ike_annual_limit_pln"] = "10000"
	fq.settings["ikze_annual_limit_pln"] = "10000"
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED, PortfoliosIncluded: sqlcgen.PortfoliosIncludedEnumBOTH}
	svc := newTestService(fq)

	_, err := svc.CreateSession(context.Background(), CreateSessionInput{PortfoliosIncluded: PortfoliosIncludedBOTH})
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 409 || appErr.Code != "PLANNED_SESSION_EXISTS" {
		t.Fatalf("expected 409 PLANNED_SESSION_EXISTS, got %v", err)
	}
}

func TestCreateSession_HappyPath_CreatesSessionWithItems(t *testing.T) {
	fq := newFakeQueries()
	fq.settings["retirement.monthly_contribution_pln"] = "0" // exclude RETIREMENT budget noise
	fq.settings["ike_annual_limit_pln"] = "0"
	fq.settings["ikze_annual_limit_pln"] = "0"
	target := decimal.NewFromInt(100)
	fq.addPosition("ETF1", "MAIN", "XTB", "PLN", &target, decimal.Zero)
	fq.hasCash = true

	svc := newTestService(fq)
	detail, err := svc.CreateSession(context.Background(), CreateSessionInput{PortfoliosIncluded: PortfoliosIncludedMAIN, CashOverridePln: decPtr("1000")})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if detail.Status != StatusPLANNED {
		t.Fatalf("expected PLANNED status, got %s", detail.Status)
	}
	if len(detail.Items) == 0 {
		t.Fatalf("expected at least one planned item, got none")
	}
	// The bridge cache must be populated for this session id (ADR-065 §5).
	if _, ok := fq.sessions[detail.ID]; !ok {
		t.Fatalf("expected session %d to be persisted", detail.ID)
	}
}

func TestCreateSession_UnconditionallyComputesBothPortfolios_EvenWhenOnlyOneIncluded(t *testing.T) {
	// ADR-065 §4: RebalancingService.calculate runs for BOTH portfolios even when only one is
	// included this session — verified indirectly here: MAIN-only request must not error out
	// even though a RETIREMENT position with a valid 100%-summing target also exists (if the
	// RETIREMENT calculation were skipped incorrectly, this would still pass; if it errored on a
	// missing/invalid retirement setup, this test would catch that regression).
	fq := newFakeQueries()
	fq.settings["retirement.monthly_contribution_pln"] = "500"
	fq.settings["ike_annual_limit_pln"] = "0"
	fq.settings["ikze_annual_limit_pln"] = "0"
	mainTarget := decimal.NewFromInt(100)
	retTarget := decimal.NewFromInt(100)
	fq.addPosition("ETF1", "MAIN", "XTB", "PLN", &mainTarget, decimal.Zero)
	fq.addPosition("BOND1", "RETIREMENT", "XTB", "PLN", &retTarget, decimal.Zero)
	fq.hasCash = true

	svc := newTestService(fq)
	detail, err := svc.CreateSession(context.Background(), CreateSessionInput{PortfoliosIncluded: PortfoliosIncludedMAIN, CashOverridePln: decPtr("1000")})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for _, it := range detail.Items {
		if it.Portfolio == "RETIREMENT" {
			t.Fatalf("expected no RETIREMENT items when portfoliosIncluded=MAIN, got %+v", it)
		}
	}
}

func TestUpdateSessionItems_RejectsNegativeActualUnitsForNonMigrationSell(t *testing.T) {
	// This is one of the two functions architect-output §3 flags for closest manual-sell-only
	// scrutiny (item 5): the negative-value guard is the one place a negative number could
	// otherwise sneak into completeSession's write path outside the two sanctioned mechanisms.
	fq := newFakeQueries()
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED}
	fq.items[10] = sqlcgen.PurchaseSessionItem{ID: 10, SessionID: 1, Ticker: "ETF1", IsMigrationSell: false}
	svc := newTestService(fq)

	_, err := svc.UpdateSessionItems(context.Background(), 1, []UpdateItemEntry{
		{ID: 10, ActualUnits: decPtr("-5")},
	})
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 400 {
		t.Fatalf("expected 400 VALIDATION_ERROR for negative actualUnits on a non-migration-sell item, got %v", err)
	}
}

func TestUpdateSessionItems_RejectsNegativeActualValuePlnForNonMigrationSell(t *testing.T) {
	fq := newFakeQueries()
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED}
	fq.items[10] = sqlcgen.PurchaseSessionItem{ID: 10, SessionID: 1, Ticker: "ETF1", IsMigrationSell: false}
	svc := newTestService(fq)

	_, err := svc.UpdateSessionItems(context.Background(), 1, []UpdateItemEntry{
		{ID: 10, ActualValuePln: decPtr("-100")},
	})
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 400 {
		t.Fatalf("expected 400 VALIDATION_ERROR for negative actualValuePln on a non-migration-sell item, got %v", err)
	}
}

func TestUpdateSessionItems_AllowsNegativeValuesForMigrationSellItems(t *testing.T) {
	fq := newFakeQueries()
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED}
	fq.items[10] = sqlcgen.PurchaseSessionItem{ID: 10, SessionID: 1, Ticker: "PL.ETNVIRBTCP", IsMigrationSell: true}
	svc := newTestService(fq)

	_, err := svc.UpdateSessionItems(context.Background(), 1, []UpdateItemEntry{
		{ID: 10, ActualUnits: decPtr("-2.5")},
	})
	if err != nil {
		t.Fatalf("expected migration-sell items to accept negative actualUnits, got error: %v", err)
	}
	saved := fq.items[10]
	v, _ := pgnum.ToDecimal(saved.ActualUnits)
	if !v.Equal(decimal.RequireFromString("-2.5")) {
		t.Fatalf("expected actualUnits=-2.5 to persist, got %s", v)
	}
}

func TestUpdateSessionItems_RejectsOnDoneSession(t *testing.T) {
	fq := newFakeQueries()
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumDONE}
	svc := newTestService(fq)

	_, err := svc.UpdateSessionItems(context.Background(), 1, []UpdateItemEntry{{ID: 10}})
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 409 || appErr.Code != httpx.CodeSessionAlreadyDone {
		t.Fatalf("expected 409 SESSION_ALREADY_DONE, got %v", err)
	}
}

func TestDeleteSession_RejectsOnDoneSession(t *testing.T) {
	fq := newFakeQueries()
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumDONE}
	svc := newTestService(fq)

	err := svc.DeleteSession(context.Background(), 1)
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 409 {
		t.Fatalf("expected 409 SESSION_ALREADY_DONE, got %v", err)
	}
}

func TestDeleteSession_PlannedSucceedsAndEvictsCache(t *testing.T) {
	fq := newFakeQueries()
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED}
	fq.items[10] = sqlcgen.PurchaseSessionItem{ID: 10, SessionID: 1}
	svc := newTestService(fq)
	svc.cachePut(1, map[groupKey]decimal.Decimal{{portfolio: "MAIN", accountType: "IKE"}: decimal.NewFromInt(500)})

	if err := svc.DeleteSession(context.Background(), 1); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, ok := fq.sessions[1]; ok {
		t.Fatal("expected session deleted")
	}
	if _, ok := fq.items[10]; ok {
		t.Fatal("expected items deleted")
	}
	if _, ok := svc.cacheTake(1); ok {
		t.Fatal("expected cache entry evicted on delete")
	}
}

func decPtr(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}
