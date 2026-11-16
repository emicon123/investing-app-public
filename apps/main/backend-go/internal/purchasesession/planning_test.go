package purchasesession

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/rebalancingcalc"
)

// cryptoAsset is the asset identity fixture for the crypto groups (asset_class CRYPTO is the
// donor/substitution predicate, task 156 design §5/§7).
func cryptoAsset(id int64, key string) rebalancingcalc.AssetRef {
	return rebalancingcalc.AssetRef{ID: id, Key: key, Class: "CRYPTO"}
}

func buyItem(ticker string, allocated string) rebalancingcalc.BuyListItem {
	return rebalancingcalc.BuyListItem{
		Ticker: ticker, Name: ticker, Exchange: "XTB", PurchaseCurrency: "PLN",
		AllocatedPln: decimal.RequireFromString(allocated), AllocatedNative: decimal.RequireFromString(allocated),
		UnitsToBuy: decimal.RequireFromString(allocated).Div(decimal.NewFromInt(100)),
	}
}

func TestComputePlan_FillsIkeBeforeIkzeBeforeRegular(t *testing.T) {
	positions := []rebalancingcalc.PositionRow{
		{Ticker: "ETF1", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", YahooTicker: new("ETF1.YAHOO")},
	}
	prices := map[string]decimal.Decimal{"ETF1.YAHOO": decimal.NewFromInt(100)}

	res, err := ComputePlan(nil, []rebalancingcalc.BuyListItem{buyItem("ETF1", "1000")},
		decimal.NewFromInt(400), decimal.NewFromInt(300), prices, positions)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}

	var ike, ikze, regular *SessionItem
	for _, item := range res.Items {
		switch item.AccountType {
		case "IKE":
			ike = item
		case "IKZE":
			ikze = item
		case "REGULAR":
			regular = item
		}
	}
	if ike == nil || !valueOrZero(ike.PlannedValuePln).Equal(decimal.NewFromInt(400)) {
		t.Fatalf("expected IKE filled to its full 400 remaining, got %+v", ike)
	}
	if ikze == nil || !valueOrZero(ikze.PlannedValuePln).Equal(decimal.NewFromInt(300)) {
		t.Fatalf("expected IKZE filled to its full 300 remaining, got %+v", ikze)
	}
	if regular == nil || !valueOrZero(regular.PlannedValuePln).Equal(decimal.NewFromInt(300)) {
		t.Fatalf("expected REGULAR to take the true remainder (1000-400-300=300), got %+v", regular)
	}
	if res.NewDepositPlnByBucket[groupKey{"MAIN", "IKE"}].Cmp(decimal.NewFromInt(400)) != 0 {
		t.Fatalf("expected newDepositPlnByBucket[MAIN,IKE]=400, got %v", res.NewDepositPlnByBucket)
	}
}

// TestFindBestCryptoDonor_NeverTriggeredByOverTargetAlone is the explicit reviewer-checkpoint
// test architect-output §3 item 3 demands: a position that is simply over-target (zero
// allocation in the buy list, or absent from it entirely) must NEVER become a sell candidate by
// that fact alone. Donor eligibility comes ONLY from sellableCryptoPln — seeded purely from
// actual IKE/IKZE-held balances of subassets of CRYPTO assets (seedSellableCryptoPln), completely
// independent of the buy list or deviationPct.
func TestFindBestCryptoDonor_NeverTriggeredByOverTargetAlone(t *testing.T) {
	// BTC-USD is a crypto position that received ZERO buy-list allocation (i.e. it is
	// over-target) and has NO IKE/IKZE-held companion balance at all (its companion has zero
	// units). It must never be selected as a donor.
	positions := []rebalancingcalc.PositionRow{
		{Ticker: "BTC-USD", Portfolio: "MAIN", Exchange: "BINANCE", AssetClass: "CRYPTO", Asset: cryptoAsset(1, "BTC-USD"), IsPrimary: true, YahooTicker: new("BTC-USD")},
		{Ticker: "PL.ETNVIRBTCP", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", Asset: cryptoAsset(1, "BTC-USD"),
			YahooTicker: new("PL.ETNVIRBTCP.YAHOO"),
			UnitsIke:    decimal.Zero, UnitsIkze: decimal.Zero}, // zero balance -> never sellable
	}
	prices := map[string]decimal.Decimal{"PL.ETNVIRBTCP.YAHOO": decimal.NewFromInt(500)}
	sellable := map[sellKey]decimal.Decimal{}
	seedSellableCryptoPln(positions, prices, sellable)

	// Being over target is not an input of the donor choice at all (task 192: the order is fixed).
	donor := findBestCryptoDonor("MAIN", "IKE", positions, sellable, 99) // recipient = some other asset
	if donor != nil {
		t.Fatalf("expected no donor (zero live balance), got %+v — over-target/deviation alone must never trigger a sell", donor)
	}
}

