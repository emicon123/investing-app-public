package purchasesession

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

// Characterisation golden for completeSessionTx (task 185 B4): the exact statement sequence of a
// rich completion and its result, captured before the extract-step refactor. The deposit inserts
// iterate a Go map, so that one block is compared as a set.

// traceQueries serves a fixed, SQL-ordered book and records every statement it is asked to run.
type traceQueries struct {
	*fakeQueries
	session   sqlcgen.PurchaseSession
	items     []sqlcgen.PurchaseSessionItem
	positions []sqlcgen.Position
	prices    []sqlcgen.GetAllPriceCacheRow
	calls     []string
}

func (q *traceQueries) GetSessionForUpdate(_ context.Context, id int64) (sqlcgen.PurchaseSession, error) {
	q.calls = append(q.calls, fmt.Sprintf("GetSessionForUpdate %d", id))
	return q.session, nil
}
func (q *traceQueries) ListSessionItemsOrderedForCompletion(_ context.Context, id int64) ([]sqlcgen.PurchaseSessionItem, error) {
	q.calls = append(q.calls, fmt.Sprintf("ListSessionItemsOrderedForCompletion %d", id))
	return q.items, nil
}
func (q *traceQueries) ListAllPositions(context.Context) ([]sqlcgen.Position, error) {
	q.calls = append(q.calls, "ListAllPositions")
	return q.positions, nil
}
func (q *traceQueries) GetAllPriceCache(context.Context) ([]sqlcgen.GetAllPriceCacheRow, error) {
	q.calls = append(q.calls, "GetAllPriceCache")
	return q.prices, nil
}
func (q *traceQueries) InsertAssetPurchase(_ context.Context, a sqlcgen.InsertAssetPurchaseParams) (sqlcgen.AssetPurchase, error) {
	q.calls = append(q.calls, fmt.Sprintf("InsertAssetPurchase session=%d %s %s %s units=%s price=%s total=%s type=%s",
		*a.SessionID, a.Portfolio, a.Ticker, a.AccountType, traceNum(a.UnitsDelta), traceNum(a.PricePln), traceNum(a.TotalPln), a.TradeType))
	return sqlcgen.AssetPurchase{}, nil
}
func (q *traceQueries) UpdatePositionDelta(_ context.Context, a sqlcgen.UpdatePositionDeltaParams) (sqlcgen.Position, error) {
	q.calls = append(q.calls, fmt.Sprintf("UpdatePositionDelta id=%d ike=%s ikze=%s eur=%s usd=%s pln=%s cost=%s", a.ID,
		traceNum(a.UnitsIke), traceNum(a.UnitsIkze), traceNum(a.UnitsRegularEur), traceNum(a.UnitsRegularUsd), traceNum(a.UnitsRegularPln), traceNum(a.TotalSpentPln)))
	return sqlcgen.Position{}, nil
}
func (q *traceQueries) UpdatePositionCost(_ context.Context, a sqlcgen.UpdatePositionCostParams) error {
	q.calls = append(q.calls, fmt.Sprintf("UpdatePositionCost id=%d cost=%s", a.ID, traceNum(a.TotalSpentPln)))
	return nil
}
func (q *traceQueries) UpdateSessionStatusDone(_ context.Context, a sqlcgen.UpdateSessionStatusDoneParams) (sqlcgen.PurchaseSession, error) {
	q.calls = append(q.calls, fmt.Sprintf("UpdateSessionStatusDone %d", a.ID))
	return sqlcgen.PurchaseSession{}, nil
}
func (q *traceQueries) InsertDeposit(_ context.Context, a sqlcgen.InsertDepositParams) (sqlcgen.Deposit, error) {
	q.calls = append(q.calls, fmt.Sprintf("InsertDeposit %s %s %s %s amount=%s source=%s comment=%q",
		a.Portfolio, a.AccountType, a.Exchange, a.Currency, traceNum(a.Amount), a.CapitalSource, *a.Comment))
	return sqlcgen.Deposit{}, nil
}
func (q *traceQueries) InsertBinanceTransferSession(_ context.Context, a sqlcgen.InsertBinanceTransferSessionParams) (sqlcgen.BinanceTransfer, error) {
	q.calls = append(q.calls, fmt.Sprintf("InsertBinanceTransferSession session=%d amount=%s comment=%q", *a.SessionID, traceNum(a.AmountPln), *a.Comment))
	return sqlcgen.BinanceTransfer{}, nil
}

