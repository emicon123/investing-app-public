// Package costbasis is the shared pure kernel for the asset-level average cost of a portfolio
// (task 161a-1, design docs/architecture/diagrams/161-asset-entry-price.md §2 and §5).
//
// It answers one question: given the rows (subassets) of ONE asset and a confirmed trade, what
// does each row's recorded cost (positions.total_spent_pln) become? The rules, in short:
//
//   - A sale reduces the asset's cost in proportion to the VALUE sold:
//     f = min(1, soldValue / assetValueBefore); every row with a recorded cost is scaled by
//     (1 - f), NULL stays NULL, the rounding remainder goes to the largest row so the sum is
//     exact, and an asset left with no units gets exactly 0. Entry price and profit % are
//     therefore unchanged by any partial sale.
//   - A buy adds the PLN paid to the bought row - but only if the asset already has recorded
//     cost or holds no units (design §2 rule 7: never invent a cost that covers only the new
//     units of an asset whose baseline is unknown).
//   - A transfer is a sale plus buy legs. A leg on the SAME asset as the sold row CARRIES cost
//     (asset cost unchanged, owner decision D3) up to gamma = min(1, V_same / proceeds); the
//     unmatched (1 - gamma) share of the sold slice is a realized sale, and any V_same excess
//     over the proceeds is a market buy (top-up). A leg on ANOTHER asset is a market buy.
//   - A batch of trades (a purchase-session completion: donor sells, migration sells, buys and
//     compensating buys) is netted PER ASSET before any cost is touched: with S = the value sold
//     and B = the value bought in one asset, min(S, B) carries cost (a swap), a buy excess is a
//     market buy, a sale excess is a proportional sale. Flows on different assets stay independent
//     (PlanTrades; PlanTransfer is the one-sale special case).
//
// Everything here is a pure function over plain values: no DB, no network, no clock, no
// knowledge of which row is the asset's primary (a set-primary flip cannot change a result).
// Callers (the trade services of the three apps) read the asset's rows and cached prices inside
// their existing transaction, call the kernel once per trade, and write the changed rows'
// cost back atomically with the unit changes.
//
// Rows out are always copies of the rows in, in the same order, with ONLY CostPln possibly
// different - Units are echoed exactly as given (they are the units held BEFORE the trade; the
// caller applies the trade's own units itself). Costs are rounded to 4 decimals, the scale of
// the NUMERIC(18,4) column.
package costbasis

import "github.com/shopspring/decimal"

// costScale is the number of decimals of positions.total_spent_pln (NUMERIC(18,4)).
const costScale = 4

// Row is one subasset (one positions row) of an asset.
type Row struct {
	PositionID int64
	AssetID    int64
	// Units held BEFORE the trade, summed over every account bucket.
	Units decimal.Decimal
	// PricePln is the current PLN price of one unit; nil = unpriced (skipped when valuing).
	PricePln *decimal.Decimal
	// CostPln is the recorded cost (total_spent_pln); nil = NULL, "no cost recorded".
	CostPln *decimal.Decimal
}

// Sale is the sold slice of one row.
type Sale struct {
	PositionID int64
	Units      decimal.Decimal // units sold, positive
	PricePln   decimal.Decimal // sale price of the sold row (the request price, not the cache)
	// ValuePln, when non-nil, is the exact proceeds and overrides Units x PricePln. The session
	// path sets it because its price is derived from the executed value at 8 decimals.
	ValuePln *decimal.Decimal
}

// Leg is one buy leg of a transfer or batch.
type Leg struct {
	PositionID int64
	AssetID    int64
	Units      decimal.Decimal // units bought, positive
	PricePln   decimal.Decimal // buy price per unit
	// ValuePln, when non-nil, is the exact PLN paid and overrides Units x PricePln (see Sale).
	ValuePln *decimal.Decimal
}

func (s Sale) value() decimal.Decimal {
	if s.ValuePln != nil {
		return *s.ValuePln
	}
	return s.Units.Mul(s.PricePln)
}