func TestFindBestCryptoDonor_OnlyEligibleWithLiveIkeIkzeBalance(t *testing.T) {
	positions := []rebalancingcalc.PositionRow{
		{Ticker: "PL.ETNVIRBTCP", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", Asset: cryptoAsset(1, "BTC-USD"),
			YahooTicker: new("PL.ETNVIRBTCP.YAHOO"),
			UnitsIke:    decimal.NewFromInt(10), UnitsIkze: decimal.Zero},
		{Ticker: "BTC-USD", Portfolio: "MAIN", Exchange: "BINANCE", AssetClass: "CRYPTO", Asset: cryptoAsset(1, "BTC-USD"), IsPrimary: true, YahooTicker: new("BTC-USD")},
	}
	prices := map[string]decimal.Decimal{"PL.ETNVIRBTCP.YAHOO": decimal.NewFromInt(500)}
	sellable := map[sellKey]decimal.Decimal{}
	seedSellableCryptoPln(positions, prices, sellable)

	// Eligible for IKE (has live IKE balance)...
	donor := findBestCryptoDonor("MAIN", "IKE", positions, sellable, 99)
	if donor == nil || donor.Ticker != "PL.ETNVIRBTCP" {
		t.Fatalf("expected PL.ETNVIRBTCP donor for IKE, got %+v", donor)
	}
	// ...but NOT for IKZE (zero IKZE balance seeded).
	donor = findBestCryptoDonor("MAIN", "IKZE", positions, sellable, 99)
	if donor != nil {
		t.Fatalf("expected no IKZE donor (zero IKZE balance), got %+v", donor)
	}
}

func TestFindBestCryptoDonor_SelfDonationExcludedInSamePortfolioTier(t *testing.T) {
	positions := []rebalancingcalc.PositionRow{
		{Ticker: "PL.ETNVIRBTCP", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", Asset: cryptoAsset(1, "BTC-USD"),
			YahooTicker: new("PL.ETNVIRBTCP.YAHOO"),
			UnitsIke:    decimal.NewFromInt(10)},
	}
	prices := map[string]decimal.Decimal{"PL.ETNVIRBTCP.YAHOO": decimal.NewFromInt(500)}
	sellable := map[sellKey]decimal.Decimal{}
	seedSellableCryptoPln(positions, prices, sellable)

	// A recipient item that IS BTC-USD itself must not sell its own wrapper subasset in tier 1
	// (same-portfolio self-donation exclusion, ADR-061 §3) — and since there is no other
	// portfolio with a candidate, findBestCryptoDonor returns nil.
	donor := findBestCryptoDonor("MAIN", "IKE", positions, sellable, 1) // recipient = the donor's own asset
	if donor != nil {
		t.Fatalf("expected self-donation exclusion to block this donor, got %+v", donor)
	}
}

