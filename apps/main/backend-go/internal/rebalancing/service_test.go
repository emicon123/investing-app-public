package rebalancing

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

type fakeQueries struct {
	rows     map[int64]sqlcgen.Position
	nextID   int64
	deposits []sqlcgen.InsertDepositParams
	audits   []sqlcgen.InsertAssetPurchaseParams
	prices   []sqlcgen.GetAllPriceCacheRow
	// calls records every query in call order (trace_test.go pins it).
	calls []string
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{rows: map[int64]sqlcgen.Position{}, nextID: 1}
}

func (f *fakeQueries) ListAllPositions(_ context.Context) ([]sqlcgen.Position, error) {
	f.calls = append(f.calls, "ListAllPositions")
	out := make([]sqlcgen.Position, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeQueries) UpdatePositionDelta(_ context.Context, arg sqlcgen.UpdatePositionDeltaParams) (sqlcgen.Position, error) {
	f.calls = append(f.calls, fmt.Sprintf("UpdatePositionDelta id=%d ike=%s ikze=%s eur=%s usd=%s pln=%s cost=%s", arg.ID,
		numText(arg.UnitsIke), numText(arg.UnitsIkze), numText(arg.UnitsRegularEur), numText(arg.UnitsRegularUsd),
		numText(arg.UnitsRegularPln), numText(arg.TotalSpentPln)))
	row := f.rows[arg.ID]
	row.UnitsIke, row.UnitsIkze = arg.UnitsIke, arg.UnitsIkze
	row.UnitsRegularEur, row.UnitsRegularUsd, row.UnitsRegularPln = arg.UnitsRegularEur, arg.UnitsRegularUsd, arg.UnitsRegularPln
	row.TotalSpentPln = arg.TotalSpentPln
	f.rows[arg.ID] = row
	return row, nil
}

func (f *fakeQueries) UpdatePositionCost(_ context.Context, arg sqlcgen.UpdatePositionCostParams) error {
	f.calls = append(f.calls, fmt.Sprintf("UpdatePositionCost id=%d cost=%s", arg.ID, numText(arg.TotalSpentPln)))
	row := f.rows[arg.ID]
	row.TotalSpentPln = arg.TotalSpentPln
	f.rows[arg.ID] = row
	return nil
}

func (f *fakeQueries) GetAllPriceCache(_ context.Context) ([]sqlcgen.GetAllPriceCacheRow, error) {
	f.calls = append(f.calls, "GetAllPriceCache")
	return f.prices, nil
}

func (f *fakeQueries) InsertAssetPurchase(_ context.Context, arg sqlcgen.InsertAssetPurchaseParams) (sqlcgen.AssetPurchase, error) {
	f.calls = append(f.calls, fmt.Sprintf("InsertAssetPurchase %s %s %s units=%s price=%s total=%s type=%s session=%v",
		arg.Portfolio, arg.Ticker, arg.AccountType, numText(arg.UnitsDelta), numText(arg.PricePln), numText(arg.TotalPln),
		arg.TradeType, arg.SessionID))
	f.audits = append(f.audits, arg)
	return sqlcgen.AssetPurchase{ID: int64(len(f.audits))}, nil
}

func (f *fakeQueries) InsertDeposit(_ context.Context, arg sqlcgen.InsertDepositParams) (sqlcgen.Deposit, error) {
	f.calls = append(f.calls, fmt.Sprintf("InsertDeposit %s %s %s %s amount=%s source=%s comment=%q",
		arg.Portfolio, arg.Exchange, arg.AccountType, arg.Currency, numText(arg.Amount), arg.CapitalSource, *arg.Comment))
	f.deposits = append(f.deposits, arg)
	return sqlcgen.Deposit{ID: int64(len(f.deposits))}, nil
}

func mkRow(id int64, ticker, portfolio, exchange, priceCurrency string, regularPln, ike, ikze decimal.Decimal) sqlcgen.Position {
	return sqlcgen.Position{
		ID: id, AssetID: id, Ticker: ticker, Name: ticker, Portfolio: sqlcgen.PortfolioEnum(portfolio),
		Exchange: sqlcgen.ExchangeEnum(exchange), AssetClass: sqlcgen.AssetClassEnumETF, Exposure: sqlcgen.ExposureEnumCASH,
		PriceCurrency: sqlcgen.CurrencyEnum(priceCurrency), PurchaseCurrency: sqlcgen.CurrencyEnum(priceCurrency),
		UnitsIke: pgnum.FromDecimal(ike), UnitsIkze: pgnum.FromDecimal(ikze),
		UnitsRegularEur: pgnum.FromDecimal(decimal.Zero), UnitsRegularUsd: pgnum.FromDecimal(decimal.Zero),
		UnitsRegularPln: pgnum.FromDecimal(regularPln), CompanyBacked: true,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

func newService(fq *fakeQueries) *Service { return NewService(fq, nil) }

func TestExecuteTransfer_SoldLegComesEntirelyFromRequest_NeverComputed(t *testing.T) {
	// Manual-sell-only invariant (architect-output §3 item 2): the sold ticker/units/accountType
	// are read directly off the request and applied without any rebalancing-target computation.
	// This test asserts the RESULT matches exactly what the request said to sell, regardless of
	// each position's target allocation (none are even set on these fixtures) — proving no
	// deviation/target logic is consulted anywhere in this path.
	fq := newFakeQueries()
	fq.rows[1] = mkRow(1, "SOLD", "MAIN", "XTB", "PLN", decimal.NewFromInt(100), decimal.Zero, decimal.Zero)
	fq.rows[2] = mkRow(2, "BOUGHT", "MAIN", "XTB", "PLN", decimal.Zero, decimal.Zero, decimal.Zero)
	svc := newService(fq)

	result, err := svc.ExecuteTransfer(context.Background(), TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "REGULAR", Units: decimal.NewFromInt(40), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "REGULAR", Units: decimal.NewFromInt(4), PricePln: decimal.NewFromInt(100)}},
	})
	if err != nil {
		t.Fatalf("ExecuteTransfer: %v", err)
	}
	if len(result.AffectedTickers) != 2 || result.AffectedTickers[0] != "SOLD" || result.AffectedTickers[1] != "BOUGHT" {
		t.Fatalf("unexpected affected tickers: %v", result.AffectedTickers)
	}
	sold, err := pgnum.ToDecimal(fq.rows[1].UnitsRegularPln)
	if err != nil || !sold.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("expected sold position left with 60 units (100-40), got %v err=%v", sold, err)
	}
	bought, err := pgnum.ToDecimal(fq.rows[2].UnitsRegularPln)
	if err != nil || !bought.Equal(decimal.NewFromInt(4)) {
		t.Fatalf("expected bought position with 4 units, got %v err=%v", bought, err)
	}
	if len(fq.audits) != 2 {
		t.Fatalf("expected 2 audit rows, got %d", len(fq.audits))
	}
	if fq.audits[0].TradeType != sqlcgen.TradeTypeEnumREBALANCINGTRIMSELL {
		t.Fatalf("expected sold leg trade_type=REBALANCING_TRIM_SELL, got %v", fq.audits[0].TradeType)
	}
	if fq.audits[1].TradeType != sqlcgen.TradeTypeEnumREBALANCINGREINVESTBUY {
		t.Fatalf("expected buy leg trade_type=REBALANCING_REINVEST_BUY, got %v", fq.audits[1].TradeType)
	}
}

