package coretest

import (
	"context"
	"errors"
	"maps"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/siblingcore"
)

// ErrBoom is the injected failure of Book.FailPrices / FailReads.
var ErrBoom = errors.New("boom")

// Book is an in-memory database for one app: the row sets the siblingcore ports read and the
// writes they make. Reads follow the adapters' semantics (portfolio filter, live rows only, the
// SQL ORDER BY of each query); every write is appended to Writes so tests can assert order.
type Book struct {
	Assets    []siblingcore.Asset
	Subs      []siblingcore.Subasset
	Deposits  []siblingcore.Deposit
	Prices    map[string]decimal.Decimal // price_cache: yahoo ticker -> PLN
	Rates     map[string]decimal.Decimal // fx_rate_cache (PLN = 1 is added by AllRatesToPln)
	FetchedAt map[string]time.Time

	FailPrices bool // PricePlnByYahooTicker / AllRatesToPln / FetchedAtByYahooTicker fail
	FailReads  bool // ListAssets / ListSubassets fail

	Writes []string
	nextID int64
}

// NextID returns a fresh row id.
func (b *Book) NextID() int64 {
	b.nextID++
	return 1000 + b.nextID
}

// Log records one write.
func (b *Book) Log(w string) { b.Writes = append(b.Writes, w) }

func (b *Book) ListAssets(_ context.Context, p siblingcore.Portfolio) ([]siblingcore.Asset, error) {
	if b.FailReads {
		return nil, ErrBoom
	}
	var out []siblingcore.Asset
	for _, a := range b.Assets {
		if a.Portfolio == p && !a.Deleted {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (b *Book) ListSubassets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error) {
	all, err := b.ListSubassetsIncludingDeleted(ctx, p)
	if err != nil {
		return nil, err
	}
	var out []siblingcore.Subasset
	for _, s := range all {
		if !s.Deleted {
			out = append(out, s)
		}
	}
	return out, nil
}

func (b *Book) ListSubassetsIncludingDeleted(_ context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error) {
	if b.FailReads {
		return nil, ErrBoom
	}
	var out []siblingcore.Subasset
	for _, s := range b.Subs {
		if s.Portfolio == p {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ticker < out[j].Ticker })
	return out, nil
}

func (b *Book) PricePlnByYahooTicker(context.Context) (map[string]decimal.Decimal, error) {
	if b.FailPrices {
		return nil, ErrBoom
	}
	out := make(map[string]decimal.Decimal, len(b.Prices))
	maps.Copy(out, b.Prices)
	return out, nil
}

func (b *Book) AllRatesToPln(context.Context) (map[string]decimal.Decimal, error) {
	if b.FailPrices {
		return nil, ErrBoom
	}
	out := map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}
	maps.Copy(out, b.Rates)
	return out, nil
}

func (b *Book) FetchedAtByYahooTicker(context.Context) (map[string]time.Time, error) {
	if b.FailPrices {
		return nil, ErrBoom
	}
	return b.FetchedAt, nil
}

func (b *Book) ListDepositsAsc(_ context.Context, p siblingcore.Portfolio) ([]siblingcore.Deposit, error) {
	var out []siblingcore.Deposit
	for _, d := range b.Deposits {
		if d.Portfolio == p {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].DepositDate.Equal(out[j].DepositDate) {
			return out[i].DepositDate.Before(out[j].DepositDate)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (b *Book) InsertDeposit(_ context.Context, d siblingcore.Deposit) (siblingcore.Deposit, error) {
	d.ID = b.NextID()
	d.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	b.Deposits = append(b.Deposits, d)
	b.Log("deposit " + string(d.Portfolio) + " " + d.Amount.String())
	return d, nil
}

// Sub returns a pointer to the stored subasset with id (nil when unknown).
func (b *Book) Sub(id int64) *siblingcore.Subasset {
	for i := range b.Subs {
		if b.Subs[i].ID == id {
			return &b.Subs[i]
		}
	}
	return nil
}

func (b *Book) WriteHolding(_ context.Context, id int64, h siblingcore.Holding) error {
	s := b.Sub(id)
	if s == nil {
		return errors.New("coretest: no subasset")
	}
	s.Holding = h
	b.Log("holding " + s.Ticker)
	return nil
}

func (b *Book) WriteCost(_ context.Context, id int64, cost *decimal.Decimal) error {
	s := b.Sub(id)
	if s == nil {
		return errors.New("coretest: no subasset")
	}
	s.Holding.TotalSpentPln = cost
	b.Log("cost " + s.Ticker)
	return nil
}

// U builds Units from bucket/amount pairs ("IKE_TATA", "1.5", …).
func U(pairs ...string) siblingcore.Units {
	m := map[siblingcore.Bucket]decimal.Decimal{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[siblingcore.Bucket(pairs[i])] = decimal.RequireFromString(pairs[i+1])
	}
	return siblingcore.NewUnits(m)
}

// D parses a decimal.
func D(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// DP parses a decimal pointer.
func DP(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

// Day is a UTC calendar date.
func Day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Configs runs fn once per production configuration, with the portfolio each test should use.
func Configs(fn func(name string, cfg siblingcore.Config, p siblingcore.Portfolio)) {
	fn("portfolioB", PortfolioB, siblingcore.WholeBook)
	fn("portfolioC", PortfolioC, "VARIABLE")
}

// Bucket is the bucket a single-bucket test writes into for cfg (portfolioB IKE_TATA, portfolioC the
// single bucket).
func Bucket(cfg siblingcore.Config) siblingcore.Bucket { return cfg.Buckets[0].Bucket }
