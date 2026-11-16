package rebalancing

import (
	"fmt"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/position"
	"investing-app/common-go/pgnum"
)

// decodeToMap builds the (ticker, portfolio) -> row map every leg resolves against — the direct
// port of RebalancingTransferService's own PositionKey map, matching its comment verbatim:
// ticker alone is unsafe since `positions` has a (ticker, exchange, portfolio) unique
// constraint, not a ticker-alone one (ADR-063).
func decodeToMap(rows []sqlcgen.Position) (map[positionKey]*row, error) {
	out := make(map[positionKey]*row, len(rows))
	for _, r := range rows {
		units, err := decodeUnits(r)
		if err != nil {
			return nil, err
		}
		out[positionKey{ticker: r.Ticker, portfolio: string(r.Portfolio)}] = &row{
			id: r.ID, assetID: r.AssetID, yahooTicker: r.YahooTicker,
			ticker: r.Ticker, portfolio: string(r.Portfolio), exchange: string(r.Exchange), units: units,
		}
	}
	return out, nil
}

func decodeUnits(r sqlcgen.Position) (position.PositionUnits, error) {
	ike, err := pgnum.ToDecimal(r.UnitsIke)
	if err != nil {
		return position.PositionUnits{}, fmt.Errorf("rebalancing: decode units_ike for %s: %w", r.Ticker, err)
	}
	ikze, err := pgnum.ToDecimal(r.UnitsIkze)
	if err != nil {
		return position.PositionUnits{}, fmt.Errorf("rebalancing: decode units_ikze for %s: %w", r.Ticker, err)
	}
	eur, err := pgnum.ToDecimal(r.UnitsRegularEur)
	if err != nil {
		return position.PositionUnits{}, fmt.Errorf("rebalancing: decode units_regular_eur for %s: %w", r.Ticker, err)
	}
	usd, err := pgnum.ToDecimal(r.UnitsRegularUsd)
	if err != nil {
		return position.PositionUnits{}, fmt.Errorf("rebalancing: decode units_regular_usd for %s: %w", r.Ticker, err)
	}
	pln, err := pgnum.ToDecimal(r.UnitsRegularPln)
	if err != nil {
		return position.PositionUnits{}, fmt.Errorf("rebalancing: decode units_regular_pln for %s: %w", r.Ticker, err)
	}
	// NULL-preserving: a sell on a NULL cost basis must write back NULL, not 0 (see
	// position.PositionUnits.TotalSpentPln).
	totalSpent, err := pgnum.ToDecimalPtr(r.TotalSpentPln)
	if err != nil {
		return position.PositionUnits{}, fmt.Errorf("rebalancing: decode total_spent_pln for %s: %w", r.Ticker, err)
	}
	return position.PositionUnits{
		Exchange: string(r.Exchange), PriceCurrency: string(r.PriceCurrency),
		UnitsIke: ike, UnitsIkze: ikze, UnitsRegularEur: eur, UnitsRegularUsd: usd, UnitsRegularPln: pln,
		TotalSpentPln: totalSpent,
	}, nil
}

func toDeltaParams(r *row) sqlcgen.UpdatePositionDeltaParams {
	return position.ToDeltaParams(r.id, r.units)
}
