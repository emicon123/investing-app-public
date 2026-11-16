package rebalancing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// Characterisation tests for ExecuteTransfer (task 185 B2): the exact query sequence a transfer
// issues inside its transaction, and the exact error (and the reads before it) for every rejection
// path, so the validate/load/plan/apply split is provably behaviour-neutral.

// traceFixture is one asset with three subassets (SOLDA, SIBA, SAMEA, asset 10) plus another asset
// (BUYB, asset 20), all MAIN/XTB/PLN and priced in price_cache.
func traceFixture() *fakeQueries {
	fq := newFakeQueries()
	add := func(id, assetID int64, ticker string, pln, ike decimal.Decimal, cost *decimal.Decimal) {
		r := mkRow(id, ticker, "MAIN", "XTB", "PLN", pln, ike, decimal.Zero)
		r.AssetID = assetID
		yahoo := ticker + ".Y"
		r.YahooTicker = &yahoo
		r.TotalSpentPln = pgnum.FromDecimalPtr(cost)
		fq.rows[id] = r
	}
	dec := func(v int64) *decimal.Decimal { d := decimal.NewFromInt(v); return &d }
	add(1, 10, "SOLDA", decimal.NewFromInt(100), decimal.NewFromInt(20), dec(1000))
	add(2, 10, "SIBA", decimal.NewFromInt(50), decimal.Zero, dec(600))
	add(3, 20, "BUYB", decimal.Zero, decimal.Zero, nil)
	add(4, 10, "SAMEA", decimal.Zero, decimal.Zero, nil)
	for ticker, price := range map[string]int64{"SOLDA.Y": 12, "SIBA.Y": 12, "BUYB.Y": 30, "SAMEA.Y": 24} {
		fq.prices = append(fq.prices, sqlcgen.GetAllPriceCacheRow{Ticker: ticker, PricePln: pgnum.FromDecimal(decimal.NewFromInt(price))})
	}
	return fq
}

func leg(ticker, account string, units, price int64) Leg {
	return Leg{Ticker: ticker, AccountType: account, Units: decimal.NewFromInt(units), PricePln: decimal.NewFromInt(price)}
}

