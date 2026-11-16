// Package snapshot writes the portfolioB + portfolioC daily historia rows (ADR-113): one row per portfolio
// per date with the book's value from the price/FX caches, the total deposited, the XIRR at that
// date and the per-position values. SnapshotAll is the nightly job; BackfillSnapshot is the
// ADR-097 recovery entry point for a missed night.
package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/xirr"
)

// Store is the snapshotter's port (cache-only prices: a snapshot never fetches).
type Store interface {
	ListSubassets(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Subasset, error)
	PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error)
	AllRatesToPln(ctx context.Context) (map[string]decimal.Decimal, error)
	ListDepositsAsc(ctx context.Context, p siblingcore.Portfolio) ([]siblingcore.Deposit, error)
	UpsertSnapshot(ctx context.Context, s Snapshot) error
}

// Snapshot is one `historia` row to upsert (on (snapshot_date) in portfolioB, (portfolio,
// snapshot_date) in portfolioC — the adapter's query decides).
type Snapshot struct {
	Portfolio          siblingcore.Portfolio
	Date               time.Time
	TotalDeposits      decimal.Decimal
	PortfolioValue     decimal.Decimal
	Xirr               *decimal.Decimal
	PositionValuesJSON []byte // historiaquery.MarshalPositionValues output
}

// Snapshotter writes historia rows.
type Snapshotter struct {
	cfg   siblingcore.Config
	store Store
	now   func() time.Time
}

// NewSnapshotter: now nil = time.Now.
func NewSnapshotter(cfg siblingcore.Config, store Store, now func() time.Time) *Snapshotter {
	if now == nil {
		now = time.Now
	}
	return &Snapshotter{cfg: cfg, store: store, now: now}
}

// SnapshotPortfolio upserts p's row for today's Warsaw date.
func (s *Snapshotter) SnapshotPortfolio(ctx context.Context, p siblingcore.Portfolio) error {
	return s.snapshotAsOf(ctx, p, historiaquery.WarsawDate(s.now()))
}

