package macro

import (
	"math"
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// This file holds MacroDataService's derivation math (ported 1:1 from Java, ADR-094) — every
// function here is pure (no DB, no network, no clock), operating on []MacroPoint. Keep it that way:
// the blend factor feeds the rebalancing gate, so this file gets unit tests against fixtures
// derived from the Java implementation, not interface ceremony (YAGNI — no BlendCalculator
// interface, per doc 13 §7 row 1B).

// ── NETLIQ ───────────────────────────────────────────────────────────────────────────────

// computeNetLiq ports MacroDataService.computeNetLiq: NETLIQ = WALCL − WDTGAL − RRPONTSYD×1000,
// aligned on WALCL (weekly) dates. The TGA leg is required (skip the week without it); a
// missing RRP leg counts as zero.
func computeNetLiq(walcl, wdtgal, rrpBillions []MacroPoint) []MacroPoint {
	walAsc := sortAsc(walcl)
	tgaAsc := sortAsc(wdtgal)
	rrpAsc := sortAsc(rrpBillions)
	out := []MacroPoint{}
	for _, w := range walAsc {
		tga, ok := atOrBefore(tgaAsc, w.Date)
		if !ok {
			continue
		}
		var rrpMln decimal.Decimal
		if rrp, ok := atOrBefore(rrpAsc, w.Date); ok {
			rrpMln = rrp.Mul(thousand)
		}
		out = append(out, MacroPoint{Date: w.Date, Value: w.Value.Sub(tga).Sub(rrpMln)})
	}
	return out
}

// ── GLOBAL_M2_USD ────────────────────────────────────────────────────────────────────────

// computeGlobalM2 ports MacroDataService.computeGlobalM2: monthly GLOBAL_M2_USD in USD millions.
// A month qualifies only when the four core blocks (US, Euro area, China, Japan) are all
// present; UK is added when available. months iterate over the US block's key set, ascending.
// The second result holds the UK leg (USD mln) of every month that includes it, so YoY can be
// taken on a consistent basket (ukBasketConsistent) when UK is present on only one side.
func computeGlobalM2(usBn, eaEurMln, ukGbpMln, cnCnyMln, jpJpyMln []MacroPoint,
	fxEur, fxGbp, fxCny, fxJpy []MacroPoint) ([]MacroPoint, map[yearMonth]decimal.Decimal) {
	us := monthlyScaled(usBn, thousand) // USD bn → mln
	ea := monthlyUSDByMul(eaEurMln, fxEur)
	uk := monthlyUSDByMul(ukGbpMln, fxGbp)
	cn := monthlyUSDByDiv(cnCnyMln, fxCny)
	jp := monthlyUSDByDiv(jpJpyMln, fxJpy)

	months := sortedKeys(us)
	out := []MacroPoint{}
	ukIncluded := map[yearMonth]decimal.Decimal{}
	for _, m := range months {
		usV, ok1 := us[m]
		eaV, ok2 := ea[m]
		cnV, ok3 := cn[m]
		jpV, ok4 := jp[m]
		if !ok1 || !ok2 || !ok3 || !ok4 {
			continue
		}
		sum := usV.Add(eaV).Add(cnV).Add(jpV)
		if ukV, ok := uk[m]; ok {
			sum = sum.Add(ukV)
			ukIncluded[m] = ukV
		}
		out = append(out, MacroPoint{Date: monthStart(m), Value: sum})
	}
	return out, ukIncluded
}

// ukBasketConsistent drops the UK leg from whichever side has it when only one of the two
// compared months includes UK, so a UK publication lag cannot show up as a fake Global-M2
// YoY drop (or jump). A nil ukUSD map (non-Global-M2 series) leaves both values untouched.
func ukBasketConsistent(cur, prev decimal.Decimal, curM, prevM yearMonth,
	ukUSD map[yearMonth]decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	ukCur, hasCur := ukUSD[curM]
	ukPrev, hasPrev := ukUSD[prevM]
	if hasCur == hasPrev {
		return cur, prev
	}
	if hasCur {
		cur = cur.Sub(ukCur)
	}
	if hasPrev {
		prev = prev.Sub(ukPrev)
	}
	return cur, prev
}

type yearMonth struct{ y, m int }

func ymOf(t time.Time) yearMonth { y, m, _ := t.Date(); return yearMonth{y, int(m)} }

func monthStart(ym yearMonth) time.Time {
	return time.Date(ym.y, time.Month(ym.m), 1, 0, 0, 0, 0, time.UTC)
}

func monthEnd(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC) // day 0 of next month = last day
}

