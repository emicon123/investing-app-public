package macro

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// Pure derivation-math tests — the direct port of
// backend/src/test/java/pl/investing/service/macro/MacroDataMathTest.java. Every numeric
// fixture below is copied verbatim from the Java test so a divergence fails here first,
// before it can silently mis-tilt Phase 7's rebalancing targets via blendFactor.

func mp(date, value string) MacroPoint {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	return MacroPoint{Date: d, Value: decimal.RequireFromString(value)}
}

func requireDecimal(t *testing.T, v decimal.Decimal, ok bool, what string) decimal.Decimal {
	t.Helper()
	if !ok {
		t.Fatalf("%s: expected a value, got none", what)
	}
	return v
}

func assertCmp(t *testing.T, got decimal.Decimal, want string, what string) {
	t.Helper()
	if got.Cmp(decimal.RequireFromString(want)) != 0 {
		t.Fatalf("%s: expected %s, got %s", what, want, got.String())
	}
}

func TestNetLiq_SubtractsTgaAndRrpBillionsConvertedToMillions(t *testing.T) {
	netliq := computeNetLiq(
		[]MacroPoint{mp("2026-06-24", "7000000")},
		[]MacroPoint{mp("2026-06-24", "900000")},
		[]MacroPoint{mp("2026-06-23", "3.5")},
	)
	if len(netliq) != 1 {
		t.Fatalf("expected 1 point, got %d", len(netliq))
	}
	assertCmp(t, netliq[0].Value, "6096500", "netliq") // 7000000-900000-3500
}

func TestYoY_IsYearOverYearGrowthDecimal(t *testing.T) {
	asOf, _ := time.Parse("2006-01-02", "2026-06-05")
	series := []MacroPoint{
		{Date: asOf.AddDate(0, 0, -364), Value: decimal.NewFromInt(100)},
		{Date: asOf, Value: decimal.NewFromInt(110)},
	}
	yoyV, yoyOK := yoy(series, asOf, 364)
	assertCmp(t, requireDecimal(t, yoyV, yoyOK, "yoy"), "0.10", "yoy")
}

func TestMonthlyYoYAndAccel_SignalDeceleration(t *testing.T) {
	// YoY at 2026-05 = +10%, YoY at 2026-02 = +30% → accel = -20% (risk-off leg).
	series := []MacroPoint{
		mp("2025-02-01", "100"),
		mp("2025-05-01", "100"),
		mp("2026-02-01", "130"),
		mp("2026-05-01", "110"),
	}
	myV, myOK := yoyMonthly(series, nil, 12)
	assertCmp(t, requireDecimal(t, myV, myOK, "yoyMonthly"), "0.10", "yoyMonthly")
	accelV, accelOK := yoyAccelMonthly(series, nil, 12, 3)
	accel := requireDecimal(t, accelV, accelOK, "accel")
	assertCmp(t, accel, "-0.20", "accel")
	if accel.Sign() >= 0 {
		t.Fatalf("expected negative accel, got %s", accel)
	}
}

func TestGlobalM2_SumsConvertedCoreBlocks(t *testing.T) {
	gm2, _ := computeGlobalM2(
		[]MacroPoint{mp("2026-05-01", "23000")},
		[]MacroPoint{mp("2026-05-01", "16000000")},
		nil, // UK missing → excluded
		[]MacroPoint{mp("2026-05-31", "300000000")},
		[]MacroPoint{mp("2026-05-31", "1200000000")},
		[]MacroPoint{mp("2026-05-01", "1.1")},
		[]MacroPoint{mp("2026-05-01", "1.3")},
		[]MacroPoint{mp("2026-05-01", "7.0")},
		[]MacroPoint{mp("2026-05-01", "150")},
	)
	if len(gm2) != 1 {
		t.Fatalf("expected 1 month, got %d", len(gm2))
	}
	if got := gm2[0].Date; got.Format("2006-01-02") != "2026-05-01" {
		t.Fatalf("expected 2026-05-01, got %s", got.Format("2006-01-02"))
	}
	// 23,000,000 + 17,600,000 + 42,857,142.8571 + 8,000,000 ≈ 91,457,142.86
	diff := gm2[0].Value.Sub(decimal.RequireFromString("91457142.86")).Abs()
	if diff.Cmp(decimal.RequireFromString("0.5")) > 0 {
		t.Fatalf("expected ≈91457142.86, got %s", gm2[0].Value.String())
	}
}

