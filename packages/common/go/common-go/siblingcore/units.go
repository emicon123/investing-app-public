package siblingcore

import (
	"maps"

	"github.com/shopspring/decimal"

	"investing-app/common-go/costmember"
)

// Units is an immutable set of per-bucket unit amounts; the zero value holds nothing. Every
// method returning Units returns a copy, so a Units value can be stored and passed by value.
type Units struct {
	m map[Bucket]decimal.Decimal
}

// NewUnits copies amounts into a new Units.
func NewUnits(amounts map[Bucket]decimal.Decimal) Units {
	m := make(map[Bucket]decimal.Decimal, len(amounts))
	maps.Copy(m, amounts)
	return Units{m: m}
}

// Of returns the amount in bucket b (zero when absent).
func (u Units) Of(b Bucket) decimal.Decimal {
	return u.m[b]
}

// Total returns the exact decimal sum over all buckets (order-independent).
func (u Units) Total() decimal.Decimal {
	total := decimal.Zero
	for _, v := range u.m {
		total = total.Add(v)
	}
	return total
}

// Add returns a copy with Of(b) increased by delta (delta may be negative).
func (u Units) Add(b Bucket, delta decimal.Decimal) Units {
	return u.With(b, u.Of(b).Add(delta))
}

// With returns a copy with Of(b) set to v.
func (u Units) With(b Bucket, v decimal.Decimal) Units {
	out := NewUnits(u.m)
	out.m[b] = v
	return out
}

// Holding is the money state of one subasset row: its units plus its recorded cost.
// *Holding satisfies costmember.Holding and positioncalc.UnitsTotaler.
type Holding struct {
	Units         Units
	TotalSpentPln *decimal.Decimal // nil = SQL NULL, preserved on every round trip
}

// UnitsTotal is Units.Total (costmember.Holding, positioncalc.UnitsTotaler).
func (h *Holding) UnitsTotal() decimal.Decimal { return h.Units.Total() }

// CostPln is the recorded cost (nil = none).
func (h *Holding) CostPln() *decimal.Decimal { return h.TotalSpentPln }

// SetCostPln replaces the recorded cost.
func (h *Holding) SetCostPln(v *decimal.Decimal) { h.TotalSpentPln = v }

var _ costmember.Holding = (*Holding)(nil)

// CostMember is the costbasis write-back member over a subasset's Holding.
type CostMember = costmember.Member[*Holding]
