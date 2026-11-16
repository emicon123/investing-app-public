package purchasesession

import (
	"slices"
	"sort"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/rebalancingcalc"
)

// donorHolding is one crypto wrapper row of a donor-order fixture: its IKE/IKZE units.
type donorHolding struct {
	portfolio, ticker string
	assetID           int64
	ike, ikze         int64
}

// donorBook builds the book as ListAllPositions returns it (ORDER BY ticker), every wrapper being
// the primary of its own CRYPTO asset, all priced at 10 PLN.
func donorBook(holdings ...donorHolding) ([]rebalancingcalc.PositionRow, map[sellKey]decimal.Decimal) {
	positions := make([]rebalancingcalc.PositionRow, 0, len(holdings))
	prices := map[string]decimal.Decimal{}
	for _, h := range holdings {
		positions = append(positions, rebalancingcalc.PositionRow{
			Ticker: h.ticker, Portfolio: h.portfolio, Exchange: "XTB", AssetClass: "ETF",
			Asset: cryptoAsset(h.assetID, h.ticker), IsPrimary: true, YahooTicker: new(h.ticker + ".Y"),
			UnitsIke: decimal.NewFromInt(h.ike), UnitsIkze: decimal.NewFromInt(h.ikze),
		})
		prices[h.ticker+".Y"] = decimal.NewFromInt(10)
	}
	sort.SliceStable(positions, func(i, j int) bool { return positions[i].Ticker < positions[j].Ticker })
	sellable := map[sellKey]decimal.Decimal{}
	seedSellableCryptoPln(positions, prices, sellable)
	return positions, sellable
}

const (
	btcETN  = "PL.ETNVIRBTCP"
	ethETN  = "PL.ETNVIRETH"
	solETN  = "PL.ETNVIRSOL"
	xrpETN  = "PL.ETNVIRXRP"
	linkETN = "ETNVIRLINK.PL"
)

func mainIKE(ticker string, assetID int64) donorHolding {
	return donorHolding{portfolio: "MAIN", ticker: ticker, assetID: assetID, ike: 5}
}

// TestFindBestCryptoDonor_Order pins the owner's fixed donor order BTC -> ETH -> SOL -> XRP ->
// unlisted (alphabetical) on the cases that decided the order before task 192. "old" in a case
// comment is what the buy-list-deviation ranking chose on the same input.
func TestFindBestCryptoDonor_Order(t *testing.T) {
	cases := []struct {
		name      string
		holdings  []donorHolding
		account   string
		exclude   int64
		portfolio string
		want      string // "" = no donor
	}{
		{name: "all four plus LINK, ", holdings: []donorHolding{
			mainIKE(btcETN, 1), mainIKE(ethETN, 2), mainIKE(solETN, 3), mainIKE(xrpETN, 4), mainIKE(linkETN, 5)},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: btcETN},
		{name: "no BTC: ETH (old: LINK, first by ticker)", holdings: []donorHolding{
			mainIKE(ethETN, 2), mainIKE(solETN, 3), mainIKE(xrpETN, 4), mainIKE(linkETN, 5)},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: ethETN},
		{name: "only XRP and LINK: XRP before the unlisted LINK (old: LINK)", holdings: []donorHolding{
			mainIKE(xrpETN, 4), mainIKE(linkETN, 5)},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: xrpETN},
		{name: "recipient is BTC: tier 1 excludes its wrappers, ETH next", holdings: []donorHolding{
			mainIKE(btcETN, 1), mainIKE(ethETN, 2), mainIKE(solETN, 3)},
			account: "IKE", exclude: 1, portfolio: "MAIN", want: ethETN},
		{name: "IKZE: only wrappers with IKZE units qualify", holdings: []donorHolding{
			mainIKE(btcETN, 1), {portfolio: "MAIN", ticker: xrpETN, assetID: 4, ike: 5, ikze: 3}},
			account: "IKZE", exclude: 99, portfolio: "MAIN", want: xrpETN},
		{name: "tier 2: own portfolio has no donor, the other portfolio's BTC goes first", holdings: []donorHolding{
			{portfolio: "RETIREMENT", ticker: solETN, assetID: 13, ike: 5}, {portfolio: "RETIREMENT", ticker: btcETN, assetID: 11, ike: 5}},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: btcETN},
		{name: "tier 1 wins over a higher-ranked crypto in the other portfolio", holdings: []donorHolding{
			mainIKE(xrpETN, 4), {portfolio: "RETIREMENT", ticker: btcETN, assetID: 11, ike: 5}},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: xrpETN},
		{name: "no holdings in the account: no donor", holdings: []donorHolding{
			{portfolio: "MAIN", ticker: btcETN, assetID: 1, ike: 0, ikze: 0}},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: ""},
		{name: "SOL had a small positive buy-list deviation: BTC still first (old: SOL)", holdings: []donorHolding{
			mainIKE(btcETN, 1), mainIKE(ethETN, 2), mainIKE(solETN, 3), mainIKE(xrpETN, 4)},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: btcETN},
		{name: "BTC was underweight on the buy list: BTC still first (old: ETH)", holdings: []donorHolding{
			mainIKE(btcETN, 1), mainIKE(ethETN, 2), mainIKE(solETN, 3)},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: btcETN},
		{name: "SOL far over target: BTC still first (old: BTC)", holdings: []donorHolding{
			mainIKE(btcETN, 1), mainIKE(solETN, 3)},
			account: "IKE", exclude: 99, portfolio: "MAIN", want: btcETN},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			positions, sellable := donorBook(c.holdings...)
			donor := findBestCryptoDonor(c.portfolio, c.account, positions, sellable, c.exclude)
			got := ""
			if donor != nil {
				got = donor.Ticker
			}
			if got != c.want {
				t.Fatalf("donor = %q, want %q", got, c.want)
			}
		})
	}
}

