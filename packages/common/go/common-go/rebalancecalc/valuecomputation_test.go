package rebalancecalc

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestComputeValues_BasicValuation(t *testing.T) {
	inputs := []PositionInput{
		{Ticker: "A", Name: "Asset A", PriceCurrency: "PLN", UnitsTotal: decimal.RequireFromString("10"), YahooTicker: new("A.WA")},
		{Ticker: "B", Name: "Asset B", PriceCurrency: "EUR", UnitsTotal: decimal.RequireFromString("2"), YahooTicker: new("B.DE")},
	}
	fxRates := map[string]decimal.Decimal{"EUR": decimal.RequireFromString("4")}
	prices := map[string]decimal.Decimal{"A.WA": decimal.RequireFromString("100"), "B.DE": decimal.RequireFromString("50")}

	result := ComputeValues(inputs, fxRates, prices)

	assertDecEq(t, "A value", result.ValuePlnByTicker["A"], "1000")
	assertDecEq(t, "B value", result.ValuePlnByTicker["B"], "100")
	if len(result.Tradable) != 2 {
		t.Fatalf("expected 2 tradable rows, got %d", len(result.Tradable))
	}
}

func TestComputeValues_CompanionFoldsIntoParent(t *testing.T) {
	inputs := []PositionInput{
		{Ticker: "PARENT", Name: "Parent", PriceCurrency: "PLN", UnitsTotal: decimal.RequireFromString("5"), YahooTicker: new("P.WA")},
		{Ticker: "CHILD", Name: "Child", PriceCurrency: "PLN", UnitsTotal: decimal.RequireFromString("3"),
			YahooTicker: new("C.WA"), IsCompanionRow: true, CompanionParentTicker: new("PARENT")},
	}
	prices := map[string]decimal.Decimal{"P.WA": decimal.RequireFromString("10"), "C.WA": decimal.RequireFromString("20")}

	result := ComputeValues(inputs, nil, prices)

	// PARENT: 5*10 = 50, plus CHILD's 3*20=60 folded in => 110. CHILD never appears as its own row.
	assertDecEq(t, "PARENT value", result.ValuePlnByTicker["PARENT"], "110")
	if _, ok := result.ValuePlnByTicker["CHILD"]; ok {
		t.Fatalf("companion row must not appear as its own valuePln entry")
	}
	if len(result.Tradable) != 1 || result.Tradable[0].Ticker != "PARENT" {
		t.Fatalf("expected exactly the parent row in Tradable, got %+v", result.Tradable)
	}
}

func TestComputeValues_MissingPrice_TradableFallsBackToZero_CompanionSkipped(t *testing.T) {
	inputs := []PositionInput{
		{Ticker: "NOPRICE", Name: "No Price", PriceCurrency: "PLN", UnitsTotal: decimal.RequireFromString("10"), YahooTicker: new("MISSING.WA")},
		{Ticker: "PARENT", Name: "Parent", PriceCurrency: "PLN", UnitsTotal: decimal.RequireFromString("1"), YahooTicker: new("P.WA")},
		{Ticker: "CHILD", Name: "Child", PriceCurrency: "PLN", UnitsTotal: decimal.RequireFromString("1"),
			YahooTicker: new("MISSING2.WA"), IsCompanionRow: true, CompanionParentTicker: new("PARENT")},
	}
	prices := map[string]decimal.Decimal{"P.WA": decimal.RequireFromString("100")}

	result := ComputeValues(inputs, nil, prices)

	assertDecEq(t, "NOPRICE value", result.ValuePlnByTicker["NOPRICE"], "0")
	// PARENT gets no contribution from the price-less companion (skipped, not zero-added — same
	// numeric outcome here, but the companion loop must never resolve via the tradable fallback).
	assertDecEq(t, "PARENT value", result.ValuePlnByTicker["PARENT"], "100")
}

func TestComputeValues_ChfUsesRateAsPriceWithOneFallback(t *testing.T) {
	inputs := []PositionInput{
		{Ticker: "CHFCASH", Name: "CHF cash", PriceCurrency: "CHF", UnitsTotal: decimal.RequireFromString("10")},
	}
	// CHF rate present.
	withRate := ComputeValues(inputs, map[string]decimal.Decimal{"CHF": decimal.RequireFromString("4.5")}, nil)
	assertDecEq(t, "CHF with rate", withRate.ValuePlnByTicker["CHFCASH"], "45")

	// CHF rate missing -> defaults to 1 (never "unavailable"), matching Java's
	// fxRates.getOrDefault(CHF, ONE).
	withoutRate := ComputeValues(inputs, nil, nil)
	assertDecEq(t, "CHF without rate", withoutRate.ValuePlnByTicker["CHFCASH"], "10")
}

func TestTotalPortfolioValuePln_SumsAllTickers(t *testing.T) {
	result := ComputationResult{ValuePlnByTicker: map[string]decimal.Decimal{
		"A": decimal.RequireFromString("100"), "B": decimal.RequireFromString("250.5"),
	}}
	assertDecEq(t, "total", TotalPortfolioValuePln(result), "350.5")
}