func traceNum(n pgtype.Numeric) string {
	if !n.Valid {
		return "NULL"
	}
	return pgnum.ToDecimalOrZero(n).String()
}

func num(s string) pgtype.Numeric { return pgnum.FromDecimal(decimal.RequireFromString(s)) }

// completionBook: asset 1 (bitcoin) = BTC-USD (BINANCE) + PL.ETNVIRBTCP (XTB) + BTC-XTB (XTB, a
// sibling no item writes); STK (asset 2), ETF2 (asset 3), XAD6 (RETIREMENT, asset 4).
func completionBook() *traceQueries {
	pos := func(id, assetID int64, ticker, portfolio, exchange string, ike, ikze, pln, cost string) sqlcgen.Position {
		y := ticker + ".Y"
		var spent pgtype.Numeric
		if cost != "" {
			spent = num(cost)
		}
		return sqlcgen.Position{ID: id, AssetID: assetID, Ticker: ticker, Portfolio: sqlcgen.PortfolioEnum(portfolio),
			Exchange: sqlcgen.ExchangeEnum(exchange), PriceCurrency: "PLN", PurchaseCurrency: "PLN", YahooTicker: &y,
			UnitsIke: num(ike), UnitsIkze: num(ikze), UnitsRegularEur: num("0"), UnitsRegularUsd: num("0"), UnitsRegularPln: num(pln),
			TotalSpentPln: spent}
	}
	item := func(id int64, portfolio, ticker, account, plannedUnits, plannedValue, actualUnits, actualValue, price string, sell, buy bool) sqlcgen.PurchaseSessionItem {
		opt := func(s string) pgtype.Numeric {
			if s == "" {
				return pgtype.Numeric{}
			}
			return num(s)
		}
		return sqlcgen.PurchaseSessionItem{ID: id, SessionID: 7, Portfolio: sqlcgen.PortfolioEnum(portfolio), Ticker: ticker,
			AccountType: sqlcgen.AccountTypeEnum(account), PlannedUnits: opt(plannedUnits), PlannedValuePln: opt(plannedValue),
			ActualUnits: opt(actualUnits), ActualValuePln: opt(actualValue), PricePlnAtPlan: opt(price),
			IsMigrationSell: sell, IsMigrationBuy: buy}
	}
	q := &traceQueries{fakeQueries: newFakeQueries(),
		session: sqlcgen.PurchaseSession{ID: 7, Status: sqlcgen.SessionStatusEnumPLANNED},
		// ListAllPositions is ORDER BY ticker.
		positions: []sqlcgen.Position{
			pos(1, 1, "BTC-USD", "MAIN", "BINANCE", "0", "0", "0.01", "3000"),
			pos(3, 1, "BTC-XTB", "MAIN", "XTB", "0", "0", "0.005", "1500"),
			pos(4, 3, "ETF2", "MAIN", "XTB", "0", "0", "10", "900"),
			pos(2, 1, "PL.ETNVIRBTCP", "MAIN", "XTB", "4", "0", "0", "1000"),
			pos(5, 2, "STK", "MAIN", "XTB", "0", "0", "0", ""),
			pos(6, 4, "XAD6", "RETIREMENT", "XTB", "0", "1", "0", "80"),
		},
		// ORDER BY is_migration_sell DESC, portfolio ASC, planned_value_pln DESC.
		items: []sqlcgen.PurchaseSessionItem{
			item(10, "MAIN", "PL.ETNVIRBTCP", "IKE", "2", "600", "-2", "", "300", true, false),
			item(11, "MAIN", "BTC-USD", "REGULAR", "0.002", "600", "", "", "300000", false, true),
			item(12, "MAIN", "STK", "IKE", "5", "500", "", "", "100", false, false),
			item(13, "MAIN", "ETF2", "REGULAR", "3", "300", "3", "290", "100", false, false),
			item(14, "MAIN", "ETF2", "IKZE", "0", "50", "", "", "100", false, false), // zero units: skipped
			item(15, "MAIN", "STK", "REGULAR", "1", "", "", "", "", false, false),    // no value, no price: skipped
			item(16, "RETIREMENT", "XAD6", "IKZE", "1.25", "100", "", "", "80", false, false),
		},
	}
	for ticker, price := range map[string]string{"BTC-USD.Y": "300000", "BTC-XTB.Y": "300000", "ETF2.Y": "100", "PL.ETNVIRBTCP.Y": "300", "STK.Y": "100", "XAD6.Y": "80"} {
		q.prices = append(q.prices, sqlcgen.GetAllPriceCacheRow{Ticker: ticker, PricePln: num(price)})
	}
	sort.Slice(q.prices, func(i, j int) bool { return q.prices[i].Ticker < q.prices[j].Ticker })
	return q
}