func TestComputePlan_CryptoDonorSellIsAlwaysPairedWithCompensatingBuy(t *testing.T) {
	// BOTH IKE and IKZE are exhausted (0 remaining) so the crypto-donor waterfall is the only
	// path left. ETH-USD needs an IKE allocation but can't hold crypto directly on IKE, so its
	// wrapper subasset PL.ETNVIRETH substitutes; the only sellable donor is BTC's own wrapper
	// (held on IKE). Expect: a migration-sell of the BTC companion PLUS a compensating
	// REGULAR buy of BTC-USD restoring the exact same PLN of crypto exposure.
	positions := []rebalancingcalc.PositionRow{
		{Ticker: "ETH-USD", Portfolio: "MAIN", Exchange: "BINANCE", AssetClass: "CRYPTO", Asset: cryptoAsset(1, "ETH-USD"), IsPrimary: true, YahooTicker: new("ETH-USD")},
		{Ticker: "PL.ETNVIRETH", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", Asset: cryptoAsset(1, "ETH-USD"),
			YahooTicker: new("PL.ETNVIRETH.YAHOO")},
		{Ticker: "BTC-USD", Portfolio: "MAIN", Exchange: "BINANCE", AssetClass: "CRYPTO", Asset: cryptoAsset(2, "BTC-USD"), IsPrimary: true, YahooTicker: new("BTC-USD")},
		{Ticker: "PL.ETNVIRBTCP", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", Asset: cryptoAsset(2, "BTC-USD"),
			YahooTicker: new("PL.ETNVIRBTCP.YAHOO"),
			UnitsIke:    decimal.NewFromInt(1)},
	}
	prices := map[string]decimal.Decimal{
		"ETH-USD": decimal.NewFromInt(2000), "PL.ETNVIRETH.YAHOO": decimal.NewFromInt(2000),
		"BTC-USD": decimal.NewFromInt(50000), "PL.ETNVIRBTCP.YAHOO": decimal.NewFromInt(50000),
	}
	res, err := ComputePlan(nil, []rebalancingcalc.BuyListItem{buyItem("ETH-USD", "100")},
		decimal.Zero, decimal.Zero, prices, positions)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}

	var sell, compensatingBuy *SessionItem
	for _, item := range res.Items {
		if item.Ticker == "PL.ETNVIRBTCP" && item.IsMigrationSell {
			sell = item
		}
		if item.Ticker == "BTC-USD" && item.IsMigrationBuy {
			compensatingBuy = item
		}
	}
	if sell == nil {
		t.Fatalf("expected a migration-sell of the BTC companion, got items: %+v", res.Items)
	}
	if compensatingBuy == nil {
		t.Fatalf("expected a compensating REGULAR buy of BTC-USD restoring the sold exposure, got items: %+v", res.Items)
	}
	if !valueOrZero(sell.PlannedValuePln).Equal(valueOrZero(compensatingBuy.PlannedValuePln)) {
		t.Fatalf("expected the sold PLN value (%s) to exactly match the compensating buy (%s) — the donor's exposure must be fully restored",
			valueOrZero(sell.PlannedValuePln), valueOrZero(compensatingBuy.PlannedValuePln))
	}
}

func TestComputePlan_NoDonorAvailable_LeavesRemainderAsPlainBuy(t *testing.T) {
	positions := []rebalancingcalc.PositionRow{
		{Ticker: "ETH-USD", Portfolio: "MAIN", Exchange: "BINANCE", AssetClass: "CRYPTO", YahooTicker: new("ETH-USD")},
	}
	prices := map[string]decimal.Decimal{"ETH-USD": decimal.NewFromInt(2000)}
	res, err := ComputePlan(nil, []rebalancingcalc.BuyListItem{buyItem("ETH-USD", "100")},
		decimal.Zero, decimal.Zero, prices, positions)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].AccountType != "REGULAR" || res.Items[0].IsMigrationSell {
		t.Fatalf("expected a single plain REGULAR buy with no donor available, got %+v", res.Items)
	}
}

// ── Task 156: asset-model re-expression proofs ─────────────────────────────────────────────

