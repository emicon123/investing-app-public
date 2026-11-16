package purchasesession

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
)

// Characterisation golden for CreateSession (task 185 B4): the statement order inside the
// lifecycle transaction, the price-source reads, the returned detail and the cached new-deposit
// figures, captured before the extract-step refactor.

// createTraceQueries records each statement and serves positions/assets in their SQL order.
type createTraceQueries struct {
	*fakeQueries
	calls *[]string
}

func (q createTraceQueries) rec(s string) { *q.calls = append(*q.calls, s) }

func (q createTraceQueries) ExistsPlannedSession(ctx context.Context) (bool, error) {
	q.rec("ExistsPlannedSession")
	return q.fakeQueries.ExistsPlannedSession(ctx)
}
func (q createTraceQueries) GetPlannedSession(ctx context.Context) (sqlcgen.PurchaseSession, error) {
	q.rec("GetPlannedSession")
	return q.fakeQueries.GetPlannedSession(ctx)
}
func (q createTraceQueries) GetSettingByKey(ctx context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	q.rec("GetSettingByKey " + key)
	return q.fakeQueries.GetSettingByKey(ctx, key)
}
func (q createTraceQueries) ListAllPositions(ctx context.Context) ([]sqlcgen.Position, error) {
	q.rec("ListAllPositions")
	rows, err := q.fakeQueries.ListAllPositions(ctx)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Ticker < rows[j].Ticker })
	return rows, err
}
func (q createTraceQueries) ListAllAssets(ctx context.Context) ([]sqlcgen.Asset, error) {
	q.rec("ListAllAssets")
	rows, err := q.fakeQueries.ListAllAssets(ctx)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, err
}
func (q createTraceQueries) MacroStateLatestByComputedAt(ctx context.Context) (sqlcgen.MacroState, error) {
	q.rec("MacroStateLatestByComputedAt")
	return q.fakeQueries.MacroStateLatestByComputedAt(ctx)
}
func (q createTraceQueries) LatestCashRecordedAt(ctx context.Context) (pgtype.Timestamptz, error) {
	q.rec("LatestCashRecordedAt")
	return q.fakeQueries.LatestCashRecordedAt(ctx)
}
func (q createTraceQueries) CashPositionsByRecordedAt(ctx context.Context, at pgtype.Timestamptz) ([]sqlcgen.CashPosition, error) {
	q.rec("CashPositionsByRecordedAt")
	return q.fakeQueries.CashPositionsByRecordedAt(ctx, at)
}
func (q createTraceQueries) SumDepositAmountByAccountTypeAndYear(ctx context.Context, arg sqlcgen.SumDepositAmountByAccountTypeAndYearParams) (pgtype.Numeric, error) {
	q.rec("SumDepositAmountByAccountTypeAndYear " + string(arg.AccountType))
	return q.fakeQueries.SumDepositAmountByAccountTypeAndYear(ctx, arg)
}
func (q createTraceQueries) GetAccountCapAdjustment(ctx context.Context, arg sqlcgen.GetAccountCapAdjustmentParams) (sqlcgen.AccountCapAdjustment, error) {
	q.rec("GetAccountCapAdjustment " + string(arg.AccountType))
	return q.fakeQueries.GetAccountCapAdjustment(ctx, arg)
}
func (q createTraceQueries) InsertPurchaseSession(ctx context.Context, arg sqlcgen.InsertPurchaseSessionParams) (sqlcgen.PurchaseSession, error) {
	q.rec(fmt.Sprintf("InsertPurchaseSession %s ret=%s main=%s included=%s", arg.Status, traceNum(arg.RetirementBudgetPln), traceNum(arg.MainBudgetPln), arg.PortfoliosIncluded))
	return q.fakeQueries.InsertPurchaseSession(ctx, arg)
}
func (q createTraceQueries) InsertPurchaseSessionItem(ctx context.Context, arg sqlcgen.InsertPurchaseSessionItemParams) (sqlcgen.PurchaseSessionItem, error) {
	q.rec(fmt.Sprintf("InsertPurchaseSessionItem %s %s %s val=%s units=%s price=%s sell=%v buy=%v", arg.Portfolio, arg.Ticker, arg.AccountType,
		traceNum(arg.PlannedValuePln), traceNum(arg.PlannedUnits), traceNum(arg.PricePlnAtPlan), arg.IsMigrationSell, arg.IsMigrationBuy))
	return q.fakeQueries.InsertPurchaseSessionItem(ctx, arg)
}

