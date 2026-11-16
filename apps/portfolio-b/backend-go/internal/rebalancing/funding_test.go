package rebalancing

import (
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/rebalancecalc"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestAccountWaterfall_FillsInFixedOrder(t *testing.T) {
	got := accountWaterfall([]rebalancecalc.BuyListItem{{Ticker: "A", AllocatedPln: dec("1000"), UnitsToBuy: dec("100")}},
		map[string]decimal.Decimal{"IKE_TATA": dec("300"), "IKE_MAMA": dec("300"), "IKZE_TATA": dec("300"), "IKZE_MAMA": dec("300")})
	want := []struct{ account, pln, units string }{
		{"IKE_TATA", "300.0000", "30"}, {"IKE_MAMA", "300.0000", "30"}, {"IKZE_TATA", "300.0000", "30"}, {"IKZE_MAMA", "100.0000", "10"},
	}
	allocs := got[0].AccountAllocations
	if len(allocs) != len(want) {
		t.Fatalf("allocations %+v", allocs)
	}
	for i, w := range want {
		if allocs[i].AccountType != w.account || allocs[i].AllocPln.StringFixed(4) != w.pln || !allocs[i].UnitsToBuy.Equal(dec(w.units)) {
			t.Fatalf("alloc %d: %+v, want %+v", i, allocs[i], w)
		}
	}
}

func TestAccountWaterfall_NoBalancesNoAllocationsAndNegativeIsZero(t *testing.T) {
	got := accountWaterfall([]rebalancecalc.BuyListItem{{Ticker: "A", AllocatedPln: dec("500"), UnitsToBuy: dec("50")}}, map[string]decimal.Decimal{})
	if got[0].AccountAllocations != nil {
		t.Fatalf("no cash, no allocations: %+v", got[0].AccountAllocations)
	}
	got = accountWaterfall([]rebalancecalc.BuyListItem{{Ticker: "A", AllocatedPln: dec("100"), UnitsToBuy: dec("10")}},
		map[string]decimal.Decimal{"IKE_TATA": dec("-50"), "IKE_MAMA": dec("100")})
	if len(got[0].AccountAllocations) != 1 || got[0].AccountAllocations[0].AccountType != "IKE_MAMA" {
		t.Fatalf("a negative balance contributes nothing: %+v", got[0].AccountAllocations)
	}
}

func TestFunding_CheckAcceptsEveryConfirm(t *testing.T) {
	if err := NewFunding().Check(screbalancing.ConfirmRequest{}); err != nil {
		t.Fatal(err)
	}
}
