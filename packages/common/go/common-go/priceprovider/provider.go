// Package priceprovider defines the PriceProvider Strategy interface and its real Yahoo Finance /
// Binance implementations (the design improvement doc 13 §7 names: the Java backend had no
// PriceProvider interface — each client was called directly), plus the CachingDecorator around
// them. This is the reusable price-fetching foundation crypto trades, the bot subsystem and
// rebalancing depend on.
package priceprovider

import (
	"context"

	"github.com/shopspring/decimal"
)

// PriceProvider is a Strategy — one implementation per upstream price source (Yahoo, Binance), per
// doc 13 §1 ("Strategy — adopted, carried forward... no redesign needed") since this shape is
// already idiomatic Go.
type PriceProvider interface {
	// GetPrice returns the latest known price for ticker, in the provider's native currency. A
	// non-nil error means "unavailable" — never a panic; every implementation in this package fails
	// soft ("never crash the API endpoint on a price fetch failure").
	GetPrice(ctx context.Context, ticker string) (decimal.Decimal, error)

	// Name identifies the provider for logging/observability, used by FallbackProvider to compose a
	// "PRIMARY→FALLBACK" label and by callers logging which source actually answered.
	Name() string
}
