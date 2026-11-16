package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// ErrorDetail is one entry of the error envelope. Field is omitted from the JSON payload when
// empty.
type ErrorDetail struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// ErrorResponse is the single error envelope shape every domain package answers with (the OpenAPI
// ErrorResponse schema).
type ErrorResponse struct {
	Errors []ErrorDetail `json:"errors"`
	Meta   Meta          `json:"meta"`
}

// AppError is the one error type every domain service/handler should return for an expected,
// user-facing failure. It carries the HTTP status the error maps to — see the Code constants below
// for the wire codes (the legacy Java GlobalExceptionHandler mapping).
type AppError struct {
	Status  int
	Code    string
	Field   string
	Message string
}

func (e *AppError) Error() string { return e.Message }

// Error codes, kept 1:1 from the legacy Java GlobalExceptionHandler so `errors[].code` values did
// not change for the frontend.
const (
	CodeValidationError         = "VALIDATION_ERROR"
	CodeNotFound                = "NOT_FOUND"
	CodeDuplicateEntry          = "DUPLICATE_ENTRY"
	CodeServiceUnavailable      = "SERVICE_UNAVAILABLE"
	CodePlannedSessionExists    = "PLANNED_SESSION_EXISTS"
	CodeSessionAlreadyDone      = "SESSION_ALREADY_DONE"
	CodeRebalancingInvariantErr = "REBALANCING_INVARIANT_ERROR"
	CodeAuthError               = "AUTH_ERROR"
	CodeUnauthorized            = "UNAUTHORIZED"
	CodeInternalError           = "INTERNAL_ERROR"
	// CodeConflict is a task-156 addition (no legacy predecessor): 409 for asset-model state
	// conflicts that are not duplicate rows — deleting a primary subasset that still has siblings,
	// or attaching a second primary to an asset.
	CodeConflict = "CONFLICT"
	// CodeIdempotencyInProgress / CodeIdempotencyKeyReused are the ADR-111 Idempotency-Key
	// answers (task 177): 409 while a request with the same key is running or ended with a 5xx
	// (outcome unknown), 422 when the key was already used for a different method, path or body.
	CodeIdempotencyInProgress = "IDEMPOTENCY_IN_PROGRESS"
	CodeIdempotencyKeyReused  = "IDEMPOTENCY_KEY_REUSED"
)

// NewValidationError mirrors handleMethodArgumentNotValid / handleIllegalArgument (400).
func NewValidationError(field, message string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Code: CodeValidationError, Field: field, Message: message}
}

// NewNotFoundError mirrors handleNoSuchElement (404).
func NewNotFoundError(message string) *AppError {
	return &AppError{Status: http.StatusNotFound, Code: CodeNotFound, Message: message}
}

// NewConflictError mirrors handleDataIntegrityViolation (409).
func NewConflictError(code, message string) *AppError {
	return &AppError{Status: http.StatusConflict, Code: code, Message: message}
}

// NewServiceUnavailableError mirrors handlePriceUnavailable (503).
func NewServiceUnavailableError(message string) *AppError {
	return &AppError{Status: http.StatusServiceUnavailable, Code: CodeServiceUnavailable, Message: message}
}

// NewUnauthorizedError builds a generic 401, using the UNAUTHORIZED code named in
// docs/openapi/components/responses.yaml's Unauthorized example. It is the answer of the
// RequireAuth and service-token middleware for a missing or invalid credential.
func NewUnauthorizedError(message string) *AppError {
	return &AppError{Status: http.StatusUnauthorized, Code: CodeUnauthorized, Message: message}
}

// NewAuthError is the answer of POST /auth/token (a public endpoint, not behind the JWT middleware)
// to a failed token exchange. The legacy wire contract uses the code "AUTH_ERROR" for every such
// failure regardless of its status, so this constructor does too.
func NewAuthError(status int, message string) *AppError {
	return &AppError{Status: status, Code: CodeAuthError, Message: message}
}

// WriteJSON writes v as a JSON body with the given status code. An httpx envelope (Single, List,
// Paged, ErrorResponse) gets the X-Request-Id response header's id as its meta.requestId, so the
// id in the body is the id in the logs (F4.2); any other value is written unchanged.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	if env, ok := v.(withRequestID); ok {
		if id, ok := responseRequestID(w); ok {
			v = env.withRequestID(id)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError maps err to the shared ErrorResponse envelope and writes it, mirroring
// GlobalExceptionHandler's exception -> {status, ErrorResponse} mapping. An *AppError is mapped
// using its own Status/Code/Field/Message; an error with Unavailable() true is a 503
// SERVICE_UNAVAILABLE with its own message; any other error is treated as
// handleGeneric(Exception) — logged server-side, answered as a generic 500 INTERNAL_ERROR so
// internal details never leak on the wire.
func WriteError(w http.ResponseWriter, err error) {
	if appErr, ok := errors.AsType[*AppError](err); ok {
		body := ErrorResponse{
			Errors: []ErrorDetail{{Code: appErr.Code, Field: appErr.Field, Message: appErr.Message}},
			Meta:   NewMeta(),
		}
		WriteJSON(w, appErr.Status, body)
		return
	}
	// A domain error that cannot construct HTTP errors (rebalancingcalc.UnavailableError, task
	// 182 / F1.5 option A) reports the behaviour instead: Unavailable() true is a 503 with its
	// own message, exactly what NewServiceUnavailableError would have answered.
	var unavailable interface {
		error
		Unavailable() bool
	}
	if errors.As(err, &unavailable) && unavailable.Unavailable() {
		WriteError(w, NewServiceUnavailableError(unavailable.Error()))
		return
	}

	slog.Error("unhandled error", "error", err, "request_id", w.Header().Get(RequestIDHeader))
	body := ErrorResponse{
		Errors: []ErrorDetail{{Code: CodeInternalError, Message: "An unexpected error occurred"}},
		Meta:   NewMeta(),
	}
	WriteJSON(w, http.StatusInternalServerError, body)
}
