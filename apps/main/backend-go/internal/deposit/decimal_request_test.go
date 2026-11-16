package deposit

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
func TestDecodeCreateBody_AmountAsDecimal(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/deposits", strings.NewReader(`{"depositDate":"2026-11-02",`+
		`"amount":12345678901234.5678,"currency":"PLN","exchange":"XTB","accountType":"IKE","portfolio":"RETIREMENT"}`))
	in, appErr := decodeCreateBody(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	if in.Amount.String() != "12345678901234.5678" || in.Portfolio != "RETIREMENT" || in.AccountType != "IKE" {
		t.Fatalf("input %+v", in)
	}
}