func TestCompleteSessionTx_CallTrace_Golden(t *testing.T) {
	cases := []struct {
		name   string
		cache  map[groupKey]decimal.Decimal
		result string
		trace  []string
	}{
		{
			name:   "no create-session cache: deposits reconstructed per group",
			result: "session=7 status=DONE purchases=5 updated=[2 1 5 4 6] deposits=3",
			trace: []string{
				"GetSessionForUpdate 7",
				"ListSessionItemsOrderedForCompletion 7",
				"ListAllPositions",
				"GetAllPriceCache",
				"InsertAssetPurchase session=7 MAIN PL.ETNVIRBTCP IKE units=-2 price=300 total=-600 type=MIGRATION_SELL",
				"UpdatePositionDelta id=2 ike=2 ikze=0 eur=0 usd=0 pln=0 cost=894.7368",
				"InsertAssetPurchase session=7 MAIN BTC-USD REGULAR units=0.002 price=300000 total=600 type=MIGRATION_BUY",
				"UpdatePositionDelta id=1 ike=0 ikze=0 eur=0 usd=0 pln=0.012 cost=3263.1579",
				"InsertAssetPurchase session=7 MAIN STK IKE units=5 price=100 total=500 type=DCA_BUY",
				"UpdatePositionDelta id=5 ike=5 ikze=0 eur=0 usd=0 pln=0 cost=500",
				"InsertAssetPurchase session=7 MAIN ETF2 REGULAR units=3 price=96.66666667 total=290.00000001 type=DCA_BUY",
				"UpdatePositionDelta id=4 ike=0 ikze=0 eur=0 usd=0 pln=13 cost=1190",
				"InsertAssetPurchase session=7 RETIREMENT XAD6 IKZE units=1.25 price=80 total=100 type=DCA_BUY",
				"UpdatePositionDelta id=6 ike=0 ikze=2.25 eur=0 usd=0 pln=0 cost=180",
				"UpdatePositionCost id=3 cost=1342.1053",
				"UpdateSessionStatusDone 7",
				`InsertDeposit MAIN REGULAR BINANCE PLN amount=600 source=EXTERNAL comment="Auto from session #7"`,
				`InsertDeposit MAIN REGULAR XTB PLN amount=300 source=EXTERNAL comment="Auto from session #7"`,
				`InsertDeposit RETIREMENT IKZE XTB PLN amount=100 source=EXTERNAL comment="Auto from session #7"`,
				`InsertBinanceTransferSession session=7 amount=600 comment="Auto from session #7"`,
			},
		},
		{
			name: "create-session cache: IKE/IKZE from the cached new-deposit figures",
			cache: map[groupKey]decimal.Decimal{
				{portfolio: "MAIN", accountType: "IKE"}: decimal.NewFromInt(450), {portfolio: "RETIREMENT", accountType: "IKZE"}: decimal.NewFromInt(100),
				{portfolio: "MAIN", accountType: "IKZE"}: decimal.Zero,
			},
			result: "session=7 status=DONE purchases=5 updated=[2 1 5 4 6] deposits=4",
			trace: []string{
				"GetSessionForUpdate 7",
				"ListSessionItemsOrderedForCompletion 7",
				"ListAllPositions",
				"GetAllPriceCache",
				"InsertAssetPurchase session=7 MAIN PL.ETNVIRBTCP IKE units=-2 price=300 total=-600 type=MIGRATION_SELL",
				"UpdatePositionDelta id=2 ike=2 ikze=0 eur=0 usd=0 pln=0 cost=894.7368",
				"InsertAssetPurchase session=7 MAIN BTC-USD REGULAR units=0.002 price=300000 total=600 type=MIGRATION_BUY",
				"UpdatePositionDelta id=1 ike=0 ikze=0 eur=0 usd=0 pln=0.012 cost=3263.1579",
				"InsertAssetPurchase session=7 MAIN STK IKE units=5 price=100 total=500 type=DCA_BUY",
				"UpdatePositionDelta id=5 ike=5 ikze=0 eur=0 usd=0 pln=0 cost=500",
				"InsertAssetPurchase session=7 MAIN ETF2 REGULAR units=3 price=96.66666667 total=290.00000001 type=DCA_BUY",
				"UpdatePositionDelta id=4 ike=0 ikze=0 eur=0 usd=0 pln=13 cost=1190",
				"InsertAssetPurchase session=7 RETIREMENT XAD6 IKZE units=1.25 price=80 total=100 type=DCA_BUY",
				"UpdatePositionDelta id=6 ike=0 ikze=2.25 eur=0 usd=0 pln=0 cost=180",
				"UpdatePositionCost id=3 cost=1342.1053",
				"UpdateSessionStatusDone 7",
				`InsertDeposit MAIN IKE XTB PLN amount=450 source=EXTERNAL comment="Auto from session #7"`,
				`InsertDeposit MAIN REGULAR BINANCE PLN amount=600 source=EXTERNAL comment="Auto from session #7"`,
				`InsertDeposit MAIN REGULAR XTB PLN amount=300 source=EXTERNAL comment="Auto from session #7"`,
				`InsertDeposit RETIREMENT IKZE XTB PLN amount=100 source=EXTERNAL comment="Auto from session #7"`,
				`InsertBinanceTransferSession session=7 amount=600 comment="Auto from session #7"`,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := completionBook()
			peek := func(id int64) (map[groupKey]decimal.Decimal, bool) {
				if id != 7 || c.cache == nil {
					return nil, false
				}
				return c.cache, true
			}
			res, err := completeSessionTx(context.Background(), q, 7, peek)
			if err != nil {
				t.Fatalf("completeSessionTx: %v", err)
			}
			gotResult := fmt.Sprintf("session=%d status=%s purchases=%d updated=%v deposits=%d",
				res.SessionID, res.Status, res.PurchasesWritten, res.UpdatedPositionIds, res.DepositsCreated)
			if gotResult != c.result {
				t.Errorf("result = %s, want %s", gotResult, c.result)
			}
			if got, want := strings.Join(q.calls, "\n"), strings.Join(c.trace, "\n"); got != want {
				t.Errorf("call trace differs\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestCompleteSessionTx_Rejections_NoWrite(t *testing.T) {
	cases := []struct {
		name  string
		setup func(q *traceQueries)
		err   string
		calls string
	}{
		{"already DONE", func(q *traceQueries) { q.session.Status = sqlcgen.SessionStatusEnumDONE },
			"Purchase session 7 is already DONE", "GetSessionForUpdate 7"},
		{"an applicable item whose position is gone", func(q *traceQueries) { q.positions = q.positions[1:] },
			"purchasesession: no position for BTC-USD/MAIN while completing session 7 (deleted after the session was created?)",
			"GetSessionForUpdate 7|ListSessionItemsOrderedForCompletion 7|ListAllPositions|GetAllPriceCache"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := completionBook()
			c.setup(q)
			_, err := completeSessionTx(context.Background(), q, 7, func(int64) (map[groupKey]decimal.Decimal, bool) { return nil, false })
			if err == nil || err.Error() != c.err {
				t.Fatalf("err = %v, want %q", err, c.err)
			}
			if got := strings.Join(q.calls, "|"); got != c.calls {
				t.Errorf("statements = %s, want %s", got, c.calls)
			}
		})
	}
}
