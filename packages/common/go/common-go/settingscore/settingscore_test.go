package settingscore

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"investing-app/common-go/httpx"
)

// fakeStore is an in-memory stand-in for the per-app *sqlcgen.Queries adapter — business logic
// (existence check, length check, Validator dispatch, persistence) is table-tested here; the
// real Postgres round-trip is covered by each app's own internal/settings/integration_test.go
// per the "never mock the database" gate.
type fakeStore struct {
	rows map[string]Row
	now  time.Time
}

func newFakeStore(seed map[string]string) *fakeStore {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	rows := make(map[string]Row, len(seed))
	for k, v := range seed {
		rows[k] = Row{Key: k, Value: v, UpdatedAt: now}
	}
	return &fakeStore{rows: rows, now: now}
}

func (f *fakeStore) Get(_ context.Context, key string) (Row, error) {
	r, ok := f.rows[key]
	if !ok {
		return Row{}, ErrNotFound
	}
	return r, nil
}

func (f *fakeStore) List(_ context.Context) ([]Row, error) {
	out := make([]Row, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) Update(_ context.Context, key, value string) (Row, error) {
	r, ok := f.rows[key]
	if !ok {
		return Row{}, ErrNotFound
	}
	f.now = f.now.Add(time.Second)
	r.Value = value
	r.UpdatedAt = f.now
	f.rows[key] = r
	return r, nil
}

// portfolioBLikeValidator mirrors portfolioB's SettingService.validateSettingValue: 3 numeric keys must
// parse as a positive decimal; every other key (including ones this app has not registered)
// passes through unvalidated.
func portfolioBLikeValidator(key, value string) error {
	switch key {
	case "monthly_contribution_pln", "ike_annual_limit_pln", "ikze_annual_limit_pln":
		if value == "" || !isPositiveDecimal(value) {
			return httpx.NewValidationError("", key+" must be a valid decimal number greater than 0")
		}
	}
	return nil
}

func isPositiveDecimal(s string) bool {
	// Minimal test-only decimal check — real per-app Validators use shopspring/decimal.
	if strings.TrimSpace(s) != s || s == "" {
		return false
	}
	return s != "0" && !strings.HasPrefix(s, "-") && strings.Trim(s, "0123456789.") == ""
}

func TestService_List_ReturnsEveryRow(t *testing.T) {
	svc := NewService(newFakeStore(map[string]string{
		"monthly_contribution_pln": "4000",
		"ike_annual_limit_pln":     "23472",
	}), portfolioBLikeValidator)

	dtos, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dtos) != 2 {
		t.Fatalf("expected 2 settings, got %d", len(dtos))
	}
}

func TestService_Update_ValidValuePersists(t *testing.T) {
	svc := NewService(newFakeStore(map[string]string{"monthly_contribution_pln": "4000"}), portfolioBLikeValidator)

	dto, err := svc.Update(context.Background(), "monthly_contribution_pln", "5000")
	if err != nil {
		t.Fatal(err)
	}
	if dto.Value != "5000" {
		t.Fatalf("expected 5000, got %s", dto.Value)
	}
}

func TestService_Update_UnknownKeyIs404(t *testing.T) {
	svc := NewService(newFakeStore(nil), portfolioBLikeValidator)

	_, err := svc.Update(context.Background(), "no.such.key", "x")
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusNotFound || appErr.Code != httpx.CodeNotFound {
		t.Fatalf("expected 404 NOT_FOUND, got %v", err)
	}
}

func TestService_Update_TooLongValueIs400(t *testing.T) {
	svc := NewService(newFakeStore(map[string]string{"monthly_contribution_pln": "4000"}), portfolioBLikeValidator)

	_, err := svc.Update(context.Background(), "monthly_contribution_pln", strings.Repeat("x", 1001))
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusBadRequest || appErr.Code != httpx.CodeValidationError {
		t.Fatalf("expected 400 VALIDATION_ERROR, got %v", err)
	}
}

func TestService_Update_ValidatorRejectionIs400(t *testing.T) {
	svc := NewService(newFakeStore(map[string]string{"monthly_contribution_pln": "4000"}), portfolioBLikeValidator)

	cases := []string{"abc", "-100", "0", ""}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			_, err := svc.Update(context.Background(), "monthly_contribution_pln", v)
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) || appErr.Status != http.StatusBadRequest || appErr.Code != httpx.CodeValidationError {
				t.Fatalf("expected 400 VALIDATION_ERROR for %q, got %v", v, err)
			}
		})
	}
}

func TestService_Update_UnregisteredButExistingKeyPassesThroughUnvalidated(t *testing.T) {
	// Java's own default switch branch: a key that exists in the table but carries no
	// registered validation rule accepts any value within the length limit.
	svc := NewService(newFakeStore(map[string]string{"custom.freeform_key": "old"}), portfolioBLikeValidator)

	dto, err := svc.Update(context.Background(), "custom.freeform_key", "anything goes 123 !@#")
	if err != nil {
		t.Fatalf("expected freeform key to pass through, got %v", err)
	}
	if dto.Value != "anything goes 123 !@#" {
		t.Fatalf("expected freeform value kept, got %q", dto.Value)
	}
}

func TestService_Update_NilValidatorAcceptsAnything(t *testing.T) {
	// Defensive: a Service with no Validator (not expected in production — both apps always
	// pass a real one) must not panic and must behave like Java's default branch everywhere.
	svc := NewService(newFakeStore(map[string]string{"any.key": "old"}), nil)

	dto, err := svc.Update(context.Background(), "any.key", "new value")
	if err != nil {
		t.Fatal(err)
	}
	if dto.Value != "new value" {
		t.Fatalf("expected new value, got %q", dto.Value)
	}
}
