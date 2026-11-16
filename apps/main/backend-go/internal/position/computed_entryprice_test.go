package position

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"

	"investing-app/common-go/positioncalc"
)

// computeGroupFx is computeGroup for a primary priced in a non-PLN currency: prices are PLN per
// unit (as price_cache serves them), fx is the CURRENT PLN rate of the primary's price currency.
func computeGroupFx(t *testing.T, portfolio, currency, fx string, subs []profitSub) ComputedFields {
	t.Helper()
	fq := newFakeQueries()
	aid := fq.putAsset(portfolio, "GRP", "10")
	prices := newFakePrices()
	prices.rates[currency] = decimal.RequireFromString(fx)
	for _, s := range subs {
		yahoo := ""
		if s.price != "" {
			yahoo = s.ticker + ".Y"
			prices.pricePlnByTicker[yahoo] = decimal.RequireFromString(s.price)
		}
		id := fq.putSub(aid, s.ticker, portfolio, s.units, yahoo, s.primary)
		if s.primary {
			r := fq.rows[id]
			r.PriceCurrency = sqlcgen.CurrencyEnum(currency)
			fq.rows[id] = r
		}
		if s.cost != "" {
			fq.setCost(id, s.cost)
		}
	}
	svc := &Service{prices: prices}
	totals := map[string]decimal.Decimal{portfolio: decimal.NewFromInt(1_000_000)}
	// positioncalc.OrderSubassets = the production order (primary first, then ticker ASC), so the costless list
	// order asserted below is the one the wire carries.
	computed, err := svc.computeFields(context.Background(), testAsset(t, fq, aid), positioncalc.OrderSubassets(testSubs(t, fq, aid), aid), totals, nil, prices.pricePlnByTicker)
	if err != nil {
		t.Fatal(err)
	}
	return computed
}

func wantNilDec(t *testing.T, name string, got *decimal.Decimal) {
	t.Helper()
	if got != nil {
		t.Fatalf("%s = %v, want nil", name, got)
	}
}

// ownerRows is the owner's example (design §1.3-A): 0.5 BTC (cost 200 000 PLN) + 5 000 ETN units
// worth 0.0001 BTC each (cost 100 000 PLN), BTC 480 000 PLN (120 000 USD at 4.0).
func ownerRows(btcPrimary bool) []profitSub {
	return []profitSub{
		{"BTC-USD", "0.5", "480000", "200000", btcPrimary},
		{"PL.ETNVIRBTCP", "5000", "48", "100000", !btcPrimary},
	}
}

// TestComputeFields_EntryPrice_OwnerExample_U1: 300 000 PLN = 75 000 USD, equivalent units 1 BTC,
// and the consistency identities the contract states.
func TestComputeFields_EntryPrice_OwnerExample_U1(t *testing.T) {
	c := computeGroupFx(t, "MAIN", "USD", "4", ownerRows(true))
	wantDec(t, "profitPct", c.ProfitPct, "0.6")
	wantDec(t, "avgBuyPricePln", c.AvgBuyPricePln, "300000")
	wantDec(t, "avgBuyPriceNative", c.AvgBuyPriceNative, "75000")
	wantDec(t, "equivalentUnits", c.EquivalentUnits, "1")
	wantDec(t, "priceNative", c.PriceNative, "120000")
	if c.CostlessSubassets != nil {
		t.Fatalf("every row carries cost: no costless list, got %v", c.CostlessSubassets)
	}
	// avgBuyPricePln × equivalentUnits = Σcost
	if !c.AvgBuyPricePln.Mul(*c.EquivalentUnits).Equal(decimal.NewFromInt(300000)) {
		t.Fatalf("entry × equivalentUnits must equal Σcost, got %s", c.AvgBuyPricePln.Mul(*c.EquivalentUnits))
	}
}

