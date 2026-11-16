package position

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// fakePriceSource is a fixed cache view for the integration tests that pin prices directly.
type fakePriceSource struct {
	prices map[string]decimal.Decimal
	rates  map[string]decimal.Decimal
}

func (f *fakePriceSource) PricePlnByYahooTicker(context.Context) (map[string]decimal.Decimal, error) {
	return f.prices, nil
}
func (f *fakePriceSource) AllRatesToPln(context.Context) (map[string]decimal.Decimal, error) {
	return f.rates, nil
}
func (f *fakePriceSource) FetchedAtByYahooTicker(context.Context) (map[string]time.Time, error) {
	return map[string]time.Time{}, nil
}
