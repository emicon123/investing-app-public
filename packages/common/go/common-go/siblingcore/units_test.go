package siblingcore_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/costmember"
	"investing-app/common-go/siblingcore"
)

var one = decimal.NewFromInt(1)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestUnits_NewUnitsCopiesItsInput(t *testing.T) {
	in := map[siblingcore.Bucket]decimal.Decimal{"A": dec("2")}
	u := siblingcore.NewUnits(in)
	in["A"] = dec("99")
	if !u.Of("A").Equal(dec("2")) {
		t.Fatalf("NewUnits must copy: %s", u.Of("A"))
	}
}

func TestUnits_OfAbsentIsZeroAndTotalSumsEveryBucket(t *testing.T) {
	u := siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{"A": dec("1.5"), "B": dec("2.25")})
	if !u.Of("C").IsZero() {
		t.Fatal("absent bucket must be zero")
	}
	if !u.Total().Equal(dec("3.75")) {
		t.Fatalf("total %s", u.Total())
	}
	if !(siblingcore.Units{}).Total().IsZero() {
		t.Fatal("zero value holds nothing")
	}
}

func TestUnits_AddAndWithReturnCopies(t *testing.T) {
	base := siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{"A": dec("5")})
	added := base.Add("A", dec("-2")).Add("B", one)
	set := base.With("A", dec("7"))
	if !base.Of("A").Equal(dec("5")) || !base.Of("B").IsZero() {
		t.Fatalf("receiver changed: %s %s", base.Of("A"), base.Of("B"))
	}
	if !added.Of("A").Equal(dec("3")) || !added.Of("B").Equal(one) {
		t.Fatalf("add: %s %s", added.Of("A"), added.Of("B"))
	}
	if !set.Of("A").Equal(dec("7")) {
		t.Fatalf("with: %s", set.Of("A"))
	}
	var zero siblingcore.Units
	if !zero.Add("X", one).Of("X").Equal(one) || !zero.Of("X").IsZero() {
		t.Fatal("Add on the zero value must not share state")
	}
}

func TestHolding_ImplementsCostHoldingAndKeepsNilCost(t *testing.T) {
	var _ costmember.Holding = (*siblingcore.Holding)(nil)
	h := &siblingcore.Holding{Units: siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{"A": dec("2"), "B": dec("3")})}
	if h.CostPln() != nil {
		t.Fatal("nil cost must stay nil")
	}
	if !h.UnitsTotal().Equal(dec("5")) {
		t.Fatalf("units total %s", h.UnitsTotal())
	}
	c := dec("10")
	h.SetCostPln(&c)
	if h.CostPln() == nil || !h.CostPln().Equal(c) {
		t.Fatal("SetCostPln")
	}
	h.SetCostPln(nil)
	if h.CostPln() != nil {
		t.Fatal("SetCostPln(nil) clears")
	}
}
