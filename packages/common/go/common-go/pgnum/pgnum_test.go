package pgnum

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
)

func TestRoundTrip(t *testing.T) {
	tests := []string{
		"0",
		"1",
		"-1",
		"123456789012.12345678",
		"0.00000001",
		"-4.28000000",
		"65432.10000000",
	}
	for _, s := range tests {
		want := decimal.RequireFromString(s)
		n := FromDecimal(want)
		got, err := ToDecimal(n)
		if err != nil {
			t.Fatalf("ToDecimal(%s): %v", s, err)
		}
		if !got.Equal(want) {
			t.Errorf("round-trip mismatch for %s: got %s", s, got)
		}
	}
}

func TestToDecimal_NullIsError(t *testing.T) {
	if _, err := ToDecimal(pgtype.Numeric{Valid: false}); err == nil {
		t.Fatal("expected an error for a NULL numeric")
	}
}

func TestToDecimal_NaNIsError(t *testing.T) {
	if _, err := ToDecimal(pgtype.Numeric{Valid: true, NaN: true}); err == nil {
		t.Fatal("expected an error for a NaN numeric")
	}
}

func TestToDecimalOrZero_NullReturnsZero(t *testing.T) {
	if got := ToDecimalOrZero(pgtype.Numeric{Valid: false}); !got.Equal(decimal.Zero) {
		t.Fatalf("expected zero, got %s", got)
	}
}

func TestDate_KeepsTheInstantsOwnCalendarDateAtUTCMidnight(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Skip("no tzdata")
	}
	// 00:30 in Warsaw on 2026-03-01 is still 2026-02-28 in UTC; the date must be Warsaw's.
	got := Date(time.Date(2026, 3, 1, 0, 30, 0, 0, warsaw))
	want := pgtype.Date{Time: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	if got != want {
		t.Fatalf("Date = %+v, want %+v", got, want)
	}
}

func TestDate_UTCInstant(t *testing.T) {
	got := Date(time.Date(2026, 11, 5, 23, 59, 59, 999, time.UTC))
	want := pgtype.Date{Time: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), Valid: true}
	if got != want {
		t.Fatalf("Date = %+v, want %+v", got, want)
	}
}