func (l Leg) value() decimal.Decimal {
	if l.ValuePln != nil {
		return *l.ValuePln
	}
	return l.Units.Mul(l.PricePln)
}

// Transfer is one sale funding one or more buy legs, confirmed in a single transaction.
type Transfer struct {
	Sold Sale
	Buys []Leg
}

// ApplySale returns a's rows with their recorded costs reduced for the sale s. The sold row is
// valued at s.PricePln, every other priced row at its own PricePln, unpriced rows are skipped in
// the asset's value (their recorded cost, if any, still scales). A sale that is not applicable
// (non-positive units or price, unknown position, no recorded cost anywhere) changes nothing.
func ApplySale(a []Row, s Sale) []Row {
	out := cloneRows(a)
	scaleForSales(out, []Sale{s})
	return out
}

// ApplyBuy returns a's rows with paidPln added to the recorded cost of positionID - rule 7: only
// if the asset has recorded cost (any row > 0) or holds no units. Otherwise (an asset that holds
// units but no recorded cost) nothing is invented and the rows come back unchanged. paidPln <= 0
// and an unknown position are no-ops.
func ApplyBuy(a []Row, positionID int64, paidPln decimal.Decimal) []Row {
	out := cloneRows(a)
	addBuy(out, positionID, paidPln)
	return out
}

// PlanTransfer applies a transfer: the sale scales the sold asset's cost, legs on the sold asset
// (AssetID == soldAssetID) carry cost, legs on other assets are market buys. assets maps AssetID
// to that asset's rows (units BEFORE the trade); the result has an entry for the sold asset and
// for every asset a leg touched - copies, in input order, only CostPln possibly changed. If the
// sold asset is missing from assets no sale effect is computed (its legs are ignored, legs on
// other assets are still market buys); a leg whose asset or position is unknown is ignored.
func PlanTransfer(assets map[int64][]Row, soldAssetID int64, t Transfer) map[int64][]Row {
	return planBatch(assets, []assetSale{{assetID: soldAssetID, sale: t.Sold}}, t.Buys)
}

// PlanTrades applies a batch of confirmed trades (a purchase-session completion) netted PER
// ASSET: for every asset with sales, S = the value sold and B = the value bought in that same
// asset (legs whose AssetID matches); min(S, B) CARRIES cost (the asset's cost is unchanged by the
// swap part - the carried cost lands on the bought rows pro rata to leg value), a buy excess
// B - S is a market buy (rule 7), a sale excess S - B scales the cost proportionally. Assets
// without sales get plain market buys. Flows on different assets stay independent. assets maps
// AssetID to that asset's rows (units BEFORE any trade of the batch); a sale's asset is the one
// holding its PositionID. The result has an entry for every asset with an applicable sale and
// every asset a leg touched - copies, in input order, only CostPln possibly changed. Sales of
// unknown positions and legs of unknown assets or positions are ignored; the result does not
// depend on the order of sales or legs.
func PlanTrades(assets map[int64][]Row, sales []Sale, buys []Leg) map[int64][]Row {
	assetOf := map[int64]int64{}
	for assetID, rows := range assets {
		for _, r := range rows {
			assetOf[r.PositionID] = assetID
		}
	}
	on := make([]assetSale, 0, len(sales))
	for _, s := range sales {
		if assetID, ok := assetOf[s.PositionID]; ok {
			on = append(on, assetSale{assetID: assetID, sale: s})
		}
	}
	return planBatch(assets, on, buys)
}

type assetSale struct {
	assetID int64
	sale    Sale
}