type scnRow struct {
	portfolio, ticker, parent, exchange, class string
	ike, ikze                                  int64
}

// scnBook is the real main book's group shapes (13 companions across MAIN/RETIREMENT) in
// ListAllPositions order (ORDER BY ticker; MAIN before RETIREMENT on equal tickers).
var scnBook = []scnRow{
	{"MAIN", "4GLD.DE", "ETCGLDRMAU.PL", "XTB", "ETF", 0, 0},
	{"MAIN", "BTC-USD", "", "XTB", "CRYPTO", 0, 0},
	{"RETIREMENT", "BTC-USD", "", "XTB", "CRYPTO", 0, 0},
	{"MAIN", "CIFR", "", "XTB", "STOCK", 5, 0},
	{"MAIN", "ETCGLDRMAU.PL", "", "XTB", "ETF", 182, 83},
	{"RETIREMENT", "ETCGLDRMAU.PL", "", "XTB", "ETF", 48, 28},
	{"MAIN", "ETH-USD", "", "XTB", "CRYPTO", 0, 0},
	{"RETIREMENT", "ETH-USD", "", "XTB", "CRYPTO", 0, 0},
	{"MAIN", "ETNVIRLINK.PL", "LINK-USD", "XTB", "ETF", 0, 0},
	{"MAIN", "IGLN.L", "ETCGLDRMAU.PL", "XTB", "ETF", 0, 0},
	{"RETIREMENT", "ISLN.L", "XAD6", "XTB", "ETF", 0, 0},
	{"MAIN", "LINK-USD", "", "BINANCE", "CRYPTO", 0, 0},
	{"MAIN", "PL.ETNVIRBTCP", "BTC-USD", "XTB", "ETF", 574, 0},
	{"RETIREMENT", "PL.ETNVIRBTCP", "BTC-USD", "XTB", "ETF", 153, 0},
	{"MAIN", "PL.ETNVIRETH", "ETH-USD", "XTB", "ETF", 0, 0},
	{"RETIREMENT", "PL.ETNVIRETH", "ETH-USD", "XTB", "ETF", 0, 0},
	{"MAIN", "PL.ETNVIRSOL", "SOL-USD", "XTB", "ETF", 378, 0},
	{"RETIREMENT", "PL.ETNVIRSOL", "SOL-USD", "XTB", "ETF", 281, 0},
	{"MAIN", "PL.ETNVIRXRP", "XRP-USD", "XTB", "ETF", 32, 22},
	{"MAIN", "QUTM.DE", "WQTM.DE", "XTB", "ETF", 0, 0},
	{"MAIN", "SOL-USD", "", "XTB", "CRYPTO", 0, 0},
	{"RETIREMENT", "SOL-USD", "", "XTB", "CRYPTO", 0, 0},
	{"MAIN", "SUI-USDT", "", "BINANCE", "CRYPTO", 0, 0},
	{"MAIN", "VRTU.DE", "SUI-USDT", "XTB", "ETF", 0, 0},
	{"MAIN", "WQTM.DE", "", "XTB", "ETF", 0, 0},
	{"RETIREMENT", "XAD6", "", "XTB", "ETF", 3, 0},
	{"MAIN", "XRP-USD", "", "BINANCE", "CRYPTO", 0, 0},
}