func TestExecuteTransfer_CallTrace_Golden(t *testing.T) {
	cases := []struct {
		name     string
		req      TransferRequest
		affected []string
		message  string
		trace    []string
	}{
		{
			name: "REGULAR sale funding a cross-asset REGULAR buy and a same-asset IKE buy",
			req: TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 40, 12),
				Buys: []Leg{leg("BUYB", "REGULAR", 8, 30), leg("SAMEA", "IKE", 10, 24)}},
			affected: []string{"SOLDA", "BUYB", "SAMEA"},
			message:  "Rebalancing transfer completed: sold SOLDA, bought 2 asset(s).",
			trace: []string{
				"ListAllPositions",
				"GetAllPriceCache",
				"UpdatePositionDelta id=1 ike=20 ikze=0 eur=0 usd=0 pln=60 cost=764.7059",
				"InsertAssetPurchase MAIN SOLDA REGULAR units=-40 price=12 total=-480 type=REBALANCING_TRIM_SELL session=<nil>",
				"UpdatePositionDelta id=3 ike=0 ikze=0 eur=0 usd=0 pln=8 cost=240",
				"InsertAssetPurchase MAIN BUYB REGULAR units=8 price=30 total=240 type=REBALANCING_REINVEST_BUY session=<nil>",
				"UpdatePositionDelta id=4 ike=10 ikze=0 eur=0 usd=0 pln=0 cost=188.2353",
				"InsertAssetPurchase MAIN SAMEA IKE units=10 price=24 total=240 type=REBALANCING_REINVEST_BUY session=<nil>",
				`InsertDeposit MAIN XTB IKE PLN amount=240 source=INTERNAL_TRANSFER comment="Auto from rebalancing transfer: sold SOLDA (REGULAR) to fund SAMEA (IKE)"`,
				"UpdatePositionCost id=2 cost=458.8235",
			},
		},
		{
			name: "the sold row bought back into IKE, then a second leg (one row written twice)",
			req: TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 10, 12),
				Buys: []Leg{leg("SOLDA", "IKE", 5, 24), leg("BUYB", "REGULAR", 1, 30)}},
			affected: []string{"SOLDA", "BUYB"},
			message:  "Rebalancing transfer completed: sold SOLDA, bought 2 asset(s).",
			trace: []string{
				"ListAllPositions",
				"GetAllPriceCache",
				"UpdatePositionDelta id=1 ike=20 ikze=0 eur=0 usd=0 pln=90 cost=1035.2941",
				"InsertAssetPurchase MAIN SOLDA REGULAR units=-10 price=12 total=-120 type=REBALANCING_TRIM_SELL session=<nil>",
				"UpdatePositionDelta id=1 ike=25 ikze=0 eur=0 usd=0 pln=90 cost=1035.2941",
				"InsertAssetPurchase MAIN SOLDA IKE units=5 price=24 total=120 type=REBALANCING_REINVEST_BUY session=<nil>",
				`InsertDeposit MAIN XTB IKE PLN amount=120 source=INTERNAL_TRANSFER comment="Auto from rebalancing transfer: sold SOLDA (REGULAR) to fund SOLDA (IKE)"`,
				"UpdatePositionDelta id=3 ike=0 ikze=0 eur=0 usd=0 pln=1 cost=30",
				"InsertAssetPurchase MAIN BUYB REGULAR units=1 price=30 total=30 type=REBALANCING_REINVEST_BUY session=<nil>",
				"UpdatePositionCost id=2 cost=564.7059",
			},
		},
		{
			name: "IKE sale reinvested inside IKE writes no deposit",
			req: TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "IKE", 20, 12),
				Buys: []Leg{leg("BUYB", "IKE", 8, 30)}},
			affected: []string{"SOLDA", "BUYB"},
			message:  "Rebalancing transfer completed: sold SOLDA, bought 1 asset(s).",
			trace: []string{
				"ListAllPositions",
				"GetAllPriceCache",
				"UpdatePositionDelta id=1 ike=0 ikze=0 eur=0 usd=0 pln=100 cost=882.3529",
				"InsertAssetPurchase MAIN SOLDA IKE units=-20 price=12 total=-240 type=REBALANCING_TRIM_SELL session=<nil>",
				"UpdatePositionDelta id=3 ike=8 ikze=0 eur=0 usd=0 pln=0 cost=240",
				"InsertAssetPurchase MAIN BUYB IKE units=8 price=30 total=240 type=REBALANCING_REINVEST_BUY session=<nil>",
				"UpdatePositionCost id=2 cost=529.4118",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fq := traceFixture()
			got, err := newService(fq).ExecuteTransfer(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("ExecuteTransfer: %v", err)
			}
			if strings.Join(got.AffectedTickers, ",") != strings.Join(tc.affected, ",") || got.Message != tc.message {
				t.Errorf("result = %+v, want affected=%v message=%q", got, tc.affected, tc.message)
			}
			if strings.Join(fq.calls, "\n") != strings.Join(tc.trace, "\n") {
				t.Errorf("call trace differs\n got:\n%s\nwant:\n%s", strings.Join(fq.calls, "\n"), strings.Join(tc.trace, "\n"))
			}
		})
	}
}

