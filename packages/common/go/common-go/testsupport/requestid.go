package testsupport

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"investing-app/common-go/httpx"
)

// AssertOneRequestID serves req through h (an app's real router: httpx.NewRouter plus the app's
// route mounting) with the default slog logger captured, and asserts that the X-Request-Id
// response header, the response envelope's meta.requestId and the access-log line's request_id
// are one UUID, equal to req's own X-Request-Id when it sends a valid one (task 182 B2, F4.2:
// the id the UI shows is the id in docker logs). It returns the recorded response.
// Not parallel-safe: slog.Default is swapped for the duration of the call.
func AssertOneRequestID(t *testing.T, h http.Handler, req *http.Request, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	slog.SetDefault(prev)

	if rec.Code != wantStatus {
		t.Fatalf("%s %s: status %d, want %d: %s", req.Method, req.URL.Path, rec.Code, wantStatus, rec.Body.String())
	}
	header := rec.Header().Get(httpx.RequestIDHeader)
	if id, err := uuid.Parse(header); err != nil || id == uuid.Nil {
		t.Fatalf("%s %s: X-Request-Id header %q is not a UUID (%v)", req.Method, req.URL.Path, header, err)
	}
	if inbound := req.Header.Get(httpx.RequestIDHeader); inbound != "" && inbound != header {
		t.Errorf("%s %s: X-Request-Id %q, want the inbound %q", req.Method, req.URL.Path, header, inbound)
	}

	var env struct {
		Meta struct {
			RequestID string `json:"requestId"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: body is not a JSON envelope: %v\n%s", req.Method, req.URL.Path, err, rec.Body.String())
	}
	if env.Meta.RequestID != header {
		t.Errorf("%s %s: meta.requestId %q, want the X-Request-Id header %q", req.Method, req.URL.Path, env.Meta.RequestID, header)
	}

	if got := accessLogRequestID(t, &logs, req); got != header {
		t.Errorf("%s %s: access log request_id %q, want the X-Request-Id header %q\nlogs:\n%s",
			req.Method, req.URL.Path, got, header, logs.String())
	}
	return rec
}

// accessLogRequestID returns request_id of the one http_request log line for req's method and path.
func accessLogRequestID(t *testing.T, logs *bytes.Buffer, req *http.Request) string {
	t.Helper()
	var ids []string
	sc := bufio.NewScanner(bytes.NewReader(logs.Bytes()))
	for sc.Scan() {
		var line struct {
			Msg       string `json:"msg"`
			Method    string `json:"method"`
			Path      string `json:"path"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, sc.Text())
		}
		if line.Msg == "http_request" && line.Method == req.Method && line.Path == req.URL.Path {
			ids = append(ids, line.RequestID)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("%s %s: expected one access-log line, got %d:\n%s", req.Method, req.URL.Path, len(ids), logs.String())
	}
	return ids[0]
}
