// Package backfill holds the pure kernels shared by portfolioB's and portfolioC's historia
// position-values backfill (ADR-097 recovery path; also run by each app's startup
// "historia-backfill" worker): rewinding today's units to a past day, and scaling a raw
// per-ticker breakdown to the stored total. The per-app orchestration (portfolio loop, cutoff,
// pricing, subasset folding, warning texts) stays in each app's internal/admin.
package backfill

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
)

// UnitChange is one recorded change to a subasset's units: a purchase lot (+units) or one leg of
// a position transfer (signed units_delta). Day is the calendar day (UTC midnight) from which the
// change is held.
type UnitChange struct {
	Ticker string
	Day    time.Time
	Units  decimal.Decimal
}

// LotChange: a lot is held from its purchased_at date.
func LotChange(ticker string, purchasedAt time.Time, units decimal.Decimal) UnitChange {
	return UnitChange{Ticker: ticker, Day: calendarDay(purchasedAt), Units: units}
}

// TransferChange: a transfer leg is held from the Europe/Warsaw calendar day of transferredAt
// (the day the 23:00 snapshot first reflects it).
func TransferChange(ticker string, transferredAt time.Time, unitsDelta decimal.Decimal) UnitChange {
	return UnitChange{Ticker: ticker, Day: historiaquery.WarsawDate(transferredAt), Units: unitsDelta}
}

// Rewind indexes today's units and the recorded changes by ticker. Immutable after NewRewind.
type Rewind struct {
	current  map[string]decimal.Decimal
	byTicker map[string][]UnitChange
}

// NewRewind copies current (today's units per ticker) and indexes changes by ticker.
func NewRewind(current map[string]decimal.Decimal, changes []UnitChange) *Rewind {
	r := &Rewind{
		current:  make(map[string]decimal.Decimal, len(current)),
		byTicker: map[string][]UnitChange{},
	}
	for k, v := range current {
		r.current[k] = v
	}
	for _, c := range changes {
		r.byTicker[c.Ticker] = append(r.byTicker[c.Ticker], c)
	}
	return r
}

// UnitsAt is today's units of ticker minus every change whose Day is after day's calendar date,
// floored at zero. An unknown ticker counts as zero units today.
func (r *Rewind) UnitsAt(ticker string, day time.Time) decimal.Decimal {
	d := calendarDay(day)
	at := r.current[ticker]
	for _, c := range r.byTicker[ticker] {
		if c.Day.After(d) {
			at = at.Sub(c.Units)
		}
	}
	if at.IsNegative() {
		return decimal.Zero
	}
	return at
}

// calendarDay is t's own calendar date at UTC midnight.
func calendarDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ScaleToPortfolioValue scales raw values to sum exactly to portfolioValue (scale at 10dp, values
// at 2dp, the last key in sorted order plugs the rounding remainder, negatives clamp to zero).
// Sorted order keeps the plugged row deterministic. raw must be non-empty with a non-zero sum
// (callers skip such rows with a warning first).
func ScaleToPortfolioValue(raw map[string]decimal.Decimal, portfolioValue decimal.Decimal) map[string]decimal.Decimal {
	rawSum := decimal.Zero
	for _, v := range raw {
		rawSum = rawSum.Add(v)
	}
	scale := portfolioValue.DivRound(rawSum, 10)
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	scaled := make(map[string]decimal.Decimal, len(raw))
	scaledSum := decimal.Zero
	for i, k := range keys {
		v := raw[k].Mul(scale).Round(2)
		if i == len(keys)-1 {
			v = portfolioValue.Sub(scaledSum).Round(2)
		}
		if v.IsNegative() {
			v = decimal.Zero
		}
		scaled[k] = v
		scaledSum = scaledSum.Add(v)
	}
	return scaled
}