func TestExecuteTransfer_InsufficientUnits_Rejected(t *testing.T) {
	fq := newFakeQueries()
	fq.rows[1] = mkRow(1, "SOLD", "MAIN", "XTB", "PLN", decimal.NewFromInt(10), decimal.Zero, decimal.Zero)
	fq.rows[2] = mkRow(2, "BOUGHT", "MAIN", "XTB", "PLN", decimal.Zero, decimal.Zero, decimal.Zero)
	svc := newService(fq)

	_, err := svc.ExecuteTransfer(context.Background(), TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "REGULAR", Units: decimal.NewFromInt(999), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "REGULAR", Units: decimal.NewFromInt(1), PricePln: decimal.NewFromInt(10)}},
	})
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 400 {
		t.Fatalf("expected 400 VALIDATION_ERROR, got %v", err)
	}
}

func TestExecuteTransfer_WrapperLock_IkeSoldCannotFundRegularBuy(t *testing.T) {
	fq := newFakeQueries()
	fq.rows[1] = mkRow(1, "SOLD", "MAIN", "XTB", "PLN", decimal.Zero, decimal.NewFromInt(10), decimal.Zero)
	fq.rows[2] = mkRow(2, "BOUGHT", "MAIN", "XTB", "PLN", decimal.Zero, decimal.Zero, decimal.Zero)
	svc := newService(fq)

	_, err := svc.ExecuteTransfer(context.Background(), TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "IKE", Units: decimal.NewFromInt(5), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "REGULAR", Units: decimal.NewFromInt(1), PricePln: decimal.NewFromInt(10)}},
	})
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 400 {
		t.Fatalf("expected 400 VALIDATION_ERROR (wrapper lock), got %v", err)
	}
}

func TestExecuteTransfer_WrapperLock_IkeToIkzeRejected(t *testing.T) {
	fq := newFakeQueries()
	fq.rows[1] = mkRow(1, "SOLD", "MAIN", "XTB", "PLN", decimal.Zero, decimal.NewFromInt(10), decimal.Zero)
	fq.rows[2] = mkRow(2, "BOUGHT", "MAIN", "XTB", "PLN", decimal.Zero, decimal.Zero, decimal.Zero)
	svc := newService(fq)

	_, err := svc.ExecuteTransfer(context.Background(), TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "IKE", Units: decimal.NewFromInt(5), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "IKZE", Units: decimal.NewFromInt(1), PricePln: decimal.NewFromInt(10)}},
	})
	if !errors.As(err, new(*httpx.AppError)) {
		t.Fatalf("expected VALIDATION_ERROR for IKE->IKZE, got %v", err)
	}
}