func monthlyScaled(pts []MacroPoint, factor decimal.Decimal) map[yearMonth]decimal.Decimal {
	m := map[yearMonth]decimal.Decimal{}
	for _, p := range sortAsc(pts) {
		m[ymOf(p.Date)] = p.Value.Mul(factor)
	}
	return m
}

// monthlyUSDByMul ports monthlyUsdMul: native millions × USD-per-native FX at month-end
// (falling back to at-or-before the observation date), skipping non-positive/missing rates.
func monthlyUSDByMul(nativePts, fx []MacroPoint) map[yearMonth]decimal.Decimal {
	fxAsc := sortAsc(fx)
	m := map[yearMonth]decimal.Decimal{}
	for _, p := range sortAsc(nativePts) {
		rate, ok := atOrBefore(fxAsc, monthEnd(p.Date))
		if !ok {
			rate, ok = atOrBefore(fxAsc, p.Date)
		}
		if !ok || rate.Sign() <= 0 {
			continue
		}
		m[ymOf(p.Date)] = p.Value.Mul(rate)
	}
	return m
}

// monthlyUSDByDiv ports monthlyUsdDiv: native millions ÷ native-per-USD FX (scale 4 HALF_UP).
func monthlyUSDByDiv(nativePts, fx []MacroPoint) map[yearMonth]decimal.Decimal {
	fxAsc := sortAsc(fx)
	m := map[yearMonth]decimal.Decimal{}
	for _, p := range sortAsc(nativePts) {
		rate, ok := atOrBefore(fxAsc, monthEnd(p.Date))
		if !ok {
			rate, ok = atOrBefore(fxAsc, p.Date)
		}
		if !ok || rate.Sign() <= 0 {
			continue
		}
		m[ymOf(p.Date)] = divHalfUp(p.Value, rate, 4)
	}
	return m
}

func sortedKeys(m map[yearMonth]decimal.Decimal) []yearMonth {
	keys := make([]yearMonth, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].y != keys[j].y {
			return keys[i].y < keys[j].y
		}
		return keys[i].m < keys[j].m
	})
	return keys
}

// ── YoY / acceleration ───────────────────────────────────────────────────────────────────

// yoy ports MacroDataService.yoy: YoY growth (decimal) at asOf using the observation
// ~lookbackDays earlier. Scale 10 HALF_UP. Returns (value, false) when uncomputable.
func yoy(series []MacroPoint, asOf time.Time, lookbackDays int) (decimal.Decimal, bool) {
	asc := sortAsc(series)
	cur, ok1 := atOrBefore(asc, asOf)
	prev, ok2 := atOrBefore(asc, asOf.AddDate(0, 0, -lookbackDays))
	if !ok1 || !ok2 || prev.Sign() == 0 {
		return decimal.Zero, false
	}
	return divHalfUp(cur, prev, 10).Sub(decimal.NewFromInt(1)), true
}

// yoyAtMonth ports yoyAtMonth: monthly YoY at month using monthsBack earlier, on a
// UK-consistent basket (ukBasketConsistent; pass nil ukUSD for a plain YoY).
func yoyAtMonth(asc []MacroPoint, ukUSD map[yearMonth]decimal.Decimal, month yearMonth, monthsBack int) (decimal.Decimal, bool) {
	prevM := addMonths(month, -monthsBack)
	cur, ok1 := valueAtMonth(asc, month)
	prev, ok2 := valueAtMonth(asc, prevM)
	if !ok1 || !ok2 {
		return decimal.Zero, false
	}
	cur, prev = ukBasketConsistent(cur, prev, month, prevM, ukUSD)
	if prev.Sign() == 0 {
		return decimal.Zero, false
	}
	return divHalfUp(cur, prev, 10).Sub(decimal.NewFromInt(1)), true
}

func addMonths(ym yearMonth, delta int) yearMonth {
	t := monthStart(ym).AddDate(0, delta, 0)
	return ymOf(t)
}

func valueAtMonth(asc []MacroPoint, month yearMonth) (decimal.Decimal, bool) {
	for _, p := range asc {
		if ymOf(p.Date) == month {
			return p.Value, true
		}
	}
	return decimal.Zero, false
}

// yoyMonthly ports yoyMonthly: monthly YoY at the latest month.
func yoyMonthly(series []MacroPoint, ukUSD map[yearMonth]decimal.Decimal, monthsBack int) (decimal.Decimal, bool) {
	asc := sortAsc(series)
	if len(asc) == 0 {
		return decimal.Zero, false
	}
	return yoyAtMonth(asc, ukUSD, ymOf(asc[len(asc)-1].Date), monthsBack)
}

