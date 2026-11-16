package positioncalc

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
)

// sub builds one SubCost the way the apps do: units held, current PLN price (nil = unpriced) and
// recorded cost (nil = NULL). ValuePln is units × price, exactly as ComputeFields folds it.
func sub(ticker, units string, price, cost *string) SubCost {
	u := decimal.RequireFromString(units)
	sc := SubCost{Ticker: ticker, Units: u}
	if price != nil {
		v := u.Mul(decimal.RequireFromString(*price))
		sc.ValuePln = &v
	}
	if cost != nil {
		sc.TotalSpentPln = dp(*cost)
	}
	return sc
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// wantDec asserts a *decimal.Decimal equals s exactly ("" = must be nil).
func wantDec(t *testing.T, label string, got *decimal.Decimal, s string) {
	t.Helper()
	if s == "" {
		if got != nil {
			t.Fatalf("%s: expected nil, got %s", label, got)
		}
		return
	}
	if got == nil || !got.Equal(dec(s)) {
		t.Fatalf("%s: expected %s, got %v", label, s, got)
	}
}

// wantNear asserts got is within tol of want (for values that pass through an 8-decimal profit).
func wantNear(t *testing.T, label string, got *decimal.Decimal, want, tol string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: expected ~%s, got nil", label, want)
	}
	if got.Sub(dec(want)).Abs().GreaterThan(dec(tol)) {
		t.Fatalf("%s: expected %s ± %s, got %s", label, want, tol, got)
	}
}

// ownerAsset is the owner's own example (design §1.3-A) at today's prices: BTC 120 000 USD at
// 4.0 PLN/USD = 480 000 PLN; 0.5 BTC (cost 200 000 PLN) + 5 000 ETN units of 0.0001 BTC each
// (48 PLN, cost 100 000 PLN). V = 480 000, C = 300 000, profit +60 % -> 300 000 PLN / 75 000 USD.
func ownerAsset() []SubCost {
	return []SubCost{
		sub("BTC-USD", "0.5", new("480000"), new("200000")),
		sub("PL.ETNVIRBTCP", "5000", new("48"), new("100000")),
	}
}