// BackfillSnapshot upserts p's row for date (its calendar day), valued at today's cached prices
// (ADR-097: the operator checks the trade window before calling it).
func (s *Snapshotter) BackfillSnapshot(ctx context.Context, p siblingcore.Portfolio, date time.Time) error {
	y, m, d := date.Date()
	return s.snapshotAsOf(ctx, p, time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

// SnapshotAll snapshots every configured portfolio; one portfolio's failure never skips the
// others; the returned error is errors.Join of the failures. It is the job each app hands to
// historiascheduler.RunDaily.
func (s *Snapshotter) SnapshotAll(ctx context.Context) error {
	var errs []error
	for _, p := range s.cfg.Portfolios {
		if err := s.SnapshotPortfolio(ctx, p); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Snapshotter) snapshotAsOf(ctx context.Context, p siblingcore.Portfolio, asOf time.Time) error {
	if err := s.cfg.CheckPortfolio("portfolio", p); err != nil {
		return err
	}
	values := s.valuesOrZero(ctx, p)
	deposits, err := s.store.ListDepositsAsc(ctx, p)
	if err != nil {
		return fmt.Errorf("historia: snapshot%s deposits: %w", named(p), err)
	}
	total := decimal.Zero
	for _, d := range deposits {
		total = total.Add(d.Amount)
	}
	positionValues, err := historiaquery.MarshalPositionValues(values)
	if err != nil {
		return fmt.Errorf("historia: snapshot%s position values: %w", named(p), err)
	}
	value := sum(values)
	if err := s.store.UpsertSnapshot(ctx, Snapshot{
		Portfolio: p, Date: asOf, TotalDeposits: total, PortfolioValue: value,
		Xirr: xirrAt(deposits, value, asOf), PositionValuesJSON: positionValues,
	}); err != nil {
		return fmt.Errorf("historia: snapshot%s upsert: %w", named(p), err)
	}
	slog.Info("historia: snapshot stored", append(portfolioAttr(p),
		"date", asOf.Format("2006-01-02"), "value", value.String(), "deposits", total.String())...)
	return nil
}

// valuesOrZero is the value per primary ticker: its own units × price plus every other subasset
// of its asset priced (an unpriced subasset is skipped, an unpriced primary counts zero). A read
// failure degrades to an empty map (the row still records deposits and XIRR).
func (s *Snapshotter) valuesOrZero(ctx context.Context, p siblingcore.Portfolio) map[string]decimal.Decimal {
	subs, err := s.store.ListSubassets(ctx, p)
	if err != nil {
		slog.Warn("historia: could not read positions (will store zero)", append(portfolioAttr(p), "error", err)...)
		return map[string]decimal.Decimal{}
	}
	rates, err := s.store.AllRatesToPln(ctx)
	if err != nil {
		slog.Warn("historia: could not read fx rates", append(portfolioAttr(p), "error", err)...)
		rates = map[string]decimal.Decimal{}
	}
	prices, err := s.store.PricePlnByYahooTicker(ctx)
	if err != nil {
		slog.Warn("historia: could not read prices", append(portfolioAttr(p), "error", err)...)
		prices = map[string]decimal.Decimal{}
	}
	primaryTicker := map[int64]string{}
	for _, sub := range subs {
		if sub.IsPrimary {
			primaryTicker[sub.AssetID] = sub.Ticker
		}
	}
	siblings := map[string]decimal.Decimal{}
	for _, sub := range subs {
		if sub.IsPrimary {
			continue
		}
		primary, ok := primaryTicker[sub.AssetID]
		if !ok {
			continue
		}
		price, ok := pricePln(sub, prices, rates)
		if !ok {
			slog.Warn("historia: no price for subasset, skipping", append(portfolioAttr(p), "ticker", sub.Ticker)...)
			continue
		}
		siblings[primary] = siblings[primary].Add(sub.Holding.Units.Total().Mul(price))
	}
	values := map[string]decimal.Decimal{}
	for _, sub := range subs {
		if !sub.IsPrimary {
			continue
		}
		price, ok := pricePln(sub, prices, rates)
		if !ok {
			slog.Warn("historia: no price for position, using zero", append(portfolioAttr(p), "ticker", sub.Ticker)...)
			price = decimal.Zero
		}
		values[sub.Ticker] = sub.Holding.Units.Total().Mul(price).Add(siblings[sub.Ticker])
	}
	return values
}

// pricePln: a CHF holding is priced at the CHF rate (1 when the rate is missing); everything else
// by its yahoo ticker from the price cache.
func pricePln(sub siblingcore.Subasset, prices, rates map[string]decimal.Decimal) (decimal.Decimal, bool) {
	if sub.PriceCurrency == "CHF" {
		if rate, ok := rates["CHF"]; ok {
			return rate, true
		}
		return decimal.NewFromInt(1), true
	}
	if sub.YahooTicker == nil || *sub.YahooTicker == "" {
		return decimal.Zero, false
	}
	price, ok := prices[*sub.YahooTicker]
	return price, ok
}

// xirrAt solves the deposits (negative) against value on asOf; nil without deposits or without a
// solution.
func xirrAt(deposits []siblingcore.Deposit, value decimal.Decimal, asOf time.Time) *decimal.Decimal {
	if len(deposits) == 0 {
		return nil
	}
	flows := make([]xirr.Flow, 0, len(deposits)+1)
	for _, d := range deposits {
		flows = append(flows, xirr.Flow{Date: d.DepositDate, Amount: d.Amount.Neg()})
	}
	return xirr.Solve(append(flows, xirr.Flow{Date: asOf, Amount: value}))
}

func sum(values map[string]decimal.Decimal) decimal.Decimal {
	total := decimal.Zero
	for _, v := range values {
		total = total.Add(v)
	}
	return total
}

// named / portfolioAttr name a named portfolio in error texts and log lines (omitted for WholeBook).
func named(p siblingcore.Portfolio) string {
	if p == siblingcore.WholeBook {
		return ""
	}
	return " " + string(p)
}

func portfolioAttr(p siblingcore.Portfolio) []any {
	if p == siblingcore.WholeBook {
		return nil
	}
	return []any{"portfolio", string(p)}
}
