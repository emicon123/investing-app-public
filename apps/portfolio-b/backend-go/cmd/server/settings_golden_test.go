package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"investing-app/common-go/settingscore"
	"portfolio-b/backend-go/internal/settings"
)

// Golden wire test for GET /settings and PUT /settings/{key}, driven through this app's own
// ServerInterface forwards (task 179 B9.2). Recorded against the per-app handler before it moved
// to common-go/settingscore.Handler; they did not change when it moved (byte-identical). This
// app answers request-body and value validation with 400 (the documented per-app contract
// difference: main 422, portfolio-b/portfolio-c 400). Shares runGolden/maskVolatile with
// marketdata_golden_test.go.

// goldenSettingsStore is a deterministic settingscore.Store (fixed order, no DB).
type goldenSettingsStore struct{ rows []settingscore.Row }

func newGoldenSettingsStore() *goldenSettingsStore {
	return &goldenSettingsStore{rows: []settingscore.Row{
		{Key: settings.KeyIkeAnnualLimit, Value: "23472", UpdatedAt: goldenFetched},
		{Key: settings.KeyIkzeAnnualLimit, Value: "11836.80", UpdatedAt: goldenFetched},
		{Key: settings.KeyMonthlyContribution, Value: "4000", UpdatedAt: goldenFetched},
		{Key: "unregistered.key", Value: "x", UpdatedAt: goldenFetched},
	}}
}

func (s *goldenSettingsStore) Get(_ context.Context, key string) (settingscore.Row, error) {
	for _, r := range s.rows {
		if r.Key == key {
			return r, nil
		}
	}
	return settingscore.Row{}, settingscore.ErrNotFound
}

func (s *goldenSettingsStore) List(context.Context) ([]settingscore.Row, error) {
	return append([]settingscore.Row(nil), s.rows...), nil
}

func (s *goldenSettingsStore) Update(_ context.Context, key, value string) (settingscore.Row, error) {
	return settingscore.Row{Key: key, Value: value, UpdatedAt: goldenFetched.Add(time.Minute)}, nil
}

func putSetting(body string) *http.Request {
	return httptest.NewRequest(http.MethodPut, "/api/settings/x", strings.NewReader(body))
}

func TestSettingsGolden(t *testing.T) {
	s := &apiServer{settingsAPI: settings.NewHandler(settingscore.NewService(newGoldenSettingsStore(), settings.Validate))}
	update := func(key, body string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) { s.UpdateSetting(w, putSetting(body), key) }
	}
	cases := []goldenCase{
		{"list-settings", func(w http.ResponseWriter, r *http.Request) { s.ListSettings(w, r) }},
		{"update-ok", update(settings.KeyMonthlyContribution, `{"value":"5000"}`)},
		{"update-unregistered-key-passes-through", update("unregistered.key", `{"value":"anything"}`)},
		{"update-null-value", update("unregistered.key", `{"value":null}`)},
		{"update-unknown-key", update("no_such_key", `{"value":"1"}`)},
		{"update-malformed-body", update(settings.KeyMonthlyContribution, `{`)},
		{"update-missing-value", update(settings.KeyMonthlyContribution, `{"other":"1"}`)},
		{"update-non-string-value", update(settings.KeyMonthlyContribution, `{"value":5}`)},
		{"update-invalid-value", update(settings.KeyMonthlyContribution, `{"value":"abc"}`)},
		{"update-non-positive-value", update(settings.KeyMonthlyContribution, `{"value":"0"}`)},
		{"update-too-long-value", update("unregistered.key", `{"value":"`+strings.Repeat("x", 1001)+`"}`)},
	}
	runGolden(t, "testdata/settings.golden", cases)
}
