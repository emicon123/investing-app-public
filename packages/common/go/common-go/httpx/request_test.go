package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON_ValidBodyDecodes(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x","n":3}`))
	var v struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	if err := DecodeJSON(r, &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "x" || v.N != 3 {
		t.Fatalf("decoded %+v", v)
	}
}

func TestDecodeJSON_MalformedBodyIs422(t *testing.T) {
	for _, body := range []string{`{`, ``, `{"n":"not-a-number"}`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		var v struct {
			N int `json:"n"`
		}
		err := DecodeJSON(r, &v)
		var appErr *AppError
		if !errors.As(err, &appErr) {
			t.Fatalf("body %q: err = %v, want *AppError", body, err)
		}
		want := AppError{Status: http.StatusUnprocessableEntity, Code: CodeValidationError, Message: "Invalid request body"}
		if *appErr != want {
			t.Fatalf("body %q: err = %+v, want %+v", body, *appErr, want)
		}
	}
}
