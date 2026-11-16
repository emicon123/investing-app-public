// Composition-root glue: adapters that satisfy the domain packages' price/FX interfaces over the
// marketdata service. Both directions stay interface-typed, so no domain package imports another.
package main

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"investing-app/common-go/marketdatacore"
)

// moneyClusterPriceAdapter routes the money cluster's read-only price/FX needs
// (rebalancingcalc.Calculate, position's priceNative computation, deposit's XIRR currency
// conversion) through the marketdata service — the composition-root glue between the domain
// packages (both directions stay interface-typed, so neither package imports the other), the same
// convention as botPriceAdapter/historiaFxAdapter. One concrete adapter satisfies
// rebalancingcalc.PriceSource, position.PriceSource, deposit.PriceSource, and
// purchasesession.PriceSource simultaneously (Go's structural typing).
type moneyClusterPriceAdapter struct {
	marketData *marketdatacore.Service
}

// PricePlnByYahooTicker returns every cached PLN price by Yahoo ticker — cache-only, no fetch.
func (a moneyClusterPriceAdapter) PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error) {
	entries, err := a.marketData.GetAllPriceEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]decimal.Decimal, len(entries))
	for _, e := range entries {
		out[e.Ticker] = e.PricePln
	}
	return out, nil
}

// AllRatesToPln returns every cached rate to PLN — cache-only, no fetch, PLN=1 always present.
func (a moneyClusterPriceAdapter) AllRatesToPln(ctx context.Context) (map[string]decimal.Decimal, error) {
	entries, err := a.marketData.GetAllFxRateEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]decimal.Decimal, len(entries))
	for _, e := range entries {
		out[e.FromCurrency] = e.RateToPln
	}
	return out, nil
}

// RateToPln returns one currency's rate to PLN — MAY trigger a live fetch on a stale/missing cache
// entry.
func (a moneyClusterPriceAdapter) RateToPln(ctx context.Context, currency string) (decimal.Decimal, error) {
	return a.marketData.GetRateToPln(ctx, currency)
}

// botPriceAdapter routes the bot subsystem's read-only price needs through the marketdata service —
// the composition-root glue between the domain packages (both directions stay interface-typed, so
// neither package imports the other). BTCPrice is cache-only (GetAllPriceEntries, no external
// fetch); FxRateToPln may refresh a stale rate (the audit-column path).
type botPriceAdapter struct {
	marketData *marketdatacore.Service
}

func (a botPriceAdapter) BTCPrice(ctx context.Context) (decimal.Decimal, error) {
	entries, err := a.marketData.GetAllPriceEntries(ctx)
	if err != nil {
		return decimal.Zero, err
	}
	for _, e := range entries {
		if e.Ticker == "BTC-USD" {
			return e.Price, nil
		}
	}
	return decimal.Zero, fmt.Errorf("bot: no BTC-USD price available")
}

func (a botPriceAdapter) FxRateToPln(ctx context.Context, currency string) (decimal.Decimal, error) {
	return a.marketData.GetRateToPln(ctx, currency)
}

// historiaFxAdapter routes historia's PLN conversions through the marketdata service — the
// composition-root glue between the two domain packages (both directions stay interface-typed, so
// neither package imports the other).
type historiaFxAdapter struct {
	marketData *marketdatacore.Service
}

func (a historiaFxAdapter) GetRateToPln(ctx context.Context, currency string) (decimal.Decimal, error) {
	return a.marketData.GetRateToPln(ctx, currency)
}

func (a historiaFxAdapter) AllRates(ctx context.Context) (map[string]decimal.Decimal, error) {
	entries, err := a.marketData.GetAllFxRateEntries(ctx)
	if err != nil {
		return nil, err
	}
	rates := make(map[string]decimal.Decimal, len(entries))
	for _, e := range entries {
		rates[e.FromCurrency] = e.RateToPln
	}
	return rates, nil
}
