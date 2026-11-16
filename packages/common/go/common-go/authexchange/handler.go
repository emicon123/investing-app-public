// Package authexchange wires common-go/auth.Service.Exchange to POST /auth/token (operationId
// exchangeToken; ported from the Java AuthController, ADR-094 / doc 13 §6.4). Every login on every
// app depends on this endpoint.
package authexchange

import (
	"context"
	"encoding/json"
	"net/http"

	"investing-app/common-go/auth"
	"investing-app/common-go/httpx"
)

// Exchanger is the one method this handler needs from *auth.Service — a small
// consumer-defined seam so handler-level tests can substitute a fake without a real Google ID
// token (task 152 §1.2 finding 4: hitting live Google JWKS in tests is impractical/YAGNI for a
// single call site).
type Exchanger interface {
	Exchange(ctx context.Context, googleIDToken string) (auth.TokenExchangeResponse, error)
}

// Handler adapts an Exchanger to the oapi-codegen ServerInterface's ExchangeToken method — the same
// thin-Facade convention as every other domain's Handler (no business logic here; service ->
// wire-response mapping only).
type Handler struct {
	svc Exchanger
}

// NewHandler constructs a Handler around svc (normally auth.NewService(authCfg)).
func NewHandler(svc Exchanger) *Handler { return &Handler{svc: svc} }

// tokenRequest mirrors openapi.TokenExchangeRequest's wire shape ({"googleIdToken": "..."}).
// Decoded directly rather than through the generated per-app type, because ExchangeToken's
// generated ServerInterface signature (openapi/server.gen.go) takes no typed body parameter —
// unlike operations with query/path params, oapi-codegen leaves body decoding to the handler
// for this operation.
type tokenRequest struct {
	GoogleIdToken string `json:"googleIdToken"`
}

// ExchangeToken implements POST /auth/token — the direct port of
// AuthController.exchangeToken(TokenExchangeRequest). PUBLIC endpoint (security: [] in
// docs/openapi/paths/auth.yaml) — see the auth-middleware bypass for the other half of
// that contract (common-go/auth/middleware.go's isPublicAuthTokenRoute exception; root
// additionally wires it through smartAuth in backend-go/cmd/server/botauth.go); without it
// this handler is unreachable by the unauthenticated caller it exists to serve (task 152
// FIX 1, a real lockout bug, not hypothetical).
func (h *Handler) ExchangeToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, unprocessable("", "Invalid request body"))
		return
	}

	resp, err := h.svc.Exchange(r.Context(), req.GoogleIdToken)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	// Raw {token, expiresAt} — no data/meta envelope (docs/openapi/paths/auth.yaml's 200 example is
	// unwrapped); auth.TokenExchangeResponse's own json tags already match that wire shape exactly,
	// so it is written as-is rather than remapped through this app's own openapi-generated
	// TokenExchangeResponse type (whose field shapes differ across the 3 apps' generated code —
	// main: non-pointer Token/time.Time ExpiresAt; portfolioB/portfolioC: pointer *string/*string).
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi/paths/auth.yaml documents for this endpoint's request-validation failures (same
// helper shape as settings'/macro's own 422 helpers).
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: http.StatusUnprocessableEntity, Code: httpx.CodeValidationError, Field: field, Message: message}
}
