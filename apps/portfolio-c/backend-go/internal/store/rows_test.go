package store

import (
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/pgnum"
	"investing-app/common-go/siblingcore"
	"portfolio-c/backend-go/db/sqlcgen"
)

func num(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestSubassetFromRow_UnitsColumnIsTheSingleBucketAndThePortfolioDecodes(t *testing.T) {
	s, err := subassetFromRow(sqlcgen.Position{ID: 1, Portfolio: sqlcgen.PortfolioEnumFIXED, Ticker: "T", Units: pgnum.FromDecimal(num("7.5"))})
	if err != nil {
		t.Fatal(err)
	}
	if s.Portfolio != "FIXED" || !s.Holding.Units.Of(siblingcore.SingleBucket).Equal(num("7.5")) || !s.Holding.Units.Total().Equal(num("7.5")) {
		t.Fatalf("subasset %+v", s)
	}
	if s.Holding.TotalSpentPln != nil {
		t.Fatal("NULL cost must stay nil")
	}
}

func TestAssetAndDepositFromRow_DecodeTheirPortfolio(t *testing.T) {
	a, err := assetFromRow(sqlcgen.Asset{ID: 1, Portfolio: sqlcgen.PortfolioEnumVARIABLE, Key: "K"})
	if err != nil || a.Portfolio != "VARIABLE" {
		t.Fatalf("asset %+v %v", a, err)
	}
	d, err := depositFromRow(sqlcgen.Deposit{ID: 2, Portfolio: sqlcgen.PortfolioEnumFIXED, Amount: pgnum.FromDecimal(num("10"))})
	if err != nil || d.Portfolio != "FIXED" {
		t.Fatalf("deposit %+v %v", d, err)
	}
	if enum("VARIABLE") != sqlcgen.PortfolioEnumVARIABLE {
		t.Fatal("portfolio binds to the enum")
	}
}

func TestPositionParams_UnitsColumnIsTheSingleBucketAndThePortfolioBinds(t *testing.T) {
	sub := siblingcore.Subasset{ID: 4, Portfolio: "FIXED", Holding: siblingcore.Holding{Units: unitsOf(num("2.5"))}}
	if v := pgnum.ToDecimalOrZero(updatePositionFieldsParams(sub).Units); !v.Equal(num("2.5")) {
		t.Fatalf("units %s", v)
	}
	ins := insertPositionParams(sub)
	if ins.Portfolio != sqlcgen.PortfolioEnumFIXED || !ins.Units.Valid || !pgnum.ToDecimalOrZero(ins.Units).IsZero() {
		t.Fatalf("insert %+v", ins)
	}
}

func TestPositionDeltaParams_UnitsAndCost(t *testing.T) {
	cost := num("9")
	p := positionDeltaParams(7, siblingcore.Holding{Units: unitsOf(num("3")), TotalSpentPln: &cost})
	if p.ID != 7 || !pgnum.ToDecimalOrZero(p.Units).Equal(num("3")) || !pgnum.ToDecimalOrZero(p.TotalSpentPln).Equal(cost) {
		t.Fatalf("params %+v", p)
	}
}