// yoyAccelMonthly ports yoyAccelMonthly: 3-month acceleration of the monthly YoY at the latest.
func yoyAccelMonthly(series []MacroPoint, ukUSD map[yearMonth]decimal.Decimal, monthsBack, accelMonths int) (decimal.Decimal, bool) {
	asc := sortAsc(series)
	if len(asc) == 0 {
		return decimal.Zero, false
	}
	return yoyAccelAtMonth(asc, ukUSD, ymOf(asc[len(asc)-1].Date), monthsBack, accelMonths)
}

// yoyAccelAtMonth ports yoyAccelAtMonth — same accel formula anchored at an arbitrary month
// (used to reconstruct the historical accel series the blend z-score needs).
// Each of the two YoYs is basket-consistent on its own pair of months.
func yoyAccelAtMonth(asc []MacroPoint, ukUSD map[yearMonth]decimal.Decimal, cur yearMonth, monthsBack, accelMonths int) (decimal.Decimal, bool) {
	now, ok1 := yoyAtMonth(asc, ukUSD, cur, monthsBack)
	earlier, ok2 := yoyAtMonth(asc, ukUSD, addMonths(cur, -accelMonths), monthsBack)
	if !ok1 || !ok2 {
		return decimal.Zero, false
	}
	return now.Sub(earlier), true
}

// netLiqYoYAccelAsOf ports netLiqYoyAccelAsOf — the weekly accel formula anchored at an
// arbitrary date (used to resample NETLIQ onto Global-M2's monthly cadence).
func netLiqYoYAccelAsOf(netliq []MacroPoint, asOf time.Time) (decimal.Decimal, bool) {
	yoyNow, ok1 := yoy(netliq, asOf, yoyDays)
	yoyPrev, ok2 := yoy(netliq, asOf.AddDate(0, 0, -accelDays), yoyDays)
	if !ok1 || !ok2 {
		return decimal.Zero, false
	}
	return yoyNow.Sub(yoyPrev), true
}

// ── ADR-044 blend series ─────────────────────────────────────────────────────────────────

// computeBlendSeries ports MacroDataService.computeBlendSeries: resampled onto Global-M2's
// monthly cadence (the coarser frequency). For each month in globalM2, net-liq YoY accel is
// evaluated as-of that month's last day and Global-M2 YoY accel anchored at that month; each
// resampled accel series is independently z-scored against its own expanding, no-lookahead
// history (sample stddev), combined 0.7/0.3, then smoothed with a trailing 3-month average
// requiring a full gap-free window. globalM2UK is computeGlobalM2's UK leg map (UK-consistent
// Global-M2 YoY); nil means no basket adjustment.
func computeBlendSeries(netliq, globalM2 []MacroPoint, globalM2UK map[yearMonth]decimal.Decimal) []BlendPoint {
	months := sortedMonths(globalM2)
	if len(months) == 0 {
		return []BlendPoint{}
	}
	globalM2Asc := sortAsc(globalM2)
	netliqAccel := make([]*decimal.Decimal, len(months))
	gm2Accel := make([]*decimal.Decimal, len(months))
	for i, m := range months {
		if v, ok := netLiqYoYAccelAsOf(netliq, monthEnd(monthStart(m))); ok {
			v := v
			netliqAccel[i] = &v
		}
		if v, ok := yoyAccelAtMonth(globalM2Asc, globalM2UK, m, m2YoYMonths, m2AccelMonths); ok {
			v := v
			gm2Accel[i] = &v
		}
	}

	zNetliq := expandingZScores(netliqAccel)
	zGm2 := expandingZScores(gm2Accel)

	composite := make([]*decimal.Decimal, len(months))
	for i := range months {
		if zNetliq[i] == nil || zGm2[i] == nil {
			continue
		}
		c := netLiqZScoreWeight.Mul(*zNetliq[i]).Add(globalM2ZScoreWeight.Mul(*zGm2[i]))
		composite[i] = &c
	}

	out := make([]BlendPoint, len(months))
	for i, m := range months {
		smoothed := trailingAverage(composite, i, blendSmoothingMonths)
		var blend *decimal.Decimal
		if smoothed != nil {
			b := clipBlend(*smoothed)
			blend = &b
		}
		out[i] = BlendPoint{Month: monthStart(m), CompositeZ: composite[i], SmoothedZ: smoothed, Blend: blend}
	}
	return out
}

