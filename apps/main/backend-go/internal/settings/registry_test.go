package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
)

// stubReader is an in-memory SettingReader for registry table tests.
type stubReader struct {
	values map[string]string
	err    error
}

func (s *stubReader) GetSettingByKey(_ context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	if s.err != nil {
		return sqlcgen.GetSettingByKeyRow{}, s.err
	}
	v, ok := s.values[key]
	if !ok {
		return sqlcgen.GetSettingByKeyRow{}, pgx.ErrNoRows
	}
	return sqlcgen.GetSettingByKeyRow{Key: key, Value: v}, nil
}

func TestRegistry_DecimalNonStrict_MissingOrMalformedFallsBack(t *testing.T) {
	ctx := context.Background()
	fallback := decimal.NewFromInt(1000)
	for _, tc := range []struct {
		name   string
		values map[string]string
		want   decimal.Decimal
	}{
		{"missing key", map[string]string{}, fallback},
		{"malformed value", map[string]string{DefRetirementMonthlyContributionPln.Key: "not-a-number"}, fallback},
		{"whitespace-padded is malformed like Java BigDecimal(String)", map[string]string{DefRetirementMonthlyContributionPln.Key: " 2500 "}, fallback},
		{"valid value", map[string]string{DefRetirementMonthlyContributionPln.Key: "2500.50"}, decimal.RequireFromString("2500.50")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Get(ctx, &stubReader{values: tc.values}, DefRetirementMonthlyContributionPln, fallback, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("expected %s, got %s", tc.want, got)
			}
		})
	}
}

func TestRegistry_Bool_MissingFallsBackTrueButMalformedIsFalse(t *testing.T) {
	ctx := context.Background()
	// The documented asymmetry, ported from the Java CODE (not its javadoc): missing →
	// fallback true, present-but-malformed → Boolean.parseBoolean semantics → false.
	for _, tc := range []struct {
		name    string
		values  map[string]string
		want    bool
		wantErr bool
	}{
		{"missing key falls back to true", map[string]string{}, true, false},
		{"explicit true", map[string]string{DefMacroDeriskEnabled.Key: "true"}, true, false},
		{"case-insensitive TRUE", map[string]string{DefMacroDeriskEnabled.Key: "TRUE"}, true, false},
		{"whitespace-padded true (Java strips)", map[string]string{DefMacroDeriskEnabled.Key: "  true  "}, true, false},
		{"explicit false", map[string]string{DefMacroDeriskEnabled.Key: "false"}, false, false},
		{"malformed is false, NOT the true fallback", map[string]string{DefMacroDeriskEnabled.Key: "yes"}, false, false},
		{"empty string is false", map[string]string{DefMacroDeriskEnabled.Key: ""}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Get(ctx, &stubReader{values: tc.values}, DefMacroDeriskEnabled, MacroDeriskEnabledFallback, false)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v, got err=%v", tc.wantErr, err)
			}
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestRegistry_Strict_MissingOrMalformedIs404(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		def    Def
		values map[string]string
	}{
		{"missing ike limit", DefIkeAnnualLimitPln, map[string]string{}},
		{"malformed ike limit", DefIkeAnnualLimitPln, map[string]string{DefIkeAnnualLimitPln.Key: "garbage"}},
		{"missing ikze limit", DefIkzeAnnualLimitPln, map[string]string{DefIkeAnnualLimitPln.Key: "23472.00"}},
		{"malformed ikze limit", DefIkzeAnnualLimitPln, map[string]string{DefIkeAnnualLimitPln.Key: "23472.00", DefIkzeAnnualLimitPln.Key: "9,388.80"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Get(ctx, &stubReader{values: tc.values}, tc.def, decimal.Zero, true)
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) || appErr.Status != 404 || appErr.Code != httpx.CodeNotFound {
				t.Fatalf("expected 404 NOT_FOUND, got %v", err)
			}
		})
	}

	t.Run("valid strict values parse", func(t *testing.T) {
		q := &stubReader{values: map[string]string{
			DefIkeAnnualLimitPln.Key:  "23472.00",
			DefIkzeAnnualLimitPln.Key: "9388.80",
		}}
		ike, err := Get(ctx, q, DefIkeAnnualLimitPln, decimal.Zero, true)
		if err != nil || !ike.Equal(decimal.RequireFromString("23472.00")) {
			t.Fatalf("expected 23472.00, got %v,%v", ike, err)
		}
		ikze, err := Get(ctx, q, DefIkzeAnnualLimitPln, decimal.Zero, true)
		if err != nil || !ikze.Equal(decimal.RequireFromString("9388.80")) {
			t.Fatalf("expected 9388.80, got %v,%v", ikze, err)
		}
	})
}

// AllDefs is the registry the settings list is checked against: keys must be unique and the retired
// cut-factor key (V37 deleted it) must not come back.
func TestRegistry_AllDefsKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range AllDefs {
		if d.Key == "rebalancing.macro_derisk_cut_factor" {
			t.Fatal("retired cut-factor key must NOT be registered (V37 deleted it)")
		}
		if seen[d.Key] {
			t.Fatalf("duplicate registry key %q", d.Key)
		}
		seen[d.Key] = true
	}
}
