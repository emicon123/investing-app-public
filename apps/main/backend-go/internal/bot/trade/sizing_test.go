package trade

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestSize_DefaultsSanity(t *testing.T) {
	cfg := DefaultExecutionConfig()
	// Defaults: 1% base risk, confidence 1.0 → 1.5x multiplier clamped to 3% max.
	sizing, ok := Size(cfg,
		decimal.RequireFromString("10000"), decimal.RequireFromString("1.0"), true,
		10, decimal.RequireFromString("60000"))
	if !ok {
		t.Fatal("expected sizing to pass at defaults")
	}
	// margin = 10000 × 0.01 × 1.5 = 150; quantity = 150/60000 = 0.0025.
	if sizing.MarginUsdt.String() != "150" {
		t.Fatalf("expected margin 150, got %s", sizing.MarginUsdt)
	}
	if sizing.QuantityBtc.String() != "0.0025" {
		t.Fatalf("expected quantity 0.0025, got %s", sizing.QuantityBtc)
	}
	if sizing.AppliedLeverage != 10 {
		t.Fatalf("expected leverage 10 (10×1%% = 10%% ≤ 30%% tripwire never binds at defaults), got %d",
			sizing.AppliedLeverage)
	}
}

func TestSize_LeverageLossCapTripwire(t *testing.T) {
	cfg := DefaultExecutionConfig()
	cfg.StopDistancePct = decimal.RequireFromString("0.05") // 5% stop: 10×5% = 50% > 30%
	sizing, ok := Size(cfg,
		decimal.RequireFromString("10000"), decimal.RequireFromString("0.5"), true,
		10, decimal.RequireFromString("60000"))
	if !ok {
		t.Fatal("expected leverage reduction, not skip")
	}
	// 6×5% = 30% ≤ 30% — reduce-leverage-first floors at 6, never widens the stop.
	if sizing.AppliedLeverage != 6 {
		t.Fatalf("expected leverage reduced to 6, got %d", sizing.AppliedLeverage)
	}
	cfg.LeverageLossCapPct = decimal.RequireFromString("0.01") // 1%: even 1×5% violates
	if _, ok := Size(cfg,
		decimal.RequireFromString("10000"), decimal.RequireFromString("0.5"), true,
		10, decimal.RequireFromString("60000")); ok {
		t.Fatal("expected skip when even 1x violates the cap")
	}
}

func TestSize_ConfidenceScalingAndClamps(t *testing.T) {
	cfg := DefaultExecutionConfig()
	equity := decimal.RequireFromString("10000")
	entry := decimal.RequireFromString("60000")
	// confidence 0.5 → 1.0x → margin 100.
	sizing, ok := Size(cfg, equity, decimal.RequireFromString("0.5"), true, 2, entry)
	if !ok || sizing.MarginUsdt.String() != "100" {
		t.Fatalf("expected margin 100, got %+v/%v", sizing, ok)
	}
	// confidence 0.3 → 0.6x → margin 60.
	sizing, ok = Size(cfg, equity, decimal.RequireFromString("0.3"), true, 2, entry)
	if !ok || sizing.MarginUsdt.String() != "60" {
		t.Fatalf("expected margin 60, got %+v/%v", sizing, ok)
	}
	// Unset confidence pivots to 0.5 → 1.0x → margin 100.
	sizing, ok = Size(cfg, equity, decimal.Zero, false, 2, entry)
	if !ok || sizing.MarginUsdt.String() != "100" {
		t.Fatalf("expected pivot margin 100, got %+v/%v", sizing, ok)
	}
	// Zero/negative equity or price vetoes.
	if _, ok := Size(cfg, decimal.Zero, decimal.RequireFromString("0.5"), true, 2, entry); ok {
		t.Fatal("expected veto on zero equity")
	}
	if _, ok := Size(cfg, equity, decimal.RequireFromString("0.5"), true, 0, entry); ok {
		t.Fatal("expected veto on zero leverage")
	}
}

