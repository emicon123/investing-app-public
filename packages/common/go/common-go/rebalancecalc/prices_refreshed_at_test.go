package rebalancecalc

import (
	"testing"
	"time"
)

// TestPricesRefreshedAt_ReturnsUTC merges the portfolioB (commit e68ca04, 2026-09-22) and portfolioC
// (hotfix 268a4e8, 2026-09-28) regression tests: fetchedAt timestamps loaded from Postgres carry
// the server's local zone (Europe/Warsaw), so pricesRefreshedAt serialized with "+02:00"; the
// frontend's zod schema then rejected every calculate response ("Błąd obliczenia") although the
// backend answered 200. The result must be the same instant, in UTC.
func TestPricesRefreshedAt_ReturnsUTC(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skipf("tzdata unavailable in this environment: %v", err)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		ticker, yahoo string
		fetched       time.Time
	}{
		{"SUI", "VRTU.DE", time.Date(2026, 9, 22, 11, 41, 31, 605896000, warsaw)},
		{"QDV5.DE", "QDV5.DE", time.Date(2026, 9, 28, 11, 49, 52, 783035000, warsaw)},
	} {
		inputs := []PositionInput{{Ticker: tc.ticker, YahooTicker: new(tc.yahoo)}}
		got := PricesRefreshedAt(inputs, map[string]time.Time{tc.yahoo: tc.fetched}, now)
		if got.Location() != time.UTC {
			t.Fatalf("%s: expected UTC location, got %v", tc.ticker, got.Location())
		}
		if !got.Equal(tc.fetched) {
			t.Fatalf("%s: expected same instant as input, got %v want %v", tc.ticker, got, tc.fetched)
		}
	}
}

func TestPricesRefreshedAt_EarliestQualifyingInputCappedAtNow(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	older := now.Add(-3 * time.Hour)
	old := now.Add(-1 * time.Hour)
	inputs := []PositionInput{
		{Ticker: "A", YahooTicker: new("A.Y")},
		{Ticker: "B", YahooTicker: new("B.Y")},
		{Ticker: "COMP", YahooTicker: new("COMP.Y"), IsCompanionRow: true}, // companion: skipped
		{Ticker: "NIL", YahooTicker: nil},                                  // no yahoo ticker: skipped
		{Ticker: "EMPTY", YahooTicker: new("")},                            // empty yahoo ticker: skipped
		{Ticker: "UNCACHED", YahooTicker: new("UNCACHED.Y")},               // no fetchedAt entry: skipped
	}
	fetchedAt := map[string]time.Time{
		"A.Y":    old,
		"B.Y":    older,
		"COMP.Y": now.Add(-48 * time.Hour),
		"":       now.Add(-72 * time.Hour),
	}
	if got := PricesRefreshedAt(inputs, fetchedAt, now); !got.Equal(older) {
		t.Fatalf("got %v, want the earliest non-companion fetch %v", got, older)
	}

	future := map[string]time.Time{"A.Y": now.Add(time.Hour)}
	if got := PricesRefreshedAt(inputs[:1], future, now); !got.Equal(now) {
		t.Fatalf("a fetch after now must be capped at now: got %v, want %v", got, now)
	}
}

func TestPricesRefreshedAt_NoQualifyingInputIsNowInUTC(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skipf("tzdata unavailable in this environment: %v", err)
	}
	now := time.Date(2026, 11, 5, 11, 0, 0, 0, warsaw)
	for name, inputs := range map[string][]PositionInput{
		"empty input":    nil,
		"only companion": {{Ticker: "C", YahooTicker: new("C.Y"), IsCompanionRow: true}},
	} {
		got := PricesRefreshedAt(inputs, map[string]time.Time{"C.Y": now.Add(-time.Hour)}, now)
		if got.Location() != time.UTC || !got.Equal(now) {
			t.Fatalf("%s: got %v, want now in UTC (%v)", name, got, now.UTC())
		}
	}
}
