package cryptotrades

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
	r := httptest.NewRequest(http.MethodPost, "/crypto-trades", strings.NewReader(`{"tradeDate":"2026-11-02","ticker":"BTC",`+
		`"exchange":"BINANCE","accountType":"REGULAR","side":"BUY","quantity":0.123456789012345678,"unitPrice":1234567.8912,`+
		`"priceCurrency":"USD","totalAmount":12345678901234.5678,"amountPln":12345678901234.5679}`))
	in, appErr := decodeCreateBody(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	for name, c := range map[string][2]string{
		"quantity":    {in.Quantity.String(), "0.123456789012345678"},
		"unitPrice":   {in.UnitPrice.String(), "1234567.8912"},
		"totalAmount": {in.TotalAmount.String(), "12345678901234.5678"},
		"amountPln":   {in.AmountPln.String(), "12345678901234.5679"},
	} {
		if c[0] != c[1] {
			t.Errorf("%s = %s, want exactly %s", name, c[0], c[1])
		}
	}
}
