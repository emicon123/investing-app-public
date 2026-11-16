package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
)

// Kind names the value type of a registered setting — the compile-time-checked replacement for the
// Java SettingKeys' stringly-typed sprawl (doc 13 §7 design-improvement item).
type Kind string

const (
	KindDecimal Kind = "decimal"
	KindBool    Kind = "bool"
)

// Def declares one typed setting: its key plus the value type every reader/writer must
// agree on. The four entries below are the complete live key set — V1 seeded
// emerytura.monthly_contribution_pln (renamed to retirement.* by V22), V33 seeded the
// rebalancing.macro_derisk_* pair, V37 deleted the cut-factor and derisk_sink_* keys as
// superseded by per-position interpolation (deliberately NOT ported — dead keys stay dead),
// and V9 seeded the ike/ikze annual limits.
type Def struct {
	Key  string
	Kind Kind
}

var (
	DefRetirementMonthlyContributionPln = Def{Key: "retirement.monthly_contribution_pln", Kind: KindDecimal}
	DefMacroDeriskEnabled               = Def{Key: "rebalancing.macro_derisk_enabled", Kind: KindBool}
	DefIkeAnnualLimitPln                = Def{Key: "ike_annual_limit_pln", Kind: KindDecimal}
	DefIkzeAnnualLimitPln               = Def{Key: "ikze_annual_limit_pln", Kind: KindDecimal}
)

// AllDefs lists every registered setting — the single place to consult for "which keys exist".
var AllDefs = []Def{
	DefRetirementMonthlyContributionPln,
	DefMacroDeriskEnabled,
	DefIkeAnnualLimitPln,
	DefIkzeAnnualLimitPln,
}

// RetirementMonthlyContributionFallback mirrors
// SettingService.getRetirementMonthlyContributionPln's 1000 PLN fallback (missing OR
// malformed value).
var RetirementMonthlyContributionFallback = decimal.NewFromInt(1000)

// MacroDeriskEnabledFallback is the missing-key default for macro_derisk_enabled (true). NOTE the
// asymmetry, kept exactly as the original Java code behaved (not as its javadoc claimed): a MISSING
// key falls back to true, but a PRESENT malformed value answers false — only a (trimmed,
// case-insensitive) "true" parses as true.
const MacroDeriskEnabledFallback = true

// SettingValue constrains Get's type parameter to the two value types settings carry.
type SettingValue interface {
	decimal.Decimal | bool
}

// SettingReader is the narrow slice of *sqlcgen.Queries the registry needs — the same
// GetSettingByKey func macro's package already uses (defined in db/queries/macro.sql and REUSED
// here, not duplicated).
type SettingReader interface {
	GetSettingByKey(ctx context.Context, key string) (sqlcgen.GetSettingByKeyRow, error)
}

// Get reads the typed value of def, the generic replacement for the four hand-written Java
// accessors (retirement monthly contribution, macro derisk flag, IKE and IKZE annual limits). Every
// reader of settings goes through here.
//
// Semantics, kept 1:1 from the Java accessors:
//   - decimal, non-strict (retirement): missing or malformed value → fallback (warn logged).
//     Parsing uses the raw stored string with NO trim — surrounding whitespace is malformed,
//     as it was for Java's BigDecimal(String).
//   - bool (macro_derisk_enabled): missing → fallback (true); present → trimmed,
//     case-insensitive =="true" (malformed → FALSE, never the fallback).
//     strict is meaningless for bools and ignored.
//   - strict (IKE/IKZE limits): missing OR malformed → 404 NOT_FOUND (both cases, as in the
//     original) — callers that cannot proceed without the limit (purchase planning) must fail loudly,
//     not silently plan against a default.
//
// Any other DB failure is returned as-is (500 via httpx.WriteError's generic path).
func Get[T SettingValue](ctx context.Context, q SettingReader, def Def, fallback T, strict bool) (T, error) {
	var zero T
	row, err := q.GetSettingByKey(ctx, def.Key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if strict {
				return zero, httpx.NewNotFoundError("Setting '" + def.Key + "' not found")
			}
			return fallback, nil
		}
		return zero, fmt.Errorf("settings: read %s: %w", def.Key, err)
	}
	switch any(zero).(type) {
	case bool:
		v := strings.EqualFold(strings.TrimSpace(row.Value), "true")
		out, _ := any(v).(T)
		return out, nil
	case decimal.Decimal:
		d, err := decimal.NewFromString(row.Value)
		if err != nil {
			if strict {
				return zero, httpx.NewNotFoundError("Setting " + def.Key + " has invalid value: " + row.Value)
			}
			slog.Warn("settings: invalid value, using default", "key", def.Key, "value", row.Value)
			return fallback, nil
		}
		out, _ := any(d).(T)
		return out, nil
	default:
		return zero, fmt.Errorf("settings: unsupported value type for %s", def.Key)
	}
}
