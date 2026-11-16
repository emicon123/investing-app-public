package deposit

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/internal/coretest"
)

// store is coretest.Book plus the paginated deposit reads only this port has.
type store struct{ *coretest.Book }

func (s store) ListDeposits(ctx context.Context, p siblingcore.Portfolio, year *int, limit, offset int) ([]siblingcore.Deposit, error) {
	all := s.filtered(p, year)
	if offset > len(all) {
		offset = len(all)
	}
	end := min(offset+limit, len(all))
	return all[offset:end], nil
}

func (s store) CountDeposits(_ context.Context, p siblingcore.Portfolio, year *int) (int64, error) {
	return int64(len(s.filtered(p, year))), nil
}

// filtered mirrors ListDeposits' ORDER BY deposit_date DESC, id DESC.
func (s store) filtered(p siblingcore.Portfolio, year *int) []siblingcore.Deposit {
	asc, _ := s.ListDepositsAsc(context.Background(), p)
	var out []siblingcore.Deposit
	for _, a := range slices.Backward(asc) {
		if year == nil || a.DepositDate.Year() == *year {
			out = append(out, a)
		}
	}
	return out
}

var fixedNow = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

func newService(cfg siblingcore.Config, b *coretest.Book) *Service {
	return NewService(cfg, store{b}, b, fixedNow)
}

func assertValidation(t *testing.T, err error, field, message string) {
	t.Helper()
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != 400 || appErr.Field != field || appErr.Message != message {
		t.Fatalf("expected 400 %s %q, got %#v", field, message, err)
	}
}

func TestService_Create_RejectsNonPositiveAmount(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		b := &coretest.Book{}
		_, err := newService(cfg, b).Create(context.Background(), siblingcore.Deposit{Portfolio: p, DepositDate: coretest.Day(2026, 1, 1), Amount: coretest.D("0")})
		assertValidation(t, err, "amount", "amount must be positive")
		if len(b.Writes) != 0 {
			t.Fatalf("%s: rejected deposit was written", name)
		}
	})
}

func TestService_CreateThenListScopedToPortfolio(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		b := &coretest.Book{Deposits: []siblingcore.Deposit{{ID: 1, Portfolio: "FIXED", DepositDate: coretest.Day(2026, 1, 1), Amount: coretest.D("400")}}}
		svc := newService(cfg, b)
		comment := "c"
		created, err := svc.Create(context.Background(), siblingcore.Deposit{Portfolio: p, DepositDate: coretest.Day(2026, 2, 1), Amount: coretest.D("150"), Comment: &comment})
		if err != nil {
			t.Fatal(err)
		}
		if created.ID == 0 || created.Portfolio != p || *created.Comment != "c" {
			t.Fatalf("%s: created %+v", name, created)
		}
		items, total, err := svc.List(context.Background(), p, nil, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(items) != 1 || items[0].ID != created.ID {
			t.Fatalf("%s: expected only this book's deposit, got %d %+v", name, total, items)
		}
	})
}

func TestService_List_FiltersByYearAndPages(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		b := &coretest.Book{Deposits: []siblingcore.Deposit{
			{ID: 1, Portfolio: p, DepositDate: coretest.Day(2025, 1, 1), Amount: coretest.D("1")},
			{ID: 2, Portfolio: p, DepositDate: coretest.Day(2026, 1, 1), Amount: coretest.D("2")},
			{ID: 3, Portfolio: p, DepositDate: coretest.Day(2026, 3, 1), Amount: coretest.D("3")},
		}}
		svc := newService(cfg, b)
		y := 2026
		items, total, err := svc.List(context.Background(), p, &y, 0, 50)
		if err != nil || total != 2 || len(items) != 2 || items[0].ID != 3 {
			t.Fatalf("%s: year filter %d %+v %v", name, total, items, err)
		}
		items, total, err = svc.List(context.Background(), p, nil, 1, 1)
		if err != nil || total != 3 || len(items) != 1 || items[0].ID != 2 {
			t.Fatalf("%s: page 1 size 1 %d %+v %v", name, total, items, err)
		}
	})
}

