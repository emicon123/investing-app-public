package purchasesession

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// TestToOpenAPISummary_MapsBudgetsTotalsAndLimits guards the wire parity the Phase 7
// coexistence smoke demanded: the session-history table reads retirementBudgetPln,
// mainBudgetPln, totalActualPln and itemCount from the LIST endpoint, and the service layer
// already computes all of them — the handler must not drop them (docs/openapi.yaml carries
// explicit slots since the Phase 7 spec amendment).
func TestToOpenAPISummary_MapsBudgetsTotalsAndLimits(t *testing.T) {
	total := decimal.NewFromInt(2373)
	s := SessionSummary{
		ID: 94, Status: StatusDONE, PlannedAt: time.Date(2026, 8, 27, 8, 37, 21, 0, time.UTC),
		PortfoliosIncluded:  PortfoliosIncludedMAIN,
		RetirementBudgetPln: decimal.Zero, MainBudgetPln: decimal.NewFromInt(2372),
		TotalActualPln: &total, ItemCount: 19,
	}
	out := toOpenAPISummary(s)
	if out.PlannedAt == nil || out.PlannedAt.Year() != 2026 {
		t.Fatalf("expected plannedAt mapped, got %+v", out.PlannedAt)
	}
	if out.RetirementBudgetPln == nil || *out.RetirementBudgetPln != 0 {
		t.Fatalf("expected retirementBudgetPln=0, got %+v", out.RetirementBudgetPln)
	}
	if out.MainBudgetPln == nil || *out.MainBudgetPln != 2372 {
		t.Fatalf("expected mainBudgetPln=2372, got %+v", out.MainBudgetPln)
	}
	if out.TotalActualPln == nil || *out.TotalActualPln != 2373 {
		t.Fatalf("expected totalActualPln=2373, got %+v", out.TotalActualPln)
	}
	if out.ItemCount == nil || *out.ItemCount != 19 {
		t.Fatalf("expected itemCount=19, got %+v", out.ItemCount)
	}
	if out.Items != nil || out.CurrencyBreakdown != nil {
		t.Fatal("summary must not populate items/currencyBreakdown (never loaded)")
	}
}

// TestToOpenAPISession_MapsDetailFields guards the ACTIVE-session view: budgets, plannedAt,
// account limits, item priceCurrency/isMigrationBuy and the breakdown PLN legs/rates must all
// reach the wire (each is read by frontend-web/src/routes/purchases/).
func TestToOpenAPISession_MapsDetailFields(t *testing.T) {
	planned := decimal.NewFromInt(100)
	d := SessionDetail{
		ID: 1, Status: StatusPLANNED, PlannedAt: time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC),
		PortfoliosIncluded:  PortfoliosIncludedBOTH,
		RetirementBudgetPln: decimal.NewFromInt(1000), MainBudgetPln: decimal.NewFromInt(2000),
		RetirementLimitsRemaining: AccountLimits{IkeUsedPln: decimal.NewFromInt(100), IkeRemainingPln: decimal.NewFromInt(900), IkeAnnualLimitPln: decimal.NewFromInt(1000)},
		Items: []*SessionItem{
			{ID: 7, Ticker: "ETF1", Portfolio: "MAIN", AccountType: "IKE", PlannedUnits: &planned, IsMigrationBuy: true, PriceCurrency: "PLN"},
		},
		CurrencyBreakdown: CurrencyBreakdown{XtbPln: decimal.NewFromInt(100), XtbUsdPln: decimal.NewFromInt(50), UsdPlnRate: decimal.RequireFromString("4.0")},
	}
	out := toOpenAPISession(d)
	if out.PlannedAt == nil {
		t.Fatal("expected plannedAt mapped")
	}
	if out.RetirementBudgetPln == nil || *out.RetirementBudgetPln != 1000 {
		t.Fatalf("expected retirementBudgetPln=1000, got %+v", out.RetirementBudgetPln)
	}
	if out.MainBudgetPln == nil || *out.MainBudgetPln != 2000 {
		t.Fatalf("expected mainBudgetPln=2000, got %+v", out.MainBudgetPln)
	}
	if out.RetirementLimitsRemaining == nil || out.RetirementLimitsRemaining.IkeRemainingPln == nil ||
		*out.RetirementLimitsRemaining.IkeRemainingPln != 900 {
		t.Fatalf("expected ikeRemainingPln=900, got %+v", out.RetirementLimitsRemaining)
	}
	if out.MainLimitsRemaining == nil {
		t.Fatal("expected mainLimitsRemaining mapped (zero-valued, never nil)")
	}
	items := *out.Items
	if items[0].IsMigrationBuy == nil || !*items[0].IsMigrationBuy {
		t.Fatalf("expected isMigrationBuy=true, got %+v", items[0].IsMigrationBuy)
	}
	if items[0].PriceCurrency == nil || string(*items[0].PriceCurrency) != "PLN" {
		t.Fatalf("expected priceCurrency=PLN, got %+v", items[0].PriceCurrency)
	}
	if out.CurrencyBreakdown == nil || out.CurrencyBreakdown.XtbUsdPln == nil || *out.CurrencyBreakdown.XtbUsdPln != 50 {
		t.Fatalf("expected xtbUsdPln=50, got %+v", out.CurrencyBreakdown)
	}
	if out.CurrencyBreakdown.UsdPlnRate == nil || *out.CurrencyBreakdown.UsdPlnRate != 4.0 {
		t.Fatalf("expected usdPlnRate=4.0, got %+v", out.CurrencyBreakdown.UsdPlnRate)
	}
}
