package historia

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as
// Phase 2 (business logic table-tested here; real Postgres covered by
// integration_test.go). Historia rows keyed exactly like the UNIQUE constraint:
// (snapshot_date, portfolio).
type fakeQueries struct {
	historia  map[string]*fakeHistoriaRow
	positions map[string][]sqlcgen.ListHistoriaPositionsRow
	assets    map[string]int64
	deposits  map[string][]sqlcgen.ListHistoriaExternalDepositsRow
	inflation []sqlcgen.ListHistoriaInflationAscRow
	prices    map[string]decimal.Decimal
	upserts   []sqlcgen.UpsertHistoriaParams
	nextID    int64
}

type fakeHistoriaRow struct {
	id        int64
	date      time.Time
	portfolio string
	deposits  decimal.Decimal
	value     decimal.Decimal
	xirr      *decimal.Decimal
	pvJSON    []byte
	created   time.Time
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{
		historia:  map[string]*fakeHistoriaRow{},
		positions: map[string][]sqlcgen.ListHistoriaPositionsRow{},
		deposits:  map[string][]sqlcgen.ListHistoriaExternalDepositsRow{},
		prices:    map[string]decimal.Decimal{},
		nextID:    1,
	}
}

func historiaKey(date time.Time, portfolio string) string {
	return dateKey(date) + "|" + portfolio
}

func seedHistoria(t *testing.T, fq *fakeQueries, date string, portfolio string, deposits, value string) {
	t.Helper()
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatal(err)
	}
	fq.historia[historiaKey(d, portfolio)] = &fakeHistoriaRow{
		id:        fq.nextID,
		date:      d,
		portfolio: portfolio,
		deposits:  decimal.RequireFromString(deposits),
		value:     decimal.RequireFromString(value),
		created:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	fq.nextID++
}

func (f *fakeQueries) allHistoriaSorted() []*fakeHistoriaRow {
	out := []*fakeHistoriaRow{}
	for _, r := range f.historia {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].date.Before(out[j].date) })
	return out
}

func (f *fakeQueries) toByPortfolioRow(r *fakeHistoriaRow) sqlcgen.ListHistoriaByPortfolioRow {
	return sqlcgen.ListHistoriaByPortfolioRow{
		ID: r.id, SnapshotDate: pgnum.Date(r.date), Portfolio: sqlcgen.PortfolioEnum(r.portfolio),
		TotalDeposits: pgnum.FromDecimal(r.deposits), PortfolioValue: pgnum.FromDecimal(r.value),
		Xirr: numOrNull(r.xirr), PositionValues: r.pvJSON,
		CreatedAt: pgtype.Timestamptz{Time: r.created, Valid: true},
	}
}

func numOrNull(d *decimal.Decimal) pgtype.Numeric {
	if d == nil {
		return pgtype.Numeric{}
	}
	return pgnum.FromDecimal(*d)
}

func (f *fakeQueries) ListHistoriaByPortfolio(_ context.Context, arg sqlcgen.ListHistoriaByPortfolioParams) ([]sqlcgen.ListHistoriaByPortfolioRow, error) {
	var out []sqlcgen.ListHistoriaByPortfolioRow
	for _, r := range f.allHistoriaSorted() {
		if r.portfolio != string(arg.Portfolio) {
			continue
		}
		if r.date.Before(arg.SnapshotDate.Time) || r.date.After(arg.SnapshotDate_2.Time) {
			continue
		}
		out = append(out, f.toByPortfolioRow(r))
	}
	return out, nil
}

func (f *fakeQueries) ListHistoriaAllRange(_ context.Context, arg sqlcgen.ListHistoriaAllRangeParams) ([]sqlcgen.ListHistoriaAllRangeRow, error) {
	var out []sqlcgen.ListHistoriaAllRangeRow
	for _, r := range f.allHistoriaSorted() {
		if r.date.Before(arg.SnapshotDate.Time) || r.date.After(arg.SnapshotDate_2.Time) {
			continue
		}
		out = append(out, sqlcgen.ListHistoriaAllRangeRow{
			ID: r.id, SnapshotDate: pgnum.Date(r.date), Portfolio: sqlcgen.PortfolioEnum(r.portfolio),
			TotalDeposits: pgnum.FromDecimal(r.deposits), PortfolioValue: pgnum.FromDecimal(r.value),
			Xirr: numOrNull(r.xirr), PositionValues: r.pvJSON,
			CreatedAt: pgtype.Timestamptz{Time: r.created, Valid: true},
		})
	}
	return out, nil
}

