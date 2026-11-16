package position

import (
	"testing"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func decPtr(s string) *decimal.Decimal { d := dec(s); return &d }

func TestApplyDelta_IkeIkze(t *testing.T) {
	pos := &PositionUnits{Exchange: ExchangeXTB, PriceCurrency: CurrencyUSD}
	ApplyDelta(pos, AccountTypeIKE, dec("2.5"))
	if !pos.UnitsIke.Equal(dec("2.5")) {
		t.Fatalf("expected UnitsIke=2.5, got %s", pos.UnitsIke)
	}
	ApplyDelta(pos, AccountTypeIKZE, dec("1.0"))
	if !pos.UnitsIkze.Equal(dec("1.0")) {
		t.Fatalf("expected UnitsIkze=1.0, got %s", pos.UnitsIkze)
	}
	// negative delta (a sell) subtracts.
	ApplyDelta(pos, AccountTypeIKE, dec("-0.5"))
	if !pos.UnitsIke.Equal(dec("2.0")) {
		t.Fatalf("expected UnitsIke=2.0 after sell, got %s", pos.UnitsIke)
	}
}

func TestApplyDelta_RegularBucketSelection(t *testing.T) {
	cases := []struct {
		name     string
		exchange string
		currency string
		want     func(PositionUnits) decimal.Decimal
	}{
		{"XTB USD -> regular USD bucket", ExchangeXTB, CurrencyUSD, func(p PositionUnits) decimal.Decimal { return p.UnitsRegularUsd }},
		{"XTB EUR -> regular EUR bucket", ExchangeXTB, CurrencyEUR, func(p PositionUnits) decimal.Decimal { return p.UnitsRegularEur }},
		{"XTB PLN -> regular PLN bucket", ExchangeXTB, CurrencyPLN, func(p PositionUnits) decimal.Decimal { return p.UnitsRegularPln }},
		{"Binance (non-XTB) always -> regular PLN bucket regardless of priceCurrency", ExchangeBINANCE, CurrencyUSD, func(p PositionUnits) decimal.Decimal { return p.UnitsRegularPln }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := &PositionUnits{Exchange: tc.exchange, PriceCurrency: tc.currency}
			ApplyDelta(pos, AccountTypeREGULAR, dec("3"))
			if got := tc.want(*pos); !got.Equal(dec("3")) {
				t.Fatalf("expected bucket=3, got %s (pos=%+v)", got, pos)
			}
		})
	}
}

func TestApplyDeltaWithCurrencyOverride_FallsBackToPriceCurrencyWhenEmpty(t *testing.T) {
	pos := &PositionUnits{Exchange: ExchangeBINANCE, PriceCurrency: CurrencyUSD}
	// No override supplied -> falls back to pos.PriceCurrency (USD), NOT the
	// exchange-based default ApplyDelta would have used (which would have picked PLN
	// for a non-XTB exchange). This is the documented divergence between the two
	// entry points.
	ApplyDeltaWithCurrencyOverride(pos, AccountTypeREGULAR, "", dec("5"))
	if !pos.UnitsRegularUsd.Equal(dec("5")) {
		t.Fatalf("expected fallback to priceCurrency USD bucket, got %+v", pos)
	}
}

func TestApplyDeltaWithCurrencyOverride_ExplicitOverrideWins(t *testing.T) {
	pos := &PositionUnits{Exchange: ExchangeXTB, PriceCurrency: CurrencyUSD}
	ApplyDeltaWithCurrencyOverride(pos, AccountTypeREGULAR, CurrencyPLN, dec("7"))
	if !pos.UnitsRegularPln.Equal(dec("7")) {
		t.Fatalf("expected explicit PLN override to win over priceCurrency USD, got %+v", pos)
	}
	if !pos.UnitsRegularUsd.IsZero() {
		t.Fatalf("expected USD bucket untouched, got %s", pos.UnitsRegularUsd)
	}
}

func TestUnitsTotal_SumsAllFiveBuckets(t *testing.T) {
	pos := PositionUnits{
		UnitsIke:        dec("1"),
		UnitsIkze:       dec("2"),
		UnitsRegularEur: dec("3"),
		UnitsRegularUsd: dec("4"),
		UnitsRegularPln: dec("5"),
	}
	if got := pos.UnitsTotal(); !got.Equal(dec("15")) {
		t.Fatalf("expected 15, got %s", got)
	}
}
