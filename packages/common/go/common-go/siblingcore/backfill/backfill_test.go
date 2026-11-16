package backfill

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func assertUnits(t *testing.T, r *Rewind, ticker string, at time.Time, want string) {
	t.Helper()
	if got := r.UnitsAt(ticker, at); !got.Equal(dec(want)) {
		t.Errorf("UnitsAt(%s, %s) = %s, want %s", ticker, at.Format("2006-01-02"), got, want)
	}
}

func TestUnitsAt_SubtractsChangesStrictlyAfterDay(t *testing.T) {
	r := NewRewind(
		map[string]decimal.Decimal{"A": dec("10")},
		[]UnitChange{
			LotChange("A", day(2026, 8, 10), dec("2")),
			LotChange("A", day(2026, 8, 1), dec("1")),
		})
	assertUnits(t, r, "A", day(2026, 8, 1), "8") // a lot on the day is held that day
	assertUnits(t, r, "A", day(2026, 8, 10), "10")
	assertUnits(t, r, "A", day(2026, 7, 31), "7")
}

func TestUnitsAt_IgnoresTimeOfDayOfTheQueriedDay(t *testing.T) {
	r := NewRewind(map[string]decimal.Decimal{"A": dec("10")},
		[]UnitChange{LotChange("A", day(2026, 8, 10), dec("2"))})
	assertUnits(t, r, "A", time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC), "10")
	assertUnits(t, r, "A", time.Date(2026, 8, 9, 23, 59, 0, 0, time.UTC), "8")
}

func TestTransferChange_UsesWarsawCalendarDay(t *testing.T) {
	late := TransferChange("A", time.Date(2026, 8, 5, 22, 30, 0, 0, time.UTC), dec("-3"))
	if !late.Day.Equal(day(2026, 8, 6)) { // 00:30 CEST
		t.Fatalf("22:30Z must be Warsaw 08-06, got %s", late.Day)
	}
	early := TransferChange("A", time.Date(2026, 8, 5, 21, 59, 59, 0, time.UTC), dec("-3"))
	if !early.Day.Equal(day(2026, 8, 5)) {
		t.Fatalf("21:59:59Z must be Warsaw 08-05, got %s", early.Day)
	}
	r := NewRewind(map[string]decimal.Decimal{"A": dec("7")}, []UnitChange{late})
	assertUnits(t, r, "A", day(2026, 8, 5), "10")
	assertUnits(t, r, "A", day(2026, 8, 6), "7")
	if early.Ticker != "A" || !early.Units.Equal(dec("-3")) {
		t.Fatalf("ticker/units must pass through, got %+v", early)
	}
}

func TestUnitsAt_LotsAndTransferLegsTogether(t *testing.T) {
	r := NewRewind(
		map[string]decimal.Decimal{"B": dec("13")},
		[]UnitChange{
			TransferChange("B", time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC), dec("8")),
			LotChange("B", day(2026, 8, 15), dec("2")),
		})
	assertUnits(t, r, "B", day(2026, 8, 10), "3")
	assertUnits(t, r, "B", day(2026, 8, 16), "5")
	assertUnits(t, r, "B", day(2026, 8, 20), "13")
}

func TestUnitsAt_FloorsAtZero_UnknownTickerZero(t *testing.T) {
	r := NewRewind(map[string]decimal.Decimal{"A": dec("1")},
		[]UnitChange{LotChange("A", day(2026, 8, 10), dec("5"))})
	assertUnits(t, r, "A", day(2026, 8, 1), "0")
	assertUnits(t, r, "ZZZ", day(2026, 8, 1), "0")
}

func TestNewRewind_CopiesCurrent(t *testing.T) {
	cur := map[string]decimal.Decimal{"A": dec("10")}
	r := NewRewind(cur, nil)
	cur["A"] = dec("99")
	assertUnits(t, r, "A", day(2026, 8, 1), "10")
}

// TestScaleToPortfolioValue_Golden pins the scaling tail both sibling backfills end with: scale at
// 10dp, values at 2dp, last row (sorted ticker order) plugs the rounding remainder, negatives clamp
// to zero, and the invariant the services only warn on — |sum - portfolioValue| <= 0.02 — holds
// exactly here. Moved from portfolioB's and portfolioC's internal/admin (task 187 B5d).
func TestScaleToPortfolioValue_Golden(t *testing.T) {
	raw := map[string]decimal.Decimal{
		"SXRV.DE": decimal.RequireFromString("600.0045"),
		"ICGA.DE": decimal.RequireFromString("250.0015"),
		"4GLD.DE": decimal.RequireFromString("150.0055"),
	}
	pv := decimal.RequireFromString("1050.75")
	scaled := ScaleToPortfolioValue(raw, pv)

	sum := decimal.Zero
	for _, v := range scaled {
		sum = sum.Add(v)
	}
	if !sum.Equal(pv) {
		t.Fatalf("scaled sum must equal portfolio value exactly, got %s vs %s", sum, pv)
	}
	// Deterministic plug: last in sorted order (SXRV.DE) absorbs the remainder.
	scale := pv.DivRound(decimal.RequireFromString("1000.0115"), 10)
	wantICGA := decimal.RequireFromString("250.0015").Mul(scale).Round(2)
	if !scaled["ICGA.DE"].Equal(wantICGA) {
		t.Fatalf("expected ICGA.DE %s, got %s", wantICGA, scaled["ICGA.DE"])
	}
	wantSXRV := pv.Sub(scaled["ICGA.DE"]).Sub(scaled["4GLD.DE"])
	if !scaled["SXRV.DE"].Equal(wantSXRV) {
		t.Fatalf("expected the last sorted key SXRV.DE to plug %s, got %s", wantSXRV, scaled["SXRV.DE"])
	}
	if scaled["SXRV.DE"].Add(scaled["ICGA.DE"]).Add(scaled["4GLD.DE"]).Sub(pv).Abs().Compare(decimal.RequireFromString("0.02")) > 0 {
		t.Fatal("sum invariant 0.02 broken")
	}
}

func TestScaleToPortfolioValue_SingleRowScalesToTheValue(t *testing.T) {
	// A zero rawSum would divide by zero — callers never pass an empty map (they skip with a
	// warning first); a single-row map scales to the value exactly.
	scaled := ScaleToPortfolioValue(
		map[string]decimal.Decimal{"A": decimal.RequireFromString("3")},
		decimal.RequireFromString("10"))
	if !scaled["A"].Equal(decimal.RequireFromString("10")) {
		t.Fatalf("expected 10, got %s", scaled["A"])
	}
}

func TestScaleToPortfolioValue_NegativePlugClampsToZeroAndInputIsUntouched(t *testing.T) {
	// B is tiny, so the sorted-last plug (B) comes out as the remainder after A's rounded share.
	raw := map[string]decimal.Decimal{
		"A": decimal.RequireFromString("1000"),
		"B": decimal.RequireFromString("0.001"),
	}
	scaled := ScaleToPortfolioValue(raw, decimal.RequireFromString("1"))

	if scaled["B"].IsNegative() || scaled["A"].IsNegative() {
		t.Fatalf("negatives must clamp to zero, got %v", scaled)
	}
	if !raw["A"].Equal(decimal.RequireFromString("1000")) || len(raw) != 2 {
		t.Fatalf("input map must not be modified, got %v", raw)
	}
}