func (f *fakeQueries) ListHistoriaAll(_ context.Context) ([]sqlcgen.ListHistoriaAllRow, error) {
	var out []sqlcgen.ListHistoriaAllRow
	for _, r := range f.allHistoriaSorted() {
		out = append(out, sqlcgen.ListHistoriaAllRow{
			ID: r.id, SnapshotDate: pgnum.Date(r.date), Portfolio: sqlcgen.PortfolioEnum(r.portfolio),
			TotalDeposits: pgnum.FromDecimal(r.deposits), PortfolioValue: pgnum.FromDecimal(r.value),
			Xirr: numOrNull(r.xirr), PositionValues: r.pvJSON,
			CreatedAt: pgtype.Timestamptz{Time: r.created, Valid: true},
		})
	}
	return out, nil
}

func (f *fakeQueries) UpsertHistoria(_ context.Context, arg sqlcgen.UpsertHistoriaParams) error {
	f.upserts = append(f.upserts, arg)
	dep, err := pgnum.ToDecimal(arg.TotalDeposits)
	if err != nil {
		return err
	}
	val, err := pgnum.ToDecimal(arg.PortfolioValue)
	if err != nil {
		return err
	}
	var xirr *decimal.Decimal
	if arg.Xirr.Valid {
		x, err := pgnum.ToDecimal(arg.Xirr)
		if err != nil {
			return err
		}
		xirr = &x
	}
	key := historiaKey(arg.SnapshotDate.Time, string(arg.Portfolio))
	if existing, ok := f.historia[key]; ok {
		// ON CONFLICT DO UPDATE the four snapshot fields — created_at untouched.
		existing.deposits, existing.value, existing.xirr, existing.pvJSON = dep, val, xirr, arg.PositionValues
		return nil
	}
	f.historia[key] = &fakeHistoriaRow{
		id: f.nextID, date: dateOnly(arg.SnapshotDate.Time), portfolio: string(arg.Portfolio),
		deposits: dep, value: val, xirr: xirr, pvJSON: arg.PositionValues,
		created: time.Now().UTC(),
	}
	f.nextID++
	return nil
}

func (f *fakeQueries) ListHistoriaPositions(_ context.Context, portfolio sqlcgen.PortfolioEnum) ([]sqlcgen.ListHistoriaPositionsRow, error) {
	return f.positions[string(portfolio)], nil
}

func (f *fakeQueries) ListHistoriaExternalDeposits(_ context.Context, arg sqlcgen.ListHistoriaExternalDepositsParams) ([]sqlcgen.ListHistoriaExternalDepositsRow, error) {
	if arg.CapitalSource != sqlcgen.CapitalSourceEnumEXTERNAL {
		return nil, fmt.Errorf("fake only serves EXTERNAL, got %s", arg.CapitalSource)
	}
	return f.deposits[string(arg.Portfolio)], nil
}

func (f *fakeQueries) ListHistoriaInflationAsc(_ context.Context) ([]sqlcgen.ListHistoriaInflationAscRow, error) {
	return f.inflation, nil
}

func (f *fakeQueries) GetPriceCachePlnByTicker(_ context.Context, ticker string) (pgtype.Numeric, error) {
	p, ok := f.prices[ticker]
	if !ok {
		return pgtype.Numeric{}, pgx.ErrNoRows
	}
	return pgnum.FromDecimal(p), nil
}

// stubFx serves fixed FX rates with no network — PLN is always 1.0, anything
// unlisted (and non-PLN) errors like a currency with no cache and a failed fetch.
type stubFx struct {
	rates map[string]decimal.Decimal
	errOn map[string]bool
}

func (s stubFx) GetRateToPln(_ context.Context, currency string) (decimal.Decimal, error) {
	if s.errOn[currency] {
		return decimal.Zero, errors.New("stub fx unavailable for " + currency)
	}
	if r, ok := s.rates[currency]; ok {
		return r, nil
	}
	if currency == "PLN" {
		return decimal.NewFromInt(1), nil
	}
	return decimal.Zero, errors.New("stub fx unavailable for " + currency)
}

func (s stubFx) AllRates(_ context.Context) (map[string]decimal.Decimal, error) {
	out := map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}
	maps.Copy(out, s.rates)
	return out, nil
}

