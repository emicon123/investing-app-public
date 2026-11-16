package store

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// The cache-only price/FX reads (siblingcore.PriceSource) come from the shared reference-table
// adapter bound to the same connection or transaction — never a live fetch.

// PricePlnByYahooTicker is price_cache as yahoo ticker → PLN price.
func (s *Queries) PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error) {
	return s.ref.PricePlnByYahooTicker(ctx)
}

// AllRatesToPln is fx_rate_cache as currency → PLN rate (PLN = 1 always present).
func (s *Queries) AllRatesToPln(ctx context.Context) (map[string]decimal.Decimal, error) {
	return s.ref.AllRatesToPln(ctx)
}

// FetchedAtByYahooTicker is when each cached price was fetched.
func (s *Queries) FetchedAtByYahooTicker(ctx context.Context) (map[string]time.Time, error) {
	return s.ref.FetchedAtByYahooTicker(ctx)
}
