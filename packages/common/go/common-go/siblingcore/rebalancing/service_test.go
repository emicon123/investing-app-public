package rebalancing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/internal/coretest"
)

type fakeStore struct {
	*coretest.Book
	lots []PurchaseLot
	inTx bool
}

func (f *fakeStore) ListPurchaseLots(_ context.Context, p siblingcore.Portfolio) ([]PurchaseLot, error) {
	var out []PurchaseLot
	for _, l := range f.lots {
		if l.Portfolio == p {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeStore) InsertPurchaseLot(_ context.Context, lot PurchaseLot) error {
	lot.ID, lot.CreatedAt = f.NextID(), time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	f.lots = append(f.lots, lot)
	f.Log("lot " + lot.Ticker)
	return nil
}

// fakeFunding records the skeleton's calls.
type fakeFunding struct {
	budget    Budget
	budgetErr error
	checkErr  error
	deposit   decimal.Decimal
	gotPaid   map[siblingcore.Bucket]decimal.Decimal
	checked   bool
}

func (f *fakeFunding) Budget(context.Context, *fakeStore, siblingcore.Portfolio, *decimal.Decimal) (Budget, error) {
	return f.budget, f.budgetErr
}
func (f *fakeFunding) Check(ConfirmRequest) error { f.checked = true; return f.checkErr }
func (f *fakeFunding) Settle(_ context.Context, q *fakeStore, _ ConfirmRequest, paid map[siblingcore.Bucket]decimal.Decimal) (decimal.Decimal, error) {
	if !q.inTx {
		return decimal.Zero, errors.New("Settle must run inside the transaction")
	}
	f.gotPaid = paid
	q.Log("settle")
	return f.deposit, nil
}

var (
	ctx   = context.Background()
	today = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
)

func newService(cfg siblingcore.Config, f *fakeStore, fund *fakeFunding) *Service {
	tx := func(ctx context.Context, fn func(*fakeStore) error) error {
		f.inTx = true
		defer func() { f.inTx = false }()
		f.Log("begin")
		return fn(f)
	}
	return NewService[*fakeStore](cfg, f, tx, f.Book, fund, func() time.Time { return today })
}

// book: asset 1 (target 60) = A1 primary + A2 sibling, asset 2 (target 40) = B1; prices 100/40/20.
func book(cfg siblingcore.Config, p siblingcore.Portfolio) *fakeStore {
	b := string(coretest.Bucket(cfg))
	return &fakeStore{Book: &coretest.Book{
		Assets: []siblingcore.Asset{{ID: 1, Portfolio: p, Key: "A", TargetAllocationPct: coretest.DP("60")}, {ID: 2, Portfolio: p, Key: "B", TargetAllocationPct: coretest.DP("40")}},
		Subs: []siblingcore.Subasset{
			{ID: 11, AssetID: 1, Portfolio: p, IsPrimary: true, Ticker: "A1", Name: "A one", PriceCurrency: "PLN", YahooTicker: new("A1.WA"), Holding: siblingcore.Holding{Units: coretest.U(b, "5"), TotalSpentPln: coretest.DP("450")}},
			{ID: 12, AssetID: 1, Portfolio: p, Ticker: "A2", Name: "A two", PriceCurrency: "PLN", YahooTicker: new("A2.WA"), Holding: siblingcore.Holding{Units: coretest.U(b, "1")}},
			{ID: 21, AssetID: 2, Portfolio: p, IsPrimary: true, Ticker: "B1", Name: "B one", PriceCurrency: "PLN", YahooTicker: new("B1.WA"), Holding: siblingcore.Holding{Units: coretest.U(b, "10"), TotalSpentPln: coretest.DP("180")}},
		},
		Prices:    map[string]decimal.Decimal{"A1.WA": coretest.D("100"), "A2.WA": coretest.D("40"), "B1.WA": coretest.D("20")},
		FetchedAt: map[string]time.Time{"A1.WA": time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), "B1.WA": time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)},
	}}
}

func appErr(t *testing.T, err error, status int, field, message string) {
	t.Helper()
	var e *httpx.AppError
	if !errors.As(err, &e) || e.Status != status || e.Field != field || e.Message != message {
		t.Fatalf("expected %d %s %q, got %#v", status, field, message, err)
	}
}