func testService(fq *fakeQueries, fx FxProvider) *Service {
	if fx == nil {
		fx = stubFx{rates: map[string]decimal.Decimal{}}
	}
	return NewService(fq, func(ctx context.Context, fn func(Queries) error) error {
		return fn(fq)
	}, fx)
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func seedPosition(fq *fakeQueries, portfolio, assetKey, ticker string, yahoo *string, currency string, units map[string]string, primary bool) {
	if fq.assets == nil {
		fq.assets = map[string]int64{}
	}
	assetID, ok := fq.assets[portfolio+"/"+assetKey]
	if !ok {
		assetID = int64(len(fq.assets) + 1)
		fq.assets[portfolio+"/"+assetKey] = assetID
	}
	row := sqlcgen.ListHistoriaPositionsRow{
		Ticker: ticker, YahooTicker: yahoo, PriceCurrency: sqlcgen.CurrencyEnum(currency),
		AssetID: assetID, IsPrimary: primary,
	}
	set := func(dst *pgtype.Numeric, v string) {
		*dst = pgnum.FromDecimal(decimal.RequireFromString(v))
	}
	set(&row.UnitsIke, units["ike"])
	set(&row.UnitsIkze, units["ikze"])
	set(&row.UnitsRegularEur, units["eur"])
	set(&row.UnitsRegularUsd, units["usd"])
	set(&row.UnitsRegularPln, units["pln"])
	fq.positions[portfolio] = append(fq.positions[portfolio], row)
}

func seedDeposit(fq *fakeQueries, portfolio, date, amount, currency string) {
	fq.deposits[portfolio] = append(fq.deposits[portfolio], sqlcgen.ListHistoriaExternalDepositsRow{
		DepositDate: pgnum.Date(mustDate(date)),
		Amount:      pgnum.FromDecimal(decimal.RequireFromString(amount)),
		Currency:    sqlcgen.CurrencyEnum(currency),
	})
}

func seedInflation(fq *fakeQueries, year, month int, cpi string) {
	fq.inflation = append(fq.inflation, sqlcgen.ListHistoriaInflationAscRow{
		Year: int32(year), Month: int32(month),
		CpiValue: pgnum.FromDecimal(decimal.RequireFromString(cpi)),
	})
}

func isValidation400(err error) bool {
	var appErr *httpx.AppError
	return errors.As(err, &appErr) && appErr.Status == 400 && appErr.Code == httpx.CodeValidationError
}

func TestService_List_AllMergesBothPortfoliosDateAsc(t *testing.T) {
	fq := newFakeQueries()
	seedHistoria(t, fq, "2024-01-02", "MAIN", "1000", "1100")
	seedHistoria(t, fq, "2024-01-01", "RETIREMENT", "500", "550")
	seedHistoria(t, fq, "2024-01-03", "MAIN", "1000", "1200")
	svc := testService(fq, nil)
	ctx := context.Background()

	for _, p := range []*string{nil, new("ALL"), new("all")} {
		page, err := svc.List(ctx, ListParams{Portfolio: p})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 3 || len(page.Items) != 3 {
			t.Fatalf("expected merged total 3, got %+v", page)
		}
		if page.Items[0].Portfolio != "RETIREMENT" || page.Items[1].Portfolio != "MAIN" {
			t.Fatalf("expected date-ASC merge, got %v %v %v",
				page.Items[0].SnapshotDate, page.Items[1].SnapshotDate, page.Items[2].SnapshotDate)
		}
		if page.Number != 0 || page.Size != 50 {
			t.Fatalf("expected default page 0/50, got %d/%d", page.Number, page.Size)
		}
	}
}

func TestService_List_PortfolioFilterCaseInsensitiveAndInvalid400(t *testing.T) {
	fq := newFakeQueries()
	seedHistoria(t, fq, "2024-01-01", "MAIN", "1000", "1100")
	seedHistoria(t, fq, "2024-01-01", "RETIREMENT", "500", "550")
	svc := testService(fq, nil)
	ctx := context.Background()

	page, err := svc.List(ctx, ListParams{Portfolio: new("main")})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Portfolio != "MAIN" {
		t.Fatalf("expected only MAIN, got %+v", page)
	}

	_, err = svc.List(ctx, ListParams{Portfolio: new("FOO")})
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != 400 || appErr.Code != httpx.CodeValidationError {
		t.Fatalf("expected 400 VALIDATION_ERROR (Java IllegalArgumentException parity), got %v", err)
	}
	if got := appErr.Message; got != "No enum constant pl.investing.entity.enums.Portfolio.FOO" {
		t.Fatalf("expected Java valueOf message, got %q", got)
	}
}

func TestService_List_PaginationInMemory(t *testing.T) {
	fq := newFakeQueries()
	for _, d := range []string{"2024-01-01", "2024-01-02", "2024-01-03", "2024-01-04", "2024-01-05"} {
		seedHistoria(t, fq, d, "MAIN", "1000", "1100")
	}
	svc := testService(fq, nil)
	ctx := context.Background()

	page, err := svc.List(ctx, ListParams{Page: new(1), Size: new(2)})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || len(page.Items) != 2 || page.Number != 1 || page.Size != 2 {
		t.Fatalf("expected page 1/2 total 5, got %+v", page)
	}
	if got := dateKey(page.Items[0].SnapshotDate); got != "2024-01-03" {
		t.Fatalf("expected second page to start 2024-01-03, got %s", got)
	}

	// Out of range → empty data, total preserved.
	page, err = svc.List(ctx, ListParams{Page: new(10), Size: new(2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.Total != 5 {
		t.Fatalf("expected empty data with total 5, got %+v", page)
	}

	// size<=0 → 50, like HistoriaController/service.
	for _, size := range []int{0, -3} {
		page, err = svc.List(ctx, ListParams{Size: new(size)})
		if err != nil {
			t.Fatal(err)
		}
		if page.Size != 50 || len(page.Items) != 5 {
			t.Fatalf("expected effective size 50, got %+v", page)
		}
	}

	if _, err := svc.List(ctx, ListParams{Page: new(-1)}); !isValidation400(err) {
		t.Fatalf("expected 400 for negative page (Java PageRequest parity), got %v", err)
	}
}

func TestService_List_DateRangeInclusive(t *testing.T) {
	fq := newFakeQueries()
	for _, d := range []string{"2024-01-01", "2024-01-02", "2024-01-03"} {
		seedHistoria(t, fq, d, "MAIN", "1000", "1100")
	}
	svc := testService(fq, nil)
	ctx := context.Background()

	page, err := svc.List(ctx, ListParams{From: new(mustDate("2024-01-02")), To: new(mustDate("2024-01-03"))})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("expected inclusive range total 2, got %+v", page)
	}
}

func TestService_List_ReturnMath(t *testing.T) {
	fq := newFakeQueries()
	seedHistoria(t, fq, "2024-01-01", "MAIN", "1000", "1250")
	seedHistoria(t, fq, "2024-01-02", "MAIN", "0", "300")
	seedHistoria(t, fq, "2024-01-03", "MAIN", "300", "1000")
	svc := testService(fq, nil)
	ctx := context.Background()

	page, err := svc.List(ctx, ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Items[0].ReturnPct.String(); got != "0.25" {
		t.Fatalf("expected returnPct 0.25, got %s", got)
	}
	if got := page.Items[0].ProfitPln.String(); got != "250" {
		t.Fatalf("expected profitPln 250, got %s", got)
	}
	if !page.Items[1].ReturnPct.IsZero() {
		t.Fatalf("expected returnPct 0 for zero deposits, got %s", page.Items[1].ReturnPct)
	}
	// 1000/300-1 at scale 8 HALF_UP.
	if got := page.Items[2].ReturnPct.String(); got != "2.33333333" {
		t.Fatalf("expected returnPct 2.33333333, got %s", got)
	}
}

func TestService_List_EnrichmentNullWithoutInflation(t *testing.T) {
	fq := newFakeQueries()
	seedHistoria(t, fq, "2024-01-01", "MAIN", "1000", "1100")
	svc := testService(fq, nil)

	page, err := svc.List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	d := page.Items[0]
	if d.InflationAdjustedDeposits != nil || d.RealReturnPct != nil || d.AnnualizedInflationPct != nil {
		t.Fatalf("expected null inflation fields without index data, got %+v", d)
	}
}

func TestService_List_EnrichmentValues(t *testing.T) {
	fq := newFakeQueries()
	seedHistoria(t, fq, "2022-09-20", "MAIN", "1000", "1100")
	seedInflation(fq, 2022, 8, "100.0")
	seedInflation(fq, 2022, 9, "101.6")
	seedDeposit(fq, "MAIN", "2022-08-15", "1000", "PLN")
	svc := testService(fq, nil)

	page, err := svc.List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	d := page.Items[0]
	// Break-even line: the 1000 PLN deposit of 2022-08-15 compounds only September's
	// CPI (its own month is exclusive): 1000 * 1.016 = 1016.
	if d.InflationAdjustedDeposits == nil || d.InflationAdjustedDeposits.String() != "1016" {
		t.Fatalf("expected inflation-adjusted deposits 1016, got %v", d.InflationAdjustedDeposits)
	}
	if d.AnnualizedInflationPct == nil {
		t.Fatal("expected annualized set")
	}
	// 1 month since base (< 12): raw cumulative, not extrapolated via ^12.
	if d.AnnualizedInflationPct.String() != "0.016" {
		t.Fatalf("expected annualized 0.016 (unextrapolated), got %v", d.AnnualizedInflationPct)
	}
	// Real: 1100 / 1016 - 1 = 0.08267717 at scale 8.
	if d.RealReturnPct == nil || d.RealReturnPct.String() != "0.08267717" {
		t.Fatalf("expected real 0.08267717, got %v", d.RealReturnPct)
	}
}

func TestService_List_BreakEvenLineIsPerPortfolioAndTimeAware(t *testing.T) {
	fq := newFakeQueries()
	seedHistoria(t, fq, "2022-09-20", "MAIN", "1000", "1100")
	seedHistoria(t, fq, "2024-03-20", "RETIREMENT", "500", "480")
	seedInflation(fq, 2022, 8, "100.0")
	seedInflation(fq, 2022, 9, "101.6")
	seedInflation(fq, 2024, 3, "100.5")
	seedDeposit(fq, "MAIN", "2022-08-15", "1000", "PLN")
	seedDeposit(fq, "MAIN", "2022-09-25", "300", "PLN") // after the 2022-09-20 snapshot
	seedDeposit(fq, "RETIREMENT", "2024-02-10", "500", "PLN")
	svc := testService(fq, nil)

	page, err := svc.List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Items))
	}
	byPortfolio := map[string]EntryDTO{}
	for _, it := range page.Items {
		byPortfolio[it.Portfolio] = it
	}
	// The later same-month MAIN deposit is not in that day's total deposits, so it is
	// not on the break-even line either (1000 * 1.016, not + 300).
	if got := byPortfolio["MAIN"].InflationAdjustedDeposits; got == nil || got.String() != "1016" {
		t.Fatalf("MAIN: expected 1016, got %v", got)
	}
	// RETIREMENT's 500 deposited 2024-02 compounds March only (CPI 100.5): 502.5;
	// MAIN's deposits never leak in.
	if got := byPortfolio["RETIREMENT"].InflationAdjustedDeposits; got == nil || got.String() != "502.5" {
		t.Fatalf("RETIREMENT: expected 502.5, got %v", got)
	}
	// 480 is below the 502.5 break-even line → a real loss.
	if r := byPortfolio["RETIREMENT"].RealReturnPct; r == nil || r.Sign() >= 0 {
		t.Fatalf("RETIREMENT: expected a negative real return, got %v", r)
	}
}

func TestService_Snapshot_UpsertSemanticsAndInternalTransferExcluded(t *testing.T) {
	fq := newFakeQueries()
	yahoo := "VUA.WA"
	seedPosition(fq, "MAIN", "VUA", "VUA", &yahoo, "USD",
		map[string]string{"ike": "0", "ikze": "0", "eur": "0", "usd": "10", "pln": "0"}, true)
	fq.prices["VUA.WA"] = decimal.RequireFromString("400")
	seedDeposit(fq, "MAIN", "2024-01-05", "1000", "PLN")
	seedDeposit(fq, "MAIN", "2024-02-05", "100", "USD")
	fx := stubFx{rates: map[string]decimal.Decimal{"USD": decimal.RequireFromString("4")}}
	svc := testService(fq, fx)
	svc.now = func() time.Time { return mustDate("2024-03-01") }
	ctx := context.Background()

	if err := svc.SnapshotPortfolio(ctx, "MAIN"); err != nil {
		t.Fatal(err)
	}
	if len(fq.upserts) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(fq.upserts))
	}
	up := fq.upserts[0]
	if dep, _ := pgnum.ToDecimal(up.TotalDeposits); dep.String() != "1400" {
		t.Fatalf("expected deposits 1000+100*4=1400, got %s", dep)
	}
	if val, _ := pgnum.ToDecimal(up.PortfolioValue); val.String() != "4000" {
		t.Fatalf("expected value 10*400=4000, got %s", val)
	}

	// INTERNAL_TRANSFER deposits must never count (ADR-059) — the fake only serves
	// EXTERNAL, so this asserts the service asks for EXTERNAL, not "all deposits".
	if len(fq.deposits["MAIN"]) != 2 {
		t.Fatalf("test setup: expected 2 EXTERNAL deposits, got %d", len(fq.deposits["MAIN"]))
	}

	// Second snapshot same day updates in place (ON CONFLICT), no duplicate row.
	fq.prices["VUA.WA"] = decimal.RequireFromString("410")
	if err := svc.SnapshotPortfolio(ctx, "MAIN"); err != nil {
		t.Fatal(err)
	}
	if len(fq.historia) != 1 {
		t.Fatalf("expected upsert to update in place (1 row), got %d", len(fq.historia))
	}
	for _, r := range fq.historia {
		if r.value.String() != "4100" {
			t.Fatalf("expected updated value 4100, got %s", r.value)
		}
	}
}

