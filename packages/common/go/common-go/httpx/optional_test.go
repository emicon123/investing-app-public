package httpx

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
)

func TestOptional_AbsentNullValue(t *testing.T) {
	type body struct {
		Target Optional[decimal.Decimal] `json:"target"`
		Name   Optional[string]          `json:"name"`
	}
	tests := []struct {
		name       string
		in         string
		wantSet    bool
		wantValue  string // "" = nil
		wantNameOK bool
	}{
		{name: "absent leaves the field alone", in: `{}`, wantSet: false},
		{name: "explicit null clears", in: `{"target": null}`, wantSet: true},
		{name: "zero is a value, not a clear", in: `{"target": 0}`, wantSet: true, wantValue: "0"},
		{name: "decimal keeps exact digits", in: `{"target": 12.3456}`, wantSet: true, wantValue: "12.3456"},
		{name: "other fields are independent", in: `{"name": "x"}`, wantSet: false, wantNameOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b body
			if err := json.Unmarshal([]byte(tt.in), &b); err != nil {
				t.Fatal(err)
			}
			if b.Target.Set != tt.wantSet {
				t.Fatalf("Set = %v, want %v", b.Target.Set, tt.wantSet)
			}
			switch {
			case tt.wantValue == "" && b.Target.Value != nil:
				t.Fatalf("Value = %s, want nil", b.Target.Value)
			case tt.wantValue != "" && (b.Target.Value == nil || b.Target.Value.String() != tt.wantValue):
				t.Fatalf("Value = %v, want %s", b.Target.Value, tt.wantValue)
			}
			if b.Name.Set != tt.wantNameOK {
				t.Fatalf("Name.Set = %v, want %v", b.Name.Set, tt.wantNameOK)
			}
		})
	}
}

func TestOptional_RejectsWrongType(t *testing.T) {
	var b struct {
		Target Optional[decimal.Decimal] `json:"target"`
	}
	if err := json.Unmarshal([]byte(`{"target": {"x": 1}}`), &b); err == nil {
		t.Fatal("expected an error for a non-numeric value")
	}
}
