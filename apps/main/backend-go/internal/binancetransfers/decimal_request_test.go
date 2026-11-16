package binancetransfers

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
	r := httptest.NewRequest(http.MethodPost, "/binance-transfers", strings.NewReader(
		`{"transferDate":"2026-11-02","amountPln":12345678901234.5678,"amountNative":0.123456789012345678,"nativeCurrency":"USD"}`))
	in, appErr := decodeCreateBody(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	if in.AmountPln.String() != "12345678901234.5678" || in.AmountNative == nil || in.AmountNative.String() != "0.123456789012345678" {
		t.Fatalf("amountPln %s amountNative %v", in.AmountPln, in.AmountNative)
	}
	r = httptest.NewRequest(http.MethodPost, "/binance-transfers", strings.NewReader(`{"transferDate":"2026-11-02","amountPln":1234567.8912}`))
	if in, appErr = decodeCreateBody(r); appErr != nil || in.AmountPln.String() != "1234567.8912" || in.AmountNative != nil {
		t.Fatalf("absent amountNative: %+v (%v)", in, appErr)
	}
}
