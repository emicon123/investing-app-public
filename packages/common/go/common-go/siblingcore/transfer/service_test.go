package transfer

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/internal/coretest"
)

type fakeStore struct {
	*coretest.Book
	legs  []LegRecord
	calls int
}

func (f *fakeStore) GetSubassetByTicker(_ context.Context, p siblingcore.Portfolio, ticker string) (siblingcore.Subasset, error) {
	f.calls++
	for _, s := range f.Subs {
		if s.Portfolio == p && s.Ticker == ticker && !s.Deleted {
			return s, nil
		}
	}
	return siblingcore.Subasset{}, pgx.ErrNoRows
}

func (f *fakeStore) InsertTransferLeg(_ context.Context, r LegRecord) error {
	f.legs = append(f.legs, r)
	f.Log("leg " + r.Ticker)
	return nil
}

var ctx = context.Background()

// newService runs fn on the fake directly (the real runner opens the serialised transaction).
func newService(cfg siblingcore.Config, f *fakeStore) *Service {
	return NewService[*fakeStore](cfg, func(ctx context.Context, fn func(*fakeStore) error) error { return fn(f) })
}

// book: asset 1 = SOLD (primary) + SIB, asset 2 = BUY; units in the config's first bucket.
func book(cfg siblingcore.Config, p siblingcore.Portfolio) *fakeStore {
	b := string(coretest.Bucket(cfg))
	return &fakeStore{Book: &coretest.Book{
		Subs: []siblingcore.Subasset{
			{ID: 1, AssetID: 1, Portfolio: p, IsPrimary: true, Ticker: "SOLD", YahooTicker: new("S.WA"), Holding: siblingcore.Holding{Units: coretest.U(b, "10"), TotalSpentPln: coretest.DP("1000")}},
			{ID: 2, AssetID: 1, Portfolio: p, Ticker: "SIB", YahooTicker: new("SIB.WA"), Holding: siblingcore.Holding{Units: coretest.U(b, "0")}},
			{ID: 3, AssetID: 2, Portfolio: p, IsPrimary: true, Ticker: "BUY", YahooTicker: new("B.WA"), Holding: siblingcore.Holding{Units: coretest.U(b, "2"), TotalSpentPln: coretest.DP("40")}},
			{ID: 4, AssetID: 9, Portfolio: "FIXED", IsPrimary: true, Ticker: "ELSEWHERE", Holding: siblingcore.Holding{Units: coretest.U(b, "100")}},
		},
		Prices: map[string]decimal.Decimal{"S.WA": coretest.D("100"), "SIB.WA": coretest.D("50"), "B.WA": coretest.D("25")},
	}}
}

func leg(cfg siblingcore.Config, ticker, units, price string) Leg {
	return Leg{Ticker: ticker, Bucket: coretest.Bucket(cfg), Units: coretest.D(units), PricePln: coretest.D(price)}
}

func appErr(t *testing.T, err error, status int, field, message string) {
	t.Helper()
	var e *httpx.AppError
	if !errors.As(err, &e) || e.Status != status || e.Field != field || e.Message != message {
		t.Fatalf("expected %d %s %q, got %#v", status, field, message, err)
	}
}

func TestExecuteTransfer_RecordsExactlyTheRequestAndMovesUnitsAndCost(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		req := Request{Portfolio: p, Sold: leg(cfg, "SOLD", "4", "100"), Buys: []Leg{leg(cfg, "SIB", "4", "50"), leg(cfg, "BUY", "8", "25")}}
		res, err := newService(cfg, f).ExecuteTransfer(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if res.Sold != req.Sold || len(res.Buys) != 2 || res.TransferID.String() == "" {
			t.Fatalf("%s: result echoes the request: %+v", name, res)
		}
		bk := coretest.Bucket(cfg)
		if !f.Sub(1).Holding.Units.Of(bk).Equal(coretest.D("6")) || !f.Sub(2).Holding.Units.Of(bk).Equal(coretest.D("4")) || !f.Sub(3).Holding.Units.Of(bk).Equal(coretest.D("10")) {
			t.Fatalf("%s: units %v %v %v", name, f.Sub(1).Holding.Units.Total(), f.Sub(2).Holding.Units.Total(), f.Sub(3).Holding.Units.Total())
		}
		// Kernel rules (same-asset swap carries cost, cross-asset buy is a market buy): pinned by the
		// per-app golden DB state; here only that costs moved and the other book is untouched.
		if c := f.Sub(1).Holding.TotalSpentPln; c == nil || !c.LessThan(coretest.D("1000")) {
			t.Fatalf("%s: sold cost must shrink: %v", name, c)
		}
		if !f.Sub(4).Holding.Units.Of(bk).Equal(coretest.D("100")) {
			t.Fatalf("%s: another book's row changed", name)
		}
		if len(f.legs) != 3 || !f.legs[0].UnitsDelta.Equal(coretest.D("-4")) || !f.legs[0].TotalPln.Equal(coretest.D("-400")) ||
			f.legs[1].Ticker != "SIB" || !f.legs[2].TotalPln.Equal(coretest.D("200")) || f.legs[0].Portfolio != p || f.legs[0].Bucket != bk {
			t.Fatalf("%s: legs %+v", name, f.legs)
		}
		if f.Writes[0] != "holding SOLD" || f.Writes[len(f.Writes)-1] != "leg BUY" {
			t.Fatalf("%s: holdings first, audit rows last: %v", name, f.Writes)
		}
	})
}

