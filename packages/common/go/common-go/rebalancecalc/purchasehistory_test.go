package rebalancecalc

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestBuildPurchaseHistory_EmptyLotsReturnsNil(t *testing.T) {
	if got := BuildPurchaseHistory(nil, nil, nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestBuildPurchaseHistory_SingleSessionAggregates(t *testing.T) {
	session := uuid.New()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	purchasedAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	lots := []PurchaseLotInput{
		{SessionID: session, PurchasedAt: purchasedAt, CreatedAt: now, Ticker: "B",
			Units: decimal.RequireFromString("2"), PricePln: decimal.RequireFromString("50"), CostPln: decimal.RequireFromString("100")},
		{SessionID: session, PurchasedAt: purchasedAt, CreatedAt: now, Ticker: "A",
			Units: decimal.RequireFromString("10"), PricePln: decimal.RequireFromString("10"), CostPln: decimal.RequireFromString("100")},
	}
	positions := map[string]PositionLookup{
		"A": {Name: "Asset A", YahooTicker: new("A.WA")},
		"B": {Name: "Asset B", YahooTicker: new("B.WA")},
	}
	prices := map[string]decimal.Decimal{"A.WA": decimal.RequireFromString("12"), "B.WA": decimal.RequireFromString("40")}

	got := BuildPurchaseHistory(lots, positions, prices)
	if len(got) != 1 {
		t.Fatalf("expected 1 session, got %d", len(got))
	}
	s := got[0]
	if len(s.Lots) != 2 || s.Lots[0].Ticker != "A" || s.Lots[1].Ticker != "B" {
		t.Fatalf("expected lots ordered by ticker ASC [A, B], got %+v", s.Lots)
	}
	// A: current value 10*12=120, gain=20. B: current value 2*40=80, gain=-20.
	assertDecEq(t, "A current value", *s.Lots[0].CurrentValuePln, "120")
	assertDecEq(t, "B current value", *s.Lots[1].CurrentValuePln, "80")
	assertDecEq(t, "total cost", s.TotalCostPln, "200")
	assertDecEq(t, "total current value", s.TotalCurrentValuePln, "200")
	assertDecEq(t, "total gain", s.TotalGainPln, "0")
}

func TestBuildPurchaseHistory_SessionsOrderedNewestFirst(t *testing.T) {
	older := uuid.New()
	newer := uuid.New()
	lots := []PurchaseLotInput{
		{SessionID: older, PurchasedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), CreatedAt: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC),
			Ticker: "A", Units: decimal.RequireFromString("1"), PricePln: decimal.RequireFromString("1"), CostPln: decimal.RequireFromString("1")},
		{SessionID: newer, PurchasedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), CreatedAt: time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
			Ticker: "A", Units: decimal.RequireFromString("1"), PricePln: decimal.RequireFromString("1"), CostPln: decimal.RequireFromString("1")},
	}
	got := BuildPurchaseHistory(lots, nil, nil)
	if len(got) != 2 || got[0].SessionID != newer || got[1].SessionID != older {
		t.Fatalf("expected newest-purchasedAt-first ordering, got %+v", got)
	}
}

func TestBuildPurchaseHistory_MissingPrice_NoGainFieldsNoZeroSubstitution(t *testing.T) {
	session := uuid.New()
	lots := []PurchaseLotInput{
		{SessionID: session, PurchasedAt: time.Now(), CreatedAt: time.Now(), Ticker: "GONE",
			Units: decimal.RequireFromString("5"), PricePln: decimal.RequireFromString("2"), CostPln: decimal.RequireFromString("10")},
	}
	got := BuildPurchaseHistory(lots, map[string]PositionLookup{"GONE": {Name: "Gone", YahooTicker: new("GONE.WA")}}, nil)
	if len(got) != 1 || len(got[0].Lots) != 1 {
		t.Fatalf("unexpected result shape: %+v", got)
	}
	lot := got[0].Lots[0]
	if lot.PriceAvailable {
		t.Fatalf("expected priceAvailable=false when the price cache has no entry")
	}
	if lot.CurrentValuePln != nil || lot.GainPln != nil || lot.GainPct != nil {
		t.Fatalf("expected nil current-value/gain fields, got %+v", lot)
	}
	// totalCurrentValue must NOT include a zero stand-in for the unavailable lot.
	assertDecEq(t, "totalCurrentValue", got[0].TotalCurrentValuePln, "0")
	assertDecEq(t, "totalCost", got[0].TotalCostPln, "10")
}

func TestPercentOf_NilOnZeroDenominator(t *testing.T) {
	if got := percentOf(decimal.RequireFromString("5"), decimal.Zero); got != nil {
		t.Fatalf("expected nil, got %v", *got)
	}
	got := percentOf(decimal.RequireFromString("10"), decimal.RequireFromString("50"))
	if got == nil {
		t.Fatal("expected a value")
	}
	assertDecEq(t, "percentOf", *got, "20.0000")
}