type createTracePrices struct{ calls *[]string }

func (p createTracePrices) PricePlnByYahooTicker(ctx context.Context) (map[string]decimal.Decimal, error) {
	*p.calls = append(*p.calls, "prices.PricePlnByYahooTicker")
	return fakePrices{}.PricePlnByYahooTicker(ctx)
}
func (p createTracePrices) AllRatesToPln(ctx context.Context) (map[string]decimal.Decimal, error) {
	*p.calls = append(*p.calls, "prices.AllRatesToPln")
	return fakePrices{}.AllRatesToPln(ctx)
}

func createTraceService() (*Service, *fakeQueries, *[]string) {
	fq := newFakeQueries()
	fq.settings["retirement.monthly_contribution_pln"] = "500"
	fq.settings["ike_annual_limit_pln"] = "700"
	fq.settings["ikze_annual_limit_pln"] = "0"
	fq.depositSum["IKE"] = decimal.NewFromInt(100)
	fq.hasCash = true
	sixty, forty, hundred := decimal.NewFromInt(60), decimal.NewFromInt(40), decimal.NewFromInt(100)
	fq.addPosition("CT-ETF1", "MAIN", "XTB", "PLN", &sixty, decimal.NewFromInt(2))
	fq.addPosition("CT-ETF2", "MAIN", "XTB", "EUR", &forty, decimal.Zero)
	fq.addPosition("CT-BOND1", "RETIREMENT", "XTB", "PLN", &hundred, decimal.Zero)
	calls := &[]string{}
	q := createTraceQueries{fakeQueries: fq, calls: calls}
	return NewService(q, nil, nil, createTracePrices{calls: calls}), fq, calls
}

func renderDetail(d SessionDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "status=%s ret=%s main=%s included=%s\n", d.Status, d.RetirementBudgetPln, d.MainBudgetPln, d.PortfoliosIncluded)
	for _, it := range d.Items {
		fmt.Fprintf(&b, "item %s|%s|%s|val=%s|units=%s|price=%s|cur=%s|ex=%s|sell=%v|buy=%v\n", it.Portfolio, it.Ticker, it.AccountType,
			valueOrZero(it.PlannedValuePln), valueOrZero(it.PlannedUnits), valueOrZero(it.PricePlnAtPlan), it.PriceCurrency, it.Exchange,
			it.IsMigrationSell, it.IsMigrationBuy)
	}
	c := d.CurrencyBreakdown
	fmt.Fprintf(&b, "breakdown xtbPln=%s xtbEur=%s/%s xtbUsd=%s/%s binanceEur=%s/%s eur=%s usd=%s\n", c.XtbPln, c.XtbEur, c.XtbEurPln,
		c.XtbUsd, c.XtbUsdPln, c.BinanceEur, c.BinanceEurPln, c.EurPlnRate, c.UsdPlnRate)
	l := d.MainLimitsRemaining
	fmt.Fprintf(&b, "limits ike=%s/%s/%s ikze=%s/%s/%s same=%v\n", l.IkeUsedPln, l.IkeRemainingPln, l.IkeAnnualLimitPln,
		l.IkzeUsedPln, l.IkzeRemainingPln, l.IkzeAnnualLimitPln, d.RetirementLimitsRemaining == d.MainLimitsRemaining)
	return b.String()
}

