// Package golden pins HTTP responses and database state as JSON files under the calling test
// package's testdata/golden/ directory (task 180 wave 0: the wire-compatibility gate for moving the
// portfolioB and portfolioC services into common-go/siblingcore). A response body is compacted, its volatile
// values are masked by key, and it is indented with json.Indent, which keeps the server's field
// order and number literals byte for byte. Run the tests with -update to rewrite the files.
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden/*.json")

// Masked replaces every volatile value.
const Masked = "<masked>"

// Volatile lists the JSON keys whose values change on every run (request metadata, database ids,
// timestamps, generated uuids, "today"). Tests pass extra keys to Response for values that depend
// on the wall clock in one endpoint only (e.g. xirrPct).
var Volatile = []string{
	"requestId", "timestamp", "id", "assetId", "createdAt", "updatedAt",
	"transferId", "computedAt", "sessionId", "purchasedAt", "pricesRefreshedAt",
}

// jsonValue matches one JSON scalar (string, number, true/false/null) in compact JSON.
const jsonValue = `(?:"(?:[^"\\]|\\.)*"|-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|true|false|null)`

// Response compares status and body with testdata/golden/<name>.json. Empty bodies (204) are
// recorded as null.
func Response(t *testing.T, name string, status int, body []byte, extraVolatile ...string) {
	t.Helper()
	normalised := []byte("null")
	if len(bytes.TrimSpace(body)) > 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, body); err != nil {
			t.Fatalf("golden %s: response is not JSON: %v\n%s", name, err, body)
		}
		masked := mask(compact.Bytes(), append(append([]string(nil), Volatile...), extraVolatile...))
		var indented bytes.Buffer
		if err := json.Indent(&indented, masked, "  ", "  "); err != nil {
			t.Fatalf("golden %s: indent: %v", name, err)
		}
		normalised = indented.Bytes()
	}
	got := fmt.Sprintf("{\n  \"status\": %d,\n  \"body\": %s\n}\n", status, normalised)
	compare(t, name, []byte(got))
}

// Call runs h on one request (body may be empty) and returns the status and body — the handler
// is driven directly, the way the app's ServerInterface forward calls it.
func Call(h http.HandlerFunc, method, target, body string) (int, []byte) {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w.Code, w.Body.Bytes()
}

// Value compares v (marshalled with indentation, struct field order) with
// testdata/golden/<name>.json — for database state the test selects without volatile columns.
func Value(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("golden %s: marshal: %v", name, err)
	}
	compare(t, name, append(b, '\n'))
}

func mask(compact []byte, keys []string) []byte {
	out := compact
	for _, k := range keys {
		re := regexp.MustCompile(`"` + regexp.QuoteMeta(k) + `":` + jsonValue)
		out = re.ReplaceAll(out, []byte(`"`+k+`":"`+Masked+`"`))
	}
	return out
}

func compare(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v (run with -update to record it)", name, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s mismatch\n--- want (%s)\n%s\n--- got\n%s", name, path, want, got)
	}
}