func TestService_Snapshot_PriceMissingYieldsZeroAndEmptyBreakdown(t *testing.T) {
	fq := newFakeQueries()
	yahoo := "MISSING.WA"
	seedPosition(fq, "MAIN", "MISSING", "MISSING", &yahoo, "USD",
		map[string]string{"ike": "1", "ikze": "0", "eur": "0", "usd": "0", "pln": "0"}, true)
	svc := testService(fq, nil)
	svc.now = func() time.Time { return mustDate("2024-03-01") }

	if err := svc.SnapshotPortfolio(context.Background(), "MAIN"); err != nil {
		t.Fatal(err)
	}
	up := fq.upserts[0]
	if val, _ := pgnum.ToDecimal(up.PortfolioValue); !val.IsZero() {
		t.Fatalf("expected ZERO value on missing price, got %s", val)
	}
	if string(up.PositionValues) != "{}" {
		t.Fatalf("expected cleared breakdown {}, got %s", up.PositionValues)
	}
	if up.Xirr.Valid {
		t.Fatalf("expected null xirr, got %v", up.Xirr)
	}
}

func TestService_Snapshot_WarsawDateNotUTC(t *testing.T) {
	fq := newFakeQueries()
	svc := testService(fq, nil)
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("Europe/Warsaw zone data unavailable")
	}
	// 00:30 Warsaw on 2026-01-01 is still 2025-12-31 in UTC — the snapshot must be
	// keyed by the Warsaw date.
	svc.now = func() time.Time { return time.Date(2026, 1, 1, 0, 30, 0, 0, loc) }

	if err := svc.SnapshotPortfolio(context.Background(), "MAIN"); err != nil {
		t.Fatal(err)
	}
	up := fq.upserts[0]
	if got := dateKey(up.SnapshotDate.Time); got != "2026-01-01" {
		t.Fatalf("expected Warsaw snapshot date 2026-01-01, got %s", got)
	}
}