// TestComputeEntryPrice is task 161c-1's table U1-U6 (design §7): the entry price in PLN and in the
// primary's native currency, and the costless-subassets note.
func TestComputeEntryPrice(t *testing.T) {
	tests := []struct {
		name         string
		pricePln     string
		fx           string
		subs         []SubCost
		wantProfit   string // "" = nil
		wantPln      string // exact, "" = nil
		wantNative   string // exact, "" = nil
		wantCostless []string
	}{
		{
			// U1 — the owner's example, USD primary: 300 000 PLN = 75 000 USD.
			name: "U1 owner example, USD primary", pricePln: "480000", fx: "4", subs: ownerAsset(),
			wantProfit: "0.6", wantPln: "300000", wantNative: "75000",
		},
		{
			// U1 — PLN primary (the ETN): native line equals the PLN line, 48 / 1.6 = 30 PLN.
			name: "U1 same rows, PLN primary: native equals PLN", pricePln: "48", fx: "1", subs: ownerAsset(),
			wantProfit: "0.6", wantPln: "30", wantNative: "30",
		},
		{
			// U2 — EUR primary: 20 PLN, EURPLN 4.3, profit +25 % -> 16 PLN = 3.7209 EUR (4 dp).
			name: "U2 EUR primary", pricePln: "20", fx: "4.3",
			subs:       []SubCost{sub("EUNL.DE", "50", new("20"), new("800"))},
			wantProfit: "0.25", wantPln: "16", wantNative: "3.7209",
		},
		{
			// The native line moves with the CURRENT fx by design (PLN cost is fixed): same asset,
			// USD at 3.8 instead of 4.0 -> the price in PLN falls with it, so use the same PLN price
			// and only change the rate: 300 000 PLN / 3.8 = 78 947.3684 USD.
			name: "native line follows the current fx", pricePln: "480000", fx: "3.8", subs: ownerAsset(),
			wantProfit: "0.6", wantPln: "300000", wantNative: "78947.3684",
		},
		{
			// U3 — an unpriced sibling contributes neither value nor cost, and is not "costless"
			// either (it is unpriced): profit stays the priced rows' 100/80 - 1 = +25 %.
			name: "U3 unpriced sibling is ignored entirely", pricePln: "10", fx: "1",
			subs: []SubCost{
				sub("A", "10", new("10"), new("80")),
				sub("UNPRICED", "5", nil, nil),
				sub("UNPRICED_COSTED", "5", nil, new("500")),
			},
			wantProfit: "0.25", wantPln: "8", wantNative: "8",
		},
		{
			// U4 — portfolioB today: every cost NULL -> no profit, no entry price, no native, no note.
			name: "U4 all cost NULL (portfolioB)", pricePln: "40", fx: "1",
			subs: []SubCost{sub("A", "250", new("40"), nil), sub("B", "0.02", new("400000"), nil)},
		},
		{name: "U4 sum of cost is zero", pricePln: "40", fx: "1", subs: []SubCost{sub("A", "10", new("40"), new("0"))}},
		{name: "U4 only a negative cost", pricePln: "40", fx: "1", subs: []SubCost{sub("A", "10", new("40"), new("-5"))}},
		{name: "U4 costed row that cannot be priced", pricePln: "40", fx: "1", subs: []SubCost{sub("A", "10", nil, new("100"))}},
		{name: "U4 no subassets", pricePln: "40", fx: "1"},
		{
			// U6 — no rate (zero): the native line is absent but the PLN entry price stays.
			name: "U6 zero fx: no native line", pricePln: "480000", fx: "0", subs: ownerAsset(),
			wantProfit: "0.6", wantPln: "300000",
		},
		{
			// Wipe-out (value 0 against a cost): profit -100 % exists but no entry price does.
			name: "wipe-out has a profit but no entry price", pricePln: "0", fx: "1",
			subs: []SubCost{sub("A", "10", new("0"), new("100"))}, wantProfit: "-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeEntryPrice(dec(tt.pricePln), dec(tt.fx), tt.subs)
			wantDec(t, "profitPct", got.ProfitPct, tt.wantProfit)
			wantDec(t, "avgBuyPricePln", got.AvgBuyPricePln, tt.wantPln)
			wantDec(t, "avgBuyPriceNative", got.AvgBuyPriceNative, tt.wantNative)
			if !reflect.DeepEqual(got.CostlessSubassets, tt.wantCostless) {
				t.Fatalf("costlessSubassets: expected %v, got %v", tt.wantCostless, got.CostlessSubassets)
			}
		})
	}
}

// TestComputeEntryPrice_ConsistencyByConstruction: entry × equivalentUnits = Σcost and
// price / entry − 1 = profit, on the owner's example and on a messy asset whose 8-decimal profit
// rounding shows up (tolerances cover exactly that rounding).
func TestComputeEntryPrice_ConsistencyByConstruction(t *testing.T) {
	cases := map[string]struct {
		pricePln, fx, wantCost string
		subs                   []SubCost
	}{
		"owner example": {"480000", "4", "300000", ownerAsset()},
		"LINK + ETN, USD (design 1.3-D)": {"54.60", "3.90", "1890", []SubCost{
			sub("LINK-USD", "20", new("54.60"), new("1290")),
			sub("ETNVIRLINK.PL", "12.82", new("54.60"), new("600")),
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := ComputeEntryPrice(dec(c.pricePln), dec(c.fx), c.subs)
			value := decimal.Zero
			for _, s := range c.subs {
				value = value.Add(*s.ValuePln)
			}
			u := EquivalentUnits(value, dec(c.pricePln))
			if got.AvgBuyPricePln == nil || u == nil || got.ProfitPct == nil || got.AvgBuyPriceNative == nil {
				t.Fatalf("expected a full entry price, got %+v / %v", got, u)
			}
			cost := got.AvgBuyPricePln.Mul(*u)
			wantNear(t, "entry × equivalentUnits = Σcost", &cost, c.wantCost, "0.5")
			back := dec(c.pricePln).DivRound(*got.AvgBuyPricePln, 8).Sub(decimal.NewFromInt(1))
			wantNear(t, "price/entry − 1 = profit", &back, got.ProfitPct.String(), "0.0001")
			native := got.AvgBuyPricePln.DivRound(dec(c.fx), 4)
			wantNear(t, "native = entry / fx", got.AvgBuyPriceNative, native.String(), "0.0001")
		})
	}
}

