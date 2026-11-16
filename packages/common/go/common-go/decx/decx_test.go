package decx

import (
	"errors"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
)

func TestToFloat(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"1234.5678", 1234.5678},
		{"-0.0001", -0.0001},
		{"0.12345678", 0.12345678},
	}
	for _, tc := range tests {
		if got := ToFloat(decimal.RequireFromString(tc.in)); got != tc.want {
			t.Errorf("ToFloat(%s) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestToFloatPtr_NilIsNil(t *testing.T) {
	if got := ToFloatPtr(nil); got != nil {
		t.Fatalf("ToFloatPtr(nil) = %v, want nil", *got)
	}
}

func TestToFloatPtr_NonNil(t *testing.T) {
	d := decimal.RequireFromString("42.125")
	got := ToFloatPtr(&d)
	if got == nil || *got != 42.125 {
		t.Fatalf("ToFloatPtr(42.125) = %v, want 42.125", got)
	}
}

func TestValidatePercentRange(t *testing.T) {
	dec := func(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }
	tests := []struct {
		name    string
		v       *decimal.Decimal
		wantErr bool
	}{
		{"nil allowed", nil, false},
		{"zero", dec("0"), false},
		{"hundred", dec("100"), false},
		{"inside", dec("37.5"), false},
		{"just below zero", dec("-0.0001"), true},
		{"just above hundred", dec("100.0001"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePercentRange("targetAllocationPctRiskOff", tc.v)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) {
				t.Fatalf("err = %v, want *httpx.AppError", err)
			}
			want := httpx.AppError{Status: http.StatusBadRequest, Code: httpx.CodeValidationError,
				Field: "targetAllocationPctRiskOff", Message: "must be between 0 and 100"}
			if *appErr != want {
				t.Fatalf("err = %+v, want %+v", *appErr, want)
			}
		})
	}
}
