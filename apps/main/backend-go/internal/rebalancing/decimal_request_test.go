package rebalancing

import (
	"encoding/json"
	"testing"

	"investing-app/backend-go/openapi"
)

// Task 182 B6 (F3.4): request numbers decode as decimals (x-go-type on the generated request
// type, or a hand-written decimal struct where the schema is not annotated), so values float64
// cannot hold (18 significant digits) reach the service input exactly. The wire still takes JSON
// numbers.
func TestToLeg_MoneyAsDecimals(t *testing.T) {
	var body openapi.RebalancingTransferRequest
	if err := json.Unmarshal([]byte(`{"portfolio":"MAIN","sold":{"ticker":"BTC-USD","accountType":"REGULAR",`+
		`"regularCurrency":"USD","units":0.123456789012345678,"pricePln":1234567.8912},`+
		`"buys":[{"ticker":"GLD","accountType":"IKE","units":12345678.123456789012345678,"pricePln":0.01}]}`), &body); err != nil {
		t.Fatal(err)
	}
	sold, buy := toLeg(body.Sold), toLeg(body.Buys[0])
	if sold.Units.String() != "0.123456789012345678" || sold.PricePln.String() != "1234567.8912" {
		t.Errorf("sold %+v", sold)
	}
	if buy.Units.String() != "12345678.123456789012345678" || buy.PricePln.String() != "0.01" {
		t.Errorf("buy %+v", buy)
	}
}