func TestContractsFor_CoinMarginedLots(t *testing.T) {
	// qty 0.0025 BTC × 60000 / ctVal 100 = 1.5 contracts.
	got := contractsFor(
		decimal.RequireFromString("0.0025"),
		decimal.RequireFromString("100"),
		decimal.RequireFromString("60000"))
	if got != "1.5" {
		t.Fatalf("expected 1.5, got %q", got)
	}
	// Sub-lot notionals skip (never rounded up): 0.0001×60000/100 = 0.06 < 0.1.
	if got := contractsFor(
		decimal.RequireFromString("0.0001"),
		decimal.RequireFromString("100"),
		decimal.RequireFromString("60000")); got != "" {
		t.Fatalf("expected skip, got %q", got)
	}
	// Truncation, not rounding: 0.00259×60000/100 = 1.554 → 1.5.
	if got := contractsFor(
		decimal.RequireFromString("0.00259"),
		decimal.RequireFromString("100"),
		decimal.RequireFromString("60000")); got != "1.5" {
		t.Fatalf("expected 1.5, got %q", got)
	}
}

func TestParseEventValue_Table(t *testing.T) {
	cases := []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{"213K", "213", true},
		{"0.3%", "0.3", true},
		{"-0.2%", "-0.2", true},
		{"1,234", "1234", true},
		{"+5.5M", "5.5", true},
		{"$1.2M", "1.2", true}, // one leading $ stripped, then one suffix — still parses
		{"$1.2B", "1.2", true},
		{"2.5b", "2.5", true},
		{"  3.0 % ", "3", true}, // shopspring trims the scale; the numeric value is what matters
		{"-", "", false},
		{"n/a", "", false},
		{"N/A", "", false},
		{"", "", false},
		{"TBD", "", false},
		{"50.2 (rev)", "", false},
	}
	for _, c := range cases {
		got, ok := ParseEventValue(c.raw)
		if ok != c.wantOK || (ok && got.String() != c.want) {
			t.Fatalf("ParseEventValue(%q) = %v,%v, want %s,%v", c.raw, got, ok, c.want, c.wantOK)
		}
	}
}

func TestMatchSurprise_LadderOrderAndTieBreak(t *testing.T) {
	ladder := []LadderRung{
		{scenarioID: 1, label: "MISS", action: "SHORT", min: decimalPtr("-100"), max: decimalPtr("-100")},
		{scenarioID: 2, label: "EXTREME_MISS", action: "SHORT", max: decimalPtr("-100")},
		{scenarioID: 3, label: "INLINE", action: "HOLD"},
	}
	// Shared boundary −100 resolves to the MORE EXTREME rung (ladder order, not input order).
	match := MatchSurprise(decimal.RequireFromString("-100"), ladder)
	if match == nil || match.label != "EXTREME_MISS" {
		t.Fatalf("expected EXTREME_MISS tie-break, got %+v", match)
	}
	// Open bounds match everything on their side.
	if match := MatchSurprise(decimal.RequireFromString("5"), ladder); match == nil || match.label != "INLINE" {
		t.Fatalf("expected INLINE catch-all, got %+v", match)
	}
	// Empty ladder matches nothing → HOLD path downstream.
	if match := MatchSurprise(decimal.RequireFromString("5"), nil); match != nil {
		t.Fatalf("expected no match, got %+v", match)
	}
}

func TestNetCluster_SignedSumAndCaps(t *testing.T) {
	conf := decimal.RequireFromString("0.6")
	// LONG x5 + SHORT x3 → net LONG x2 (signed sum), confidence = mean of contributors.
	net := NetCluster([]ClusterLeg{
		{SignedLeverage: 5, Confidence: &conf},
		{SignedLeverage: -3, Confidence: &conf},
	})
	if net.Action != "LONG" || net.Leverage != 2 {
		t.Fatalf("expected LONG x2, got %+v", net)
	}
	if net.Confidence == nil || net.Confidence.String() != "0.6" {
		t.Fatalf("expected mean confidence 0.6, got %v", net.Confidence)
	}
	// Near-cancel → HOLD.
	if net := NetCluster([]ClusterLeg{{SignedLeverage: 0}}); net.Action != "HOLD" {
		t.Fatalf("expected HOLD, got %+v", net)
	}
	// V65 1–10 cap applies to net leverage.
	big := decimal.RequireFromString("0.9")
	legs := []ClusterLeg{}
	for i := 0; i < 5; i++ {
		legs = append(legs, ClusterLeg{SignedLeverage: 10, Confidence: &big})
	}
	if net := NetCluster(legs); net.Action != "LONG" || net.Leverage != 10 {
		t.Fatalf("expected capped LONG x10, got %+v", net)
	}
}

func decimalPtr(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}