// TestComputeEntryPrice_FlipRoundTrip is U2/U3 at the pure level: the same rows re-expressed for
// another primary (only the two scalars change) and back give the IDENTICAL decimals, and the
// ratio of the two entry prices is the ratio of the two primaries' prices.
func TestComputeEntryPrice_FlipRoundTrip(t *testing.T) {
	subs := ownerAsset()
	btc1 := ComputeEntryPrice(dec("480000"), dec("4"), subs)
	etn := ComputeEntryPrice(dec("48"), dec("1"), subs)
	btc2 := ComputeEntryPrice(dec("480000"), dec("4"), subs)

	wantDec(t, "ETN entry", etn.AvgBuyPricePln, "30")
	if !btc1.AvgBuyPricePln.Equal(*btc2.AvgBuyPricePln) || !btc1.AvgBuyPriceNative.Equal(*btc2.AvgBuyPriceNative) ||
		!btc1.ProfitPct.Equal(*btc2.ProfitPct) {
		t.Fatalf("flip and flip back must return identical numbers, got %+v then %+v", btc1, btc2)
	}
	// entry(ETN) / entry(BTC) = p_ETN / p_BTC = 48 / 480000
	ratio := etn.AvgBuyPricePln.DivRound(*btc1.AvgBuyPricePln, 8)
	wantDec(t, "entry ratio", &ratio, "0.0001")
	if !etn.ProfitPct.Equal(*btc1.ProfitPct) {
		t.Fatalf("profit must not depend on the primary, got %s vs %s", etn.ProfitPct, btc1.ProfitPct)
	}
}

// TestComputeEntryPrice_CostlessSubassets is U5: the live shapes. The go-live import put the whole
// group's cost on the head row, so the wrappers beside it are "assumed included" — named here.
func TestComputeEntryPrice_CostlessSubassets(t *testing.T) {
	tests := []struct {
		name string
		subs []SubCost
		want []string
	}{
		{
			name: "Bitcoin: BTC-USD head cost, PL.ETNVIRBTCP cost-less",
			subs: []SubCost{
				sub("BTC-USD", "0.6", new("100000"), new("100000")),
				sub("PL.ETNVIRBTCP", "500", new("40"), nil),
			},
			want: []string{"PL.ETNVIRBTCP"},
		},
		{
			name: "Gold: ETC head cost, two cost-less wrappers keep input order",
			subs: []SubCost{
				sub("ETCGLDRMAU.PL", "10", new("4000"), new("50000")),
				sub("4GLD.DE", "4", new("5000"), nil),
				sub("IGLN.L", "2", new("7500"), nil),
			},
			want: []string{"4GLD.DE", "IGLN.L"},
		},
		{
			name: "Quantum: head cost, QUTM.DE cost-less",
			subs: []SubCost{sub("QTUM", "8", new("100"), new("1000")), sub("QUTM.DE", "4", new("100"), nil)},
			want: []string{"QUTM.DE"},
		},
		{
			name: "every row costed: no list",
			subs: []SubCost{sub("A", "1", new("100"), new("90")), sub("B", "1", new("100"), new("90"))},
		},
		{
			name: "cost-less row holding no units (sold out) is not listed",
			subs: []SubCost{sub("A", "1", new("100"), new("90")), sub("B", "0", new("100"), nil)},
		},
		{
			name: "cost-less row that cannot be priced is not listed",
			subs: []SubCost{sub("A", "1", new("100"), new("90")), sub("B", "4", nil, nil)},
		},
		{
			name: "zero and negative recorded cost count as no cost",
			subs: []SubCost{
				sub("A", "1", new("100"), new("90")),
				sub("ZERO", "1", new("10"), new("0")),
				sub("NEG", "1", new("10"), new("-3")),
			},
			want: []string{"ZERO", "NEG"},
		},
		{
			name: "primary itself cost-less while a sibling carries the cost: listed first",
			subs: []SubCost{sub("ETN", "5", new("50"), nil), sub("BTC-USD", "1", new("100"), new("300"))},
			want: []string{"ETN"},
		},
		{
			name: "no cost anywhere: no entry price, so no list either (portfolioB)",
			subs: []SubCost{sub("A", "1", new("100"), nil), sub("B", "1", new("100"), nil)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeEntryPrice(dec("100"), dec("1"), tt.subs)
			if !reflect.DeepEqual(got.CostlessSubassets, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got.CostlessSubassets)
			}
		})
	}
}