var scnPrices = map[string]decimal.Decimal{
	"4GLD.DE.Y": decimal.NewFromInt(350), "BTC-USD.Y": decimal.NewFromInt(300000), "CIFR.Y": decimal.NewFromInt(40),
	"ETCGLDRMAU.PL.Y": decimal.NewFromInt(400), "ETH-USD.Y": decimal.NewFromInt(10000), "ETNVIRLINK.PL.Y": decimal.NewFromInt(60),
	"IGLN.L.Y": decimal.NewFromInt(300), "ISLN.L.Y": decimal.NewFromInt(90), "LINK-USD.Y": decimal.NewFromInt(70),
	"PL.ETNVIRBTCP.Y": decimal.NewFromInt(300), "PL.ETNVIRETH.Y": decimal.NewFromInt(150), "PL.ETNVIRSOL.Y": decimal.NewFromInt(40),
	"PL.ETNVIRXRP.Y": decimal.NewFromInt(12), "QUTM.DE.Y": decimal.NewFromInt(45), "SOL-USD.Y": decimal.NewFromInt(800),
	"SUI-USDT.Y": decimal.NewFromInt(15), "VRTU.DE.Y": decimal.NewFromInt(25), "WQTM.DE.Y": decimal.NewFromInt(50),
	"XAD6.Y": decimal.NewFromInt(80), "XRP-USD.Y": decimal.NewFromInt(8),
}

var scnMainList = []rebalancingcalc.BuyListItem{
	buyItem("BTC-USD", "800"), buyItem("ETH-USD", "400"), buyItem("LINK-USD", "300"), buyItem("SUI-USDT", "250"),
	buyItem("XRP-USD", "200"), buyItem("ETCGLDRMAU.PL", "500"), buyItem("WQTM.DE", "100"), buyItem("CIFR", "150"),
	buyItem("SOL-USD", "350"),
}

var scnRetList = []rebalancingcalc.BuyListItem{
	buyItem("BTC-USD", "300"), buyItem("ETH-USD", "200"), buyItem("XAD6", "100"), buyItem("ETCGLDRMAU.PL", "250"),
	buyItem("SOL-USD", "150"),
}

// scnRender canonicalizes a plan for comparison: the compensating-migration-buy block is
// sorted (the legacy planner emitted it in Go map-iteration order — arbitrary), everything else
// keeps its planned order.
func scnRender(items []*SessionItem, nd map[groupKey]decimal.Decimal) string {
	var lines, buys []string
	for _, it := range items {
		line := fmt.Sprintf("%s|%s|%s|val=%s|units=%s|sell=%v|buy=%v", it.Portfolio, it.Ticker, it.AccountType,
			valueOrZero(it.PlannedValuePln).String(), valueOrZero(it.PlannedUnits).String(), it.IsMigrationSell, it.IsMigrationBuy)
		if it.IsMigrationBuy {
			buys = append(buys, line)
		} else {
			lines = append(lines, line)
		}
	}
	sort.Strings(buys)
	lines = append(lines, buys...)
	lines = append(lines, fmt.Sprintf("newDeposit MAIN IKE=%s IKZE=%s RET IKE=%s IKZE=%s",
		nd[groupKey{"MAIN", "IKE"}], nd[groupKey{"MAIN", "IKZE"}], nd[groupKey{"RETIREMENT", "IKE"}], nd[groupKey{"RETIREMENT", "IKZE"}]))
	return strings.Join(lines, "\n") + "\n"
}

var scnCases = []struct {
	name      string
	ike, ikze int64
}{
	{"limits-available", 500, 200},
	{"limits-exhausted-donor-waterfall", 0, 0},
	{"ike-only-partially-left", 100, 0},
}

// scnAssetRows expresses scnBook on the asset model: one asset per legacy group (id assigned in
// first-head order, asset_class = the head row's class as V74 backfills it), the old head is the
// primary, every old companion a non-primary subasset of the same asset.
func scnAssetRows() []rebalancingcalc.PositionRow {
	assetIDByHead := map[string]int64{}
	assetClassByHead := map[string]string{}
	next := int64(1)
	for _, r := range scnBook {
		if r.parent == "" {
			assetIDByHead[r.portfolio+"/"+r.ticker] = next
			assetClassByHead[r.portfolio+"/"+r.ticker] = r.class
			next++
		}
	}
	var out []rebalancingcalc.PositionRow
	for _, r := range scnBook {
		head := r.ticker
		if r.parent != "" {
			head = r.parent
		}
		k := r.portfolio + "/" + head
		y := r.ticker + ".Y"
		out = append(out, rebalancingcalc.PositionRow{
			Ticker: r.ticker, Portfolio: r.portfolio, Exchange: r.exchange, AssetClass: r.class,
			Asset:     rebalancingcalc.AssetRef{ID: assetIDByHead[k], Key: "asset-" + fmt.Sprint(assetIDByHead[k]), Class: assetClassByHead[k]},
			IsPrimary: r.parent == "", YahooTicker: &y,
			UnitsIke: decimal.NewFromInt(r.ike), UnitsIkze: decimal.NewFromInt(r.ikze),
		})
	}
	return out
}

