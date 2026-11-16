package rebalancingcalc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgnum"
)

// TryTotalPortfolioValuePln is the per-portfolio total for the positions listing: (total, true) on
// success, or (zero, false) when computeValues fails for this portfolio (e.g. a missing price) —
// the positions listing degrades gracefully (null computed fields for that portfolio) rather than
// failing the whole /positions request, unlike Calculate, which lets a computeValues error
// propagate as a real failure. A zero-or-negative total is treated the same as "unavailable".
func TryTotalPortfolioValuePln(ctx context.Context, q Queries, prices PriceSource, portfolio string) (decimal.Decimal, bool) {
	computation, err := computeValues(ctx, q, prices, portfolio)
	if err != nil {
		return decimal.Zero, false
	}
	total := decimal.Zero
	for _, v := range computation.valuePln {
		total = total.Add(v)
	}
	if total.Sign() <= 0 {
		return decimal.Zero, false
	}
	return total, true
}

// TotalPortfolioValuePln mirrors XirrService.computeXirr's direct, uncaught call to
// `valueComputationService.computeValues(portfolio).valuePln().values()...reduce`: a missing
// price propagates as a real error (503), unlike TryTotalPortfolioValuePln's PositionService-style
// graceful degradation. Used by internal/deposit's XIRR computation.
func TotalPortfolioValuePln(ctx context.Context, q Queries, prices PriceSource, portfolio string) (decimal.Decimal, error) {
	computation, err := computeValues(ctx, q, prices, portfolio)
	if err != nil {
		return decimal.Zero, err
	}
	total := decimal.Zero
	for _, v := range computation.valuePln {
		total = total.Add(v)
	}
	return total, nil
}

// computationResult is the direct port of ValueComputationService.ComputationResult, plus the
// fxRates/pricePlnByTicker maps it already fetched internally — exposed so Calculate doesn't
// re-fetch the identical maps a second time right after calling computeValues.
type computationResult struct {
	valuePln          map[string]decimal.Decimal // primary ticker -> value_pln, one entry per asset
	tradablePositions []PositionRow
	fxRates           map[string]decimal.Decimal
	pricePlnByTicker  map[string]decimal.Decimal
}

// computeValues values every position (ported from the Java ValueComputationService). Subasset
// values fold into their asset's entry (task 156 — value_pln(a) = Σ subassets, grouped by the
// asset_id FK instead of the dropped companion_parent_ticker string match); primaries alone appear
// as map keys, keyed by primary ticker. A missing price for ANY primary or subasset position fails
// loudly (never value-at-zero: a zero value would make the rebalancer see the asset as maximally
// underweight and dump the whole budget into it).
//
// Math-identity note: the map has one entry per group before and after (old parent ticker ↔
// new primary ticker — the migration makes the old main row the primary), with the same
// addends (same rows, same units, same prices) — only the grouping key changed, from string
// match to FK equality. |ΣQ_i − A|, |ΣV_i − A| and every buy-list
// figure are cardinality-and-value-preserving under that bijection.
func computeValues(ctx context.Context, q Queries, prices PriceSource, portfolio string) (computationResult, error) {
	rows, err := q.ListAllPositions(ctx)
	if err != nil {
		return computationResult{}, fmt.Errorf("rebalancingcalc: list positions: %w", err)
	}
	assetRows, err := q.ListAllAssets(ctx)
	if err != nil {
		return computationResult{}, fmt.Errorf("rebalancingcalc: list assets: %w", err)
	}
	all, err := DecodeAllPositions(rows, assetRows)
	if err != nil {
		return computationResult{}, err
	}

	fxRates, err := prices.AllRatesToPln(ctx)
	if err != nil {
		return computationResult{}, fmt.Errorf("rebalancingcalc: load fx rates: %w", err)
	}
	pricePlnByTicker, err := prices.PricePlnByYahooTicker(ctx)
	if err != nil {
		return computationResult{}, fmt.Errorf("rebalancingcalc: load prices: %w", err)
	}

	var tradable, subassets []PositionRow
	for _, p := range all {
		if p.Portfolio != portfolio {
			continue
		}
		if p.IsPrimary {
			tradable = append(tradable, p)
		} else {
			subassets = append(subassets, p)
		}
	}

	subassetValues := map[int64]decimal.Decimal{}
	for _, s := range subassets {
		priceNativePln, ok := nativePricePln(s, fxRates, pricePlnByTicker)
		if !ok {
			return computationResult{}, priceUnavailable(s)
		}
		val := s.UnitsTotal().Mul(priceNativePln)
		subassetValues[s.Asset.ID] = subassetValues[s.Asset.ID].Add(val)
	}

	valuePln := make(map[string]decimal.Decimal, len(tradable))
	for _, p := range tradable {
		priceNativePln, ok := nativePricePln(p, fxRates, pricePlnByTicker)
		if !ok {
			return computationResult{}, priceUnavailable(p)
		}
		value := p.UnitsTotal().Mul(priceNativePln)
		if sv, has := subassetValues[p.Asset.ID]; has {
			value = value.Add(sv)
		}
		valuePln[p.Ticker] = value
	}

	return computationResult{
		valuePln: valuePln, tradablePositions: tradable,
		fxRates: fxRates, pricePlnByTicker: pricePlnByTicker,
	}, nil
}

// nativePricePln resolves a position's native price in PLN. Returns (zero, false) when there is
// none: no yahooTicker, or the price cache has no entry for it. CHF is a special case (price IS the
// FX rate itself) with no 1.0 fallback — a missing CHF rate is also (zero, false).
func nativePricePln(p PositionRow, fxRates, pricePlnByTicker map[string]decimal.Decimal) (decimal.Decimal, bool) {
	if p.YahooTicker == nil || *p.YahooTicker == "" {
		return decimal.Zero, false
	}
	if p.PriceCurrency == "CHF" {
		rate, ok := fxRates["CHF"]
		return rate, ok
	}
	price, ok := pricePlnByTicker[*p.YahooTicker]
	return price, ok
}

func priceUnavailable(p PositionRow) *UnavailableError {
	ticker := p.Ticker
	if p.YahooTicker != nil && *p.YahooTicker != "" {
		ticker = *p.YahooTicker
	}
	return &UnavailableError{msg: "Price unavailable for ticker: " + ticker}
}

// loadCashSnapshotTotal is the direct port of ValueComputationService.loadCashSnapshotTotal —
// SUM of the latest cash_positions snapshot, or zero if none exists.
func loadCashSnapshotTotal(ctx context.Context, q Queries) (decimal.Decimal, error) {
	latest, err := q.LatestCashRecordedAt(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, nil
		}
		return decimal.Zero, fmt.Errorf("rebalancingcalc: latest cash recorded_at: %w", err)
	}
	rows, err := q.CashPositionsByRecordedAt(ctx, latest)
	if err != nil {
		return decimal.Zero, fmt.Errorf("rebalancingcalc: read cash snapshot: %w", err)
	}
	total := decimal.Zero
	for _, r := range rows {
		amt, err := pgnum.ToDecimal(r.AmountPln)
		if err != nil {
			return decimal.Zero, fmt.Errorf("rebalancingcalc: decode cash amount_pln: %w", err)
		}
		total = total.Add(amt)
	}
	return total, nil
}
