package deposit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

type fakeQueries struct {
	rows       []sqlcgen.Deposit
	nextID     int64
	settings   map[string]string
	adjustment map[string]decimal.Decimal
	depositSum map[string]decimal.Decimal
	extRows    []sqlcgen.ListHistoriaExternalDepositsRow
	macro      *sqlcgen.MacroState
	positions  []sqlcgen.Position
	hasCash    bool
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{nextID: 1, settings: map[string]string{}, adjustment: map[string]decimal.Decimal{}, depositSum: map[string]decimal.Decimal{}}
}

func (f *fakeQueries) ListDeposits(_ context.Context, _ sqlcgen.ListDepositsParams) ([]sqlcgen.Deposit, error) {
	return f.rows, nil
}
func (f *fakeQueries) CountDeposits(_ context.Context, _ sqlcgen.CountDepositsParams) (int64, error) {
	return int64(len(f.rows)), nil
}
func (f *fakeQueries) InsertDeposit(_ context.Context, arg sqlcgen.InsertDepositParams) (sqlcgen.Deposit, error) {
	row := sqlcgen.Deposit{
		ID: f.nextID, DepositDate: arg.DepositDate, Amount: arg.Amount, Currency: arg.Currency,
		Exchange: arg.Exchange, AccountType: arg.AccountType, Portfolio: arg.Portfolio, Comment: arg.Comment,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, CapitalSource: arg.CapitalSource,
	}
	f.nextID++
	f.rows = append(f.rows, row)
	return row, nil
}
func (f *fakeQueries) UpsertAccountCapAdjustment(_ context.Context, arg sqlcgen.UpsertAccountCapAdjustmentParams) (sqlcgen.AccountCapAdjustment, error) {
	v, _ := pgnum.ToDecimal(arg.AdjustmentPln)
	f.adjustment[string(arg.AccountType)] = v
	return sqlcgen.AccountCapAdjustment{ID: 1, AccountType: arg.AccountType, Year: arg.Year, AdjustmentPln: arg.AdjustmentPln}, nil
}
func (f *fakeQueries) ListHistoriaExternalDeposits(_ context.Context, arg sqlcgen.ListHistoriaExternalDepositsParams) ([]sqlcgen.ListHistoriaExternalDepositsRow, error) {
	return f.extRows, nil
}
func (f *fakeQueries) GetSettingByKey(_ context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	v, ok := f.settings[key]
	if !ok {
		return sqlcgen.GetSettingByKeyRow{}, pgx.ErrNoRows
	}
	return sqlcgen.GetSettingByKeyRow{Key: key, Value: v}, nil
}
func (f *fakeQueries) SumDepositAmountByAccountTypeAndYear(_ context.Context, arg sqlcgen.SumDepositAmountByAccountTypeAndYearParams) (pgtype.Numeric, error) {
	v := f.depositSum[string(arg.AccountType)]
	return pgnum.FromDecimal(v), nil
}
func (f *fakeQueries) GetAccountCapAdjustment(_ context.Context, arg sqlcgen.GetAccountCapAdjustmentParams) (sqlcgen.AccountCapAdjustment, error) {
	v, ok := f.adjustment[string(arg.AccountType)]
	if !ok {
		return sqlcgen.AccountCapAdjustment{}, pgx.ErrNoRows
	}
	return sqlcgen.AccountCapAdjustment{ID: 1, AccountType: arg.AccountType, Year: arg.Year, AdjustmentPln: pgnum.FromDecimal(v)}, nil
}
func (f *fakeQueries) ListAllPositions(_ context.Context) ([]sqlcgen.Position, error) {
	return f.positions, nil
}

func (f *fakeQueries) ListAllAssets(_ context.Context) ([]sqlcgen.Asset, error) {
	return nil, nil
}
func (f *fakeQueries) MacroStateLatestByComputedAt(_ context.Context) (sqlcgen.MacroState, error) {
	if f.macro == nil {
		return sqlcgen.MacroState{}, pgx.ErrNoRows
	}
	return *f.macro, nil
}
func (f *fakeQueries) LatestCashRecordedAt(_ context.Context) (pgtype.Timestamptz, error) {
	if !f.hasCash {
		return pgtype.Timestamptz{}, pgx.ErrNoRows
	}
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}, nil
}
func (f *fakeQueries) CashPositionsByRecordedAt(_ context.Context, _ pgtype.Timestamptz) ([]sqlcgen.CashPosition, error) {
	return nil, nil
}

type fakePrices struct{}

func (fakePrices) PricePlnByYahooTicker(_ context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{}, nil
}
func (fakePrices) AllRatesToPln(_ context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}, nil
}
func (fakePrices) RateToPln(_ context.Context, currency string) (decimal.Decimal, error) {
	if currency == "PLN" {
		return decimal.NewFromInt(1), nil
	}
	return decimal.RequireFromString("4.3"), nil
}

