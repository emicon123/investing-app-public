package inflation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Task 182 B6 (F3.4): request numbers decode as decimals (x-go-type on the generated request
// type, or a hand-written decimal struct where the schema is not annotated), so values float64
// cannot hold (18 significant digits) reach the service input exactly. The wire still takes JSON
// numbers.
func TestDecodeCreateBody_CpiAsDecimal(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/inflation", strings.NewReader(`{"year":2026,"month":10,"cpiValue":105.123456789012345}`))
	_, _, cpi, appErr := decodeCreateBody(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	if cpi.String() != "105.123456789012345" {
		t.Fatalf("cpiValue %s", cpi)
	}
}