func TestService_ComputeXirr_ValuesTheBookAtTheInjectedDate(t *testing.T) {
	coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
		bk := coretest.Bucket(cfg)
		b := &coretest.Book{
			Assets: []siblingcore.Asset{{ID: 1, Portfolio: p, TargetAllocationPct: coretest.DP("100")}},
			Subs: []siblingcore.Subasset{
				{ID: 10, AssetID: 1, Portfolio: p, IsPrimary: true, Ticker: "X", PriceCurrency: "PLN", YahooTicker: new("X.WA"), Holding: siblingcore.Holding{Units: coretest.U(string(bk), "8")}},
				{ID: 11, AssetID: 1, Portfolio: p, Ticker: "Y", PriceCurrency: "PLN", YahooTicker: new("Y.WA"), Holding: siblingcore.Holding{Units: coretest.U(string(bk), "1")}},
			},
			Deposits: []siblingcore.Deposit{
				{ID: 1, Portfolio: p, DepositDate: coretest.Day(2025, 1, 1), Amount: coretest.D("1000")},
				{ID: 2, Portfolio: "FIXED", DepositDate: coretest.Day(2025, 1, 1), Amount: coretest.D("400")},
			},
			Prices: map[string]decimal.Decimal{"X.WA": coretest.D("125"), "Y.WA": coretest.D("100")},
		}
		if p == "FIXED" {
			t.Fatal("test assumes a non-FIXED portfolio")
		}
		result, err := newService(cfg, b).ComputeXirr(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		if !result.TotalInvestedPln.Equal(coretest.D("1000")) || !result.CurrentValuePln.Equal(coretest.D("1100")) {
			t.Fatalf("%s: invested %s current %s", name, result.TotalInvestedPln, result.CurrentValuePln)
		}
		if result.XirrPct == nil || !result.YearsElapsed.Equal(coretest.D("1")) {
			t.Fatalf("%s: xirr %v years %v (valued at the injected date)", name, result.XirrPct, result.YearsElapsed)
		}
	})
}

func TestService_ComputeXirr_DegradesToZeroOnPriceOrReadFailure(t *testing.T) {
	for _, fail := range []string{"prices", "reads"} {
		coretest.Configs(func(name string, cfg siblingcore.Config, p siblingcore.Portfolio) {
			b := &coretest.Book{
				Subs:       []siblingcore.Subasset{{ID: 10, AssetID: 1, Portfolio: p, IsPrimary: true, Ticker: "X", YahooTicker: new("X.WA"), Holding: siblingcore.Holding{Units: coretest.U(string(coretest.Bucket(cfg)), "10")}}},
				Deposits:   []siblingcore.Deposit{{ID: 1, Portfolio: p, DepositDate: coretest.Day(2025, 1, 1), Amount: coretest.D("1000")}},
				FailPrices: fail == "prices", FailReads: fail == "reads",
			}
			result, err := newService(cfg, b).ComputeXirr(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			if !result.CurrentValuePln.IsZero() || !result.TotalInvestedPln.Equal(coretest.D("1000")) {
				t.Fatalf("%s/%s: %+v", name, fail, result)
			}
		})
	}
}

// D2: an unknown portfolioC portfolio is a 400 before any read or write (it used to reach the enum cast).
func TestService_UnknownPortfolioIs400(t *testing.T) {
	b := &coretest.Book{}
	svc := newService(coretest.PortfolioC, b)
	ctx := context.Background()
	_, _, err := svc.List(ctx, "BOGUS", nil, 0, 50)
	assertValidation(t, err, "portfolio", "No enum constant pl.shared.entity.enums.Portfolio.BOGUS")
	_, err = svc.Create(ctx, siblingcore.Deposit{Portfolio: "BOGUS", DepositDate: coretest.Day(2026, 1, 1), Amount: coretest.D("1")})
	assertValidation(t, err, "portfolio", "No enum constant pl.shared.entity.enums.Portfolio.BOGUS")
	_, err = svc.ComputeXirr(ctx, "")
	assertValidation(t, err, "portfolio", "Required request parameter 'portfolio' is missing")
	if len(b.Writes) != 0 {
		t.Fatal("nothing may be written")
	}
}