func TestCreate_RejectsNonPlnForIkeIkze(t *testing.T) {
	svc := NewService(newFakeQueries(), fakePrices{})
	_, err := svc.Create(context.Background(), CreateInput{
		DepositDate: time.Now(), Amount: decimal.NewFromInt(100), Currency: "EUR",
		Exchange: "XTB", AccountType: "IKE", Portfolio: "RETIREMENT",
	})
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 400 {
		t.Fatalf("expected 400 VALIDATION_ERROR, got %v", err)
	}
}

func TestCreate_AlwaysCapitalSourceExternal(t *testing.T) {
	fq := newFakeQueries()
	svc := NewService(fq, fakePrices{})
	_, err := svc.Create(context.Background(), CreateInput{
		DepositDate: time.Now(), Amount: decimal.NewFromInt(100), Currency: "PLN",
		Exchange: "XTB", AccountType: "IKE", Portfolio: "RETIREMENT",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fq.rows[0].CapitalSource != sqlcgen.CapitalSourceEnumEXTERNAL {
		t.Fatalf("expected EXTERNAL capital source, got %v", fq.rows[0].CapitalSource)
	}
}

func TestComputeCapUsage_UnclampedRemainingCanGoNegative(t *testing.T) {
	fq := newFakeQueries()
	fq.settings["ike_annual_limit_pln"] = "10000"
	fq.depositSum["IKE"] = decimal.NewFromInt(15000) // over the cap
	cu, err := ComputeCapUsage(context.Background(), fq, "IKE", 2026)
	if err != nil {
		t.Fatalf("ComputeCapUsage: %v", err)
	}
	if !cu.RemainingPln.Equal(decimal.NewFromInt(-5000)) {
		t.Fatalf("expected unclamped negative remaining (-5000), got %s", cu.RemainingPln)
	}
}

func TestComputeCapUsage_AdjustmentAppliedAdditively(t *testing.T) {
	fq := newFakeQueries()
	fq.settings["ike_annual_limit_pln"] = "10000"
	fq.depositSum["IKE"] = decimal.NewFromInt(8000)
	fq.adjustment["IKE"] = decimal.NewFromInt(1000) // manual correction: pretend 1000 less was used
	cu, err := ComputeCapUsage(context.Background(), fq, "IKE", 2026)
	if err != nil {
		t.Fatalf("ComputeCapUsage: %v", err)
	}
	// effectiveUsed = 8000 - 1000 = 7000; remaining = 10000 - 7000 = 3000.
	if !cu.RemainingPln.Equal(decimal.NewFromInt(3000)) {
		t.Fatalf("expected remaining=3000, got %s", cu.RemainingPln)
	}
}

func TestComputeCapUsage_RegularRejected(t *testing.T) {
	fq := newFakeQueries()
	_, err := ComputeCapUsage(context.Background(), fq, "REGULAR", 2026)
	if err == nil {
		t.Fatal("expected an error for REGULAR (no statutory cap)")
	}
}

func TestUpsertAdjustment_SolvesForDeltaThatProducesExactTypedRemaining(t *testing.T) {
	fq := newFakeQueries()
	fq.settings["ikze_annual_limit_pln"] = "5000"
	fq.depositSum["IKZE"] = decimal.NewFromInt(4000)
	item, err := UpsertAdjustment(context.Background(), fq, "IKZE", 2026, decimal.NewFromInt(2000))
	if err != nil {
		t.Fatalf("UpsertAdjustment: %v", err)
	}
	if !item.RemainingPln.Equal(decimal.NewFromInt(2000)) {
		t.Fatalf("expected remaining to read exactly the typed value 2000, got %s", item.RemainingPln)
	}
	// Re-querying via ComputeCapUsage must now also read exactly 2000.
	cu, err := ComputeCapUsage(context.Background(), fq, "IKZE", 2026)
	if err != nil {
		t.Fatalf("ComputeCapUsage: %v", err)
	}
	if !cu.RemainingPln.Equal(decimal.NewFromInt(2000)) {
		t.Fatalf("expected re-computed remaining=2000 after adjustment, got %s", cu.RemainingPln)
	}
}

func TestUpsertAdjustment_RejectsRegular(t *testing.T) {
	fq := newFakeQueries()
	_, err := UpsertAdjustment(context.Background(), fq, "REGULAR", 2026, decimal.NewFromInt(100))
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 400 {
		t.Fatalf("expected 400 VALIDATION_ERROR, got %v", err)
	}
}

func TestComputeXirr_NoDeposits_ReturnsNilXirrNoError(t *testing.T) {
	fq := newFakeQueries()
	svc := NewService(fq, fakePrices{})
	result, err := svc.ComputeXirr(context.Background(), "MAIN")
	if err != nil {
		t.Fatalf("ComputeXirr: %v", err)
	}
	if result.XirrPct != nil {
		t.Fatalf("expected nil xirrPct with no deposits, got %v", result.XirrPct)
	}
}
