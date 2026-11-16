package settings

import "investing-app/common-go/settingscore"

// NewHandler serves GET /settings and PUT /settings/{key} over service through the shared
// settingscore.Handler (task 179), answering request-body validation with this app's 422
// (docs/openapi.yaml; portfolioB/portfolioC answer 400 — the documented per-app contract difference).
func NewHandler(service *Service) *settingscore.Handler {
	return settingscore.NewHandler(service, unprocessable)
}
