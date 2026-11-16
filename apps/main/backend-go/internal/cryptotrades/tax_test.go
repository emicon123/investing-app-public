package cryptotrades

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Pure-function tests for TaxSummary — no database involved (Phase 4
// design-improvement note: trivially unit-testable without a DB).

func input(ticker, side string, amount string) TradeInput {
	return TradeInput{Ticker: ticker, Side: side, AmountPln: decimal.RequireFromString(amount)}
}

func TestTaxSummary_GroupsSortsAndTaxes(t *testing.T) {
	trades := []TradeInput{
		input("SOL-USD", "BUY", "4000"),
		input("BTC-USD", "BUY", "10000"),
		input("BTC-USD", "SELL", "16000"),
		input("BTC-USD", "BUY", "2000"),
		input("ETH-USD", "SELL", "500"),
	}
	s := TaxSummary(2025, trades)

	if s.Year != 2025 {
		t.Fatalf("expected year 2025, got %d", s.Year)
	}
	// Totals: buys 10000+2000+4000=16000, sells 16000+500=16500, net 500, belka 95.
	if !s.TotalBuysPln.Equal(decimal.NewFromInt(16000)) {
		t.Fatalf("totalBuysPln: got %s", s.TotalBuysPln)
	}
	if !s.TotalSellsPln.Equal(decimal.NewFromInt(16500)) {
		t.Fatalf("totalSellsPln: got %s", s.TotalSellsPln)
	}
	if !s.NetGainPln.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("netGainPln: got %s", s.NetGainPln)
	}
	if !s.EstimatedBelkaTaxPln.Equal(decimal.NewFromInt(95)) {
		t.Fatalf("belka: got %s", s.EstimatedBelkaTaxPln)
	}
	// Tickers sorted ascending.
	if len(s.ByTicker) != 3 || s.ByTicker[0].Ticker != "BTC-USD" ||
		s.ByTicker[1].Ticker != "ETH-USD" || s.ByTicker[2].Ticker != "SOL-USD" {
		t.Fatalf("expected 3 tickers sorted, got %+v", s.ByTicker)
	}
	btc := s.ByTicker[0]
	if !btc.TotalBuysPln.Equal(decimal.NewFromInt(12000)) ||
		!btc.TotalSellsPln.Equal(decimal.NewFromInt(16000)) ||
		!btc.NetGainPln.Equal(decimal.NewFromInt(4000)) ||
		!btc.EstimatedBelkaTaxPln.Equal(decimal.NewFromInt(760)) {
		t.Fatalf("BTC row wrong: %+v", btc)
	}
	// Sells-only ticker: negative buys(0), gain = sells.
	eth := s.ByTicker[1]
	if !eth.TotalBuysPln.IsZero() || !eth.NetGainPln.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("ETH row wrong: %+v", eth)
	}
	// Buys-only ticker: loss, zero belka.
	sol := s.ByTicker[2]
	if !sol.TotalSellsPln.IsZero() || !sol.NetGainPln.Equal(decimal.NewFromInt(-4000)) {
		t.Fatalf("SOL row wrong: %+v", sol)
	}
	if !sol.EstimatedBelkaTaxPln.IsZero() {
		t.Fatalf("loss must yield zero belka, got %s", sol.EstimatedBelkaTaxPln)
	}
}

func TestTaxSummary_EmptyYearIsZero(t *testing.T) {
	s := TaxSummary(2025, nil)
	if !s.TotalBuysPln.IsZero() || !s.TotalSellsPln.IsZero() ||
		!s.NetGainPln.IsZero() || !s.EstimatedBelkaTaxPln.IsZero() {
		t.Fatalf("empty year must be all-zero: %+v", s)
	}
	if len(s.ByTicker) != 0 {
		t.Fatalf("empty year must have no tickers: %+v", s.ByTicker)
	}
}

func TestTaxSummary_BelkaHalfUpScale4(t *testing.T) {
	// 0.0001 * 0.19 = 0.000019 -> 0.0000 at scale 4 HALF_UP.
	s := TaxSummary(2025, []TradeInput{input("BTC-USD", "SELL", "0.0001")})
	if got := s.EstimatedBelkaTaxPln.InexactFloat64(); got != 0 {
		t.Fatalf("expected 0.0000 belka, got %v", got)
	}
	// 0.0003 * 0.19 = 0.000057 -> 0.0001 at scale 4 HALF_UP.
	s = TaxSummary(2025, []TradeInput{input("BTC-USD", "SELL", "0.0003")})
	if got := s.EstimatedBelkaTaxPln.StringFixed(4); got != "0.0001" {
		t.Fatalf("expected 0.0001 belka, got %s", got)
	}
	// Exact-scale case from the spec example shape: 100.005 gain -> 19.00095 -> 19.0010.
	s = TaxSummary(2025, []TradeInput{input("BTC-USD", "SELL", "100.005")})
	if got := s.EstimatedBelkaTaxPln.StringFixed(4); got != "19.0010" {
		t.Fatalf("expected 19.0010 belka, got %s", got)
	}
}