// scnGolden is the output of the PRE-task-156 planner (parent/companion string model, commit
// 6598a2a~1) on the same book, buy lists, deviations and limits — captured by running that code,
// not derived by hand. The asset-model planner must reproduce it exactly: the group -> asset
// mapping is a bijection, so item order, values, units, sell/buy flags and the new-deposit
// figures are identical (design §7/§11: numeric outcomes unchanged by construction).
var scnGolden = map[string]string{
	"limits-available": `RETIREMENT|PL.ETNVIRETH|IKE|val=200|units=1.33333333|sell=false|buy=false
RETIREMENT|XAD6|IKZE|val=100|units=1|sell=false|buy=false
RETIREMENT|ETCGLDRMAU.PL|IKZE|val=100|units=0.25|sell=false|buy=false
RETIREMENT|ETCGLDRMAU.PL|IKE|val=150|units=0.375|sell=false|buy=false
RETIREMENT|PL.ETNVIRSOL|IKE|val=150|units=3.75|sell=false|buy=false
MAIN|PL.ETNVIRETH|IKE|val=400|units=2.66666667|sell=false|buy=false
MAIN|ETNVIRLINK.PL|IKE|val=300|units=5|sell=false|buy=false
MAIN|VRTU.DE|IKE|val=250|units=10|sell=false|buy=false
MAIN|PL.ETNVIRXRP|IKE|val=200|units=16.66666667|sell=false|buy=false
MAIN|ETCGLDRMAU.PL|IKE|val=500|units=5|sell=false|buy=false
MAIN|WQTM.DE|IKE|val=100|units=1|sell=false|buy=false
MAIN|CIFR|IKE|val=150|units=1.5|sell=false|buy=false
MAIN|PL.ETNVIRBTCP|IKE|val=1450|units=4.83333333|sell=true|buy=false
MAIN|PL.ETNVIRSOL|IKE|val=450|units=11.25|sell=true|buy=false
MAIN|BTC-USD|REGULAR|val=2250|units=0.0075|sell=false|buy=true
MAIN|SOL-USD|REGULAR|val=800|units=1|sell=false|buy=true
RETIREMENT|BTC-USD|REGULAR|val=300|units=0.001|sell=false|buy=true
newDeposit MAIN IKE=0 IKZE=0 RET IKE=500 IKZE=200
`,
	"limits-exhausted-donor-waterfall": `RETIREMENT|PL.ETNVIRETH|IKE|val=200|units=1.33333333|sell=false|buy=false
RETIREMENT|XAD6|IKE|val=100|units=1|sell=false|buy=false
RETIREMENT|ETCGLDRMAU.PL|IKE|val=250|units=2.5|sell=false|buy=false
RETIREMENT|PL.ETNVIRBTCP|IKE|val=400|units=1.33333333|sell=true|buy=false
RETIREMENT|PL.ETNVIRSOL|IKE|val=150|units=3.75|sell=true|buy=false
MAIN|PL.ETNVIRETH|IKE|val=400|units=2.66666667|sell=false|buy=false
MAIN|ETNVIRLINK.PL|IKE|val=300|units=5|sell=false|buy=false
MAIN|VRTU.DE|IKE|val=250|units=10|sell=false|buy=false
MAIN|PL.ETNVIRXRP|IKE|val=200|units=16.66666667|sell=false|buy=false
MAIN|ETCGLDRMAU.PL|IKE|val=500|units=5|sell=false|buy=false
MAIN|WQTM.DE|IKE|val=100|units=1|sell=false|buy=false
MAIN|CIFR|IKE|val=150|units=1.5|sell=false|buy=false
MAIN|PL.ETNVIRBTCP|IKE|val=1450|units=4.83333333|sell=true|buy=false
MAIN|PL.ETNVIRSOL|IKE|val=450|units=11.25|sell=true|buy=false
MAIN|BTC-USD|REGULAR|val=2250|units=0.0075|sell=false|buy=true
MAIN|SOL-USD|REGULAR|val=800|units=1|sell=false|buy=true
RETIREMENT|BTC-USD|REGULAR|val=700|units=0.00233333|sell=false|buy=true
RETIREMENT|SOL-USD|REGULAR|val=300|units=0.375|sell=false|buy=true
newDeposit MAIN IKE=0 IKZE=0 RET IKE=0 IKZE=0
`,
	"ike-only-partially-left": `RETIREMENT|PL.ETNVIRETH|IKE|val=200|units=1.33333333|sell=false|buy=false
RETIREMENT|XAD6|IKE|val=100|units=1|sell=false|buy=false
RETIREMENT|ETCGLDRMAU.PL|IKE|val=250|units=2.5|sell=false|buy=false
RETIREMENT|PL.ETNVIRBTCP|IKE|val=400|units=1.33333333|sell=true|buy=false
RETIREMENT|PL.ETNVIRSOL|IKE|val=50|units=1.25|sell=true|buy=false
MAIN|PL.ETNVIRETH|IKE|val=400|units=2.66666667|sell=false|buy=false
MAIN|ETNVIRLINK.PL|IKE|val=300|units=5|sell=false|buy=false
MAIN|VRTU.DE|IKE|val=250|units=10|sell=false|buy=false
MAIN|PL.ETNVIRXRP|IKE|val=200|units=16.66666667|sell=false|buy=false
MAIN|ETCGLDRMAU.PL|IKE|val=500|units=5|sell=false|buy=false
MAIN|WQTM.DE|IKE|val=100|units=1|sell=false|buy=false
MAIN|CIFR|IKE|val=150|units=1.5|sell=false|buy=false
MAIN|PL.ETNVIRBTCP|IKE|val=1450|units=4.83333333|sell=true|buy=false
MAIN|PL.ETNVIRSOL|IKE|val=450|units=11.25|sell=true|buy=false
MAIN|BTC-USD|REGULAR|val=2250|units=0.0075|sell=false|buy=true
MAIN|SOL-USD|REGULAR|val=800|units=1|sell=false|buy=true
RETIREMENT|BTC-USD|REGULAR|val=700|units=0.00233333|sell=false|buy=true
RETIREMENT|SOL-USD|REGULAR|val=200|units=0.25|sell=false|buy=true
newDeposit MAIN IKE=0 IKZE=0 RET IKE=100 IKZE=0
`,
}

