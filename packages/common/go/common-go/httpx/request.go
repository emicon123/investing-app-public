package httpx

import (
	"encoding/json"
	"net/http"
)

// DecodeJSON decodes the request body into v. Any decode failure — malformed JSON, an empty
// body, a type mismatch — is the 422 VALIDATION_ERROR "Invalid request body" every body-taking
// handler answers with; the decoder's own message never reaches the wire.
func DecodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return &AppError{Status: 422, Code: CodeValidationError, Message: "Invalid request body"}
	}
	return nil
}