func TestExecuteTransfer_Rejections(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		svc := newService(cfg, f)
		_, err := svc.ExecuteTransfer(ctx, Request{Portfolio: p, Sold: leg(cfg, "SOLD", "11", "100"), Buys: []Leg{leg(cfg, "BUY", "1", "25")}})
		appErr(t, err, 400, "", "Insufficient units: SOLD holds 10 units, cannot sell 11")
		_, err = svc.ExecuteTransfer(ctx, Request{Portfolio: p, Sold: leg(cfg, "ELSEWHERE", "1", "100"), Buys: []Leg{leg(cfg, "BUY", "1", "25")}})
		appErr(t, err, 404, "", "Position not found: ELSEWHERE")
		_, err = svc.ExecuteTransfer(ctx, Request{Portfolio: p, Sold: leg(cfg, "SOLD", "1", "100")})
		appErr(t, err, 400, "buys", "at least one buy leg is required")
		_, err = svc.ExecuteTransfer(ctx, Request{Portfolio: p, Sold: leg(cfg, "SOLD", "0", "100"), Buys: []Leg{leg(cfg, "BUY", "1", "25")}})
		appErr(t, err, 400, "sold.units", "units must be positive")
		_, err = svc.ExecuteTransfer(ctx, Request{Portfolio: p, Sold: leg(cfg, "SOLD", "1", "100"), Buys: []Leg{leg(cfg, "BUY", "1", "0")}})
		appErr(t, err, 400, "buys.pricePln", "pricePln must be positive")
		if len(f.Writes) != 0 {
			t.Fatalf("%s: rejected transfers write nothing: %v", name, f.Writes)
		}
	})
}

// D1 (task 180): a portfolioB leg without a valid account type is a 400 before any Store call (it
// used to read INNE, skip the delta and fail the audit insert's enum cast with a 500).
func TestExecuteTransfer_PortfolioBLegNeedsAnAccountType(t *testing.T) {
	cfg := coretest.PortfolioB
	for _, tc := range []struct {
		field string
		req   Request
	}{
		{"sold.accountType", Request{Sold: Leg{Ticker: "SOLD", Units: coretest.D("1"), PricePln: coretest.D("100")}, Buys: []Leg{leg(cfg, "BUY", "1", "25")}}},
		{"sold.accountType", Request{Sold: Leg{Ticker: "SOLD", Bucket: "IKE_BABCIA", Units: coretest.D("1"), PricePln: coretest.D("100")}, Buys: []Leg{leg(cfg, "BUY", "1", "25")}}},
		{"buys.accountType", Request{Sold: leg(cfg, "SOLD", "1", "100"), Buys: []Leg{{Ticker: "BUY", Units: coretest.D("1"), PricePln: coretest.D("25")}}}},
	} {
		f := book(cfg, siblingcore.WholeBook)
		_, err := newService(cfg, f).ExecuteTransfer(ctx, tc.req)
		appErr(t, err, 400, tc.field, "must be one of IKE_TATA, IKE_MAMA, IKZE_TATA, IKZE_MAMA, INNE")
		if f.calls != 0 || len(f.Writes) != 0 {
			t.Fatalf("no store call may be made: %d %v", f.calls, f.Writes)
		}
	}
}

func TestExecuteTransfer_PortfolioBSellsFromTheNamedBucketOnly(t *testing.T) {
	cfg := coretest.PortfolioB
	f := book(cfg, siblingcore.WholeBook)
	_, err := newService(cfg, f).ExecuteTransfer(ctx, Request{Sold: Leg{Ticker: "SOLD", Bucket: "IKZE_MAMA", Units: coretest.D("1"), PricePln: coretest.D("100")}, Buys: []Leg{leg(cfg, "BUY", "4", "25")}})
	appErr(t, err, 400, "", "Insufficient units: SOLD holds 0 units, cannot sell 1")
}

// D2 (task 180): an unknown portfolioC portfolio is a 400 before any Store call.
func TestExecuteTransfer_PortfolioCUnknownPortfolio(t *testing.T) {
	cfg := coretest.PortfolioC
	f := book(cfg, "VARIABLE")
	_, err := newService(cfg, f).ExecuteTransfer(ctx, Request{Portfolio: "BOGUS", Sold: leg(cfg, "SOLD", "1", "100"), Buys: []Leg{leg(cfg, "BUY", "1", "25")}})
	appErr(t, err, 400, "portfolio", "No enum constant pl.shared.entity.enums.Portfolio.BOGUS")
	if f.calls != 0 {
		t.Fatal("no store call")
	}
}
