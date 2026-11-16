package macro

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
func TestDecodeManualM2_ValueAsDecimal(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/market-data/macro/m2-manual", strings.NewReader(
		`{"seriesCode":"M2_CN","observationDate":"2026-10-01","value":312345678.123456789}`))
	_, _, value, appErr := decodeManualM2(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	if value.String() != "312345678.123456789" {
		t.Fatalf("value %s", value)
	}
}
