package purchasesession

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/position"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

func TestCompleteSession_RejectsAlreadyDoneSession(t *testing.T) {
	fq := newFakeQueries()
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumDONE}
	svc := newTestService(fq)

	_, err := svc.CompleteSession(context.Background(), 1)
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 409 || appErr.Code != httpx.CodeSessionAlreadyDone {
		t.Fatalf("expected 409 SESSION_ALREADY_DONE, got %v", err)
	}
}

func TestCompleteSession_UnknownSession404(t *testing.T) {
	fq := newFakeQueries()
	svc := newTestService(fq)
	_, err := svc.CompleteSession(context.Background(), 999)
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 404 {
		t.Fatalf("expected 404 NOT_FOUND, got %v", err)
	}
}

func TestCompleteSession_AppliesPositionDeltaAndMarksDone(t *testing.T) {
	fq := newFakeQueries()
	posID := fq.addPosition("ETF1", "MAIN", "XTB", "PLN", nil, decimal.NewFromInt(10))
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED}
	fq.items[100] = sqlcgen.PurchaseSessionItem{
		ID: 100, SessionID: 1, Portfolio: "MAIN", Ticker: "ETF1", AccountType: "REGULAR",
		PlannedUnits: pgnum.FromDecimal(decimal.NewFromInt(5)), PlannedValuePln: pgnum.FromDecimal(decimal.NewFromInt(500)),
		PricePlnAtPlan: pgnum.FromDecimal(decimal.NewFromInt(100)),
	}
	svc := newTestService(fq)

	result, err := svc.CompleteSession(context.Background(), 1)
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if result.Status != StatusDONE || result.PurchasesWritten != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.UpdatedPositionIds) != 1 || result.UpdatedPositionIds[0] != posID {
		t.Fatalf("expected position %d in updatedPositionIds, got %v", posID, result.UpdatedPositionIds)
	}
	updated := fq.positions[posID]
	units, _ := pgnum.ToDecimal(updated.UnitsRegularPln)
	if !units.Equal(decimal.NewFromInt(15)) {
		t.Fatalf("expected units_regular_pln=15 (10+5), got %s", units)
	}
	if fq.sessions[1].Status != sqlcgen.SessionStatusEnumDONE {
		t.Fatal("expected session marked DONE")
	}
	if len(fq.deposits) != 1 {
		t.Fatalf("expected 1 auto-deposit, got %d", len(fq.deposits))
	}
	depAmt, _ := pgnum.ToDecimal(fq.deposits[0].Amount)
	if !depAmt.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("expected deposit amount=500, got %s", depAmt)
	}
}

func TestCompleteSession_ZeroEffectiveUnitsSkipped(t *testing.T) {
	fq := newFakeQueries()
	fq.addPosition("ETF1", "MAIN", "XTB", "PLN", nil, decimal.Zero)
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED}
	fq.items[100] = sqlcgen.PurchaseSessionItem{
		ID: 100, SessionID: 1, Portfolio: "MAIN", Ticker: "ETF1", AccountType: "REGULAR",
		PlannedUnits: pgnum.FromDecimal(decimal.Zero), PlannedValuePln: pgnum.FromDecimal(decimal.Zero),
	}
	svc := newTestService(fq)

	result, err := svc.CompleteSession(context.Background(), 1)
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if result.PurchasesWritten != 0 {
		t.Fatalf("expected zero purchases written for a zero-unit item, got %d", result.PurchasesWritten)
	}
}

func TestCompleteSession_MigrationSellNettedAgainstGroup_NoDepositWhenFullyOffset(t *testing.T) {
	// A migration-sell whose proceeds fully offset the group's buys must NOT create a deposit
	// (ADR-035 netting — the fallback path, since no create-session cache entry exists here).
	fq := newFakeQueries()
	fq.addPosition("PL.ETNVIRBTCP", "MAIN", "XTB", "PLN", nil, decimal.NewFromInt(5))
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED}
	// Buy 300 PLN of IKE new money...
	fq.items[100] = sqlcgen.PurchaseSessionItem{
		ID: 100, SessionID: 1, Portfolio: "MAIN", Ticker: "PL.ETNVIRBTCP", AccountType: "IKE",
		PlannedUnits: pgnum.FromDecimal(decimal.NewFromInt(3)), PlannedValuePln: pgnum.FromDecimal(decimal.NewFromInt(300)),
		PricePlnAtPlan: pgnum.FromDecimal(decimal.NewFromInt(100)), IsMigrationSell: false,
	}
	// ...fully offset by a 300 PLN migration-sell in the same (portfolio, IKE, exchange) group.
	fq.items[101] = sqlcgen.PurchaseSessionItem{
		ID: 101, SessionID: 1, Portfolio: "MAIN", Ticker: "PL.ETNVIRBTCP", AccountType: "IKE",
		PlannedUnits: pgnum.FromDecimal(decimal.NewFromInt(3)), PlannedValuePln: pgnum.FromDecimal(decimal.NewFromInt(300)),
		ActualUnits: pgnum.FromDecimal(decimal.NewFromInt(-3)), PricePlnAtPlan: pgnum.FromDecimal(decimal.NewFromInt(100)), IsMigrationSell: true,
	}
	svc := newTestService(fq)

	_, err := svc.CompleteSession(context.Background(), 1)
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if len(fq.deposits) != 0 {
		t.Fatalf("expected no deposit (group fully offset by migration-sell proceeds), got %d: %+v", len(fq.deposits), fq.deposits)
	}
}

