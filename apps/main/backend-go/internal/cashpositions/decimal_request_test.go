package cashpositions

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
func TestDecodeCreateBody_AmountsAsDecimals(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/cash-positions", strings.NewReader(`{"recordedAt":"2026-11-02T10:00:00Z",`+
		`"positions":[{"location":"PKO","amountPln":12345678901234.5678},{"location":"XTB","amountPln":1234567.8912}]}`))
	items, _, appErr := decodeCreateBody(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	if len(items) != 2 || items[0].AmountPln.String() != "12345678901234.5678" || items[1].AmountPln.String() != "1234567.8912" {
		t.Fatalf("items %+v", items)
	}
}
