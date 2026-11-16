// Package decx — decimal values at the HTTP edge: float64 DTO mapping (display only, never
// persisted or re-used in math) and bean-style range validation. The OpenAPI-generated DTOs
// carry JSON numbers as float64; every money value inside the services stays decimal.Decimal
// and is converted here, on its way out, exactly once.
package decx

import (
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
)

var hundred = decimal.NewFromInt(100)

// ToFloat converts d to the float64 an OpenAPI DTO field carries. The exactness flag is
// dropped on purpose: the value is for display on the wire only.
func ToFloat(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}

// ToFloatPtr is ToFloat for an optional value: nil stays nil (the field is omitted).
func ToFloatPtr(d *decimal.Decimal) *float64 {
	if d == nil {
		return nil
	}
	v := ToFloat(*d)
	return &v
}

// ValidatePercentRange validates a 0..100 percentage at the request-decoding layer (there is no
// bean-validation equivalent in Go): v outside [0, 100] is a 400 VALIDATION_ERROR on field. nil
// (field omitted / explicit null) is always allowed.
func ValidatePercentRange(field string, v *decimal.Decimal) error {
	if v == nil {
		return nil
	}
	if v.Sign() < 0 || v.Cmp(hundred) > 0 {
		return httpx.NewValidationError(field, "must be between 0 and 100")
	}
	return nil
}
