package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/settings"
)

// Golden wire test for GET /settings and PUT /settings/{key}, driven through this app's own
// ServerInterface forwards (task 179 B9.2). Recorded against the per-app handler before it moved
// to common-go/settingscore.Handler; they did not change when it moved (byte-identical). This
// app answers request-body and value validation with 422 (the documented per-app contract
// difference: main 422, portfolioB/portfolioC 400). Shares runGolden/maskVolatile with
// marketdata_golden_test.go.

// goldenSettingsQueries is a deterministic settings.Queries (fixed order, no DB).
type goldenSettingsQueries struct{ rows []sqlcgen.Setting }

func newGoldenSettingsQueries() *goldenSettingsQueries {
	at := pgtype.Timestamptz{Time: goldenFetched, Valid: true}
	return &goldenSettingsQueries{rows: []sqlcgen.Setting{
		{Key: "ike_annual_limit_pln", Value: "26019", UpdatedAt: at},
		{Key: "rebalancing.macro_derisk_enabled", Value: "true", UpdatedAt: at},
		{Key: "retirement.monthly_contribution_pln", Value: "4000", UpdatedAt: at},
		{Key: "unregistered.key", Value: "x", UpdatedAt: at},
	}}
}

func (q *goldenSettingsQueries) GetSettingByKey(_ context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	for _, r := range q.rows {
		if r.Key == key {
			return sqlcgen.GetSettingByKeyRow{Key: r.Key, Value: r.Value}, nil
		}
	}
	return sqlcgen.GetSettingByKeyRow{}, pgx.ErrNoRows
}

func (q *goldenSettingsQueries) ListSettings(context.Context) ([]sqlcgen.Setting, error) {
	return append([]sqlcgen.Setting(nil), q.rows...), nil
}

func (q *goldenSettingsQueries) UpdateSettingValue(_ context.Context, arg sqlcgen.UpdateSettingValueParams) (sqlcgen.Setting, error) {
	return sqlcgen.Setting{Key: arg.Key, Value: arg.Value, UpdatedAt: pgtype.Timestamptz{Time: goldenFetched.Add(time.Minute), Valid: true}}, nil
}

func putSetting(body string) *http.Request {
	return httptest.NewRequest(http.MethodPut, "/api/settings/x", strings.NewReader(body))
}

func TestSettingsGolden(t *testing.T) {
	s := &apiServer{settingsAPI: settings.NewHandler(settings.NewService(newGoldenSettingsQueries()))}
	update := func(key, body string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) { s.UpdateSetting(w, putSetting(body), key) }
	}
	cases := []goldenCase{
		{"list-settings", func(w http.ResponseWriter, r *http.Request) { s.ListSettings(w, r) }},
		{"update-ok", update("retirement.monthly_contribution_pln", `{"value":"5000"}`)},
		{"update-unregistered-key-passes-through", update("unregistered.key", `{"value":"anything"}`)},
		{"update-null-value", update("unregistered.key", `{"value":null}`)},
		{"update-unknown-key", update("no_such_key", `{"value":"1"}`)},
		{"update-malformed-body", update("retirement.monthly_contribution_pln", `{`)},
		{"update-missing-value", update("retirement.monthly_contribution_pln", `{"other":"1"}`)},
		{"update-non-string-value", update("retirement.monthly_contribution_pln", `{"value":5}`)},
		{"update-invalid-value", update("retirement.monthly_contribution_pln", `{"value":"abc"}`)},
		{"update-non-positive-value", update("retirement.monthly_contribution_pln", `{"value":"0"}`)},
		{"update-too-long-value", update("unregistered.key", `{"value":"`+strings.Repeat("x", 1001)+`"}`)},
	}
	runGolden(t, "testdata/settings.golden", cases)
}