// expandingZScores computes an expanding (inclusive), no-lookahead z-score vs. own running history.
// Null entries are skipped entirely; a z-score is null until ≥2 non-null values have been seen and
// the running SAMPLE stddev is nonzero. The float64 math mirrors Apache Commons Math
// DescriptiveStatistics (double mean, sample variance n−1) as the Java original used it;
// comparisons against the Java-derived fixtures use a 1e-9 tolerance, not bit-for-bit.
func expandingZScores(values []*decimal.Decimal) []*decimal.Decimal {
	z := make([]*decimal.Decimal, len(values))
	var seen []float64
	for i, v := range values {
		if v == nil {
			continue
		}
		x := v.InexactFloat64()
		seen = append(seen, x)
		if len(seen) < 2 {
			continue
		}
		mean := 0.0
		for _, s := range seen {
			mean += s
		}
		mean /= float64(len(seen))
		varSum := 0.0
		for _, s := range seen {
			d := s - mean
			varSum += d * d
		}
		sd := math.Sqrt(varSum / float64(len(seen)-1))
		if sd == 0.0 {
			continue
		}
		zi := decimal.NewFromFloat((x - mean) / sd)
		z[i] = &zi
	}
	return z
}

// trailingAverage ports MacroDataService.trailingAverage: trailing window-point average ending
// at index (inclusive), scale 10 HALF_UP; null unless the window is full and gap-free.
func trailingAverage(series []*decimal.Decimal, index, window int) *decimal.Decimal {
	if index-window+1 < 0 {
		return nil
	}
	sum := decimal.Zero
	for i := index - window + 1; i <= index; i++ {
		if series[i] == nil {
			return nil
		}
		sum = sum.Add(*series[i])
	}
	avg := divHalfUp(sum, decimal.NewFromInt(int64(window)), 10)
	return &avg
}

// clipBlend ports MacroDataService.clipBlend: clip(0.5 + smoothed/0.5, 0, 1) — ADR-071
// orientation (0 = full risk-off, 1 = full risk-on).
func clipBlend(smoothed decimal.Decimal) decimal.Decimal {
	raw := half.Add(divHalfUp(smoothed, blendSteepnessDiv, 10))
	if raw.Cmp(decimal.Zero) < 0 {
		return decimal.Zero
	}
	if raw.Cmp(decimal.NewFromInt(1)) > 0 {
		return decimal.NewFromInt(1)
	}
	return raw
}

// zoneFromBlend ports MacroZone.fromBlendFactor — display-only label, never stored.
// Returns ("", false) when blend is nil (bootstrap period).
func zoneFromBlend(blend *decimal.Decimal) (string, bool) {
	if blend == nil {
		return "", false
	}
	for _, band := range []struct {
		limit decimal.Decimal
		zone  string
	}{
		{decimal.RequireFromString("0.2"), "RISK_OFF"},
		{decimal.RequireFromString("0.4"), "MID_OFF"},
		{decimal.RequireFromString("0.6"), "NEUTRAL"},
		{decimal.RequireFromString("0.8"), "MID_ON"},
	} {
		if blend.Cmp(band.limit) < 0 {
			return band.zone, true
		}
	}
	return "RISK_ON", true
}

// sortedMonths ports MacroDataService.sortedMonths: ascending de-duplicated YearMonths.
func sortedMonths(monthlySeries []MacroPoint) []yearMonth {
	seen := map[yearMonth]bool{}
	for _, p := range monthlySeries {
		seen[ymOf(p.Date)] = true
	}
	months := make([]yearMonth, 0, len(seen))
	for m := range seen {
		months = append(months, m)
	}
	sort.Slice(months, func(i, j int) bool {
		if months[i].y != months[j].y {
			return months[i].y < months[j].y
		}
		return months[i].m < months[j].m
	})
	return months
}

// ── Tiny list utilities ────────────────────────────────────────────────────────────────

func sortAsc(pts []MacroPoint) []MacroPoint {
	cp := make([]MacroPoint, len(pts))
	copy(cp, pts)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].Date.Before(cp[j].Date) })
	return cp
}

// atOrBefore ports MacroDataService.atOrBefore: value of the last point with date <= target.
func atOrBefore(asc []MacroPoint, target time.Time) (decimal.Decimal, bool) {
	var (
		value decimal.Decimal
		found bool
	)
	for _, p := range asc {
		if !p.Date.After(target) {
			value, found = p.Value, true
		} else {
			break
		}
	}
	return value, found
}

func lastDate(pts []MacroPoint) (time.Time, bool) {
	asc := sortAsc(pts)
	if len(asc) == 0 {
		return time.Time{}, false
	}
	return asc[len(asc)-1].Date, true
}

func lastValue(pts []MacroPoint) (decimal.Decimal, bool) {
	asc := sortAsc(pts)
	if len(asc) == 0 {
		return decimal.Zero, false
	}
	return asc[len(asc)-1].Value, true
}

// isStale ports MacroDataService.isStale: no observation, or older than maxDays.
func isStale(latest *time.Time, today time.Time, maxDays int) bool {
	if latest == nil {
		return true
	}
	days := int(startOfDayUTC(today).Sub(startOfDayUTC(*latest)).Hours() / 24)
	return days > maxDays
}
