package purchasesession

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
func TestDecodeSessionBodies_MoneyAsDecimals(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/purchase-sessions", strings.NewReader(
		`{"portfoliosIncluded":"BOTH","cashOverridePln":12345678901234.5678}`))
	in, appErr := decodeCreateSessionBody(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	if in.CashOverridePln == nil || in.CashOverridePln.String() != "12345678901234.5678" {
		t.Fatalf("cashOverridePln %v", in.CashOverridePln)
	}

	r = httptest.NewRequest(http.MethodPatch, "/purchase-sessions/1/items", strings.NewReader(
		`{"items":[{"id":7,"actualUnits":0.123456789012345678,"actualValuePln":1234567.8912},{"id":8}]}`))
	entries, appErr := decodePatchItemsBody(r)
	if appErr != nil {
		t.Fatal(appErr)
	}
	if len(entries) != 2 || entries[0].ID != 7 || entries[0].ActualUnits.String() != "0.123456789012345678" ||
		entries[0].ActualValuePln.String() != "1234567.8912" || entries[1].ActualUnits != nil || entries[1].ActualValuePln != nil {
		t.Fatalf("entries %+v", entries)
	}
}