// TestComputeFields_EntryPrice_NativeLineByCurrency_U2U6: PLN primary -> native equals PLN;
// EUR primary; and the native line follows the CURRENT fx (PLN cost is fixed).
func TestComputeFields_EntryPrice_NativeLineByCurrency_U2U6(t *testing.T) {
	// PLN primary (fx 1): native == PLN
	c := computeGroupFx(t, "MAIN", "PLN", "1", ownerRows(false))
	wantDec(t, "avgBuyPricePln (ETN primary)", c.AvgBuyPricePln, "30")
	wantDec(t, "avgBuyPriceNative (PLN primary)", c.AvgBuyPriceNative, "30")
	wantDec(t, "equivalentUnits (ETN primary)", c.EquivalentUnits, "10000")

	// EUR primary: 20 PLN, EURPLN 4.3, +25 % -> 16 PLN = 3.7209 EUR
	c = computeGroupFx(t, "MAIN", "EUR", "4.3", []profitSub{{"EUNL", "50", "20", "800", true}})
	wantDec(t, "avgBuyPricePln (EUR)", c.AvgBuyPricePln, "16")
	wantDec(t, "avgBuyPriceNative (EUR)", c.AvgBuyPriceNative, "3.7209")

	// same PLN price, USD moved to 3.8: the native line moves with it
	c = computeGroupFx(t, "MAIN", "USD", "3.8", ownerRows(true))
	wantDec(t, "avgBuyPricePln (fx 3.8)", c.AvgBuyPricePln, "300000")
	wantDec(t, "avgBuyPriceNative (fx 3.8)", c.AvgBuyPriceNative, "78947.3684")
}

// TestComputeFields_EntryPrice_FlipRoundTrip_U2: A -> ETN -> A leaves the PLN entry price, native
// line and equivalent units identical; the middle state is the ETN's own 30 PLN.
func TestComputeFields_EntryPrice_FlipRoundTrip_U2(t *testing.T) {
	before := computeGroupFx(t, "MAIN", "USD", "4", ownerRows(true))
	mid := computeGroupFx(t, "MAIN", "PLN", "1", ownerRows(false))
	after := computeGroupFx(t, "MAIN", "USD", "4", ownerRows(true))
	wantDec(t, "ETN entry", mid.AvgBuyPricePln, "30")
	if !before.AvgBuyPricePln.Equal(*after.AvgBuyPricePln) || !before.AvgBuyPriceNative.Equal(*after.AvgBuyPriceNative) ||
		!before.EquivalentUnits.Equal(*after.EquivalentUnits) {
		t.Fatalf("flip and flip back must be identical: %+v vs %+v", before, after)
	}
	if !before.ProfitPct.Equal(*mid.ProfitPct) || !before.ValuePln.Equal(*mid.ValuePln) {
		t.Fatalf("a flip must not move profit/value")
	}
}

// TestComputeFields_EntryPrice_UnpricedPrimaryAndMissingRate_U4: never a fabricated number.
func TestComputeFields_EntryPrice_UnpricedPrimaryAndMissingRate_U4(t *testing.T) {
	// unpriced primary (no yahoo ticker): degraded to units only
	c := computeGroupFx(t, "MAIN", "PLN", "1", []profitSub{
		{"A", "5", "", "100", true},
		{"B", "5", "10", "", false},
	})
	wantNilDec(t, "avgBuyPricePln", c.AvgBuyPricePln)
	wantNilDec(t, "avgBuyPriceNative", c.AvgBuyPriceNative)
	wantNilDec(t, "equivalentUnits", c.EquivalentUnits)
	if c.CostlessSubassets != nil {
		t.Fatalf("expected no list, got %v", c.CostlessSubassets)
	}

	// primary priced in USD but no USD rate available: the whole computed block degrades
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "GRP", "10")
	prices := newFakePrices() // PLN only
	prices.pricePlnByTicker["A.Y"] = decimal.NewFromInt(10)
	id := fq.putSub(aid, "A", "MAIN", "5", "A.Y", true)
	r := fq.rows[id]
	r.PriceCurrency = sqlcgen.CurrencyEnumUSD
	fq.rows[id] = r
	fq.setCost(id, "40")
	got, err := (&Service{prices: prices}).computeFields(context.Background(), testAsset(t, fq, aid), positioncalc.OrderSubassets(testSubs(t, fq, aid), aid),
		map[string]decimal.Decimal{"MAIN": decimal.NewFromInt(1000)}, nil, prices.pricePlnByTicker)
	if err != nil {
		t.Fatal(err)
	}
	wantNilDec(t, "avgBuyPricePln (no rate)", got.AvgBuyPricePln)
	wantNilDec(t, "avgBuyPriceNative (no rate)", got.AvgBuyPriceNative)
	wantNilDec(t, "equivalentUnits (no rate)", got.EquivalentUnits)
}