func planBatch(assets map[int64][]Row, sales []assetSale, buys []Leg) map[int64][]Row {
	out := map[int64][]Row{}
	work := map[int64][]Row{} // working copies; units are advanced as legs are applied (rule 7 needs them)
	touch := func(assetID int64) []Row {
		if rows, ok := work[assetID]; ok {
			return rows
		}
		src, ok := assets[assetID]
		if !ok {
			return nil
		}
		work[assetID] = cloneRows(src)
		return work[assetID]
	}

	var saleAssets []int64
	salesOf := map[int64][]Sale{}
	for _, as := range sales {
		if _, ok := salesOf[as.assetID]; !ok {
			saleAssets = append(saleAssets, as.assetID)
		}
		salesOf[as.assetID] = append(salesOf[as.assetID], as.sale)
	}

	// Assets with sales: net sales against the legs bought in the same asset.
	for _, assetID := range saleAssets {
		x := touch(assetID)
		if x == nil {
			continue
		}
		var same []Leg
		for _, leg := range buys {
			if leg.AssetID == assetID {
				same = append(same, leg)
			}
		}
		planSameAsset(x, salesOf[assetID], same)
	}

	// Every other leg is a market buy on its own asset (legs on a sold asset were netted above,
	// or ignored when that asset is unknown).
	for _, leg := range buys {
		if _, netted := salesOf[leg.AssetID]; netted {
			continue
		}
		rows := touch(leg.AssetID)
		if rows == nil {
			continue
		}
		addBuy(rows, leg.PositionID, leg.value())
		bumpUnits(rows, leg.PositionID, leg.Units)
	}

	for assetID := range work {
		out[assetID] = restoreUnits(work[assetID], assets[assetID])
	}
	return out
}

// ChangedRows returns the rows of after whose recorded cost differs from the same position's cost
// in before (NULL vs non-NULL counts as a difference) - the rows a caller has to write back.
func ChangedRows(before, after []Row) []Row {
	old := make(map[int64]*decimal.Decimal, len(before))
	for _, r := range before {
		old[r.PositionID] = r.CostPln
	}
	var changed []Row
	for _, r := range after {
		if !sameCost(old[r.PositionID], r.CostPln) {
			changed = append(changed, r)
		}
	}
	return changed
}

// ---------------------------------------------------------------------------------------------
// internals
// ---------------------------------------------------------------------------------------------

// planSameAsset applies the asset's sales to its working rows x and then the same-asset buy
// legs (carry + top-up). x is mutated in place; its Units are advanced by the trades.
func planSameAsset(x []Row, sales []Sale, same []Leg) {
	costBefore := recordedSum(x)
	proceeds, applied := scaleForSales(x, sales)
	released := costBefore.Sub(recordedSum(x)) // Q: the cost the sold slices took out of the asset
	for _, s := range applied {
		bumpUnits(x, s.PositionID, s.Units.Neg())
	}

	// Aggregate the legs per row (several account buckets of the same instrument), keeping only
	// legs whose row belongs to this asset.
	type slot struct {
		positionID int64
		value      decimal.Decimal
		units      decimal.Decimal
	}
	var slots []slot
	for _, leg := range same {
		if indexOf(x, leg.PositionID) < 0 {
			continue
		}
		value := leg.value()
		merged := false
		for i := range slots {
			if slots[i].positionID == leg.PositionID {
				slots[i].value = slots[i].value.Add(value)
				slots[i].units = slots[i].units.Add(leg.Units)
				merged = true
				break
			}
		}
		if !merged {
			slots = append(slots, slot{positionID: leg.PositionID, value: value, units: leg.Units})
		}
	}
	vSame := decimal.Zero
	weights := make([]weight, len(slots))
	for i, s := range slots {
		vSame = vSame.Add(s.value)
		weights[i] = weight{positionID: s.positionID, w: s.value}
	}

	if vSame.Sign() > 0 && proceeds.Sign() > 0 {
		// gamma = min(1, V_same / proceeds): the matched share of the sold slice carries its cost.
		carried := released
		if vSame.Cmp(proceeds) < 0 {
			carried = released.Mul(vSame).Div(proceeds).Round(costScale)
		}
		if carried.Sign() > 0 {
			for i, part := range distribute(carried, weights) {
				addCost(x, slots[i].positionID, part)
			}
		}
		// Top-up: any V_same beyond the proceeds is bought at market, but only on an asset that
		// was tracked before the trade (rule 7 - an untracked asset stays untracked).
		if topUp := vSame.Sub(proceeds); topUp.Sign() > 0 && recordedSumPositive(costBefore) {
			for i, part := range distribute(topUp.Round(costScale), weights) {
				addCost(x, slots[i].positionID, part)
			}
		}
	}
	for _, s := range slots {
		bumpUnits(x, s.positionID, s.units)
	}
}