// TestService_BackfillSnapshot_UsesExplicitDateNotToday pins ADR-097 Decision
// §2's split: BackfillSnapshot must upsert under the explicit date parameter, not
// under historiaquery.WarsawDate(s.now()) — the whole point of the split is decoupling the
// snapshot_date from "today."
func TestService_BackfillSnapshot_UsesExplicitDateNotToday(t *testing.T) {
	fq := newFakeQueries()
	yahoo := "VUA.WA"
	seedPosition(fq, "MAIN", "VUA", "VUA", &yahoo, "USD",
		map[string]string{"ike": "0", "ikze": "0", "eur": "0", "usd": "10", "pln": "0"}, true)
	fq.prices["VUA.WA"] = decimal.RequireFromString("400")
	svc := testService(fq, nil)
	// "now" is 2026-09-19 (the morning after the missed run) but the backfill
	// targets 2026-09-18 — the row must land on the requested date, not today.
	svc.now = func() time.Time { return mustDate("2026-09-19") }

	if err := svc.BackfillSnapshot(context.Background(), "MAIN", mustDate("2026-09-18")); err != nil {
		t.Fatal(err)
	}
	if len(fq.upserts) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(fq.upserts))
	}
	up := fq.upserts[0]
	if got := dateKey(up.SnapshotDate.Time); got != "2026-09-18" {
		t.Fatalf("expected snapshot_date 2026-09-18 (the backfill target), got %s", got)
	}
	if val, _ := pgnum.ToDecimal(up.PortfolioValue); val.String() != "4000" {
		t.Fatalf("expected value 10*400=4000 (same computation as the live path), got %s", val)
	}
}