func TestComputePlan_AssetBijection_MatchesLegacyGroupOutcomes(t *testing.T) {
	for _, c := range scnCases {
		t.Run(c.name, func(t *testing.T) {
			res, err := ComputePlan(scnRetList, scnMainList, decimal.NewFromInt(c.ike), decimal.NewFromInt(c.ikze), scnPrices, scnAssetRows())
			if err != nil {
				t.Fatalf("ComputePlan: %v", err)
			}
			got := scnRender(res.Items, res.NewDepositPlnByBucket)
			if got != scnGolden[c.name] {
				t.Fatalf("asset-model plan differs from the legacy group-model plan.\n--- legacy (golden)\n%s\n--- asset model\n%s", scnGolden[c.name], got)
			}
		})
	}
}

// TestComputePlan_ManualSellOnly_NoSellWithoutPairedBuy re-asserts the ADR-045 boundary on the
// rich scenario: every planned sell is a migration sell (IKE/IKZE crypto-donor only), and each
// portfolio's sold PLN per donor asset is matched by compensating REGULAR buys — never a
// trim/rebalancing sell.
func TestComputePlan_ManualSellOnly_NoSellWithoutPairedBuy(t *testing.T) {
	for _, c := range scnCases {
		res, err := ComputePlan(scnRetList, scnMainList, decimal.NewFromInt(c.ike), decimal.NewFromInt(c.ikze), scnPrices, scnAssetRows())
		if err != nil {
			t.Fatal(err)
		}
		var sold, migrationBought decimal.Decimal
		for _, it := range res.Items {
			if it.PlannedUnits != nil && it.PlannedUnits.Sign() < 0 || (it.ActualUnits != nil && it.ActualUnits.Sign() < 0 && !it.IsMigrationSell) {
				t.Fatalf("%s: a negative-unit item that is not a flagged migration sell: %+v", c.name, it)
			}
			if it.IsMigrationSell {
				if it.AccountType != "IKE" && it.AccountType != "IKZE" {
					t.Fatalf("%s: a sell outside IKE/IKZE: %+v", c.name, it)
				}
				sold = sold.Add(valueOrZero(it.PlannedValuePln))
			}
			if it.IsMigrationBuy {
				migrationBought = migrationBought.Add(valueOrZero(it.PlannedValuePln))
			}
		}
		if sold.Sign() > 0 && migrationBought.Sign() <= 0 {
			t.Fatalf("%s: donor sells (%s PLN) without any compensating migration buy", c.name, sold)
		}
	}
}

