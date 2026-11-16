package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/settingscore"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an interface so
// service-layer unit tests can fake the DB without a real Postgres connection (integration_test.go
// still exercises the real *sqlcgen.Queries against testcontainers-go, per the "never mock the
// database" gate). GetSettingByKey is embedded from SettingReader — the same func macro's package
// uses, defined once in db/queries/macro.sql.
type Queries interface {
	SettingReader
	ListSettings(ctx context.Context) ([]sqlcgen.Setting, error)
	UpdateSettingValue(ctx context.Context, arg sqlcgen.UpdateSettingValueParams) (sqlcgen.Setting, error)
}

// SettingDTO is the service-layer shape of one settings row — free of sqlc row types and
// openapi response types (doc 13's layering rule). It is the shared settingscore.SettingDTO
// (identical fields), so Service satisfies settingscore.Settings and is served by the shared
// settingscore.Handler; main keeps its own Service (422 validation, registry.go keys).
type SettingDTO = settingscore.SettingDTO

var _ settingscore.Settings = (*Service)(nil)

// maxValueLen mirrors UpdateSettingRequest's @Size(max = 1000).
const maxValueLen = 1000

// Service is the settings service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries Queries
}

// NewService wires queries (explicit constructor wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries) *Service {
	return &Service{queries: queries}
}

// List returns every setting ordered by key — the direct port of SettingService.listSettings.
func (s *Service) List(ctx context.Context) ([]SettingDTO, error) {
	rows, err := s.queries.ListSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("settings: list: %w", err)
	}
	out := make([]SettingDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, SettingDTO{Key: r.Key, Value: r.Value, UpdatedAt: r.UpdatedAt.Time})
	}
	return out, nil
}

// Update validates then stores one setting's new value. Existence is checked FIRST (unknown key →
// 404 NOT_FOUND), then the value is validated (422 VALIDATION_ERROR, the status docs/openapi.yaml
// documents for this endpoint, same as macro's m2-manual 422). updated_at is written by the
// set_updated_at() trigger — Go never sends it, only reads back what the trigger wrote via
// RETURNING.
func (s *Service) Update(ctx context.Context, key, value string) (SettingDTO, error) {
	if _, err := s.queries.GetSettingByKey(ctx, key); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SettingDTO{}, httpx.NewNotFoundError("Setting '" + key + "' not found")
		}
		return SettingDTO{}, fmt.Errorf("settings: read %s: %w", key, err)
	}
	if err := validateValue(key, value); err != nil {
		return SettingDTO{}, err
	}
	row, err := s.queries.UpdateSettingValue(ctx, sqlcgen.UpdateSettingValueParams{Key: key, Value: value})
	if err != nil {
		return SettingDTO{}, fmt.Errorf("settings: update %s: %w", key, err)
	}
	return SettingDTO{Key: row.Key, Value: row.Value, UpdatedAt: row.UpdatedAt.Time}, nil
}

// validateValue validates a value per its registered Def (ported 1:1 from the Java
// validateSettingValue) — including the default branch: keys that exist in the table but carry no
// registered Def (e.g. a key added by Flyway before its Def lands here) accept any value within the
// length limit. Error texts are the legacy user-facing ones; the status is 422.
func validateValue(key, value string) *httpx.AppError {
	if len([]rune(value)) > maxValueLen {
		return unprocessable("value", "size must be between 0 and 1000")
	}
	switch key {
	case DefRetirementMonthlyContributionPln.Key:
		parsed, err := decimal.NewFromString(value)
		if err != nil {
			return unprocessable("value", DefRetirementMonthlyContributionPln.Key+" must be a valid decimal number")
		}
		if parsed.Cmp(decimal.Zero) <= 0 {
			return unprocessable("value", DefRetirementMonthlyContributionPln.Key+" must be greater than 0")
		}
	case DefMacroDeriskEnabled.Key:
		v := strings.TrimSpace(value)
		if !strings.EqualFold(v, "true") && !strings.EqualFold(v, "false") {
			return unprocessable("value", DefMacroDeriskEnabled.Key+" must be 'true' or 'false'")
		}
	}
	return nil
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi.yaml documents for this endpoint's validation failures (same helper shape as
// macro's).
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}