// TestCryptoDonorOrder pins the constant itself and how unlisted cryptos rank.
func TestCryptoDonorOrder(t *testing.T) {
	want := []string{"PL.ETNVIRBTCP", "PL.ETNVIRETH", "PL.ETNVIRSOL", "PL.ETNVIRXRP"}
	if !slices.Equal(cryptoDonorOrder, want) {
		t.Fatalf("cryptoDonorOrder = %v, want %v", cryptoDonorOrder, want)
	}
	for i, ticker := range want {
		if got := cryptoDonorRank(ticker); got != i {
			t.Errorf("rank(%s) = %d, want %d", ticker, got, i)
		}
	}
	if a, b := cryptoDonorRank("ETNVIRLINK.PL"), cryptoDonorRank("ZZZ"); a != len(want) || b != len(want) {
		t.Errorf("unlisted tickers must share the rank after XRP (%d), got %d and %d", len(want), a, b)
	}
	tests := []struct {
		a, b string
		want bool
	}{
		{"PL.ETNVIRBTCP", "PL.ETNVIRETH", true},
		{"PL.ETNVIRXRP", "PL.ETNVIRSOL", false},
		{"PL.ETNVIRXRP", "ETNVIRLINK.PL", true},  // listed before unlisted although later alphabetically
		{"ETNVIRLINK.PL", "ETNVIRZZZ.PL", true},  // unlisted: alphabetical
		{"ETNVIRZZZ.PL", "ETNVIRLINK.PL", false}, // unlisted: alphabetical
	}
	for _, c := range tests {
		if got := donorBefore(c.a, c.b); got != c.want {
			t.Errorf("donorBefore(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestFindBestCryptoDonor_IndependentOfPositionOrder: the choice must not depend on the order the
// book lists the rows in (the old code let ties fall to the first row).
func TestFindBestCryptoDonor_IndependentOfPositionOrder(t *testing.T) {
	positions, sellable := donorBook(mainIKE(btcETN, 1), mainIKE(ethETN, 2), mainIKE(solETN, 3), mainIKE(linkETN, 5))
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}} {
		shuffled := make([]rebalancingcalc.PositionRow, 0, len(positions))
		for _, i := range order {
			shuffled = append(shuffled, positions[i])
		}
		if donor := findBestCryptoDonor("MAIN", "IKE", shuffled, sellable, 99); donor == nil || donor.Ticker != btcETN {
			t.Fatalf("order %v: donor = %+v, want %s", order, donor, btcETN)
		}
	}
}
