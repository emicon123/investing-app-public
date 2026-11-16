package priceprovider

import (
	"context"

	"github.com/shopspring/decimal"
)

// FallbackProvider tries primary first and falls back to fallback on any error — the direct port
// of pl.investing.client.price.FallbackPriceProvider. Used to route BINANCE-exchange positions to
// Binance first, falling back to Yahoo Finance if Binance is unreachable (mirrors
// PriceProviderRegistry.forExchange's BINANCE branch).
type FallbackProvider struct {
	primary  PriceProvider
	fallback PriceProvider
}

// NewFallbackProvider wraps primary with fallback, tried in that order.
func NewFallbackProvider(primary, fallback PriceProvider) *FallbackProvider {
	return &FallbackProvider{primary: primary, fallback: fallback}
}

// GetPrice tries primary.GetPrice first; on any error, tries fallback.GetPrice.
func (f *FallbackProvider) GetPrice(ctx context.Context, ticker string) (decimal.Decimal, error) {
	price, err := f.primary.GetPrice(ctx, ticker)
	if err == nil {
		return price, nil
	}
	return f.fallback.GetPrice(ctx, ticker)
}

// Name composes "PRIMARY→FALLBACK", matching FallbackPriceProvider.name().
func (f *FallbackProvider) Name() string {
	return f.primary.Name() + "→" + f.fallback.Name()
}
