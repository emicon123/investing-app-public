package rebalancing

import (
	"encoding/json"
	"testing"

	"investing-app/common-go/siblingcore"
)

// TestPurchaseRequests_NumbersReachTheServiceExactly pins task 182 B5 (F3.4): the deposit
// override, the confirmed deposit and the confirmed units decode as decimals.
func TestPurchaseRequests_NumbersReachTheServiceExactly(t *testing.T) {
	var calc wireCalculateRequest
	if err := json.Unmarshal([]byte(`{"portfolio":"VARIABLE","depositAmountPln":1234567.8912}`), &calc); err != nil {
		t.Fatal(err)
	}
	if calc.DepositAmountPln == nil || calc.DepositAmountPln.String() != "1234567.8912" {
		t.Errorf("calculate override %v", calc.DepositAmountPln)
	}

	var confirm wireConfirmRequest
	if err := json.Unmarshal([]byte(`{"portfolio":"VARIABLE","depositAmountPln":1234567.8912,`+
		`"items":[{"ticker":"BTC-USD","units":0.123456789012345678}]}`), &confirm); err != nil {
		t.Fatal(err)
	}
	req, err := confirm.toConfirmRequest()
	if err != nil {
		t.Fatal(err)
	}
	if req.Portfolio != "VARIABLE" || req.DepositAmountPln == nil || req.DepositAmountPln.String() != "1234567.8912" {
		t.Errorf("confirm portfolio %q deposit %v", req.Portfolio, req.DepositAmountPln)
	}
	if len(req.Items) != 1 || req.Items[0].Units.Of(siblingcore.SingleBucket).String() != "0.123456789012345678" {
		t.Errorf("confirm items %+v", req.Items)
	}
}