func TestEquivalentUnits(t *testing.T) {
	tests := []struct {
		name         string
		value, price string
		want         string // "" = nil
	}{
		{"owner example: exactly 1 BTC", "480000", "480000", "1"},
		{"whole group in ETN units", "480000", "48", "10000"},
		{"fractional, 8 dp", "80010", "100", "800.1"},
		{"nothing held is 0 units, not absent", "0", "100", "0"},
		{"zero price", "100", "0", ""},
		{"negative price", "100", "-1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantDec(t, "equivalentUnits", EquivalentUnits(dec(tt.value), dec(tt.price)), tt.want)
		})
	}
}

// entryComputed runs the shared ComputeFields for a USD-priced primary the way the apps do.
func entryComputed(ticker, yahoo, units string, cost *string, companions []Companion, prices map[string]string, rates map[string]string) Computed {
	pm := map[string]decimal.Decimal{}
	for k, v := range prices {
		pm[k] = dec(v)
	}
	rm := map[string]decimal.Decimal{}
	for k, v := range rates {
		rm[k] = dec(v)
	}
	var c *decimal.Decimal
	if cost != nil {
		c = dp(*cost)
	}
	return ComputeFields(ticker, &yahoo, "CRYPTO", "USD", nil, fixedUnits(dec(units)), c,
		decimal.NewFromInt(1_000_000), companions, pm, rm)
}

var usdRates = map[string]string{"PLN": "1", "USD": "4"}