// scaleForSales reduces the recorded costs in rows (in place) for the asset's sales. Sales are
// aggregated per sold row (a row sold in several account buckets); a sold row is valued at its
// average sale price, every other priced row at its own PricePln, unpriced rows are skipped.
// It returns the total value sold and the sales that were applicable (known row, positive units
// and value); nothing is changed when none is, or when no row carries a recorded cost.
func scaleForSales(rows []Row, sales []Sale) (soldValue decimal.Decimal, applied []Sale) {
	type sold struct{ units, value decimal.Decimal }
	perRow := map[int]*sold{}
	for _, s := range sales {
		idx := indexOf(rows, s.PositionID)
		v := s.value()
		if idx < 0 || s.Units.Sign() <= 0 || v.Sign() <= 0 {
			continue
		}
		applied = append(applied, s)
		soldValue = soldValue.Add(v)
		if perRow[idx] == nil {
			perRow[idx] = &sold{}
		}
		perRow[idx].units = perRow[idx].units.Add(s.Units)
		perRow[idx].value = perRow[idx].value.Add(v)
	}
	if len(applied) == 0 {
		return decimal.Zero, nil
	}
	recorded := recordedIndexes(rows)
	if len(recorded) == 0 {
		return soldValue, applied
	}

	before := decimal.Zero // asset value before the sales; each sold row valued at its sale price
	soldUnits := decimal.Zero
	for i, r := range rows {
		switch sr := perRow[i]; {
		case sr != nil:
			before = before.Add(sr.value.Mul(r.Units).Div(sr.units))
			soldUnits = soldUnits.Add(sr.units)
		case r.PricePln != nil:
			before = before.Add(r.Units.Mul(*r.PricePln))
		}
	}
	unitsAfter := totalUnits(rows).Sub(soldUnits)

	// Nothing left (or everything sold by value): exact zero, never via rounding.
	if unitsAfter.Sign() <= 0 || soldValue.Cmp(before) >= 0 {
		for _, i := range recorded {
			zero := decimal.Zero
			rows[i].CostPln = &zero
		}
		return soldValue, applied
	}

	remaining := before.Sub(soldValue)
	total := decimal.Zero
	for _, i := range recorded {
		total = total.Add(*rows[i].CostPln)
	}
	target := total.Mul(remaining).Div(before).Round(costScale)

	largest := recorded[0]
	sum := decimal.Zero
	scaled := make(map[int]decimal.Decimal, len(recorded))
	for _, i := range recorded {
		v := rows[i].CostPln.Mul(remaining).Div(before).Round(costScale)
		scaled[i] = v
		sum = sum.Add(v)
		if isLarger(rows[i], rows[largest]) {
			largest = i
		}
	}
	scaled[largest] = scaled[largest].Add(target.Sub(sum)) // rounding remainder -> largest row
	for _, i := range recorded {
		v := scaled[i]
		rows[i].CostPln = &v
	}
	return soldValue, applied
}

// addBuy adds paid to the row's cost under rule 7 (in place).
func addBuy(rows []Row, positionID int64, paid decimal.Decimal) {
	idx := indexOf(rows, positionID)
	if idx < 0 || paid.Sign() <= 0 {
		return
	}
	if len(recordedIndexes(rows)) == 0 && totalUnits(rows).Sign() > 0 {
		return // holds units, no recorded cost: do not invent a cost covering only the new units
	}
	addCost(rows, positionID, paid)
}