func TestCalculate_UsesTheBudgetAndFoldsSubassetsIntoTheirAsset(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		var gotBalances map[string]decimal.Decimal
		allocate := func(list []rebalancecalc.BuyListItem, balances map[string]decimal.Decimal) []rebalancecalc.BuyListItem {
			gotBalances = balances
			for i := range list {
				list[i].AccountAllocations = []rebalancecalc.AccountAlloc{{AccountType: "X", AllocPln: list[i].AllocatedPln, UnitsToBuy: list[i].UnitsToBuy}}
			}
			return list
		}
		cash := coretest.D("2000.123456")
		fund := &fakeFunding{budget: Budget{DeployablePln: coretest.D("1500"), CashOnHandPln: &cash, Allocate: allocate, Balances: map[string]decimal.Decimal{"X": coretest.D("1")}}}
		res, err := newService(cfg, f, fund).Calculate(ctx, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Portfolio != p || !res.DeployableCashPln.Equal(coretest.D("1500")) || res.CashOnHandPln == nil || !res.CashOnHandPln.Equal(coretest.D("2000.1235")) {
			t.Fatalf("%s: budget not copied: %+v", name, res)
		}
		if !res.TotalPortfolioValuePln.Equal(coretest.D("740")) || gotBalances["X"].IsZero() {
			t.Fatalf("%s: total %s balances %v", name, res.TotalPortfolioValuePln, gotBalances)
		}
		if len(res.BuyList) != 2 || res.BuyList[1].Ticker != "A1" || len(res.BuyList[1].AvailableTickers) != 2 || res.BuyList[1].AvailableTickers[1] != "A2" {
			t.Fatalf("%s: buys go to the primary, siblings offered: %+v", name, res.BuyList)
		}
		if len(res.BuyList[0].AccountAllocations) != 1 || res.BuyList[0].AccountAllocations[0].Bucket != "X" {
			t.Fatalf("%s: allocations: %+v", name, res.BuyList[0].AccountAllocations)
		}
		for _, item := range res.BuyList {
			if item.AllocatedPln.Sign() < 0 {
				t.Fatalf("%s: never a sell: %+v", name, item)
			}
		}
		if len(res.DonutData) != 2 || res.DonutData[0].Status != "OVER" || res.DonutData[1].Status != "UNDER" {
			t.Fatalf("%s: donut %+v", name, res.DonutData)
		}
		if !res.PricesRefreshedAt.Equal(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)) {
			t.Fatalf("%s: oldest primary price: %v", name, res.PricesRefreshedAt)
		}
	})
}

