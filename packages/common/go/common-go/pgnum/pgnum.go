// Package pgnum converts between pgx's pgtype values and the domain values:
//
//   - pgtype.Numeric (what sqlc generates for every NUMERIC column) ⇄ shopspring/decimal.Decimal,
//     this migration's one money type — see the Backend-Go agent's domain rules: "always
//     decimal.Decimal, never float32/float64. Matches NUMERIC(18,4) columns exactly — pgx +
//     shopspring/decimal round-trip these natively via pgtype.Numeric conversion". sqlc has no
//     built-in decimal.Decimal override wired up in this migration (no new dependency beyond what
//     ADR-094 already lists), so every domain package that reads/writes a NUMERIC column converts
//     through this one shared helper instead of each re-deriving the Int/Exp <-> coefficient/
//     exponent mapping by hand.
//   - time.Time → pgtype.Date (Date), for DATE query parameters.
package pgnum

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
)

// ToDecimal converts a pgtype.Numeric (as scanned from a NOT NULL NUMERIC column) to a
// decimal.Decimal. Returns an error if the value is SQL NULL or NaN — callers reading a NOT NULL
// column should treat either as a data-integrity fault, not a normal empty value.
func ToDecimal(n pgtype.Numeric) (decimal.Decimal, error) {
	if !n.Valid {
		return decimal.Decimal{}, fmt.Errorf("pgnum: numeric value is NULL")
	}
	if n.NaN {
		return decimal.Decimal{}, fmt.Errorf("pgnum: numeric value is NaN")
	}
	return decimal.NewFromBigInt(n.Int, n.Exp), nil
}

// ToDecimalOrZero is ToDecimal but returns decimal.Zero instead of an error for NULL/NaN —
// convenient for nullable NUMERIC columns where "no value" and "zero" are meant to collapse.
func ToDecimalOrZero(n pgtype.Numeric) decimal.Decimal {
	d, err := ToDecimal(n)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// FromDecimal converts a decimal.Decimal to a pgtype.Numeric ready to bind as a query parameter.
func FromDecimal(d decimal.Decimal) pgtype.Numeric {
	return pgtype.Numeric{Int: d.Coefficient(), Exp: d.Exponent(), Valid: true}
}

// ToDecimalPtr is ToDecimal but NULL maps to a nil pointer instead of an error — for nullable
// NUMERIC columns where the caller must preserve "no value" (e.g. positions. total_spent_pln: a
// sell on a NULL cost basis must write back NULL, not 0 — see
// backend-go/internal/position/delta.go). NaN is still an error.
func ToDecimalPtr(n pgtype.Numeric) (*decimal.Decimal, error) {
	if !n.Valid {
		return nil, nil
	}
	d, err := ToDecimal(n)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// FromDecimalPtr converts a *decimal.Decimal to a pgtype.Numeric — nil maps to SQL NULL
// for the nullable NUMERIC columns the bot module uses pervasively (surprise bounds,
// confidence, exit/pnl, tick optionals). A non-nil pointer maps exactly like FromDecimal.
func FromDecimalPtr(d *decimal.Decimal) pgtype.Numeric {
	if d == nil {
		return pgtype.Numeric{Valid: false}
	}
	return FromDecimal(*d)
}

// Date binds the calendar date of t, in t's own location, as a DATE parameter: midnight UTC of
// that date, Valid. A Europe/Warsaw instant at 00:30 therefore stays on its Warsaw date even
// though the same instant is the previous day in UTC.
func Date(t time.Time) pgtype.Date {
	y, m, d := t.Date()
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}
