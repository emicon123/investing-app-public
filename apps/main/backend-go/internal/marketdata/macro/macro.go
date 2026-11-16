// Package macro serves the macro-state/benchmark-history/manual-M2 endpoints (ported from the Java
// MacroDataService and BenchmarkHistoryService, ADR-094 / doc 13 §7 row 1B). It owns exactly two
// tables: macro_observations and macro_state (plus a read-only dependency on settings for
// rebalancing.macro_derisk_enabled, same pattern as Historia's read of positions).
//
// Scope note: prices/FX rates on the same /market-data prefix are served by internal/marketdata,
// and linear-regression/trade-chart by their own packages — this package must not serve those.
package macro

import (
	"time"

	"github.com/shopspring/decimal"
)

// ── Series / source codes (MacroDataService constants, ported 1:1) ──────────────────────

const (
	SeriesNetLiq    = "NETLIQ"
	SeriesGlobalM2  = "GLOBAL_M2_USD"
	SeriesM2CN      = "M2_CN"
	SeriesM2JP      = "M2_JP"
	seriesWALCL     = "WALCL"
	seriesWDTGAL    = "WDTGAL"
	seriesRRPONTSYD = "RRPONTSYD"
	seriesM2SL      = "M2SL"
	seriesM2EA      = "M2_EA"
	seriesM2UK      = "M2_UK"
	seriesEXUSEU    = "EXUSEU" // USD per EUR
	seriesEXUSUK    = "EXUSUK" // USD per GBP
	seriesEXCHUS    = "EXCHUS" // CNY per USD
	seriesEXJPUS    = "EXJPUS" // JPY per USD
)

const (
	SourceFred      = "FRED"
	SourceECB       = "ECB"
	SourceBOE       = "BOE"
	SourceBOJ       = "BOJ"
	SourceChinadata = "CHINADATA"
	SourceTradingEC = "TRADINGEC"
	SourcePBC       = "PBC"
	SourceYCharts   = "YCHARTS"
	SourceManual    = "MANUAL"
	SourceDerived   = "DERIVED"
)

// ── Blend-factor calibration (ADR-044 §7, tightened by ADR-048/049, flipped by ADR-071) ──

var (
	netLiqZScoreWeight   = decimal.RequireFromString("0.7")
	globalM2ZScoreWeight = decimal.RequireFromString("0.3")
	blendSteepnessDiv    = decimal.RequireFromString("0.5")
	half                 = decimal.RequireFromString("0.5")
	thousand             = decimal.NewFromInt(1000)
)

// ── Staleness cadences (days) ────────────────────────────────────────────────────────────

const (
	netLiqStaleDays = 14 // weekly FRED H.4.1 cadence
	m2StaleDays     = 90 // monthly cadence + 3-8 week publish lag (ADR-046)
)

// ── YoY / acceleration windows ───────────────────────────────────────────────────────────

const (
	yoyDays              = 364 // ~52 weeks
	accelDays            = 91  // ~13 weeks
	m2YoYMonths          = 12
	m2AccelMonths        = 3
	blendSmoothingMonths = 3
)

// ── ADR-068 tip guards ───────────────────────────────────────────────────────────────────

var m2CNTipTolerance = decimal.RequireFromString("0.30")

const m2CNTipMaxGapMonths = 18

// m2CNSourcePrecedence is the ADR-068 overwrite precedence for M2_CN rows, lowest wins.
// A candidate may overwrite a row only when its own precedence is at least as good —
// scraped tips can never clobber a backbone month, YCharts can never clobber a
// TradingEconomics/PBC read of the same month. Unknown sources never win.
var m2CNSourcePrecedence = map[string]int{
	SourceManual:    0,
	SourceChinadata: 0,
	SourceTradingEC: 1,
	SourcePBC:       1,
	SourceYCharts:   2,
}

// ── Component cadence table (COMPONENT_CADENCE_DAYS, insertion-ordered) ──────────────────

type componentCadence struct {
	series  string
	maxDays int
}

var componentCadences = []componentCadence{
	{SeriesNetLiq, netLiqStaleDays},
	{SeriesGlobalM2, m2StaleDays},
	{seriesM2SL, m2StaleDays},
	{seriesM2EA, m2StaleDays},
	{seriesM2UK, m2StaleDays},
	{SeriesM2CN, m2StaleDays},
	{SeriesM2JP, m2StaleDays},
}

// MacroPoint is one dated observation — the direct port of pl.investing.client.MacroPoint.
type MacroPoint struct {
	Date  time.Time // calendar date (UTC, day precision)
	Value decimal.Decimal
}

// ComponentStatus is one input series' freshness/provenance for the state panel — the service
// layer shape of MacroComponentStatusDto.
type ComponentStatus struct {
	SeriesCode            string
	Source                string
	LatestObservationDate time.Time
	LatestValue           decimal.Decimal
	Stale                 bool
}

// BlendPoint is one month of the ADR-044 blend-factor series.
type BlendPoint struct {
	Month      time.Time // first of the calendar month (UTC)
	CompositeZ *decimal.Decimal
	SmoothedZ  *decimal.Decimal
	Blend      *decimal.Decimal
}

// ── Exact HALF_UP decimal arithmetic ───────────────────────────────────────────────────── The
// original Java math (BigDecimal.divide(d, scale, HALF_UP) / setScale(scale, HALF_UP)) rounds half
// away from zero. shopspring/decimal's DivRound uses the same convention for the common cases, but
// the tie behavior is not contractual — so every scaled division and rescale in the blend math goes
// through divHalfUp/roundHalfUp below, making the rounding explicit rather than library folklore.
// Ties at scale 4-10 are near-impossible on real data; this is about exactness discipline (the
// rebalancing gate reads blendFactor), not observed divergence.

// divHalfUp returns a/b rounded to scale decimal places, half away from zero.
func divHalfUp(a, b decimal.Decimal, scale int32) decimal.Decimal {
	n := a.Mul(decimal.NewFromInt(1).Shift(scale))
	q, r := n.QuoRem(b, 0)
	if r.IsZero() {
		return decimal.NewFromBigInt(q.BigInt(), -scale)
	}
	if r.Abs().Mul(decimal.NewFromInt(2)).Cmp(b.Abs()) >= 0 {
		if n.Sign()*b.Sign() >= 0 {
			q = q.Add(decimal.NewFromInt(1))
		} else {
			q = q.Add(decimal.NewFromInt(-1))
		}
	}
	return decimal.NewFromBigInt(q.BigInt(), -scale)
}

// roundHalfUp returns d rescaled to scale decimal places, half away from zero — the direct
// port of BigDecimal.setScale(scale, RoundingMode.HALF_UP).
func roundHalfUp(d decimal.Decimal, scale int32) decimal.Decimal {
	return divHalfUp(d, decimal.NewFromInt(1), scale)
}

// warsawZone is the business-date zone (pl.investing.util.WarsawTime) — every STORED business
// date (as_of_date fallback, "today" for staleness) is Europe/Warsaw, never the container's
// default zone.
func warsawToday() time.Time {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		return time.Now().UTC().Truncate(24 * time.Hour)
	}
	return time.Now().In(loc)
}

// startOfDayUTC normalizes a calendar date to midnight UTC for storage/comparison.
func startOfDayUTC(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