func TestGlobalM2_DropsMonthsMissingACoreBlock(t *testing.T) {
	empty := []MacroPoint{}
	gm2, _ := computeGlobalM2(
		[]MacroPoint{mp("2026-05-01", "23000")},
		[]MacroPoint{mp("2026-05-01", "16000000")},
		empty, empty,
		[]MacroPoint{mp("2026-05-31", "1200000000")},
		[]MacroPoint{mp("2026-05-01", "1.1")},
		empty, empty,
		[]MacroPoint{mp("2026-05-01", "150")},
	)
	if len(gm2) != 0 {
		t.Fatalf("expected no qualifying month without China, got %v", gm2)
	}
}

func TestGlobalM2_ReturnsUKLegOnlyForMonthsThatIncludeIt(t *testing.T) {
	_, uk := computeGlobalM2(
		[]MacroPoint{mp("2026-04-01", "23000"), mp("2026-05-01", "23000")},
		[]MacroPoint{mp("2026-04-01", "16000000"), mp("2026-05-01", "16000000")},
		[]MacroPoint{mp("2026-04-01", "3000000")}, // UK has April only
		[]MacroPoint{mp("2026-04-30", "300000000"), mp("2026-05-31", "300000000")},
		[]MacroPoint{mp("2026-04-30", "1200000000"), mp("2026-05-31", "1200000000")},
		[]MacroPoint{mp("2026-04-01", "1.1")},
		[]MacroPoint{mp("2026-04-01", "1.3")},
		[]MacroPoint{mp("2026-04-01", "7.0")},
		[]MacroPoint{mp("2026-04-01", "150")},
	)
	if len(uk) != 1 {
		t.Fatalf("expected UK leg for 1 month, got %v", uk)
	}
	assertCmp(t, uk[yearMonth{2026, 4}], "3900000", "UK leg USD mln") // 3,000,000 GBP × 1.3
}

func TestYoYAtMonth_UKMissingOnOneSide_ComparesWithoutUK(t *testing.T) {
	// Core basket grows 100 → 110 (+10%); UK = 5 in both 2025-05 and 2026-04 but 2026-05 has
	// no UK yet. A naive total YoY would be 110/105-1 ≈ +4.76%.
	asc := sortAsc([]MacroPoint{mp("2025-05-01", "105"), mp("2026-05-01", "110")})
	uk := map[yearMonth]decimal.Decimal{{2025, 5}: decimal.NewFromInt(5)}
	v, ok := yoyAtMonth(asc, uk, yearMonth{2026, 5}, 12)
	assertCmp(t, requireDecimal(t, v, ok, "yoyAtMonth"), "0.10", "UK dropped from prev")

	// Mirror case: UK only in the current month.
	asc = sortAsc([]MacroPoint{mp("2025-05-01", "100"), mp("2026-05-01", "115")})
	uk = map[yearMonth]decimal.Decimal{{2026, 5}: decimal.NewFromInt(5)}
	v, ok = yoyAtMonth(asc, uk, yearMonth{2026, 5}, 12)
	assertCmp(t, requireDecimal(t, v, ok, "yoyAtMonth"), "0.10", "UK dropped from cur")
}

func TestYoYAtMonth_UKOnBothSides_KeepsFullBasket(t *testing.T) {
	asc := sortAsc([]MacroPoint{mp("2025-05-01", "100"), mp("2026-05-01", "120")})
	uk := map[yearMonth]decimal.Decimal{{2025, 5}: decimal.NewFromInt(5), {2026, 5}: decimal.NewFromInt(10)}
	v, ok := yoyAtMonth(asc, uk, yearMonth{2026, 5}, 12)
	assertCmp(t, requireDecimal(t, v, ok, "yoyAtMonth"), "0.20", "full basket")
}

func TestIsStale_ThresholdsOnAge(t *testing.T) {
	today, _ := time.Parse("2006-01-02", "2026-06-30")
	may, _ := time.Parse("2006-01-02", "2026-05-01")
	jun, _ := time.Parse("2006-01-02", "2026-06-01")
	if !isStale(&may, today, 45) { // 60d
		t.Fatalf("expected stale for 60d-old observation")
	}
	if isStale(&jun, today, 45) { // 29d
		t.Fatalf("expected fresh for 29d-old observation")
	}
	if !isStale(nil, today, 45) {
		t.Fatalf("expected stale for missing observation")
	}
}