func TestCreateSession_CallTraceAndDetail_Golden(t *testing.T) {
	svc, _, calls := createTraceService()
	detail, err := svc.CreateSession(context.Background(), CreateSessionInput{PortfoliosIncluded: PortfoliosIncludedBOTH, CashOverridePln: decPtr("3000")})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	const wantTrace = `ExistsPlannedSession
ListAllPositions
ListAllAssets
prices.AllRatesToPln
prices.PricePlnByYahooTicker
GetSettingByKey retirement.monthly_contribution_pln
ListAllPositions
ListAllAssets
prices.AllRatesToPln
prices.PricePlnByYahooTicker
GetSettingByKey retirement.monthly_contribution_pln
GetSettingByKey rebalancing.macro_derisk_enabled
MacroStateLatestByComputedAt
ListAllPositions
ListAllAssets
GetSettingByKey ike_annual_limit_pln
SumDepositAmountByAccountTypeAndYear IKE
GetAccountCapAdjustment IKE
GetSettingByKey ikze_annual_limit_pln
SumDepositAmountByAccountTypeAndYear IKZE
GetAccountCapAdjustment IKZE
ListAllPositions
ListAllAssets
prices.PricePlnByYahooTicker
prices.AllRatesToPln
InsertPurchaseSession PLANNED ret=500 main=2371.4286 included=BOTH
InsertPurchaseSessionItem RETIREMENT CT-BOND1 IKE val=500 units=5 price=100 sell=false buy=false
InsertPurchaseSessionItem MAIN CT-ETF2 IKE val=100 units=1 price=100 sell=false buy=false
InsertPurchaseSessionItem MAIN CT-ETF2 REGULAR val=928.5714 units=9.285714 price=100 sell=false buy=false
InsertPurchaseSessionItem MAIN CT-ETF1 REGULAR val=1342.8571 units=13.42857143 price=100 sell=false buy=false`
	const wantDetail = `status=PLANNED ret=500 main=2371.4286 included=BOTH
item RETIREMENT|CT-BOND1|IKE|val=500|units=5|price=100|cur=PLN|ex=XTB|sell=false|buy=false
item MAIN|CT-ETF2|IKE|val=100|units=1|price=100|cur=EUR|ex=XTB|sell=false|buy=false
item MAIN|CT-ETF2|REGULAR|val=928.5714|units=9.285714|price=100|cur=EUR|ex=XTB|sell=false|buy=false
item MAIN|CT-ETF1|REGULAR|val=1342.8571|units=13.42857143|price=100|cur=PLN|ex=XTB|sell=false|buy=false
breakdown xtbPln=1942.8571 xtbEur=215.9468/928.5714 xtbUsd=0/0 binanceEur=0/0 eur=4.3 usd=4
limits ike=100/600/700 ikze=0/0/0 same=true
`
	const wantCache = `true MAIN/IKE=100,RETIREMENT/IKE=500`
	if got := strings.Join(*calls, "\n"); got != wantTrace {
		t.Errorf("trace differs\n got:\n%s\nwant:\n%s", got, wantTrace)
	}
	if got := renderDetail(detail); got != wantDetail {
		t.Errorf("detail differs\n got:\n%s\nwant:\n%s", got, wantDetail)
	}
	cached, ok := svc.cachePeek(detail.ID)
	var keys []string
	for k, v := range cached {
		keys = append(keys, fmt.Sprintf("%s/%s=%s", k.portfolio, k.accountType, v))
	}
	sort.Strings(keys)
	if got := fmt.Sprintf("%v %s", ok, strings.Join(keys, ",")); got != wantCache {
		t.Errorf("cache = %s, want %s", got, wantCache)
	}
}

func TestCreateSession_PlannedSessionExists_ReadsOnly(t *testing.T) {
	svc, fq, calls := createTraceService()
	fq.sessions[42] = sqlcgen.PurchaseSession{ID: 42, Status: sqlcgen.SessionStatusEnumPLANNED}
	_, err := svc.CreateSession(context.Background(), CreateSessionInput{PortfoliosIncluded: PortfoliosIncludedMAIN})
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Code != "PLANNED_SESSION_EXISTS" ||
		appErr.Message != "A PLANNED purchase session already exists (id=42)" {
		t.Fatalf("err = %v", err)
	}
	if got := strings.Join(*calls, "|"); got != "ExistsPlannedSession|GetPlannedSession" {
		t.Errorf("statements = %s", got)
	}
}
