package historiaquery

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

type fakeQueries struct {
	rows  []Row
	mom   map[YearMonth]decimal.Decimal
	flows map[string][]CashFlow
	err   error
}

func (f *fakeQueries) ListRange(_ context.Context, portfolio string, from, to time.Time) ([]Row, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []Row
	for _, r := range f.rows {
		if portfolio != "" && r.Portfolio != portfolio {
			continue
		}
		if (r.SnapshotDate.Equal(from) || r.SnapshotDate.After(from)) &&
			(r.SnapshotDate.Equal(to) || r.SnapshotDate.Before(to)) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeQueries) ListAll(_ context.Context, portfolio string) ([]Row, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []Row
	for _, r := range f.rows {
		if portfolio != "" && r.Portfolio != portfolio {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeQueries) MomAsc(_ context.Context) (map[YearMonth]decimal.Decimal, error) {
	return f.mom, nil
}

func (f *fakeQueries) DepositFlows(_ context.Context, portfolio string) ([]CashFlow, error) {
	return f.flows[portfolio], nil
}

func testRow(date, portfolio string, deposits, value string) Row {
	d, _ := time.Parse("2006-01-02", date)
	return Row{
		ID: 1, SnapshotDate: d, Portfolio: portfolio,
		TotalDeposits:  decimal.RequireFromString(deposits),
		PortfolioValue: decimal.RequireFromString(value),
		CreatedAt:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func portfolioBCfg() Config {
	return Config{RequirePortfolio: false, InflationBase: YearMonth{2017, 1}}
}

func portfolioCCfg() Config {
	return Config{
		RequirePortfolio: true,
		ValidPortfolios:  []string{"VARIABLE", "FIXED"},
		InflationBase:    YearMonth{2026, 7},
		EnumQualifier:    "pl.shared.entity.enums.Portfolio",
	}
}

func TestList_PaginationAndComputedFields(t *testing.T) {
	fq := &fakeQueries{rows: []Row{
		testRow("2024-01-01", "", "1000", "1250"),
		testRow("2024-02-01", "", "1000", "1100"),
		testRow("2024-03-01", "", "2000", "2400"),
	}}
	svc := NewService(fq, portfolioBCfg())

	page, err := svc.List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 || page.Number != 0 || page.Size != 50 {
		t.Fatalf("expected full page size 50, got %+v", page)
	}
	if got := page.Items[0].ReturnPct.InexactFloat64(); got != 0.25 {
		t.Fatalf("expected returnPct 0.25, got %v", got)
	}
	if got := page.Items[0].ProfitPln.InexactFloat64(); got != 250 {
		t.Fatalf("expected profitPln 250, got %v", got)
	}

	size := 2
	paged, err := svc.List(context.Background(), ListParams{Page: new(1), Size: &size})
	if err != nil {
		t.Fatal(err)
	}
	if paged.Total != 3 || len(paged.Items) != 1 || paged.Number != 1 {
		t.Fatalf("expected page 1 size 2 total 3 with 1 item, got %+v", paged)
	}

	zero := 0
	fallback, err := svc.List(context.Background(), ListParams{Size: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Size != 50 {
		t.Fatalf("expected size<=0 to fall back to 50, got %d", fallback.Size)
	}

	neg := -1
	if _, err := svc.List(context.Background(), ListParams{Page: &neg}); err == nil {
		t.Fatal("expected 400 for negative page")
	}
}

func TestList_PortfolioRouting(t *testing.T) {
	fq := &fakeQueries{rows: []Row{
		testRow("2024-01-01", "VARIABLE", "1000", "1250"),
		testRow("2024-01-01", "FIXED", "400", "450"),
	}}

	// portfolioB: no portfolio dimension — every row is listed, portfolio param ignored.
	portfolioB := NewService(fq, portfolioBCfg())
	page, err := portfolioB.List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("portfolioB: expected total 2, got %+v", page)
	}

	// portfolioC: portfolio required — absent answers 400.
	portfolioC := NewService(fq, portfolioCCfg())
	if _, err := portfolioC.List(context.Background(), ListParams{}); err == nil {
		t.Fatal("portfolioC: expected 400 for missing portfolio")
	}
	empty := ""
	if _, err := portfolioC.List(context.Background(), ListParams{Portfolio: &empty}); err == nil {
		t.Fatal("portfolioC: expected 400 for empty portfolio")
	}
	bogus := "BOGUS"
	if _, err := portfolioC.List(context.Background(), ListParams{Portfolio: &bogus}); err == nil {
		t.Fatal("portfolioC: expected 400 for unknown portfolio")
	}
	// case-insensitive accept, scoped to the requested portfolio only.
	lower := "variable"
	variable, err := portfolioC.List(context.Background(), ListParams{Portfolio: &lower})
	if err != nil {
		t.Fatal(err)
	}
	if variable.Total != 1 || variable.Items[0].Portfolio != "VARIABLE" {
		t.Fatalf("portfolioC: expected 1 VARIABLE row, got %+v", variable)
	}
}

func TestList_EnrichBestEffort(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{
		{2024, 1}: decimal.RequireFromString("100.4"),
		{2024, 2}: decimal.RequireFromString("100.3"),
	}
	d := func(date, amount string) CashFlow {
		dt, _ := time.Parse("2006-01-02", date)
		return CashFlow{Date: dt, Amount: decimal.RequireFromString(amount)}
	}
	fq := &fakeQueries{
		rows:  []Row{testRow("2024-02-15", "", "1000", "1100")},
		mom:   mom,
		flows: map[string][]CashFlow{"": {d("2024-01-10", "1000")}},
	}
	svc := NewService(fq, portfolioBCfg())
	page, err := svc.List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	e := page.Items[0]
	if e.InflationAdjustedDeposits == nil || e.AnnualizedInflationPct == nil || e.RealReturnPct == nil {
		t.Fatalf("expected all three enrichments, got %+v", e)
	}
	// The January deposit compounds February's CPI only (its own month is exclusive):
	// 1000 × 100.3/100 = 1003, and real return is 1100/1003 - 1 = 0.09670987.
	if got := e.InflationAdjustedDeposits.String(); got != "1003" {
		t.Fatalf("expected break-even line 1003, got %s", got)
	}
	if got := e.RealReturnPct.String(); got != "0.09670987" {
		t.Fatalf("expected real return 0.09670987, got %s", got)
	}

	// Empty index → nulls, not an error.
	fq.mom = map[YearMonth]decimal.Decimal{}
	plain, err := svc.List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if p := plain.Items[0]; p.InflationAdjustedDeposits != nil || p.RealReturnPct != nil || p.AnnualizedInflationPct != nil {
		t.Fatalf("expected null enrichments on empty index, got %+v", p)
	}
}

func TestList_BreakEvenLineTracksDepositsOverTime(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{
		{2024, 2}: decimal.RequireFromString("100.3"),
		{2024, 3}: decimal.RequireFromString("100.5"),
	}
	d := func(date, amount string) CashFlow {
		dt, _ := time.Parse("2006-01-02", date)
		return CashFlow{Date: dt, Amount: decimal.RequireFromString(amount)}
	}
	fq := &fakeQueries{
		rows: []Row{
			testRow("2024-01-20", "", "0", "0"),
			testRow("2024-02-10", "", "1000", "1000"),
			testRow("2024-03-10", "", "1500", "1505"),
		},
		mom:   mom,
		flows: map[string][]CashFlow{"": {d("2024-01-31", "1000"), d("2024-03-01", "500")}},
	}
	page, err := NewService(fq, portfolioBCfg()).List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0", "1003", "1508.015"}
	for i, w := range want {
		got := page.Items[i].InflationAdjustedDeposits
		if got == nil || got.String() != w {
			t.Fatalf("row %d: expected break-even %s, got %v", i, w, got)
		}
	}
	// 1505 PLN is above the 1500 PLN deposited but below the 1508.015 break-even line:
	// a nominal gain that is a real loss.
	if r := page.Items[2].RealReturnPct; r == nil || r.Sign() >= 0 {
		t.Fatalf("expected a negative real return below the break-even line, got %v", r)
	}
	// Before the first deposit there is nothing to compare against.
	if page.Items[0].RealReturnPct != nil {
		t.Fatalf("expected null real return with zero adjusted deposits, got %v", page.Items[0].RealReturnPct)
	}
}

func TestInflationBase_PinPerApp(t *testing.T) {
	// The per-app INFLATION_BASE is a binding constant (2017-01 portfolioB vs 2026-07
	// portfolioC): the same CPI series must annualize differently under each config, so a
	// silent "unification" of the two bases fails this test.
	mom := map[YearMonth]decimal.Decimal{
		{2026, 7}: decimal.RequireFromString("100.5"),
		{2026, 8}: decimal.RequireFromString("100.2"),
	}
	d := func(date, amount string) CashFlow {
		dt, _ := time.Parse("2006-01-02", date)
		return CashFlow{Date: dt, Amount: decimal.RequireFromString(amount)}
	}
	rows := []Row{testRow("2026-08-15", "", "1000", "1100")}
	flows := map[string][]CashFlow{"": {d("2026-07-10", "1000")}}

	portfolioBPage, err := NewService(&fakeQueries{rows: rows, mom: mom, flows: flows}, portfolioBCfg()).List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	portfolioCPage, err := NewService(&fakeQueries{rows: rows, mom: mom, flows: flows}, portfolioCCfgNoPortfolio()).List(context.Background(), ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	l, m := portfolioBPage.Items[0].AnnualizedInflationPct, portfolioCPage.Items[0].AnnualizedInflationPct
	if l == nil || m == nil {
		t.Fatalf("expected both enrichments, got %+v / %+v", portfolioBPage.Items[0], portfolioCPage.Items[0])
	}
	// portfolioC base 2026-07 → one month since base (<12): the raw cumulative, only 2026-08
	// compounds: 100.2/100-1 = 0.002.
	if got := m.StringFixed(4); got != "0.0020" {
		t.Fatalf("portfolioC base pin broken: expected 0.0020, got %s", got)
	}
	// portfolioB base 2017-01 → a 115-month CAGR over the same index: must differ from portfolioC's.
	if l.Equal(*m) {
		t.Fatalf("bases must differ: portfolioB %s == portfolioC %s", l, m)
	}
}

// portfolioCCfgNoPortfolio is portfolioC's config with routing disabled — isolates the base
// constant from the portfolio-routing rule in the pin test above.
func portfolioCCfgNoPortfolio() Config {
	c := portfolioCCfg()
	c.RequirePortfolio = false
	return c
}
