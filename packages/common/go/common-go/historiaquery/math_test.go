package historiaquery

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func flow(date, amount string) CashFlow {
	return CashFlow{Date: mustDate(date), Amount: dec(amount)}
}

func TestCumulativeSince(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{
		{2022, 8}:  dec("100"),
		{2022, 9}:  dec("101.6"),
		{2022, 10}: dec("100.5"),
	}
	// Base month is exclusive: only September compounds → 0.016.
	if got := CumulativeSince(YearMonth{2022, 8}, mustDate("2022-09-15"), mom); got.String() != "0.016" {
		t.Fatalf("expected 0.016, got %s", got)
	}
	// 1.016 * 1.005 - 1 = 0.02108.
	if got := CumulativeSince(YearMonth{2022, 8}, mustDate("2022-10-20"), mom); got.String() != "0.02108" {
		t.Fatalf("expected 0.02108, got %s", got)
	}
	// Empty / inverted range → factor ONE → zero cumulative.
	if got := CumulativeSince(YearMonth{2022, 10}, mustDate("2022-09-15"), mom); !got.IsZero() {
		t.Fatalf("expected zero for an inverted range, got %s", got)
	}
}

func TestAnnualized(t *testing.T) {
	// Under 12 months since base, extrapolating via ^(12/months) would blow a single
	// month's 1.6% up to ~21% — return the raw cumulative fraction unextrapolated.
	if got := Annualized(dec("0.016"), YearMonth{2022, 8}, YearMonth{2022, 9}); got.String() != "0.016" {
		t.Fatalf("expected raw cumulative 0.016 under 12 months, got %s", got)
	}
	if !Annualized(dec("0.5"), YearMonth{2022, 9}, YearMonth{2022, 9}).IsZero() {
		t.Fatal("expected ZERO for a zero-month span")
	}
	// At exactly 12 months, normal CAGR extrapolation applies: 1.05^1-1 = 0.05.
	got12 := Annualized(dec("0.05"), YearMonth{2021, 9}, YearMonth{2022, 9})
	if f := got12.InexactFloat64(); math.Abs(f-0.05) > 1e-9 {
		t.Fatalf("expected 0.05 at exactly 12 months, got %f", f)
	}
}

func TestInflationAdjustedDeposits_InflatesEachDepositFromItsOwnMonth(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{
		{2024, 1}: dec("100.4"),
		{2024, 2}: dec("100.3"),
		{2024, 3}: dec("100.5"),
	}
	flows := []CashFlow{flow("2024-01-10", "1000"), flow("2024-03-05", "500")}

	// As of March: the January deposit compounds Feb × Mar, the March deposit not at all
	// (its own month is exclusive) — 1000 × 1.003 × 1.005 + 500 = 1008.015 + 500.
	got := InflationAdjustedDeposits(flows, mustDate("2024-03-20"), mom)
	if got == nil || got.String() != "1508.015" {
		t.Fatalf("expected 1508.015, got %v", got)
	}
	// As of February only the January deposit exists: 1000 × 1.003.
	got = InflationAdjustedDeposits(flows, mustDate("2024-02-15"), mom)
	if got == nil || got.String() != "1003" {
		t.Fatalf("expected 1003, got %v", got)
	}
}

func TestInflationAdjustedDeposits_IgnoresFlowsAfterSnapshotDate(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{{2024, 2}: dec("100.3")}
	flows := []CashFlow{flow("2024-02-01", "1000"), flow("2024-02-20", "400")}

	// Same month, but the 02-20 deposit lies after the 02-10 snapshot: it is not in that
	// day's total deposits, so it must not appear on the break-even line either.
	got := InflationAdjustedDeposits(flows, mustDate("2024-02-10"), mom)
	if got == nil || got.String() != "1000" {
		t.Fatalf("expected 1000 (later same-month deposit excluded), got %v", got)
	}
	got = InflationAdjustedDeposits(flows, mustDate("2024-02-20"), mom)
	if got == nil || got.String() != "1400" {
		t.Fatalf("expected 1400 on the deposit's own day, got %v", got)
	}
}

func TestInflationAdjustedDeposits_BeforeFirstDepositIsZero(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{{2024, 2}: dec("100.3")}
	got := InflationAdjustedDeposits([]CashFlow{flow("2024-02-15", "1000")}, mustDate("2024-01-31"), mom)
	if got == nil || !got.IsZero() {
		t.Fatalf("expected zero before the first deposit, got %v", got)
	}
}

func TestInflationAdjustedDeposits_FlatPricesEqualPlainDeposits(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{
		{2024, 2}: dec("100"),
		{2024, 3}: dec("100"),
	}
	flows := []CashFlow{flow("2024-01-10", "1000"), flow("2024-02-10", "250")}
	got := InflationAdjustedDeposits(flows, mustDate("2024-03-31"), mom)
	if got == nil || !got.Equal(dec("1250")) {
		t.Fatalf("with zero inflation the line must equal total deposits (1250), got %v", got)
	}
}

func TestInflationAdjustedDeposits_NilWithoutFlowsOrIndex(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{{2024, 2}: dec("100.3")}
	if InflationAdjustedDeposits(nil, mustDate("2024-02-15"), mom) != nil {
		t.Fatal("expected nil for no flows")
	}
	if InflationAdjustedDeposits([]CashFlow{flow("2024-01-10", "1000")}, mustDate("2024-02-15"), nil) != nil {
		t.Fatal("expected nil for an empty index")
	}
}

func TestRealReturn(t *testing.T) {
	mom := map[YearMonth]decimal.Decimal{
		{2022, 8}: dec("100"),
		{2022, 9}: dec("101.6"),
	}
	adjusted := InflationAdjustedDeposits([]CashFlow{flow("2022-08-15", "1000")}, mustDate("2022-09-15"), mom)
	// 1100 / (1000 × 1.016) - 1.
	got := RealReturn(dec("1100"), adjusted)
	if got == nil || got.String() != "0.08267717" {
		t.Fatalf("expected 0.08267717, got %v", got)
	}
	// Above the break-even line ⇒ positive, below ⇒ negative.
	if below := RealReturn(dec("1000"), adjusted); below == nil || below.Sign() >= 0 {
		t.Fatalf("value under the adjusted deposits must be a negative real return, got %v", below)
	}
	if RealReturn(dec("1100"), nil) != nil {
		t.Fatal("expected nil when adjusted deposits are unavailable")
	}
	zero := decimal.Zero
	if RealReturn(dec("1100"), &zero) != nil {
		t.Fatal("expected nil for a zero denominator")
	}
}
