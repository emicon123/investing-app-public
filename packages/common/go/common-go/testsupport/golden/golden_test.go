package golden

import "testing"

func TestMask_KeepsOrderAndLiteralsMasksVolatileKeys(t *testing.T) {
	in := `{"data":{"id":12,"assetId":7,"ticker":"A","units":1.50,"name":"x\"y","createdAt":"2026-11-05T10:00:00Z","n":null},"meta":{"requestId":"r-1","timestamp":"t"}}`
	got := string(mask([]byte(in), Volatile))
	want := `{"data":{"id":"<masked>","assetId":"<masked>","ticker":"A","units":1.50,"name":"x\"y","createdAt":"<masked>","n":null},"meta":{"requestId":"<masked>","timestamp":"<masked>"}}`
	if got != want {
		t.Fatalf("mask:\n got %s\nwant %s", got, want)
	}
}
