package httpx

import (
	"bytes"
	"encoding/json"
)

// Optional is a three-state PATCH-style JSON field: absent from the body (Set == false, leave
// the stored value alone), an explicit JSON null (Set == true, Value == nil, clear it), or a
// value (Set == true, Value != nil). A plain *T cannot tell "absent" from "null" — both decode
// to nil — which would make it impossible for a client to CLEAR a nullable column (NULL "no
// target" is distinct from 0 "explicit zero target", ADR-053). Declare the field as a value
// (not a pointer) in the request struct: encoding/json calls UnmarshalJSON only for keys that
// are present, and does call it for a literal null on a non-pointer field.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON records that the key was present and whether it carried a value.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}