// TestComputeFields_EntryPrice_NoRecordedCost_PortfolioBShape: Σcost <= 0 -> no profit, entry price,
// native line or costless note; equivalentUnits (needs no cost) is still there.
func TestComputeFields_EntryPrice_NoRecordedCost_PortfolioBShape(t *testing.T) {
	c := computeGroupFx(t, "MAIN", "USD", "4", []profitSub{
		{"BTC-USD", "0.5", "480000", "", true},
		{"PL.ETNVIRBTCP", "5000", "48", "", false},
	})
	wantNilDec(t, "profitPct", c.ProfitPct)
	wantNilDec(t, "avgBuyPricePln", c.AvgBuyPricePln)
	wantNilDec(t, "avgBuyPriceNative", c.AvgBuyPriceNative)
	if c.CostlessSubassets != nil {
		t.Fatalf("no entry price -> no costless list, got %v", c.CostlessSubassets)
	}
	wantDec(t, "equivalentUnits", c.EquivalentUnits, "1")
}

// TestComputeFields_EntryPrice_CostlessSubassets_U5: the live shapes — Bitcoin (BTC-USD head cost,
// PL.ETNVIRBTCP cost-less), Gold (two cost-less wrappers), Quantum — name the wrappers whose cost
// is "assumed included"; a costed sibling and an unpriced one are not listed.
func TestComputeFields_EntryPrice_CostlessSubassets_U5(t *testing.T) {
	tests := []struct {
		name string
		subs []profitSub
		want []string
	}{
		{"Bitcoin", []profitSub{
			{"BTC-USD", "600", "100", "100000", true},
			{"PL.ETNVIRBTCP", "200", "100", "", false},
		}, []string{"PL.ETNVIRBTCP"}},
		{"Gold", []profitSub{
			{"ETCGLDRMAU.PL", "10", "4000", "50000", true},
			{"4GLD.DE", "4", "5000", "", false},
			{"IGLN.L", "2", "7500", "", false},
		}, []string{"4GLD.DE", "IGLN.L"}}, // ticker ASC after the primary
		{"Quantum", []profitSub{
			{"QTUM", "8", "100", "1000", true},
			{"QUTM.DE", "4", "100", "", false},
		}, []string{"QUTM.DE"}},
		{"costed sibling and unpriced sibling are not listed", []profitSub{
			{"A", "10", "11", "100", true},
			{"B", "5", "10", "50", false},
			{"UNPRICED", "5", "", "", false},
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := computeGroupFx(t, "MAIN", "PLN", "1", tt.subs)
			if !reflect.DeepEqual(c.CostlessSubassets, tt.want) {
				t.Fatalf("costlessSubassets = %v, want %v", c.CostlessSubassets, tt.want)
			}
		})
	}
}

