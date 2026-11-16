package purchasesession

import (
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/rebalancingcalc"
)

// Characterisation goldens for the waterfall branches scnGolden does not reach (task 185 B3,
// captured from the planner before the planState refactor): a non-wrapper item whose IKE and IKZE
// new-deposit rows absorb the donor sells (mergeSellPlnIntoRow), the IKZE crypto-sell step, the
// cross-portfolio fallback tier netting against a RETIREMENT wrapper row the RETIREMENT pass
// booked, a not-IKE/IKZE-eligible item, and the unpriced-wrapper error.

func branchBook(withWrapperPrice bool) ([]rebalancingcalc.PositionRow, map[string]decimal.Decimal) {
	stock := rebalancingcalc.AssetRef{ID: 3, Key: "stk", Class: "STOCK"}
	other := rebalancingcalc.AssetRef{ID: 4, Key: "notx", Class: "ETF"}
	row := func(ticker, portfolio, exchange, class string, asset rebalancingcalc.AssetRef, primary bool, ike, ikze int64) rebalancingcalc.PositionRow {
		return rebalancingcalc.PositionRow{
			Ticker: ticker, Portfolio: portfolio, Exchange: exchange, AssetClass: class, Asset: asset, IsPrimary: primary,
			YahooTicker: new(ticker + ".Y"), UnitsIke: decimal.NewFromInt(ike), UnitsIkze: decimal.NewFromInt(ikze),
		}
	}
	positions := []rebalancingcalc.PositionRow{
		row("BTC-USD", "MAIN", "BINANCE", "CRYPTO", cryptoAsset(1, "bitcoin"), true, 0, 0),
		row("ETH-USD", "MAIN", "BINANCE", "CRYPTO", cryptoAsset(2, "ethereum"), true, 0, 0),
		row("NOTX", "MAIN", "BINANCE", "ETF", other, true, 0, 0),
		row("PL.ETNVIRBTCP", "MAIN", "XTB", "ETF", cryptoAsset(1, "bitcoin"), false, 2, 1),
		row("PL.ETNVIRETH", "MAIN", "XTB", "ETF", cryptoAsset(2, "ethereum"), false, 1, 3),
		row("PL.ETNVIRSOL", "RETIREMENT", "XTB", "ETF", cryptoAsset(5, "solana"), false, 10, 0),
		row("SOL-USD", "RETIREMENT", "BINANCE", "CRYPTO", cryptoAsset(5, "solana"), true, 0, 0),
		row("STK", "MAIN", "XTB", "STOCK", stock, true, 0, 0),
	}
	prices := map[string]decimal.Decimal{
		"BTC-USD.Y": decimal.NewFromInt(300000), "ETH-USD.Y": decimal.NewFromInt(10000), "NOTX.Y": decimal.NewFromInt(50),
		"PL.ETNVIRETH.Y": decimal.NewFromInt(150), "PL.ETNVIRSOL.Y": decimal.NewFromInt(40),
		"SOL-USD.Y": decimal.NewFromInt(800), "STK.Y": decimal.NewFromInt(100),
	}
	if withWrapperPrice {
		prices["PL.ETNVIRBTCP.Y"] = decimal.NewFromInt(300)
	}
	return positions, prices
}

// The two MAIN-pass goldens were re-recorded for task 192: they pinned the old deviation-ranked donor
// (ETH over BTC for the STK item); the fixed order picks the BTC ETN. Their sell/compensating-buy
// structure is unchanged.
func TestComputePlan_WaterfallBranches_Golden(t *testing.T) {
	mainList := []rebalancingcalc.BuyListItem{buyItem("STK", "1000"), buyItem("BTC-USD", "900"), buyItem("NOTX", "50")}
	cases := []struct {
		name      string
		retList   []rebalancingcalc.BuyListItem
		ike, ikze int64
		want      string
	}{
		{
			name: "MAIN only: merges into the item's IKE/IKZE rows, IKZE sell step, cross-portfolio tier",
			ike:  300, ikze: 200,
			want: `MAIN|STK|IKE|val=800|units=8|sell=false|buy=false
MAIN|STK|IKZE|val=200|units=2|sell=false|buy=false
MAIN|PL.ETNVIRETH|IKE|val=150|units=1|sell=true|buy=false
MAIN|PL.ETNVIRBTCP|IKE|val=350|units=1.16666667|sell=true|buy=false
MAIN|PL.ETNVIRETH|IKZE|val=450|units=3|sell=true|buy=false
MAIN|PL.ETNVIRBTCP|IKZE|val=450|units=1.5|sell=false|buy=false
MAIN|NOTX|REGULAR|val=50|units=0.5|sell=false|buy=false
MAIN|BTC-USD|REGULAR|val=800|units=0.00266667|sell=false|buy=true
MAIN|ETH-USD|REGULAR|val=600|units=0.06|sell=false|buy=true
newDeposit MAIN IKE=300 IKZE=200 RET IKE=0 IKZE=0
`,
		},
		{
			name:    "RETIREMENT pass books the wrapper row the MAIN fallback tier then nets against",
			retList: []rebalancingcalc.BuyListItem{buyItem("SOL-USD", "100")},
			ike:     400, ikze: 200,
			want: `RETIREMENT|PL.ETNVIRSOL|IKE|val=100|units=2.5|sell=false|buy=false
MAIN|STK|IKE|val=800|units=8|sell=false|buy=false
MAIN|STK|IKZE|val=200|units=2|sell=false|buy=false
MAIN|PL.ETNVIRETH|IKE|val=150|units=1|sell=true|buy=false
MAIN|PL.ETNVIRBTCP|IKE|val=350|units=1.16666667|sell=true|buy=false
MAIN|PL.ETNVIRETH|IKZE|val=450|units=3|sell=true|buy=false
MAIN|PL.ETNVIRBTCP|IKZE|val=450|units=1.5|sell=false|buy=false
MAIN|NOTX|REGULAR|val=50|units=0.5|sell=false|buy=false
MAIN|BTC-USD|REGULAR|val=800|units=0.00266667|sell=false|buy=true
MAIN|ETH-USD|REGULAR|val=600|units=0.06|sell=false|buy=true
newDeposit MAIN IKE=300 IKZE=200 RET IKE=100 IKZE=0
`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			positions, prices := branchBook(true)
			res, err := ComputePlan(c.retList, mainList, decimal.NewFromInt(c.ike), decimal.NewFromInt(c.ikze), prices, positions)
			if err != nil {
				t.Fatalf("ComputePlan: %v", err)
			}
			if got := scnRender(res.Items, res.NewDepositPlnByBucket); got != c.want {
				t.Fatalf("plan differs from the pre-refactor golden.\n--- golden\n%s\n--- got\n%s", c.want, got)
			}
		})
	}
}

func TestComputePlan_UnpricedWrapper_FailsLoud(t *testing.T) {
	positions, prices := branchBook(false)
	_, err := ComputePlan(nil, []rebalancingcalc.BuyListItem{buyItem("BTC-USD", "900")},
		decimal.NewFromInt(300), decimal.Zero, prices, positions)
	const want = "purchasesession: no price available for subasset PL.ETNVIRBTCP (yahoo: PL.ETNVIRBTCP.Y)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
