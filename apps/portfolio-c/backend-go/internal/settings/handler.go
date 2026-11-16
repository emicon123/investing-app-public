package settings

import (
	"investing-app/common-go/httpx"
	"investing-app/common-go/settingscore"
)

// NewHandler serves GET /settings and PUT /settings/{key} over the shared settingscore.Service
// through the shared settingscore.Handler (task 179), answering request-body validation with
// this app's 400 (its checked-in docs/openapi.yaml; main answers 422 — the documented per-app
// contract difference).
func NewHandler(service *settingscore.Service) *settingscore.Handler {
	return settingscore.NewHandler(service, httpx.NewValidationError)
}