func TestExecuteTransfer_RegularToIkeAutoDeposit_InternalTransfer(t *testing.T) {
	fq := newFakeQueries()
	fq.rows[1] = mkRow(1, "SOLD", "MAIN", "XTB", "PLN", decimal.NewFromInt(100), decimal.Zero, decimal.Zero)
	fq.rows[2] = mkRow(2, "BOUGHT", "MAIN", "XTB", "PLN", decimal.Zero, decimal.Zero, decimal.Zero)
	svc := newService(fq)

	_, err := svc.ExecuteTransfer(context.Background(), TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "REGULAR", Units: decimal.NewFromInt(50), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "IKE", Units: decimal.NewFromInt(5), PricePln: decimal.NewFromInt(100)}},
	})
	if err != nil {
		t.Fatalf("ExecuteTransfer: %v", err)
	}
	if len(fq.deposits) != 1 {
		t.Fatalf("expected exactly 1 auto-deposit, got %d", len(fq.deposits))
	}
	if fq.deposits[0].CapitalSource != sqlcgen.CapitalSourceEnumINTERNALTRANSFER {
		t.Fatalf("expected INTERNAL_TRANSFER capital source, got %v", fq.deposits[0].CapitalSource)
	}
	amt, _ := pgnum.ToDecimal(fq.deposits[0].Amount)
	if !amt.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("expected deposit amount 500 (5*100), got %s", amt)
	}
}

func TestExecuteTransfer_RegularToRegular_NeverCreatesDeposit(t *testing.T) {
	fq := newFakeQueries()
	fq.rows[1] = mkRow(1, "SOLD", "MAIN", "XTB", "PLN", decimal.NewFromInt(100), decimal.Zero, decimal.Zero)
	fq.rows[2] = mkRow(2, "BOUGHT", "MAIN", "XTB", "PLN", decimal.Zero, decimal.Zero, decimal.Zero)
	svc := newService(fq)

	_, err := svc.ExecuteTransfer(context.Background(), TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "REGULAR", Units: decimal.NewFromInt(50), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "REGULAR", Units: decimal.NewFromInt(5), PricePln: decimal.NewFromInt(100)}},
	})
	if err != nil {
		t.Fatalf("ExecuteTransfer: %v", err)
	}
	if len(fq.deposits) != 0 {
		t.Fatalf("expected no auto-deposit for REGULAR->REGULAR, got %d", len(fq.deposits))
	}
}

func TestExecuteTransfer_CrossPortfolioTickerIsolation_ADR063(t *testing.T) {
	// Same ticker exists in both MAIN and RETIREMENT — a MAIN-scoped transfer must never touch
	// the RETIREMENT row of the same ticker.
	fq := newFakeQueries()
	fq.rows[1] = mkRow(1, "DUAL", "MAIN", "XTB", "PLN", decimal.NewFromInt(100), decimal.Zero, decimal.Zero)
	fq.rows[2] = mkRow(2, "DUAL", "RETIREMENT", "XTB", "PLN", decimal.NewFromInt(100), decimal.Zero, decimal.Zero)
	fq.rows[3] = mkRow(3, "BOUGHT", "MAIN", "XTB", "PLN", decimal.Zero, decimal.Zero, decimal.Zero)
	svc := newService(fq)

	_, err := svc.ExecuteTransfer(context.Background(), TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "DUAL", AccountType: "REGULAR", Units: decimal.NewFromInt(30), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "REGULAR", Units: decimal.NewFromInt(1), PricePln: decimal.NewFromInt(10)}},
	})
	if err != nil {
		t.Fatalf("ExecuteTransfer: %v", err)
	}
	mainUnits, _ := pgnum.ToDecimal(fq.rows[1].UnitsRegularPln)
	if !mainUnits.Equal(decimal.NewFromInt(70)) {
		t.Fatalf("expected MAIN DUAL reduced to 70, got %s", mainUnits)
	}
	retirementUnits, _ := pgnum.ToDecimal(fq.rows[2].UnitsRegularPln)
	if !retirementUnits.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("expected RETIREMENT DUAL untouched at 100, got %s", retirementUnits)
	}
}

func TestExecuteTransfer_UnknownSoldTicker_NotFound(t *testing.T) {
	fq := newFakeQueries()
	svc := newService(fq)
	_, err := svc.ExecuteTransfer(context.Background(), TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "NOPE", AccountType: "REGULAR", Units: decimal.NewFromInt(1), PricePln: decimal.NewFromInt(1)},
		Buys:      []Leg{{Ticker: "ALSO_NOPE", AccountType: "REGULAR", Units: decimal.NewFromInt(1), PricePln: decimal.NewFromInt(1)}},
	})
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 404 {
		t.Fatalf("expected 404 NOT_FOUND, got %v", err)
	}
}

// numText renders a NUMERIC parameter for the call trace ("NULL" when unset).
func numText(n pgtype.Numeric) string {
	if !n.Valid {
		return "NULL"
	}
	return pgnum.ToDecimalOrZero(n).String()
}
