package testsupport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"investing-app/common-go/httpx"
	"investing-app/common-go/idempotency"
)

// IdempotentPOST describes one money-moving POST served by an app's real router stack (auth,
// idempotency.Middleware, generated routes) on an httptest.Server.
type IdempotentPOST struct {
	URL       string     // full URL of the POST
	Bearer    string     // a valid backend JWT for the app
	Body      string     // a valid request body (answered 2xx)
	OtherBody string     // a different valid body, for the key-reuse check
	CountRows func() int // rows of the table the POST writes
}

// AssertIdempotencyKeyEndToEnd runs the ADR-111 acceptance scenario (task 177 B8) against p:
// the first keyed call writes once; an identical replay returns the same status and body with
// Idempotent-Replayed: true and writes nothing; the same key with another body is 422
// IDEMPOTENCY_KEY_REUSED; a malformed key is 400; without the header every call writes
// (pre-ADR-111 behaviour).
func AssertIdempotencyKeyEndToEnd(t *testing.T, p IdempotentPOST) {
	t.Helper()
	key := uuid.NewString()
	base := p.CountRows()

	firstStatus, firstBody, firstHeader := sendPOST(t, p, key, p.Body)
	if firstStatus < 200 || firstStatus > 299 {
		t.Fatalf("first keyed call: status %d, body %s", firstStatus, firstBody)
	}
	if firstHeader.Get(idempotency.ReplayedHeader) != "" {
		t.Fatal("first keyed call must not be marked as a replay")
	}
	assertRows(t, p, base+1, "after the first keyed call")

	status, body, header := sendPOST(t, p, key, p.Body)
	if status != firstStatus || !bytes.Equal(body, firstBody) || header.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("replay: status %d (want %d), replayed=%q, body equal=%v",
			status, firstStatus, header.Get(idempotency.ReplayedHeader), bytes.Equal(body, firstBody))
	}
	if header.Get("Content-Type") != firstHeader.Get("Content-Type") {
		t.Fatalf("replay content type %q, want %q", header.Get("Content-Type"), firstHeader.Get("Content-Type"))
	}
	assertRows(t, p, base+1, "after the replay")

	status, body, _ = sendPOST(t, p, key, p.OtherBody)
	assertErrorCode(t, status, body, http.StatusUnprocessableEntity, httpx.CodeIdempotencyKeyReused, "")
	assertRows(t, p, base+1, "after reusing the key for another body")

	status, body, _ = sendPOST(t, p, "two words", p.Body)
	assertErrorCode(t, status, body, http.StatusBadRequest, httpx.CodeValidationError, idempotency.Header)
	assertRows(t, p, base+1, "after a malformed key")

	for i := range 2 {
		status, body, header := sendPOST(t, p, "", p.Body)
		if status != firstStatus || header.Get(idempotency.ReplayedHeader) != "" {
			t.Fatalf("call %d without the header: status %d, replayed=%q, body %s",
				i+1, status, header.Get(idempotency.ReplayedHeader), body)
		}
	}
	assertRows(t, p, base+3, "after two calls without the header")
}

func sendPOST(t *testing.T, p IdempotentPOST, key, body string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, p.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+p.Bearer)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(idempotency.Header, key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", p.URL, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, respBody, resp.Header
}

func assertRows(t *testing.T, p IdempotentPOST, want int, when string) {
	t.Helper()
	if got := p.CountRows(); got != want {
		t.Fatalf("%s: %d rows, want %d", when, got, want)
	}
}

func assertErrorCode(t *testing.T, status int, body []byte, wantStatus int, wantCode, wantField string) {
	t.Helper()
	var resp httpx.ErrorResponse
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Errors) != 1 {
		t.Fatalf("status %d: expected one error in the envelope, got %s (%v)", status, body, err)
	}
	if got := resp.Errors[0]; status != wantStatus || got.Code != wantCode || got.Field != wantField {
		t.Fatalf("got %d %+v, want %d code %s field %q", status, got, wantStatus, wantCode, wantField)
	}
}