func TestStaleDaysConstants_MatchADR046Recalibration(t *testing.T) {
	if netLiqStaleDays != 14 || m2StaleDays != 90 {
		t.Fatalf("staleness cadence changed: netliq=%d m2=%d", netLiqStaleDays, m2StaleDays)
	}
}

// ── ADR-044 blend-factor math ────────────────────────────────────────────────────────────

func TestNetLiqYoYAccelAsOf_GeneralizesAnchorToArbitraryDate(t *testing.T) {
	asOf, _ := time.Parse("2006-01-02", "2026-06-05")
	at := func(days int) time.Time { return asOf.AddDate(0, 0, -days) }
	series := []MacroPoint{
		{Date: at(91 + 364), Value: decimal.NewFromInt(100)},
		{Date: at(91), Value: decimal.NewFromInt(130)},
		{Date: at(364), Value: decimal.NewFromInt(100)},
		{Date: asOf, Value: decimal.NewFromInt(110)},
	}
	accel, ok := netLiqYoYAccelAsOf(series, asOf)
	if !ok {
		t.Fatalf("expected accel, got none")
	}
	assertCmp(t, accel, "-0.20", "netLiqYoYAccelAsOf")
}

func TestNetLiqYoYAccelAsOf_InsufficientHistory_IsNone(t *testing.T) {
	asOf, _ := time.Parse("2006-01-02", "2026-06-05")
	if _, ok := netLiqYoYAccelAsOf([]MacroPoint{mp("2026-06-05", "110")}, asOf); ok {
		t.Fatalf("expected no accel for a single point")
	}
}

func TestYoYAccelAtMonth_AnchoredAtArbitraryMonth(t *testing.T) {
	series := []MacroPoint{
		mp("2025-02-01", "100"),
		mp("2025-05-01", "100"),
		mp("2026-02-01", "130"),
		mp("2026-05-01", "110"),
		mp("2026-08-01", "999"), // later, unrelated point
	}
	asc := sortAsc(series)
	accel, ok := yoyAccelAtMonth(asc, nil, yearMonth{2026, 5}, 12, 3)
	if !ok {
		t.Fatalf("expected accel, got none")
	}
	assertCmp(t, accel, "-0.20", "yoyAccelAtMonth")
}

