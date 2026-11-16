package pgerr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"investing-app/common-go/httpx"
)

func TestNotFoundOr_NoRowsIs404WithMessage(t *testing.T) {
	err := NotFoundOr(fmt.Errorf("query: %w", pgx.ErrNoRows), "Position with id 5 not found", "position: read 5")
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("err = %v, want *httpx.AppError", err)
	}
	want := httpx.AppError{Status: http.StatusNotFound, Code: httpx.CodeNotFound, Message: "Position with id 5 not found"}
	if *appErr != want {
		t.Fatalf("err = %+v, want %+v", *appErr, want)
	}
}

func TestNotFoundOr_OtherErrorIsWrappedWithOp(t *testing.T) {
	cause := errors.New("connection reset")
	err := NotFoundOr(cause, "Position with id 5 not found", "position: read 5")
	if got, want := err.Error(), fmt.Errorf("position: read %d: %w", 5, cause).Error(); got != want {
		t.Fatalf("err = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Fatal("wrapped error lost the cause")
	}
	if _, ok := errors.AsType[*httpx.AppError](err); ok {
		t.Fatal("a non-ErrNoRows error must not become an AppError")
	}
}

func TestIsUniqueViolation(t *testing.T) {
	if IsUniqueViolation(errors.New("x")) {
		t.Fatal("plain error is not a unique violation")
	}
}