// TestComputeFields_EntryPriceFields is the owner's example end to end through ComputeFields, plus
// the degraded shapes: the new fields follow the same rules as priceNative.
func TestComputeFields_EntryPriceFields(t *testing.T) {
	etn := "ETN.Y"
	prices := map[string]string{"BTC.Y": "480000", "ETN.Y": "48"}
	comp := []Companion{{Ticker: "PL.ETNVIRBTCP", YahooTicker: &etn, UnitsTotal: dec("5000"), TotalSpentPln: dp("100000")}}

	t.Run("USD primary, owner example", func(t *testing.T) {
		got := entryComputed("BTC-USD", "BTC.Y", "0.5", new("200000"), comp, prices, usdRates)
		wantDec(t, "avgBuyPricePln", got.AvgBuyPricePln, "300000")
		wantDec(t, "avgBuyPriceNative", got.AvgBuyPriceNative, "75000")
		wantDec(t, "equivalentUnits", got.EquivalentUnits, "1")
		wantDec(t, "valuePln", got.ValuePln, "480000")
		if got.CostlessSubassets != nil {
			t.Fatalf("expected no costless subassets, got %v", got.CostlessSubassets)
		}
	})

	btcFirst := func() Computed {
		return entryComputed("BTC-USD", "BTC.Y", "0.5", new("200000"), comp, prices, usdRates)
	}
	etnFirst := func() Computed { // the same two rows with the PLN ETN promoted to primary
		return ComputeFields("PL.ETNVIRBTCP", &etn, "CRYPTO", "PLN", nil, fixedUnits(dec("5000")), dp("100000"),
			decimal.NewFromInt(1_000_000),
			[]Companion{{Ticker: "BTC-USD", YahooTicker: new("BTC.Y"), UnitsTotal: dec("0.5"), TotalSpentPln: dp("200000")}},
			map[string]decimal.Decimal{"BTC.Y": dec("480000"), "ETN.Y": dec("48")}, map[string]decimal.Decimal{"PLN": dec("1")})
	}

	t.Run("PLN primary: native equals PLN", func(t *testing.T) {
		got := etnFirst()
		wantDec(t, "avgBuyPricePln", got.AvgBuyPricePln, "30")
		wantDec(t, "avgBuyPriceNative", got.AvgBuyPriceNative, "30")
		wantDec(t, "equivalentUnits", got.EquivalentUnits, "10000")
	})

	t.Run("flip A -> ETN -> A is identical", func(t *testing.T) {
		before, mid, after := btcFirst(), etnFirst(), btcFirst()
		if !before.AvgBuyPricePln.Equal(*after.AvgBuyPricePln) || !before.AvgBuyPriceNative.Equal(*after.AvgBuyPriceNative) ||
			!before.EquivalentUnits.Equal(*after.EquivalentUnits) {
			t.Fatalf("round trip changed the numbers: %+v vs %+v", before, after)
		}
		if !mid.ProfitPct.Equal(*before.ProfitPct) || !mid.ValuePln.Equal(*before.ValuePln) {
			t.Fatalf("a flip must not move profit or group value, got %v/%v vs %v/%v",
				mid.ProfitPct, mid.ValuePln, before.ProfitPct, before.ValuePln)
		}
	})

	t.Run("portfolioB all-NULL cost: entry fields nil, equivalentUnits still present", func(t *testing.T) {
		nullComp := []Companion{{Ticker: "PL.ETNVIRBTCP", YahooTicker: &etn, UnitsTotal: dec("5000")}}
		got := entryComputed("BTC-USD", "BTC.Y", "0.5", nil, nullComp, prices, usdRates)
		if got.ProfitPct != nil || got.AvgBuyPricePln != nil || got.AvgBuyPriceNative != nil || got.CostlessSubassets != nil {
			t.Fatalf("all-NULL cost must leave profit/entry/native/costless nil, got %+v", got)
		}
		wantDec(t, "equivalentUnits", got.EquivalentUnits, "1")
	})

	t.Run("Bitcoin-shaped: cost-less ETN is named", func(t *testing.T) {
		nullComp := []Companion{{Ticker: "PL.ETNVIRBTCP", YahooTicker: &etn, UnitsTotal: dec("5000")}}
		got := entryComputed("BTC-USD", "BTC.Y", "0.5", new("300000"), nullComp, prices, usdRates)
		if !reflect.DeepEqual(got.CostlessSubassets, []string{"PL.ETNVIRBTCP"}) {
			t.Fatalf("expected [PL.ETNVIRBTCP], got %v", got.CostlessSubassets)
		}
		wantDec(t, "avgBuyPricePln", got.AvgBuyPricePln, "300000")
	})

	t.Run("missing fx rate degrades every new field like priceNative", func(t *testing.T) {
		got := entryComputed("BTC-USD", "BTC.Y", "0.5", new("200000"), comp, prices, map[string]string{"PLN": "1"})
		if got.AvgBuyPricePln != nil || got.AvgBuyPriceNative != nil || got.EquivalentUnits != nil || got.CostlessSubassets != nil {
			t.Fatalf("expected units-only result, got %+v", got)
		}
	})

	t.Run("unpriced primary: no entry fields, never invented", func(t *testing.T) {
		got := entryComputed("BTC-USD", "MISSING.Y", "0.5", new("200000"), comp, prices, usdRates)
		if got.AvgBuyPricePln != nil || got.AvgBuyPriceNative != nil || got.EquivalentUnits != nil || got.CostlessSubassets != nil {
			t.Fatalf("expected units-only result, got %+v", got)
		}
	})
}