func TestExecuteTransfer_Rejections_ExactErrorAndReads(t *testing.T) {
	cases := []struct {
		name    string
		req     TransferRequest
		status  int
		field   string
		message string
		calls   []string // reads issued before the rejection
		inTx    bool     // whether a transaction was opened
	}{
		{"no buy legs", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 1, 12)},
			400, "buys", "at least one buy leg is required", nil, false},
		{"sold units not positive", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 0, 12), Buys: []Leg{leg("BUYB", "REGULAR", 1, 30)}},
			400, "sold.units", "units must be positive", nil, false},
		{"sold price not positive", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 1, 0), Buys: []Leg{leg("BUYB", "REGULAR", 1, 30)}},
			400, "sold.pricePln", "pricePln must be positive", nil, false},
		{"buy units not positive", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 1, 12), Buys: []Leg{leg("BUYB", "REGULAR", 1, 30), leg("SAMEA", "REGULAR", -1, 30)}},
			400, "buys.units", "units must be positive", nil, false},
		{"buy price not positive", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 1, 12), Buys: []Leg{leg("BUYB", "REGULAR", 1, 0)}},
			400, "buys.pricePln", "pricePln must be positive", nil, false},
		{"unknown sold ticker", TransferRequest{Portfolio: "MAIN", Sold: leg("NOPE", "REGULAR", 1, 12), Buys: []Leg{leg("BUYB", "REGULAR", 1, 30)}},
			404, "", "Position not found for ticker: NOPE", []string{"ListAllPositions"}, true},
		{"sold ticker of the other portfolio", TransferRequest{Portfolio: "RETIREMENT", Sold: leg("SOLDA", "REGULAR", 1, 12), Buys: []Leg{leg("BUYB", "REGULAR", 1, 30)}},
			404, "", "Position not found for ticker: SOLDA", []string{"ListAllPositions"}, true},
		{"insufficient units in the sold bucket", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "IKE", 21, 12), Buys: []Leg{leg("BUYB", "IKE", 1, 30)}},
			400, "", "Insufficient units for ticker SOLDA (IKE): have 20, selling 21", []string{"ListAllPositions"}, true},
		{"insufficient units wins over an unknown buy ticker", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 101, 12), Buys: []Leg{leg("NOPE", "REGULAR", 1, 30)}},
			400, "", "Insufficient units for ticker SOLDA (REGULAR): have 100, selling 101", []string{"ListAllPositions"}, true},
		{"unknown buy ticker", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "REGULAR", 1, 12), Buys: []Leg{leg("BUYB", "REGULAR", 1, 30), leg("NOPE", "REGULAR", 1, 30)}},
			404, "", "Position not found for ticker: NOPE", []string{"ListAllPositions"}, true},
		{"unknown buy ticker wins over the wrapper lock", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "IKE", 1, 12), Buys: []Leg{leg("NOPE", "REGULAR", 1, 30)}},
			404, "", "Position not found for ticker: NOPE", []string{"ListAllPositions"}, true},
		{"wrapper lock IKE into REGULAR", TransferRequest{Portfolio: "MAIN", Sold: leg("SOLDA", "IKE", 1, 12), Buys: []Leg{leg("BUYB", "IKE", 1, 30), leg("SAMEA", "REGULAR", 1, 24)}},
			400, "", "Cannot transfer out of IKE into REGULAR: IKE holdings can only be reinvested within the same wrapper (sold SOLDA from IKE, attempted buy of SAMEA into REGULAR)",
			[]string{"ListAllPositions"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fq := traceFixture()
			txs := 0
			svc := NewService(fq, func(_ context.Context, fn func(Queries) error) error { txs++; return fn(fq) })
			_, err := svc.ExecuteTransfer(context.Background(), tc.req)
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) {
				t.Fatalf("expected *httpx.AppError, got %v", err)
			}
			if appErr.Status != tc.status || appErr.Field != tc.field || appErr.Message != tc.message {
				t.Errorf("error = %d %q %q, want %d %q %q", appErr.Status, appErr.Field, appErr.Message, tc.status, tc.field, tc.message)
			}
			if strings.Join(fq.calls, "|") != strings.Join(tc.calls, "|") {
				t.Errorf("reads before the rejection = %v, want %v", fq.calls, tc.calls)
			}
			if (txs == 1) != tc.inTx || txs > 1 {
				t.Errorf("transactions opened = %d, want inTx=%v", txs, tc.inTx)
			}
		})
	}
}
