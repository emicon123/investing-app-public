package siblingcore

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// Asset is one `assets` row (the logical holding that carries the targets).
type Asset struct {
	ID                         int64
	Portfolio                  Portfolio
	Key                        string
	Name                       string
	Exposure                   string
	AssetClass                 string
	TargetAllocationPct        *decimal.Decimal
	TargetAllocationPctRiskOff *decimal.Decimal
	Deleted                    bool // only *AnyState reads can return true
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// Subasset is one `positions` row (a concrete ticker inside an asset).
type Subasset struct {
	ID            int64
	AssetID       int64
	Portfolio     Portfolio
	IsPrimary     bool
	Ticker        string
	Name          string
	Exchange      string
	AssetClass    string
	Exposure      string
	PriceCurrency string
	Holding       Holding
	AvgBuyPrice   *decimal.Decimal
	GroupName     *string
	YahooTicker   *string
	SortOrder     int
	Deleted       bool // only *AnyState / IncludingDeleted reads can return true
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Deposit is one `deposits` row.
type Deposit struct {
	ID          int64
	Portfolio   Portfolio
	DepositDate time.Time
	Amount      decimal.Decimal
	Comment     *string
	CreatedAt   time.Time
}

// TxFunc runs fn inside one database transaction with a port value S bound to it. The
// pgtx.Runner / pgtx.SerializedRunner results are assignable to it (identical underlying type).
type TxFunc[S any] func(ctx context.Context, fn func(S) error) error

// PriceSource is the cache-only price/FX view (never a live fetch).
type PriceSource interface {
	PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error)
	AllRatesToPln(ctx context.Context) (map[string]decimal.Decimal, error) // "PLN" = 1 always present
	FetchedAtByYahooTicker(ctx context.Context) (map[string]time.Time, error)
}
