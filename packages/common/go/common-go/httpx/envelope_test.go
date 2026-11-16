package httpx

import (
	"encoding/json"
	"testing"
)

func TestSingle(t *testing.T) {
	resp := Single("hello")
	if resp.Data != "hello" {
		t.Fatalf("expected data %q, got %q", "hello", resp.Data)
	}
	if resp.Meta.RequestID.String() == "" {
		t.Fatal("expected a non-empty request id")
	}
}

func TestList_NilBecomesEmptyArrayNotNull(t *testing.T) {
	var items []string
	resp := List(items)

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(decoded["data"]) != "[]" {
		t.Fatalf("expected data to serialize as [], got %s", decoded["data"])
	}
}

func TestPaged(t *testing.T) {
	page := PageInfo{Number: 0, Size: 20, Total: 42}
	resp := Paged([]int{1, 2, 3}, page)

	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(resp.Data))
	}
	if resp.Page != page {
		t.Fatalf("expected page %+v, got %+v", page, resp.Page)
	}
}

func TestMetaJSONFieldNames(t *testing.T) {
	// The frontend must not be able to tell which backend answered — field names must match
	// pl.shared.dto.common.Meta's Jackson serialization exactly (camelCase requestId/timestamp).
	b, err := json.Marshal(NewMeta())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"requestId", "timestamp"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("expected JSON field %q, got keys %v", field, keysOf(decoded))
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
