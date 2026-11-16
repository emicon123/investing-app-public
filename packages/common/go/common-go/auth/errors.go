package auth

import (
	"net/http"

	"investing-app/common-go/httpx"
)

// These are the three 401 failure branches of Service.Exchange, as *httpx.AppError so a handler can
// pass them straight to httpx.WriteError and get the {401, AUTH_ERROR} envelope (the code is
// AUTH_ERROR for every 401 regardless of cause — the legacy wire contract, ADR-094).
var (
	errEmailClaimMissing = httpx.NewAuthError(http.StatusUnauthorized, "Google ID token does not contain an email claim")
	errEmailNotVerified  = httpx.NewAuthError(http.StatusUnauthorized, "Google account email is not verified")
	errEmailNotAllowed   = httpx.NewAuthError(http.StatusUnauthorized, "Email is not in the allowlist")
)
