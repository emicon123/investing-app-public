package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as Phase 1/1B
// (business logic table-tested here; real Postgres covered by integration_test.go).
type fakeQueries struct {
	settings map[string]fakeSetting
	now      time.Time
}

type fakeSetting struct {
	value     string
	updatedAt time.Time
}

func newFakeQueries() *fakeQueries {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return &fakeQueries{
		settings: map[string]fakeSetting{
			DefRetirementMonthlyContributionPln.Key: {value: "1000", updatedAt: now},
			DefMacroDeriskEnabled.Key:               {value: "true", updatedAt: now},
			DefIkeAnnualLimitPln.Key:                {value: "23472.00", updatedAt: now},
			DefIkzeAnnualLimitPln.Key:               {value: "9388.00", updatedAt: now},
		},
		now: now,
	}
}

func (f *fakeQueries) GetSettingByKey(_ context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	s, ok := f.settings[key]
	if !ok {
		return sqlcgen.GetSettingByKeyRow{}, pgx.ErrNoRows
	}
	return sqlcgen.GetSettingByKeyRow{Key: key, Value: s.value}, nil
}

func (f *fakeQueries) ListSettings(_ context.Context) ([]sqlcgen.Setting, error) {
	out := []sqlcgen.Setting{}
	for k, s := range f.settings {
		out = append(out, sqlcgen.Setting{
			Key: k, Value: s.value,
			UpdatedAt: pgtype.Timestamptz{Time: s.updatedAt, Valid: true},
		})
	}
	return out, nil
}

func (f *fakeQueries) UpdateSettingValue(_ context.Context, arg sqlcgen.UpdateSettingValueParams) (sqlcgen.Setting, error) {
	s, ok := f.settings[arg.Key]
	if !ok {
		return sqlcgen.Setting{}, pgx.ErrNoRows
	}
	// The set_updated_at() trigger moves updated_at on every UPDATE — the fake advances the
	// clock the same way so tests can assert the trigger contract (Go never sends updated_at).
	f.now = f.now.Add(time.Second)
	s.value = arg.Value
	s.updatedAt = f.now
	f.settings[arg.Key] = s
	return sqlcgen.Setting{
		Key: arg.Key, Value: arg.Value,
		UpdatedAt: pgtype.Timestamptz{Time: s.updatedAt, Valid: true},
	}, nil
}

func testService() *Service { return NewService(newFakeQueries()) }

func TestService_List_ReturnsAllSeededKeys(t *testing.T) {
	dtos, err := testService().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dtos) != 4 {
		t.Fatalf("expected 4 settings, got %d", len(dtos))
	}
	seen := map[string]bool{}
	for _, d := range dtos {
		seen[d.Key] = true
		if d.UpdatedAt.IsZero() {
			t.Fatalf("expected non-zero updatedAt for %s", d.Key)
		}
	}
	for _, d := range AllDefs {
		if !seen[d.Key] {
			t.Fatalf("expected registered key %s in list", d.Key)
		}
	}
}

func TestService_Update_ValidValuesPersist(t *testing.T) {
	svc := testService()
	ctx := context.Background()

	dto, err := svc.Update(ctx, DefRetirementMonthlyContributionPln.Key, "2500")
	if err != nil {
		t.Fatalf("retirement update: %v", err)
	}
	if dto.Value != "2500" {
		t.Fatalf("expected stored value 2500, got %q", dto.Value)
	}

	dto, err = svc.Update(ctx, DefMacroDeriskEnabled.Key, "FALSE")
	if err != nil {
		t.Fatalf("derisk update: %v", err)
	}
	if dto.Value != "FALSE" {
		t.Fatalf("expected stored value FALSE (raw string kept), got %q", dto.Value)
	}

	// Unknown-but-existing key passes through unvalidated (Java default branch).
	fq := newFakeQueries()
	fq.settings["custom.freeform_key"] = fakeSetting{value: "old", updatedAt: fq.now}
	svc2 := NewService(fq)
	dto, err = svc2.Update(ctx, "custom.freeform_key", "anything goes 123 !@#")
	if err != nil {
		t.Fatalf("freeform update: %v", err)
	}
	if dto.Value != "anything goes 123 !@#" {
		t.Fatalf("expected freeform value kept, got %q", dto.Value)
	}
}

func TestService_Update_UnknownKeyIs404(t *testing.T) {
	_, err := testService().Update(context.Background(), "no.such.key", "x")
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != 404 || appErr.Code != httpx.CodeNotFound {
		t.Fatalf("expected 404 NOT_FOUND, got %v", err)
	}
}

func TestService_Update_ValidationIs422(t *testing.T) {
	svc := testService()
	ctx := context.Background()
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"retirement not a number", DefRetirementMonthlyContributionPln.Key, "abc"},
		{"retirement zero", DefRetirementMonthlyContributionPln.Key, "0"},
		{"retirement negative", DefRetirementMonthlyContributionPln.Key, "-100"},
		{"derisk not a bool", DefMacroDeriskEnabled.Key, "yes"},
		{"derisk empty", DefMacroDeriskEnabled.Key, ""},
		{"value over 1000 chars", DefMacroDeriskEnabled.Key, strings.Repeat("x", 1001)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Update(ctx, c.key, c.value)
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) || appErr.Status != 422 || appErr.Code != httpx.CodeValidationError {
				t.Fatalf("expected 422 VALIDATION_ERROR, got %v", err)
			}
		})
	}
}
