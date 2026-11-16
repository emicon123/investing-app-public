package position

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/costbasis"
	"investing-app/common-go/costmember"
	"investing-app/common-go/pgnum"
)

// member builds a cost-bookkeeping member: regularPln units, optional cost ("" = NULL) and yahoo
// ticker ("" = none).
func member(id, assetID int64, units, cost, yahoo string) *CostMember {
	u := &PositionUnits{Exchange: ExchangeXTB, PriceCurrency: CurrencyPLN, UnitsRegularPln: dec(units)}
	if cost != "" {
		u.TotalSpentPln = decPtr(cost)
	}
	m := &CostMember{ID: id, AssetID: assetID, Units: u}
	if yahoo != "" {
		m.YahooTicker = &yahoo
	}
	return m
}

func costOf(m *CostMember) string {
	if m.Units.TotalSpentPln == nil {
		return "NULL"
	}
	return m.Units.TotalSpentPln.String()
}

// sellOn / buyOn run one trade the way the trade services do: kernel rows from the pre-trade state,
// kernel call, results copied back onto the members. Returns the members that changed.
func sellOn(members []*CostMember, prices map[string]decimal.Decimal, positionID int64, units, price string) []*CostMember {
	before := costmember.Rows(members, prices)
	after := costbasis.ApplySale(before, costbasis.Sale{PositionID: positionID, Units: dec(units), PricePln: dec(price)})
	return costmember.Apply(members, before, after)
}

func buyOn(members []*CostMember, prices map[string]decimal.Decimal, positionID int64, paid string) []*CostMember {
	before := costmember.Rows(members, prices)
	after := costbasis.ApplyBuy(before, positionID, dec(paid))
	return costmember.Apply(members, before, after)
}

