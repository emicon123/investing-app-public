package deposit

import (
	"encoding/json"
	"testing"
)

// TestCreateDepositRequest_AmountReachesTheServiceExactly pins task 182 B5 (F3.4): the amount
// decodes as a decimal, so a typed PLN amount and 18 significant digits arrive unchanged.
func TestCreateDepositRequest_AmountReachesTheServiceExactly(t *testing.T) {
	for _, amount := range []string{"1234567.8912", "0.123456789012345678"} {
		var body wireCreateDepositRequest
		if err := json.Unmarshal([]byte(`{"portfolio":"FIXED","depositDate":"2026-11-02","amount":`+amount+`}`), &body); err != nil {
			t.Fatal(err)
		}
		d, err := body.toDeposit()
		if err != nil {
			t.Fatal(err)
		}
		if d.Amount.String() != amount || d.Portfolio != "FIXED" {
			t.Errorf("amount %s reached the service as %s (portfolio %q)", amount, d.Amount, d.Portfolio)
		}
	}
}
