package taxreturns

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
func TestDecodeCreateBody_MoneyAsDecimals(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/tax-returns", strings.NewReader(
		`{"year":2025,"grossGain":12345678901234.5678,"taxPaid":1234567.8912,"effectiveRatePct":19.1234567890123456}`))
	_, gross, paid, rate, appErr := decodeCreateBody(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	if gross.String() != "12345678901234.5678" || paid.String() != "1234567.8912" || rate.String() != "19.1234567890123456" {
		t.Fatalf("gross %s paid %s rate %s", gross, paid, rate)
	}
}