func TestCalculate_IdentityAllocateLeavesNoAccountAllocationsAndNilCash(t *testing.T) {
	f := book(coretest.PortfolioC, "FIXED")
	identity := func(list []rebalancecalc.BuyListItem, _ map[string]decimal.Decimal) []rebalancecalc.BuyListItem {
		return list
	}
	res, err := newService(coretest.PortfolioC, f, &fakeFunding{budget: Budget{DeployablePln: coretest.D("350"), Allocate: identity}}).Calculate(ctx, "FIXED", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.CashOnHandPln != nil {
		t.Fatal("portfolioC has no cash on hand")
	}
	for _, item := range res.BuyList {
		if item.AccountAllocations != nil {
			t.Fatalf("portfolioC buy items carry no allocations: %+v", item)
		}
	}
}

func TestCalculate_BudgetErrorComesFirst(t *testing.T) {
	f := book(coretest.PortfolioC, "VARIABLE")
	f.FailPrices = true
	want := httpx.NewValidationError("depositAmountPln", "required")
	_, err := newService(coretest.PortfolioC, f, &fakeFunding{budgetErr: want}).Calculate(ctx, "VARIABLE", nil)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	_, err = newService(coretest.PortfolioC, f, &fakeFunding{}).Calculate(ctx, "BOGUS", nil)
	appErr(t, err, 400, "portfolio", "No enum constant pl.shared.entity.enums.Portfolio.BOGUS")
}

func TestConfirm_AppliesUnitsCostLotsThenSettlesThenRecordsTheDeposit(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		fund := &fakeFunding{deposit: coretest.D("1500")}
		b := string(coretest.Bucket(cfg))
		err := newService(cfg, f, fund).Confirm(ctx, ConfirmRequest{Portfolio: p, Items: []ConfirmItem{
			{Ticker: "A1", Units: coretest.U(b, "3")}, {Ticker: "A2", Units: coretest.U(b, "0")}, {Ticker: "B1", Units: coretest.U(b, "7.5")},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if !fund.checked {
			t.Fatalf("%s: Check runs", name)
		}
		want := []string{"begin", "holding A1", "holding B1", "lot A1", "lot B1", "settle", "deposit " + string(p) + " 1500"}
		if len(f.Writes) != len(want) {
			t.Fatalf("%s: writes %v", name, f.Writes)
		}
		for i := range want {
			if f.Writes[i] != want[i] {
				t.Fatalf("%s: write %d = %q, want %q (%v)", name, i, f.Writes[i], want[i], f.Writes)
			}
		}
		a1 := f.Sub(11).Holding
		if !a1.Units.Total().Equal(coretest.D("8")) || a1.TotalSpentPln == nil || !a1.TotalSpentPln.Equal(coretest.D("750")) {
			t.Fatalf("%s: A1 holding %v %v", name, a1.Units.Total(), a1.TotalSpentPln)
		}
		if f.Sub(12).Holding.TotalSpentPln != nil {
			t.Fatalf("%s: an unbought sibling's cost is never written", name)
		}
		if len(f.lots) != 2 || !f.lots[0].CostPln.Equal(coretest.D("300")) || f.lots[1].Portfolio != p || !f.lots[1].PurchasedAt.Equal(today) {
			t.Fatalf("%s: lots %+v", name, f.lots)
		}
		if !fund.gotPaid[coretest.Bucket(cfg)].Equal(coretest.D("450")) {
			t.Fatalf("%s: paid %v", name, fund.gotPaid)
		}
		d := f.Deposits[len(f.Deposits)-1]
		if !d.DepositDate.Equal(today) || d.Portfolio != p || d.Comment != nil {
			t.Fatalf("%s: deposit %+v", name, d)
		}
	})
}

func TestConfirm_PortfolioBBucketsLotsAndDuplicateRows(t *testing.T) {
	cfg := coretest.PortfolioB
	f := book(cfg, siblingcore.WholeBook)
	fund := &fakeFunding{deposit: coretest.D("4000")}
	units := siblingcore.Units{}.Add("IKE_TATA", coretest.D("1")).Add("IKE_TATA", coretest.D("2")).Add("INNE", coretest.D("1")).Add("IKZE_MAMA", coretest.D("0"))
	err := newService(cfg, f, fund).Confirm(ctx, ConfirmRequest{Items: []ConfirmItem{{Ticker: "A1", Units: units}}})
	if err != nil {
		t.Fatal(err)
	}
	h := f.Sub(11).Holding
	if !h.Units.Of("IKE_TATA").Equal(coretest.D("8")) || !h.Units.Of("INNE").Equal(coretest.D("1")) {
		t.Fatalf("buckets %v %v", h.Units.Of("IKE_TATA"), h.Units.Of("INNE"))
	}
	if !h.TotalSpentPln.Equal(coretest.D("850")) {
		t.Fatalf("cost over every bucket, INNE included: %v", h.TotalSpentPln)
	}
	if len(f.lots) != 1 || !f.lots[0].Units.Equal(coretest.D("3")) {
		t.Fatalf("lots only for IKE/IKZE units: %+v", f.lots)
	}
	// D3: the two IKE_TATA rows are one bucket, rounded once.
	if !fund.gotPaid["IKE_TATA"].Equal(coretest.D("300")) || !fund.gotPaid["INNE"].Equal(coretest.D("100")) || !fund.gotPaid["IKZE_MAMA"].IsZero() {
		t.Fatalf("paid %v", fund.gotPaid)
	}
}

func TestConfirm_RejectionsBeforeTheTransaction(t *testing.T) {
	cfg := coretest.PortfolioB
	f := book(cfg, siblingcore.WholeBook)
	svc := newService(cfg, f, &fakeFunding{})
	appErr(t, svc.Confirm(ctx, ConfirmRequest{}), 400, "items", "at least one item is required")
	// D1: an unknown or missing account type is a 400 before anything is read or written.
	for _, b := range []siblingcore.Bucket{"", "IKE_BABCIA"} {
		err := svc.Confirm(ctx, ConfirmRequest{Items: []ConfirmItem{{Ticker: "A1", Units: siblingcore.Units{}.Add(b, coretest.D("1"))}}})
		appErr(t, err, 400, "items.accountAllocations.accountType", "must be one of IKE_TATA, IKE_MAMA, IKZE_TATA, IKZE_MAMA, INNE")
	}
	checkErr := httpx.NewValidationError("depositAmountPln", "depositAmountPln must be positive")
	err := newService(coretest.PortfolioC, book(coretest.PortfolioC, "VARIABLE"), &fakeFunding{checkErr: checkErr}).Confirm(ctx, ConfirmRequest{Portfolio: "VARIABLE", Items: []ConfirmItem{{Ticker: "A1", Units: coretest.U("", "1")}}})
	if !errors.Is(err, checkErr) {
		t.Fatalf("Check's error: %v", err)
	}
	if len(f.Writes) != 0 {
		t.Fatalf("nothing written: %v", f.Writes)
	}
	err = svc.Confirm(ctx, ConfirmRequest{Items: []ConfirmItem{{Ticker: "NOPE", Units: coretest.U("IKE_TATA", "1")}}})
	appErr(t, err, 404, "", "Position not found: NOPE")
	if len(f.Writes) != 1 || f.Writes[0] != "begin" {
		t.Fatalf("a 404 inside the transaction writes nothing: %v", f.Writes)
	}
}

func TestConfirm_UnpricedTickerAppliesUnitsWithoutLotOrCost(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		delete(f.Prices, "B1.WA")
		if err := newService(cfg, f, &fakeFunding{}).Confirm(ctx, ConfirmRequest{Portfolio: p, Items: []ConfirmItem{{Ticker: "B1", Units: coretest.U(string(coretest.Bucket(cfg)), "2")}}}); err != nil {
			t.Fatal(err)
		}
		if b := f.Sub(21).Holding; !b.Units.Total().Equal(coretest.D("12")) || !b.TotalSpentPln.Equal(coretest.D("180")) || len(f.lots) != 0 {
			t.Fatalf("%s: %v %v %v", name, b.Units.Total(), b.TotalSpentPln, f.lots)
		}
	})
}

