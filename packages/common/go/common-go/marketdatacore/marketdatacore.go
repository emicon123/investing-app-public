// Package marketdatacore is the shared market-data (prices + FX rates) kernel for portfolioB and portfolioC
// (ADR-094 / doc 13 §7) and, since task 178, main (which supplies the same kind of Store adapter
// over its sqlc queries). The Java controller and the PriceCacheService/FxRateCacheService business
// rules were genuinely identical across the apps, so one Go implementation serves all three:
// BINANCE-exchange tickers go through Binance with a Yahoo Finance fallback, everything else
// through Yahoo Finance directly; prices/FX rates are cached in Postgres with a 60-minute FX TTL
// and cache-only reads (never a live fetch from GET endpoints); prices refresh on one 15-minute
// scheduler. The Java portfolioB service carried dead ceremony (a PriceProviderRegistry +
// LoggingPriceProviderDecorator + ApplicationEvent path) that doc 13 §5 says not to carry forward:
// a plain slog.Info after refresh (already present in Service.ForceRefreshAllPrices) reproduces its
// only real effect without the Observer/Decorator/Factory ceremony.
//
// Unlike settingscore (whose per-key validation genuinely differs per app), there is no
// per-app-varying business step here at all — the ENTIRE Service below is shared, going further
// than settingscore's Store+Validator split needs to.
//
// The one unavoidable per-Go-module seam, same as every other shared kernel here: portfolioB and portfolioC
// are separate Go modules with their own sqlc-generated types, so this package cannot reference
// either app's *sqlcgen.Queries directly. Store below uses only plain Go types (decimal.Decimal,
// string, time.Time) so each app's own internal/marketdata package can satisfy it with a thin
// adapter — the same shape as accountcash.Store.
package marketdatacore

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// ErrNotFound is Store's sentinel for "no fx_rate_cache row for this currency."
var ErrNotFound = errors.New("marketdatacore: not found")

// PriceEntry is the service-layer shape for one price_cache row.
type PriceEntry struct {
	Ticker    string
	Price     decimal.Decimal
	Currency  string
	PricePln  decimal.Decimal
	FetchedAt time.Time
}

// FxRateEntry is the service-layer shape for one fx_rate_cache row (plus the synthetic PLN=1.0
// entry that is never actually stored).
type FxRateEntry struct {
	FromCurrency string
	RateToPln    decimal.Decimal
	Source       string
	FetchedAt    time.Time
}

// TrackedTicker names one ticker discovered from `positions` (read-only — doc 13 §6.3) plus the
// price currency and exchange needed to route/convert it.
type TrackedTicker struct {
	YahooTicker   string
	PriceCurrency string
	Exchange      string
}

// RefreshResult reports how many of the tracked tickers were refreshed successfully — the direct
// port of pl.shared.dto.marketdata.PriceRefreshResult. Field names/shape matter: this is what
// actually goes out on the wire for POST /market-data/prices/refresh (raw, no {data,meta}
// envelope — see each app's own handler.go).
type RefreshResult struct {
	Total     int             `json:"total"`
	Succeeded int             `json:"succeeded"`
	Failures  []TickerFailure `json:"failures"`
}

// TickerFailure names one ticker that failed to refresh and why.
type TickerFailure struct {
	Ticker string `json:"ticker"`
	Reason string `json:"reason"`
}

// Store is the one per-app seam: price_cache/fx_rate_cache read+write plus the read-only
// `positions` lookup for tracked tickers. A missing fx_rate_cache row on GetFxRate maps to
// ErrNotFound.
type Store interface {
	ListPrices(ctx context.Context) ([]PriceEntry, error)
	UpsertPrice(ctx context.Context, entry PriceEntry) error
	ListFxRates(ctx context.Context) ([]FxRateEntry, error)
	GetFxRate(ctx context.Context, currency string) (FxRateEntry, error)
	UpsertFxRate(ctx context.Context, entry FxRateEntry) error
	ListTrackedTickers(ctx context.Context) ([]TrackedTicker, error)
}
