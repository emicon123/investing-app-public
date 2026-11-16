// Package settingscore is the shared settings kernel for portfolioB and portfolioC (ADR-094 / doc 13 §7).
// The list/update logic is the same shape in both apps — but the per-key business-rule validation
// (the Java SettingService.validateSettingValue switch) genuinely differs: portfolioB validates 3
// numeric keys (monthly_contribution_pln, ike_annual_limit_pln, ikze_annual_limit_pln), portfolioC
// validates 1 (fixed_monthly_deposit_default_pln). Per doc 13 §5's guidance ("one small interface
// per genuinely-varying step, or a plain higher-order function taking the one varying step as a
// parameter" — never a base-class hierarchy), that one varying step is threaded through as a
// Validator function parameter, the same shape rebalancecalc.AllocateProportional uses for its own
// one varying step (allocateToAccounts). Everything else — existence check (404), max-length check
// (400), persistence — is identical and lives here.
//
// A second per-Go-module seam is unavoidable and NOT a business-rule difference: portfolioB and portfolioC
// are separate Go modules with their own sqlc-generated types, so this package cannot reference
// either app's *sqlcgen.Queries directly. The Store interface below uses only plain Go types
// (string, time.Time) so each app's own internal/settings package can satisfy it with a thin
// adapter over its own *sqlcgen.Queries — the same shape as accountcash.Store.
package settingscore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"investing-app/common-go/httpx"
)

// ErrNotFound is Store's sentinel for "no settings row with this key", mapped to 404 by Service.
var ErrNotFound = errors.New("settingscore: setting not found")

// maxValueLen mirrors UpdateSettingRequest's shared @Size(max = 1000) constraint.
const maxValueLen = 1000

// Row is one settings row in plain Go types — free of sqlc row types (doc 13's layering rule).
type Row struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

// SettingDTO is the service-layer DTO returned to callers — identical shape to Row today, kept
// as its own type so a future divergence (e.g. a computed field) doesn't require reshaping Row.
type SettingDTO struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

// Store is the one per-app seam: read/list/write of the settings table. A missing key on Get maps
// to ErrNotFound so Service can answer 404.
type Store interface {
	Get(ctx context.Context, key string) (Row, error)
	List(ctx context.Context) ([]Row, error)
	Update(ctx context.Context, key, value string) (Row, error)
}

// Validator performs the one per-app-varying step — validation over that app's own registered keys.
// It returns a non-nil error (normally an *httpx.AppError built via httpx.NewValidationError: 400
// VALIDATION_ERROR, as both apps' docs/openapi.yaml document for this endpoint) when value fails
// that app's business rule for key. A key the app has not registered must return nil — an
// unknown-but-existing key passes through unvalidated.
type Validator func(key, value string) error

// Service is the settings service layer shared by portfolioB and portfolioC: thin per-app wiring hands it a
// Store over its own *sqlcgen.Queries plus that app's own Validator, and gets the shared behavior
// back.
type Service struct {
	store    Store
	validate Validator
}

// NewService wires store + validate (explicit constructor wiring, ADR-094 §2 — no DI framework).
// validate may be nil for a hypothetical app with no registered keys at all (accepted, not
// expected — both portfolioB and portfolioC always pass a real Validator).
func NewService(store Store, validate Validator) *Service {
	return &Service{store: store, validate: validate}
}

// List returns every setting, the direct port of SettingService.listSettings.
func (s *Service) List(ctx context.Context) ([]SettingDTO, error) {
	rows, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("settingscore: list: %w", err)
	}
	out := make([]SettingDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, SettingDTO(r))
	}
	return out, nil
}

// Update validates then stores one setting's new value. Existence is checked FIRST (unknown key ->
// 404 NOT_FOUND, the same ordering the main app's settings service uses), then the value length,
// then the per-app Validator — both mapped to 400 VALIDATION_ERROR via httpx.NewValidationError, as
// both apps' docs/openapi.yaml document for this endpoint (400/404/409/503/500 — NOT 422; the main
// app's own openapi.yaml specifies 422 for its own settings controller, a genuinely different
// documented contract). updated_at is written by the set_updated_at() trigger — Go never sends it,
// only reads back what the trigger wrote via Store.Update's own RETURNING.
func (s *Service) Update(ctx context.Context, key, value string) (SettingDTO, error) {
	if _, err := s.store.Get(ctx, key); err != nil {
		if errors.Is(err, ErrNotFound) {
			return SettingDTO{}, httpx.NewNotFoundError("Setting '" + key + "' not found")
		}
		return SettingDTO{}, fmt.Errorf("settingscore: read %s: %w", key, err)
	}
	if len([]rune(value)) > maxValueLen {
		return SettingDTO{}, httpx.NewValidationError("value", "size must be between 0 and 1000")
	}
	if s.validate != nil {
		if err := s.validate(key, value); err != nil {
			return SettingDTO{}, err
		}
	}
	row, err := s.store.Update(ctx, key, value)
	if err != nil {
		return SettingDTO{}, fmt.Errorf("settingscore: update %s: %w", key, err)
	}
	return SettingDTO(row), nil
}