func TestHistory_ScopedToThePortfolioAndNamesDeletedAssets(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		f := book(cfg, p)
		f.Sub(12).Deleted = true
		f.lots = []PurchaseLot{
			{Portfolio: p, Ticker: "A2", Units: coretest.D("1"), PricePln: coretest.D("30"), CostPln: coretest.D("30"), PurchasedAt: today},
			{Portfolio: "FIXED", Ticker: "X", Units: coretest.D("1"), PricePln: coretest.D("1"), CostPln: coretest.D("1"), PurchasedAt: today},
		}
		if p == "FIXED" {
			t.Fatal("fixture assumes another portfolio")
		}
		sessions, err := newService(cfg, f, &fakeFunding{}).History(ctx, p)
		if err != nil || len(sessions) != 1 || len(sessions[0].Lots) != 1 {
			t.Fatalf("%s: %v %+v", name, err, sessions)
		}
		if l := sessions[0].Lots[0]; l.Name == nil || *l.Name != "A two" || !l.PriceAvailable {
			t.Fatalf("%s: lot %+v", name, l)
		}
		none, err := newService(cfg, &fakeStore{Book: &coretest.Book{}}, &fakeFunding{}).History(ctx, p)
		if err != nil || none != nil {
			t.Fatalf("%s: no lots = nil: %v %v", name, err, none)
		}
	})
}

func TestDonutStatus(t *testing.T) {
	for _, tc := range []struct{ dev, want string }{{"0.001", "ON_TARGET"}, {"-0.001", "ON_TARGET"}, {"0.0011", "OVER"}, {"-0.5", "UNDER"}} {
		if got := donutStatus(coretest.D(tc.dev)); got != tc.want {
			t.Errorf("%s: %s", tc.dev, got)
		}
	}
	rows := []rebalancecalc.PositionRow{{Ticker: "Z", ValuePln: coretest.D("10")}}
	if d := donutData(rows, coretest.D("10")); d[0].Status != "ON_TARGET" || !d[0].DeviationPct.IsZero() {
		t.Fatalf("zero target is on target: %+v", d)
	}
}
