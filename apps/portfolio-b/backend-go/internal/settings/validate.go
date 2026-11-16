package settings

import (
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/settingscore"
)

// Registered setting keys (ported from the Java SettingKeys, ADR-094).
const (
	KeyMonthlyContribution = "monthly_contribution_pln"
	KeyIkeAnnualLimit      = "ike_annual_limit_pln"
	KeyIkzeAnnualLimit     = "ikze_annual_limit_pln"
)

// Validate checks the 3 registered keys: each must parse as a decimal strictly greater than zero;
// every other key (including one that exists in the table but was never registered here) passes
// through unvalidated. Errors map to 400 VALIDATION_ERROR (httpx.NewValidationError), as this app's
// docs/openapi.yaml documents (400/404/409/503/500 — no 422 for this endpoint).
func Validate(key, value string) error {
	switch key {
	case KeyMonthlyContribution, KeyIkeAnnualLimit, KeyIkzeAnnualLimit:
		parsed, err := decimal.NewFromString(value)
		if err != nil {
			return httpx.NewValidationError("", key+" must be a valid decimal number")
		}
		if parsed.Cmp(decimal.Zero) <= 0 {
			return httpx.NewValidationError("", key+" must be greater than 0")
		}
	}
	return nil
}

// NewService wires the shared kernel over store (common-go/siblingcore/refdata in production)
// with this app's key validation.
func NewService(store settingscore.Store) *settingscore.Service {
	return settingscore.NewService(store, Validate)
}