func decPtr(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

func TestExpandingZScores_NullsSkippedFirstPointNull(t *testing.T) {
	// Index 0: 1 observation → null. Index 1: null value → null, NOT counted.
	// Index 2 (20): 2nd real observation → defined. Index 3 (30): exact, sd=10.
	z := expandingZScores([]*decimal.Decimal{decPtr("10"), nil, decPtr("20"), decPtr("30")})
	if z[0] != nil || z[1] != nil {
		t.Fatalf("expected nulls at 0,1, got %v %v", z[0], z[1])
	}
	if z[2] == nil || math.Abs(z[2].InexactFloat64()-0.7071067812) > 1e-6 {
		t.Fatalf("expected z[2]≈0.7071067812, got %v", z[2])
	}
	if z[3] == nil || math.Abs(z[3].InexactFloat64()-1.0) > 1e-9 {
		t.Fatalf("expected z[3]=1.0, got %v", z[3])
	}
}

func TestExpandingZScores_ConstantSeries_StaysNull(t *testing.T) {
	z := expandingZScores([]*decimal.Decimal{decPtr("5"), decPtr("5"), decPtr("5"), decPtr("5")})
	for i, v := range z {
		if v != nil {
			t.Fatalf("expected null at %d (sd=0), got %s", i, v.String())
		}
	}
}

func TestComputeBlendSeries_EmptyGlobalM2_ReturnsEmpty(t *testing.T) {
	if out := computeBlendSeries(nil, nil, nil); len(out) != 0 {
		t.Fatalf("expected empty, got %d points", len(out))
	}
}

func TestComputeBlendSeries_BootstrapsThenStabilizes(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	netliqStart := start.AddDate(0, -30, 0)
	netliq := []MacroPoint{}
	for w := range 500 {
		netliq = append(netliq, MacroPoint{
			Date:  netliqStart.AddDate(0, 0, 7*w),
			Value: decimal.NewFromInt(6_000_000 + int64(w*1500)),
		})
	}
	const months = 30
	globalM2 := []MacroPoint{}
	for m := range months {
		globalM2 = append(globalM2, MacroPoint{
			Date:  start.AddDate(0, m, 0),
			Value: decimal.NewFromInt(90_000_000 + int64(m*300_000+m*m*1000)),
		})
	}

	series := computeBlendSeries(netliq, globalM2, nil)
	if len(series) != months {
		t.Fatalf("expected %d points, got %d", months, len(series))
	}
	if series[0].CompositeZ != nil || series[0].Blend != nil {
		t.Fatalf("expected bootstrap nulls at month 0")
	}
	last := series[len(series)-1]
	if last.CompositeZ == nil || last.SmoothedZ == nil || last.Blend == nil {
		t.Fatalf("expected defined tail, got %+v", last)
	}
	zero, one := decimal.Zero, decimal.NewFromInt(1)
	for _, p := range series {
		if p.Blend != nil && (p.Blend.Cmp(zero) < 0 || p.Blend.Cmp(one) > 0) {
			t.Fatalf("blend outside [0,1]: %s", p.Blend.String())
		}
	}
}

// ── Zone + exact HALF_UP arithmetic ──────────────────────────────────────────────────────

func TestZoneFromBlend_Thresholds(t *testing.T) {
	cases := []struct {
		blend string
		zone  string
	}{
		{"0", "RISK_OFF"},
		{"0.199999", "RISK_OFF"},
		{"0.2", "MID_OFF"},
		{"0.35", "MID_OFF"},
		{"0.4", "NEUTRAL"},
		{"0.59", "NEUTRAL"},
		{"0.6", "MID_ON"},
		{"0.79", "MID_ON"},
		{"0.8", "RISK_ON"},
		{"1", "RISK_ON"},
	}
	for _, c := range cases {
		b := decimal.RequireFromString(c.blend)
		zone, ok := zoneFromBlend(&b)
		if !ok || zone != c.zone {
			t.Fatalf("blend %s: expected %s, got %q,%v", c.blend, c.zone, zone, ok)
		}
	}
	if _, ok := zoneFromBlend(nil); ok {
		t.Fatalf("expected no zone for nil blend (bootstrap)")
	}
}

func TestDivHalfUp_MatchesJavaScaleSemantics(t *testing.T) {
	cases := []struct {
		a, b  string
		scale int32
		want  string
	}{
		{"1", "8", 2, "0.13"},   // 0.125 → half up
		{"-1", "8", 2, "-0.13"}, // HALF_UP is away from zero
		{"2", "3", 2, "0.67"},   // 0.666.. → up
		{"300000000", "7", 4, "42857142.8571"},
		{"110", "100", 10, "1.1000000000"},
	}
	for _, c := range cases {
		got := divHalfUp(decimal.RequireFromString(c.a), decimal.RequireFromString(c.b), c.scale)
		// Numeric compare (Cmp), not String: Java's BigDecimal preserves the requested scale
		// textually (1.1000000000) while shopspring normalizes (1.1) — same value, and every
		// persist path rescales explicitly anyway.
		if got.Cmp(decimal.RequireFromString(c.want)) != 0 {
			t.Fatalf("divHalfUp(%s/%s,%d): expected %s, got %s", c.a, c.b, c.scale, c.want, got.String())
		}
	}
	if got := roundHalfUp(decimal.RequireFromString("0.3500005"), 6); got.String() != "0.350001" {
		t.Fatalf("roundHalfUp tie: expected 0.350001, got %s", got.String())
	}
}

func TestClipBlend_OrientationADR071(t *testing.T) {
	// 0 = full risk-off, 1 = full risk-on; improving momentum pushes blend UP toward 1.
	if got := clipBlend(decimal.Zero); got.Cmp(decimal.RequireFromString("0.5")) != 0 {
		t.Fatalf("smoothed=0 → 0.5, got %s", got)
	}
	if got := clipBlend(decimal.RequireFromString("10")); got.Cmp(decimal.NewFromInt(1)) != 0 {
		t.Fatalf("large positive → 1, got %s", got)
	}
	if got := clipBlend(decimal.RequireFromString("-10")); got.Cmp(decimal.Zero) != 0 {
		t.Fatalf("large negative → 0, got %s", got)
	}
}
