package settingscore

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"investing-app/common-go/httpx"
)

// Settings is what Handler needs. *Service implements it; so does main's own settings.Service
// (main keeps its service: its own typed-key validator and registry, and 422 instead of 400 for
// every validation failure, the 1000-rune cap included).
type Settings interface {
	List(ctx context.Context) ([]SettingDTO, error)
	Update(ctx context.Context, key, value string) (SettingDTO, error)
}

// Handler serves GET /settings and PUT /settings/{key} for every app — the direct port of
// SettingsController (task 179; it replaces three per-app copies). invalid builds the
// request-body errors ("Invalid request body", "value must not be null"): main passes a 422
// constructor, portfolioB/portfolioC httpx.NewValidationError (400) — the documented per-app contract
// difference. Errors from svc (404 unknown key, validation) are written as they come.
type Handler struct {
	svc     Settings
	invalid func(field, message string) *httpx.AppError
}

// NewHandler wires svc and the app's request-body error constructor.
func NewHandler(svc Settings, invalid func(field, message string) *httpx.AppError) *Handler {
	return &Handler{svc: svc, invalid: invalid}
}

// settingJSON is the wire shape of one setting (the generated Setting/SettingDto JSON names, in
// their alphabetical field order). Values are always strings.
type settingJSON struct {
	Key       string    `json:"key"`
	UpdatedAt time.Time `json:"updatedAt"`
	Value     string    `json:"value"`
}

func toSettingJSON(d SettingDTO) settingJSON {
	// Timestamps render as Z (UTC); same instant, byte-identical text.
	return settingJSON{Key: d.Key, UpdatedAt: d.UpdatedAt.UTC(), Value: d.Value}
}

// ListSettings implements GET /settings — every setting as a flat list.
func (h *Handler) ListSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.svc.List(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]settingJSON, 0, len(settings))
	for _, s := range settings {
		out = append(out, toSettingJSON(s))
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List(out))
}

// UpdateSetting implements PUT /settings/{key}. The request's value is a plain string, so an absent
// field would decode as "" — presence is checked on the raw map first (an absent value is a 400
// before the service runs, the original @NotNull semantics); an explicit null decodes as "" and
// goes on to the service, as it always did.
func (h *Handler) UpdateSetting(w http.ResponseWriter, r *http.Request, key string) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		httpx.WriteError(w, h.invalid("", "Invalid request body"))
		return
	}
	rawValue, ok := raw["value"]
	if !ok {
		httpx.WriteError(w, h.invalid("value", "must not be null"))
		return
	}
	var value string
	if err := json.Unmarshal(rawValue, &value); err != nil {
		httpx.WriteError(w, h.invalid("", "Invalid request body"))
		return
	}
	dto, err := h.svc.Update(r.Context(), key, value)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toSettingJSON(dto)))
}