// TestComputePlan_WrapperFollowsRehead: after the owner re-heads a crypto asset onto its ETN
// wrapper (set-primary), the primary IS the IKE/IKZE-eligible wrapper — IKE buys land on the
// ETN (not on the raw XTB coin row), REGULAR remainders on the primary, and the ETN's live IKE
// balance keeps the asset in the donor pool.
func TestComputePlan_WrapperFollowsRehead(t *testing.T) {
	positions := []rebalancingcalc.PositionRow{
		{Ticker: "BTC-USD", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "CRYPTO", Asset: cryptoAsset(1, "bitcoin"), YahooTicker: new("BTC-USD.Y")},
		{Ticker: "PL.ETNVIRBTCP", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", Asset: cryptoAsset(1, "bitcoin"), IsPrimary: true,
			YahooTicker: new("ETN.Y"), UnitsIke: decimal.NewFromInt(10)},
	}
	prices := map[string]decimal.Decimal{"BTC-USD.Y": decimal.NewFromInt(300000), "ETN.Y": decimal.NewFromInt(300)}
	res, err := ComputePlan(nil, []rebalancingcalc.BuyListItem{buyItem("PL.ETNVIRBTCP", "1000")},
		decimal.NewFromInt(400), decimal.Zero, prices, positions)
	if err != nil {
		t.Fatal(err)
	}
	var ike, regular *SessionItem
	for _, it := range res.Items {
		if it.Ticker == "BTC-USD" {
			t.Fatalf("the raw XTB coin row must never receive the IKE buy after re-heading, got %+v", it)
		}
		switch it.AccountType {
		case "IKE":
			ike = it
		case "REGULAR":
			regular = it
		}
	}
	if ike == nil || ike.Ticker != "PL.ETNVIRBTCP" || !valueOrZero(ike.PlannedValuePln).Equal(decimal.NewFromInt(400)) {
		t.Fatalf("expected the 400 PLN IKE buy on the ETN (the wrapper = the primary), got %+v", ike)
	}
	if regular == nil || regular.Ticker != "PL.ETNVIRBTCP" || !valueOrZero(regular.PlannedValuePln).Equal(decimal.NewFromInt(600)) {
		t.Fatalf("expected the 600 PLN REGULAR remainder on the primary, got %+v", regular)
	}
}