// TestService_SnapshotPortfolio_StillUsesToday is the unchanged-behavior
// regression for the ADR-097 split: SnapshotPortfolio (the live scheduler's call
// site) must still resolve historiaquery.WarsawDate(s.now()) exactly as before the split.
func TestService_SnapshotPortfolio_StillUsesToday(t *testing.T) {
	fq := newFakeQueries()
	svc := testService(fq, nil)
	svc.now = func() time.Time { return mustDate("2026-09-19") }

	if err := svc.SnapshotPortfolio(context.Background(), "MAIN"); err != nil {
		t.Fatal(err)
	}
	up := fq.upserts[0]
	if got := dateKey(up.SnapshotDate.Time); got != "2026-09-19" {
		t.Fatalf("expected snapshot_date 2026-09-19 (today), got %s", got)
	}
}

// TestService_BackfillSnapshot_UnknownPortfolioRejected pins that the guard in
// the shared snapshotPortfolioAsOf core still applies to the BackfillSnapshot
// entry point, not just the live SnapshotPortfolio path.
func TestService_BackfillSnapshot_UnknownPortfolioRejected(t *testing.T) {
	fq := newFakeQueries()
	svc := testService(fq, nil)
	if err := svc.BackfillSnapshot(context.Background(), "BOGUS", mustDate("2026-09-18")); err == nil {
		t.Fatal("expected error for unknown portfolio")
	}
	if len(fq.upserts) != 0 {
		t.Fatalf("expected no upsert for a rejected portfolio, got %d", len(fq.upserts))
	}
}

