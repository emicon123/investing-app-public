package httpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// captureLogs routes the default slog logger to a JSON buffer for the rest of the test.
// Not parallel-safe (slog.Default is global); no httpx test runs in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// logLines decodes every JSON log line in buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, sc.Text())
		}
		lines = append(lines, m)
	}
	return lines
}

// logLine returns the one log line with the given msg, failing if there is not exactly one.
func logLine(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	var found []map[string]any
	for _, l := range logLines(t, buf) {
		if l["msg"] == msg {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one %q log line, got %d in:\n%s", msg, len(found), buf.String())
	}
	return found[0]
}

func metaRequestID(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Meta struct {
			RequestID string `json:"requestId"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", err, body)
	}
	return env.Meta.RequestID
}

// serveRequestID runs one request with the given inbound X-Request-Id (none when empty) through
// RequestID and returns the response header and the id the handler saw in its context.
func serveRequestID(t *testing.T, inbound string) (header string, inCtx uuid.UUID) {
	t.Helper()
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inCtx = RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	if inbound != "" {
		req.Header.Set(RequestIDHeader, inbound)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Header().Get(RequestIDHeader), inCtx
}

func TestRequestID_ReusesInboundUUID(t *testing.T) {
	inbound := "5f0c6a52-3a8e-4b8a-9d0e-1c2b3a4d5e6f"
	header, inCtx := serveRequestID(t, inbound)
	if header != inbound {
		t.Errorf("X-Request-Id response header = %q, want the inbound %q", header, inbound)
	}
	if inCtx.String() != inbound {
		t.Errorf("RequestIDFrom(ctx) = %s, want the inbound %s", inCtx, inbound)
	}
}

func TestRequestID_ReplacesInvalidOrMissingInbound(t *testing.T) {
	for _, inbound := range []string{"", "abc", "pi/req-000001", "00000000-0000-0000-0000-000000000000"} {
		header, inCtx := serveRequestID(t, inbound)
		id, err := uuid.Parse(header)
		if err != nil || id == uuid.Nil {
			t.Fatalf("inbound %q: response header %q is not a generated UUID (%v)", inbound, header, err)
		}
		if header == inbound {
			t.Errorf("inbound %q: must be replaced, was echoed", inbound)
		}
		if inCtx != id {
			t.Errorf("inbound %q: RequestIDFrom(ctx) = %s, want the header's %s", inbound, inCtx, id)
		}
	}
}

func TestRequestID_GeneratesDistinctIDs(t *testing.T) {
	a, _ := serveRequestID(t, "")
	b, _ := serveRequestID(t, "")
	if a == b {
		t.Fatalf("two requests got the same generated id %s", a)
	}
}

func TestRequestIDFrom_NilWithoutMiddleware(t *testing.T) {
	if got := RequestIDFrom(context.Background()); got != uuid.Nil {
		t.Fatalf("RequestIDFrom(background) = %s, want uuid.Nil", got)
	}
}

func TestNewMetaFrom_UsesContextIDOrFallsBackToRandom(t *testing.T) {
	var ctxID uuid.UUID
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFrom(r.Context())
		if got := NewMetaFrom(r.Context()).RequestID; got != ctxID {
			t.Errorf("NewMetaFrom(ctx).RequestID = %s, want the request id %s", got, ctxID)
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got := NewMetaFrom(context.Background()).RequestID; got == uuid.Nil {
		t.Fatal("NewMetaFrom without a request id must fall back to a random id, got uuid.Nil")
	}
}

// writeWithHeader calls write on a recorder whose X-Request-Id header is id (as RequestID sets it).
func writeWithHeader(id string, write func(w http.ResponseWriter)) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	rec.Header().Set(RequestIDHeader, id)
	write(rec)
	return rec
}

func TestWriteJSON_EnvelopesTakeTheHeaderRequestID(t *testing.T) {
	id := uuid.NewString()
	cases := map[string]any{
		"single":         Single("x"),
		"single pointer": new(Single(1)),
		"list":           List([]int{1}),
		"paged":          Paged([]int{1}, PageInfo{Number: 0, Size: 1, Total: 1}),
		"error envelope": ErrorResponse{Errors: []ErrorDetail{{Code: CodeNotFound, Message: "x"}}, Meta: NewMeta()},
	}
	for name, v := range cases {
		rec := writeWithHeader(id, func(w http.ResponseWriter) { WriteJSON(w, http.StatusOK, v) })
		if got := metaRequestID(t, rec.Body.Bytes()); got != id {
			t.Errorf("%s: meta.requestId = %s, want the X-Request-Id header %s", name, got, id)
		}
	}
}

func TestWriteJSON_KeepsOwnIDWithoutHeader(t *testing.T) {
	v := Single("x")
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, v)
	if got := metaRequestID(t, rec.Body.Bytes()); got != v.Meta.RequestID.String() {
		t.Errorf("without X-Request-Id the envelope keeps its own id %s, got %s", v.Meta.RequestID, got)
	}
}

func TestWriteJSON_LeavesOtherValuesAlone(t *testing.T) {
	rec := writeWithHeader(uuid.NewString(), func(w http.ResponseWriter) {
		WriteJSON(w, http.StatusOK, map[string]string{"requestId": "kept"})
	})
	if got := rec.Body.String(); got != "{\"requestId\":\"kept\"}\n" {
		t.Errorf("a non-envelope value must be written unchanged, got %q", got)
	}
}

func TestWriteError_UsesHeaderRequestIDInEnvelopeAndLog(t *testing.T) {
	logs := captureLogs(t)
	id := uuid.NewString()

	rec := writeWithHeader(id, func(w http.ResponseWriter) { WriteError(w, NewNotFoundError("gone")) })
	if got := metaRequestID(t, rec.Body.Bytes()); got != id {
		t.Errorf("AppError envelope meta.requestId = %s, want %s", got, id)
	}

	rec = writeWithHeader(id, func(w http.ResponseWriter) { WriteError(w, errors.New("boom")) })
	if got := metaRequestID(t, rec.Body.Bytes()); got != id {
		t.Errorf("500 envelope meta.requestId = %s, want %s", got, id)
	}
	if got := logLine(t, logs, "unhandled error")["request_id"]; got != id {
		t.Errorf("\"unhandled error\" log request_id = %v, want %s", got, id)
	}
}

// TestNewRouter_OneRequestIDEverywhere is the F4.2 acceptance test: for one failing request the
// access-log line, the "unhandled error" line, the X-Request-Id response header and the
// envelope's meta.requestId all carry the same id.
func TestNewRouter_OneRequestIDEverywhere(t *testing.T) {
	logs := captureLogs(t)
	r := NewRouter()
	r.Get("/api/fail", func(w http.ResponseWriter, _ *http.Request) { WriteError(w, errors.New("boom")) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/fail", nil))

	header := rec.Header().Get(RequestIDHeader)
	if _, err := uuid.Parse(header); err != nil {
		t.Fatalf("X-Request-Id header %q is not a UUID: %v", header, err)
	}
	if got := metaRequestID(t, rec.Body.Bytes()); got != header {
		t.Errorf("meta.requestId = %s, want the header %s", got, header)
	}
	if got := logLine(t, logs, "unhandled error")["request_id"]; got != header {
		t.Errorf("error log request_id = %v, want %s", got, header)
	}
	access := logLine(t, logs, "http_request")
	if got := access["request_id"]; got != header {
		t.Errorf("access log request_id = %v, want %s", got, header)
	}
	if access["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("access log status = %v, want 500", access["status"])
	}
}