// addCost adds delta to a row's cost, initializing a NULL cost to delta (in place). A zero delta
// is skipped so a NULL cost is never turned into 0 by a no-op.
func addCost(rows []Row, positionID int64, delta decimal.Decimal) {
	idx := indexOf(rows, positionID)
	if idx < 0 || delta.IsZero() {
		return
	}
	cur := decimal.Zero
	if rows[idx].CostPln != nil {
		cur = *rows[idx].CostPln
	}
	v := cur.Add(delta).Round(costScale)
	rows[idx].CostPln = &v
}

type weight struct {
	positionID int64
	w          decimal.Decimal
}

// distribute splits total across the weights pro rata, each part rounded to 4 decimals, the
// rounding remainder going to the largest weight (ties: lowest position id) so the parts sum to
// total exactly.
func distribute(total decimal.Decimal, ws []weight) []decimal.Decimal {
	parts := make([]decimal.Decimal, len(ws))
	sumW := decimal.Zero
	for _, w := range ws {
		sumW = sumW.Add(w.w)
	}
	if sumW.Sign() <= 0 {
		return parts
	}
	sum := decimal.Zero
	largest := 0
	for i, w := range ws {
		parts[i] = total.Mul(w.w).Div(sumW).Round(costScale)
		sum = sum.Add(parts[i])
		if w.w.Cmp(ws[largest].w) > 0 || (w.w.Equal(ws[largest].w) && w.positionID < ws[largest].positionID) {
			largest = i
		}
	}
	parts[largest] = parts[largest].Add(total.Sub(sum))
	return parts
}

// isLarger reports whether a should be preferred over b as the "largest row" (by recorded cost,
// ties broken by the lower position id so the choice never depends on row order).
func isLarger(a, b Row) bool {
	if c := a.CostPln.Cmp(*b.CostPln); c != 0 {
		return c > 0
	}
	return a.PositionID < b.PositionID
}

func cloneRows(a []Row) []Row {
	out := make([]Row, len(a))
	for i, r := range a {
		out[i] = r
		if r.PricePln != nil {
			v := *r.PricePln
			out[i].PricePln = &v
		}
		if r.CostPln != nil {
			v := *r.CostPln
			out[i].CostPln = &v
		}
	}
	return out
}

// restoreUnits returns work with every row's Units reset to the caller's original (before-trade)
// units, matched by position, so results only ever differ from the input in CostPln.
func restoreUnits(work, original []Row) []Row {
	units := make(map[int64]decimal.Decimal, len(original))
	for _, r := range original {
		units[r.PositionID] = r.Units
	}
	out := make([]Row, len(work))
	copy(out, work)
	for i := range out {
		out[i].Units = units[out[i].PositionID]
	}
	return out
}

func indexOf(rows []Row, positionID int64) int {
	for i, r := range rows {
		if r.PositionID == positionID {
			return i
		}
	}
	return -1
}

// recordedIndexes lists the rows that carry a recorded cost (> 0), in input order.
func recordedIndexes(rows []Row) []int {
	var idx []int
	for i, r := range rows {
		if r.CostPln != nil && r.CostPln.Sign() > 0 {
			idx = append(idx, i)
		}
	}
	return idx
}

func recordedSumPositive(d decimal.Decimal) bool { return d.Sign() > 0 }

func recordedSum(rows []Row) decimal.Decimal {
	sum := decimal.Zero
	for _, r := range rows {
		if r.CostPln != nil {
			sum = sum.Add(*r.CostPln)
		}
	}
	return sum
}

func totalUnits(rows []Row) decimal.Decimal {
	sum := decimal.Zero
	for _, r := range rows {
		sum = sum.Add(r.Units)
	}
	return sum
}

func bumpUnits(rows []Row, positionID int64, delta decimal.Decimal) {
	if idx := indexOf(rows, positionID); idx >= 0 {
		rows[idx].Units = rows[idx].Units.Add(delta)
	}
}

func sameCost(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
