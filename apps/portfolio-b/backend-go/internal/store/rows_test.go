package store

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore"
	"portfolio-b/backend-go/db/sqlcgen"
	"portfolio-b/backend-go/internal/book"
)

func num(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestSubassetFromRow_FiveColumnsAreFiveBucketsAndTheWholeBook(t *testing.T) {
	cost := num("12.5")
	row := sqlcgen.Position{
		ID: 3, AssetID: 9, Ticker: "T", IsPrimary: true,
		UnitsIkeTata: pgnum.FromDecimal(num("1")), UnitsIkeMama: pgnum.FromDecimal(num("2")),
		UnitsIkzeTata: pgnum.FromDecimal(num("3")), UnitsIkzeMama: pgnum.FromDecimal(num("4")),
		UnitsInne: pgnum.FromDecimal(num("5")), TotalSpentPln: pgnum.FromDecimalPtr(&cost),
	}
	s, err := subassetFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if s.Portfolio != siblingcore.WholeBook {
		t.Fatalf("portfolio %q", s.Portfolio)
	}
	for i, b := range []siblingcore.Bucket{book.IkeTata, book.IkeMama, book.IkzeTata, book.IkzeMama, book.Inne} {
		if !s.Holding.Units.Of(b).Equal(decimal.NewFromInt(int64(i + 1))) {
			t.Fatalf("bucket %s = %s", b, s.Holding.Units.Of(b))
		}
	}
	if !s.Holding.Units.Total().Equal(num("15")) || s.Holding.TotalSpentPln == nil || !s.Holding.TotalSpentPln.Equal(cost) {
		t.Fatalf("holding %+v", s.Holding)
	}
	if len(unitColumns) != len(book.Config.Buckets) {
		t.Fatal("every configured bucket needs a column")
	}
	for i, c := range unitColumns {
		if c.bucket != book.Config.Buckets[i].Bucket {
			t.Fatalf("column %d maps %s, config has %s", i, c.bucket, book.Config.Buckets[i].Bucket)
		}
	}
}

func TestAssetAndDepositFromRow_DecodeAsTheWholeBook(t *testing.T) {
	a, err := assetFromRow(sqlcgen.Asset{ID: 1, Key: "K"})
	if err != nil || a.Portfolio != siblingcore.WholeBook || a.TargetAllocationPct != nil {
		t.Fatalf("asset %+v %v", a, err)
	}
	d, err := depositFromRow(sqlcgen.Deposit{ID: 2, Amount: pgnum.FromDecimal(num("10"))})
	if err != nil || d.Portfolio != siblingcore.WholeBook || !d.Amount.Equal(num("10")) {
		t.Fatalf("deposit %+v %v", d, err)
	}
}

func TestPositionParams_WriteEveryBucketToItsColumn(t *testing.T) {
	sub := siblingcore.Subasset{ID: 4, Holding: siblingcore.Holding{Units: siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{
		book.IkeTata: num("1"), book.IkeMama: num("2"), book.IkzeTata: num("3"), book.IkzeMama: num("4"), book.Inne: num("5"),
	})}}
	p := updatePositionFieldsParams(sub)
	for i, n := range []pgtype.Numeric{p.UnitsIkeTata, p.UnitsIkeMama, p.UnitsIkzeTata, p.UnitsIkzeMama, p.UnitsInne} {
		if v := pgnum.ToDecimalOrZero(n); !v.Equal(decimal.NewFromInt(int64(i + 1))) {
			t.Fatalf("column %d = %s", i, v)
		}
	}
	if p.TotalSpentPln.Valid {
		t.Fatal("nil cost writes NULL")
	}
	ins := insertPositionParams(sub)
	for i, n := range []pgtype.Numeric{ins.UnitsIkeTata, ins.UnitsIkeMama, ins.UnitsIkzeTata, ins.UnitsIkzeMama, ins.UnitsInne} {
		if !n.Valid || !pgnum.ToDecimalOrZero(n).IsZero() {
			t.Fatalf("insert column %d must be a zero, not NULL: %+v", i, n)
		}
	}
}

func TestPositionDeltaParams_EveryBucketAndTheCost(t *testing.T) {
	cost := num("9")
	h := siblingcore.Holding{Units: siblingcore.NewUnits(map[siblingcore.Bucket]decimal.Decimal{book.IkzeTata: num("3"), book.Inne: num("5")}), TotalSpentPln: &cost}
	p := positionDeltaParams(7, h)
	if p.ID != 7 || !pgnum.ToDecimalOrZero(p.UnitsIkzeTata).Equal(num("3")) || !pgnum.ToDecimalOrZero(p.UnitsInne).Equal(num("5")) ||
		!p.UnitsIkeTata.Valid || !pgnum.ToDecimalOrZero(p.UnitsIkeTata).IsZero() || !pgnum.ToDecimalOrZero(p.TotalSpentPln).Equal(cost) {
		t.Fatalf("params %+v", p)
	}
	if positionDeltaParams(7, siblingcore.Holding{}).TotalSpentPln.Valid {
		t.Fatal("a nil cost stays NULL")
	}
}