func TestService_ComputeValues_SubassetAggregationAndCHF(t *testing.T) {
	fq := newFakeQueries()
	yahoo, yahooComp := "VUA.WA", "VUA2.WA"
	seedPosition(fq, "MAIN", "VUA", "VUA", &yahoo, "USD",
		map[string]string{"ike": "0", "ikze": "0", "eur": "0", "usd": "10", "pln": "0"}, true)
	seedPosition(fq, "MAIN", "VUA", "VUA-COMP", &yahooComp, "USD",
		map[string]string{"ike": "5", "ikze": "0", "eur": "0", "usd": "0", "pln": "0"}, false)
	seedPosition(fq, "MAIN", "CHF", "CHF", &yahoo, "CHF",
		map[string]string{"ike": "0", "ikze": "0", "eur": "0", "usd": "0", "pln": "100"}, true)
	fq.prices["VUA.WA"] = decimal.RequireFromString("400")
	fq.prices["VUA2.WA"] = decimal.RequireFromString("100")
	fx := stubFx{rates: map[string]decimal.Decimal{"CHF": decimal.RequireFromString("4.5")}}
	svc := testService(fq, fx)

	values, err := svc.computeValues(context.Background(), "MAIN")
	if err != nil {
		t.Fatal(err)
	}
	// VUA = 10*400 + companion 5*100 = 4500; CHF = 100*4.5 = 450.
	if values["VUA"].String() != "4500" {
		t.Fatalf("expected subasset-aggregated 4500, got %s", values["VUA"])
	}
	if values["CHF"].String() != "450" {
		t.Fatalf("expected CHF 450, got %s", values["CHF"])
	}
	if _, ok := values["VUA-COMP"]; ok {
		t.Fatal("non-primary row must not appear as its own map key")
	}
}

func TestService_ComputeValues_MissingCHFRateFailsLoudly(t *testing.T) {
	fq := newFakeQueries()
	yahoo := "X.WA"
	seedPosition(fq, "MAIN", "CHF", "CHF", &yahoo, "CHF",
		map[string]string{"ike": "0", "ikze": "0", "eur": "0", "usd": "0", "pln": "100"}, true)
	svc := testService(fq, nil) // no CHF rate anywhere — no 1.0 fallback

	if _, err := svc.computeValues(context.Background(), "MAIN"); err == nil {
		t.Fatal("expected loud failure for missing CHF rate")
	}
}