func TestCompleteSession_UsesCreateSessionCache_WhenPresent(t *testing.T) {
	fq := newFakeQueries()
	fq.addPosition("ETF1", "MAIN", "XTB", "PLN", nil, decimal.Zero)
	fq.sessions[1] = sqlcgen.PurchaseSession{ID: 1, Status: sqlcgen.SessionStatusEnumPLANNED}
	fq.items[100] = sqlcgen.PurchaseSessionItem{
		ID: 100, SessionID: 1, Portfolio: "MAIN", Ticker: "ETF1", AccountType: "IKE",
		PlannedUnits: pgnum.FromDecimal(decimal.NewFromInt(1)), PlannedValuePln: pgnum.FromDecimal(decimal.NewFromInt(100)),
		PricePlnAtPlan: pgnum.FromDecimal(decimal.NewFromInt(100)),
	}
	svc := newTestService(fq)
	// Simulate createSession having cached an authoritative new-deposit figure of 100 for
	// (MAIN, IKE) — the direct-tracked path (ADR-065 §5), which the completion loop above must
	// then SKIP re-summing (directTrackedGroup continue branch).
	svc.cachePut(1, map[groupKey]decimal.Decimal{{portfolio: "MAIN", accountType: "IKE"}: decimal.NewFromInt(100)})

	_, err := svc.CompleteSession(context.Background(), 1)
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if len(fq.deposits) != 1 {
		t.Fatalf("expected exactly 1 deposit from the cached bucket fold-in, got %d", len(fq.deposits))
	}
	amt, _ := pgnum.ToDecimal(fq.deposits[0].Amount)
	if !amt.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("expected deposit amount=100 from the cache, got %s", amt)
	}
	// Cache must be single-use (removed after completion).
	if _, ok := svc.cacheTake(1); ok {
		t.Fatal("expected cache entry consumed by completion")
	}
}

func TestClassifyTradeType(t *testing.T) {
	cases := []struct {
		item sqlcgen.PurchaseSessionItem
		want sqlcgen.TradeTypeEnum
	}{
		{sqlcgen.PurchaseSessionItem{IsMigrationSell: true}, sqlcgen.TradeTypeEnumMIGRATIONSELL},
		{sqlcgen.PurchaseSessionItem{IsMigrationBuy: true}, sqlcgen.TradeTypeEnumMIGRATIONBUY},
		{sqlcgen.PurchaseSessionItem{}, sqlcgen.TradeTypeEnumDCABUY},
	}
	for _, tc := range cases {
		if got := classifyTradeType(tc.item); got != tc.want {
			t.Fatalf("classifyTradeType(%+v) = %v, want %v", tc.item, got, tc.want)
		}
	}
}

// planSessionCosts keeps the pre-netting conventions of the session path (buys add the ABSOLUTE
// value paid and only when the item carries a value) and nets the session per asset through the
// kernel; the real-Postgres cost tests cover the same rules end to end.
func TestPlanSessionCosts(t *testing.T) {
	d := decimal.RequireFromString
	price := d("100")
	prices := map[string]decimal.Decimal{"HEAD.YAHOO": price}
	newMember := func(id int64, units, cost string) *position.CostMember {
		u := &position.PositionUnits{Exchange: position.ExchangeXTB, PriceCurrency: position.CurrencyPLN, UnitsRegularPln: d(units)}
		if cost != "" {
			c := d(cost)
			u.TotalSpentPln = &c
		}
		yahoo := "HEAD.YAHOO"
		return &position.CostMember{ID: id, AssetID: 1, YahooTicker: &yahoo, Units: u}
	}
	trade := func(m *position.CostMember, units, value string, hasValue bool) sessionTrade {
		return sessionTrade{pos: &positionRow{id: m.ID, assetID: 1, units: *m.Units}, units: d(units), pricePln: price, value: d(value), hasValue: hasValue}
	}
	tests := []struct {
		name   string
		trades func(m *position.CostMember) []sessionTrade
		want   string
	}{
		{"buy adds the value paid", func(m *position.CostMember) []sessionTrade { return []sessionTrade{trade(m, "5", "500", true)} }, "1500"},
		{"buy adds the absolute value even when passed negative", func(m *position.CostMember) []sessionTrade { return []sessionTrade{trade(m, "5", "-500", true)} }, "1500"},
		{"buy without a value leaves the cost alone", func(m *position.CostMember) []sessionTrade { return []sessionTrade{trade(m, "5", "0", false)} }, "1000"},
		{"sale scales the cost by the share of value sold", func(m *position.CostMember) []sessionTrade { return []sessionTrade{trade(m, "-4", "400", true)} }, "600"},
		{"sale without a value still scales at the execution price", func(m *position.CostMember) []sessionTrade { return []sessionTrade{trade(m, "-4", "0", false)} }, "600"},
		{"a sale and a buy of the same asset are a swap: cost unchanged", func(m *position.CostMember) []sessionTrade {
			return []sessionTrade{trade(m, "-4", "400", true), trade(m, "4", "400", true)}
		}, "1000"},
		{"buy excess over the sale enters at market", func(m *position.CostMember) []sessionTrade {
			return []sessionTrade{trade(m, "-4", "400", true), trade(m, "6", "600", true)}
		}, "1200"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newMember(1, "10", "1000")
			planSessionCosts(tc.trades(m), map[int64][]*position.CostMember{1: {m}}, prices)
			if got := m.Units.TotalSpentPln; got == nil || !got.Equal(d(tc.want)) {
				t.Fatalf("cost = %v, want %s", got, tc.want)
			}
		})
	}
}
