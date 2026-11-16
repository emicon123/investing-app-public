package rebalancing

import (
	"encoding/json"
	"testing"

	"portfolio-b/backend-go/internal/book"
)

// TestConfirmRequest_UnitsReachTheServiceExactly pins task 182 B5 (F3.4): allocation units
// decode as decimals (float64 kept ~17 significant digits).
func TestConfirmRequest_UnitsReachTheServiceExactly(t *testing.T) {
	var body wireConfirmRequest
	raw := `{"items":[{"ticker":"EUNL.DE","accountAllocations":[` +
		`{"accountType":"IKE_TATA","units":0.123456789012345678},{"accountType":"IKZE_MAMA","units":1234567.8912}]}]}`
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	req := body.toConfirmRequest()
	if len(req.Items) != 1 {
		t.Fatalf("items %+v", req.Items)
	}
	units := req.Items[0].Units
	if got := units.Of(book.IkeTata).String(); got != "0.123456789012345678" {
		t.Errorf("IKE_TATA units %s", got)
	}
	if got := units.Of(book.IkzeMama).String(); got != "1234567.8912" {
		t.Errorf("IKZE_MAMA units %s", got)
	}
}