func TestPricePlnMap_SkipsRowsWithoutPlnPrice(t *testing.T) {
	rows := []sqlcgen.GetAllPriceCacheRow{
		{Ticker: "A.YAHOO", PricePln: pgnum.FromDecimal(dec("12.5"))},
		{Ticker: "B.YAHOO", PricePln: pgtype.Numeric{}},
	}
	got, err := PricePlnMap(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got["A.YAHOO"].Equal(dec("12.5")) {
		t.Fatalf("got %v, want only A.YAHOO=12.5", got)
	}
	if _, err := PricePlnMap([]sqlcgen.GetAllPriceCacheRow{{Ticker: "C", PricePln: pgtype.Numeric{NaN: true, Valid: true}}}); err == nil {
		t.Fatal("expected a NaN price to fail loudly")
	}
}

func TestCostRows_UnitsSumAllBucketsPriceByYahooTickerAndCostCopied(t *testing.T) {
	m := &CostMember{ID: 7, AssetID: 3, Units: &PositionUnits{
		UnitsIke: dec("1"), UnitsIkze: dec("2"), UnitsRegularEur: dec("3"), UnitsRegularUsd: dec("4"), UnitsRegularPln: dec("5"),
		TotalSpentPln: decPtr("99"),
	}}
	y := "X.YAHOO"
	m.YahooTicker = &y
	unpriced := member(8, 3, "1", "", "")
	rows := costmember.Rows([]*CostMember{m, unpriced}, map[string]decimal.Decimal{"X.YAHOO": dec("10")})
	if !rows[0].Units.Equal(dec("15")) || rows[0].PricePln == nil || !rows[0].PricePln.Equal(dec("10")) {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[1].PricePln != nil || rows[1].CostPln != nil {
		t.Fatalf("unpriced NULL-cost member must yield nil price and nil cost, got %+v", rows[1])
	}
	*rows[0].CostPln = dec("1")
	if !m.Units.TotalSpentPln.Equal(dec("99")) {
		t.Fatal("mutating a kernel row must not touch the member's own cost")
	}
}

// Migrated from UpdateTotalSpentPln's old sold-row-only tests: the still-valid cases under the
// asset-level semantics (design 161 §2).

func TestAssetCost_Buy_AddsPaidToATrackedRow(t *testing.T) {
	members := []*CostMember{member(1, 1, "10", "1000", "")}
	changed := buyOn(members, nil, 1, "500")
	if len(changed) != 1 || costOf(members[0]) != "1500" {
		t.Fatalf("cost = %s, want 1500", costOf(members[0]))
	}
}

func TestAssetCost_Buy_OnNullCostAssetThatHoldsUnits_InventsNothing(t *testing.T) {
	// Was: "buy on a NULL basis initializes it to the buy only" - that understated the entry price
	// of an asset whose baseline is unknown (design 161 rule 7).
	members := []*CostMember{member(1, 1, "10", "", "")}
	if changed := buyOn(members, nil, 1, "500"); len(changed) != 0 || costOf(members[0]) != "NULL" {
		t.Fatalf("cost = %s, changed %d, want NULL/unchanged", costOf(members[0]), len(changed))
	}
}

func TestAssetCost_Buy_OnEmptyAsset_InitializesToThePaidAmount(t *testing.T) {
	members := []*CostMember{member(1, 1, "0", "", "")}
	buyOn(members, nil, 1, "500")
	if costOf(members[0]) != "500" {
		t.Fatalf("cost = %s, want 500", costOf(members[0]))
	}
}

func TestAssetCost_Sell_ReducesProportionallyRegardlessOfSalePrice(t *testing.T) {
	// Held 10 units, total cost 1000. Selling 4 units removes 40 % of the asset's value, so 40 %
	// of its cost - at any sale price (a single row is valued at the sale price).
	members := []*CostMember{member(1, 1, "10", "1000", "")}
	sellOn(members, nil, 1, "4", "200")
	if costOf(members[0]) != "600" {
		t.Fatalf("cost = %s, want 600 (1000 x 0.6)", costOf(members[0]))
	}
}

func TestAssetCost_Sell_MoreThanHeldNeverGoesNegative(t *testing.T) {
	members := []*CostMember{member(1, 1, "1", "100", "")}
	sellOn(members, nil, 1, "5", "50")
	if costOf(members[0]) != "0" {
		t.Fatalf("cost = %s, want 0 (floor)", costOf(members[0]))
	}
}

func TestAssetCost_Sell_NullStaysNullAndZeroStaysZero(t *testing.T) {
	// Coexistence-smoke regression (SMOKET1, kept): a sale on a NULL cost basis must not rewrite
	// the column to 0.
	null := []*CostMember{member(1, 1, "10", "", "")}
	if changed := sellOn(null, nil, 1, "4", "50"); len(changed) != 0 || costOf(null[0]) != "NULL" {
		t.Fatalf("NULL cost must survive a sale untouched, got %s (changed %d)", costOf(null[0]), len(changed))
	}
	zero := []*CostMember{member(1, 1, "10", "0", "")}
	if changed := sellOn(zero, nil, 1, "4", "50"); len(changed) != 0 || costOf(zero[0]) != "0" {
		t.Fatalf("a zero cost stays zero, got %s (changed %d)", costOf(zero[0]), len(changed))
	}
}

func TestAssetCost_Sell_OfNullCostSiblingScalesTheHeadRow(t *testing.T) {
	// The live bug (design 161 §3.4): the go-live cost sits on the head, the ETN sibling is NULL.
	prices := map[string]decimal.Decimal{"HEAD": dec("100"), "ETN": dec("20")}
	members := []*CostMember{member(1, 1, "10", "1000", "HEAD"), member(2, 1, "50", "", "ETN")} // V = 1000 + 1000
	changed := sellOn(members, prices, 2, "50", "20")                                           // whole ETN
	if len(changed) != 1 || changed[0].ID != 1 {
		t.Fatalf("only the head must change, got %v", changed)
	}
	if costOf(members[0]) != "500" || costOf(members[1]) != "NULL" {
		t.Fatalf("head cost %s / ETN cost %s, want 500 / NULL", costOf(members[0]), costOf(members[1]))
	}
}

type recordingWriter struct {
	got []sqlcgen.UpdatePositionCostParams
	err error
}

func (w *recordingWriter) UpdatePositionCost(_ context.Context, arg sqlcgen.UpdatePositionCostParams) error {
	if w.err != nil {
		return w.err
	}
	w.got = append(w.got, arg)
	return nil
}

func TestPersistCosts_WritesChangedRowsExceptThoseTheDeltaWriteCarries(t *testing.T) {
	prices := map[string]decimal.Decimal{"HEAD": dec("100"), "SIB": dec("100")}
	members := []*CostMember{member(1, 1, "10", "1000", "HEAD"), member(2, 1, "10", "", "SIB")}
	changed := sellOn(members, prices, 2, "5", "100") // scales the head (id 1); the sold sibling stays NULL

	w := &recordingWriter{}
	if err := PersistCosts(context.Background(), w, changed); err != nil {
		t.Fatal(err)
	}
	if len(w.got) != 1 || w.got[0].ID != 1 {
		t.Fatalf("writes = %+v, want exactly one for position 1", w.got)
	}
	if v, _ := pgnum.ToDecimalPtr(w.got[0].TotalSpentPln); v == nil || !v.Equal(dec("750")) {
		t.Fatalf("written cost = %v, want 750", v)
	}

	skipped := &recordingWriter{}
	if err := PersistCosts(context.Background(), skipped, changed, 1); err != nil || len(skipped.got) != 0 {
		t.Fatalf("a row written by UpdatePositionDelta must be skipped, got %+v err %v", skipped.got, err)
	}

	boom := &recordingWriter{err: errors.New("boom")}
	if err := PersistCosts(context.Background(), boom, changed); err == nil {
		t.Fatal("a write error must propagate so the transaction rolls back")
	}
}