// TestComputeFields_EntryPrice_Retirement: RETIREMENT computes exactly like MAIN (ADR-104 lifted
// ADR-056) — the same rows give the same profit, entry price (PLN and native), equivalent units and
// costless note, with no portfolio branch anywhere in the entry-price path.
func TestComputeFields_EntryPrice_Retirement(t *testing.T) {
	// The owner's example: every field is the one the MAIN case yields.
	ret := computeGroupFx(t, "RETIREMENT", "USD", "4", ownerRows(true))
	wantDec(t, "profitPct", ret.ProfitPct, "0.6")
	wantDec(t, "avgBuyPricePln", ret.AvgBuyPricePln, "300000")
	wantDec(t, "avgBuyPriceNative", ret.AvgBuyPriceNative, "75000")
	wantDec(t, "equivalentUnits", ret.EquivalentUnits, "1")
	if ret.CostlessSubassets != nil {
		t.Fatalf("every row carries cost: no costless list, got %v", ret.CostlessSubassets)
	}

	// Same rows, MAIN vs RETIREMENT: the whole computed block matches (not only the four fields).
	for _, rows := range []struct {
		name string
		subs []profitSub
	}{
		{"owner example", ownerRows(true)},
		{"costless wrapper", costlessRows},
	} {
		t.Run(rows.name, func(t *testing.T) {
			main := computeGroupFx(t, "MAIN", "USD", "4", rows.subs)
			ret := computeGroupFx(t, "RETIREMENT", "USD", "4", rows.subs)
			if !reflect.DeepEqual(ret, main) {
				t.Fatalf("RETIREMENT computed = %+v, want the MAIN result %+v", ret, main)
			}
		})
	}

	// A cost-less sibling is named for RETIREMENT too.
	c := computeGroupFx(t, "RETIREMENT", "PLN", "1", costlessRows)
	if !reflect.DeepEqual(c.CostlessSubassets, []string{"WRAP"}) {
		t.Fatalf("RETIREMENT costlessSubassets = %v, want [WRAP]", c.CostlessSubassets)
	}
}

// costlessRows: a costed primary plus a held, priced wrapper that has no recorded cost.
var costlessRows = []profitSub{
	{"HEAD", "10", "100", "800", true},
	{"WRAP", "2", "50", "", false},
}

// TestToOpenAPIComputed_EntryPriceFields: the wire mapping. Every new field is optional and a nil is
// omitted from the JSON (never `null`, never `[]`), as the contract requires.
func TestToOpenAPIComputed_EntryPriceFields(t *testing.T) {
	full := toOpenAPIComputed(ComputedFields{
		AvgBuyPricePln: ptrDec("300000"), AvgBuyPriceNative: ptrDec("75000"), EquivalentUnits: ptrDec("1"),
		CostlessSubassets: []string{"PL.ETNVIRBTCP"},
	})
	if full.AvgBuyPriceNative == nil || *full.AvgBuyPriceNative != 75000 || full.EquivalentUnits == nil || *full.EquivalentUnits != 1 {
		t.Fatalf("native/equivalentUnits not mapped: %+v", full)
	}
	if full.CostlessSubassets == nil || !reflect.DeepEqual(*full.CostlessSubassets, []string{"PL.ETNVIRBTCP"}) {
		t.Fatalf("costlessSubassets not mapped: %+v", full.CostlessSubassets)
	}

	empty, err := json.Marshal(toOpenAPIComputed(ComputedFields{UnitsTotal: ptrDec("1")}))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"avgBuyPriceNative", "equivalentUnits", "costlessSubassets", "avgBuyPricePln", "profitPct"} {
		if strings.Contains(string(empty), key) {
			t.Fatalf("a nil %s must be omitted from the wire, got %s", key, empty)
		}
	}
	// a nil-but-non-nil-typed empty slice is omitted too
	emptyList, err := json.Marshal(toOpenAPIComputed(ComputedFields{CostlessSubassets: []string{}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(emptyList), "costlessSubassets") {
		t.Fatalf("an empty costless list must be omitted, got %s", emptyList)
	}
}
