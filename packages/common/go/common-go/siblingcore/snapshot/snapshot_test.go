package snapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/internal/coretest"
)

type fakeStore struct {
	*coretest.Book
	rows    []Snapshot
	failFor siblingcore.Portfolio
}

func (f *fakeStore) UpsertSnapshot(_ context.Context, s Snapshot) error {
	if s.Portfolio == f.failFor && f.failFor != "" {
		return errors.New("upsert failed")
	}
	f.rows = append(f.rows, s)
	return nil
}

// book: asset 1 = HA1 (primary, PLN 10) + HA2 (sibling, 20 PLN); a CHF holding; an unpriced one.
func book(cfg siblingcore.Config, p siblingcore.Portfolio) *fakeStore {
	b := string(coretest.Bucket(cfg))
	return &fakeStore{Book: &coretest.Book{
		Subs: []siblingcore.Subasset{
			{ID: 1, AssetID: 1, Portfolio: p, IsPrimary: true, Ticker: "HA1", PriceCurrency: "PLN", YahooTicker: new("HA1.WA"), Holding: siblingcore.Holding{Units: coretest.U(b, "6")}},
			{ID: 2, AssetID: 1, Portfolio: p, Ticker: "HA2", PriceCurrency: "EUR", YahooTicker: new("HA2.DE"), Holding: siblingcore.Holding{Units: coretest.U(b, "2")}},
			{ID: 3, AssetID: 2, Portfolio: p, IsPrimary: true, Ticker: "HC1", PriceCurrency: "CHF", Holding: siblingcore.Holding{Units: coretest.U(b, "100")}},
			{ID: 4, AssetID: 3, Portfolio: p, IsPrimary: true, Ticker: "HN1", PriceCurrency: "PLN", Holding: siblingcore.Holding{Units: coretest.U(b, "3")}},
		},
		Prices:   map[string]decimal.Decimal{"HA1.WA": coretest.D("10"), "HA2.DE": coretest.D("20")},
		Rates:    map[string]decimal.Decimal{"CHF": coretest.D("4.5")},
		Deposits: []siblingcore.Deposit{{ID: 1, Portfolio: p, DepositDate: coretest.Day(2025, 1, 1), Amount: coretest.D("300")}, {ID: 2, Portfolio: p, DepositDate: coretest.Day(2025, 6, 1), Amount: coretest.D("200")}},
	}}
}

var clock = func() time.Time { return time.Date(2026, 11, 5, 22, 30, 0, 0, time.UTC) } // 23:30 in Warsaw

func TestSnapshotPortfolio_ValuesTheBookAtTheWarsawDate(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		if err := NewSnapshotter(cfg, f, clock).SnapshotPortfolio(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		r := f.rows[0]
		if r.Portfolio != p || r.Date.Format("2006-01-02") != "2026-11-05" || !r.TotalDeposits.Equal(coretest.D("500")) || !r.PortfolioValue.Equal(coretest.D("550")) {
			t.Fatalf("%s: row %+v", name, r)
		}
		if string(r.PositionValuesJSON) != `{"HA1":100,"HC1":450,"HN1":0}` {
			t.Fatalf("%s: position values %s", name, r.PositionValuesJSON)
		}
		if r.Xirr == nil || r.Xirr.Sign() <= 0 {
			t.Fatalf("%s: xirr %v", name, r.Xirr)
		}
	})
}

func TestBackfillSnapshot_UsesTheGivenCalendarDayAndNoDepositsMeansNoXirr(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		f.Deposits = nil
		if err := NewSnapshotter(cfg, f, clock).BackfillSnapshot(context.Background(), p, time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if r := f.rows[0]; !r.Date.Equal(coretest.Day(2026, 10, 1)) || r.Xirr != nil || !r.TotalDeposits.IsZero() {
			t.Fatalf("%s: row %+v", name, r)
		}
	})
}

func TestSnapshot_ReadFailuresDegradeToZeroValues(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		f.FailReads = true
		if err := NewSnapshotter(cfg, f, clock).SnapshotPortfolio(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		if r := f.rows[0]; !r.PortfolioValue.IsZero() || string(r.PositionValuesJSON) != `{}` || !r.TotalDeposits.Equal(coretest.D("500")) {
			t.Fatalf("%s: row %+v %s", name, r, r.PositionValuesJSON)
		}
	})
}

// D4 (task 180): SnapshotAll tries every portfolio and joins the failures.
func TestSnapshotAll_EveryPortfolioAttemptedErrorsJoined(t *testing.T) {
	f := book(coretest.PortfolioC, "VARIABLE")
	f.Subs = append(f.Subs, siblingcore.Subasset{ID: 9, AssetID: 9, Portfolio: "FIXED", IsPrimary: true, Ticker: "F", PriceCurrency: "PLN", YahooTicker: new("HA1.WA"), Holding: siblingcore.Holding{Units: coretest.U("", "1")}})
	f.failFor = "VARIABLE"
	err := NewSnapshotter(coretest.PortfolioC, f, clock).SnapshotAll(context.Background())
	if err == nil || len(f.rows) != 1 || f.rows[0].Portfolio != "FIXED" {
		t.Fatalf("FIXED must still be written after VARIABLE failed: %v %+v", err, f.rows)
	}
	f.failFor = ""
	f.rows = nil
	if err := NewSnapshotter(coretest.PortfolioC, f, clock).SnapshotAll(context.Background()); err != nil || len(f.rows) != 2 {
		t.Fatalf("both portfolios: %v %d", err, len(f.rows))
	}
	l := book(coretest.PortfolioB, siblingcore.WholeBook)
	if err := NewSnapshotter(coretest.PortfolioB, l, clock).SnapshotAll(context.Background()); err != nil || len(l.rows) != 1 || l.rows[0].Portfolio != siblingcore.WholeBook {
		t.Fatalf("portfolioB's whole book: %v %+v", err, l.rows)
	}
}
