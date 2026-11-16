package idempotency_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"investing-app/common-go/httpx"
	"investing-app/common-go/idempotency"
)

// fakeStore is an in-memory Store recording every call, so the middleware's decisions are
// observable without a database.
type fakeStore struct {
	mu        sync.Mutex
	records   map[string]idempotency.Record
	begins    int
	completes int
	forgets   int
	purges    int
	beginErr  error
	purgeErr  error
	ctxErrs   []error // ctx.Err() seen by Complete/Forget
}

func newFakeStore() *fakeStore { return &fakeStore{records: map[string]idempotency.Record{}} }

func (f *fakeStore) Begin(_ context.Context, key, hash string) (idempotency.Record, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins++
	if f.beginErr != nil {
		return idempotency.Record{}, false, f.beginErr
	}
	if rec, ok := f.records[key]; ok {
		return rec, false, nil
	}
	rec := idempotency.Record{RequestHash: hash}
	f.records[key] = rec
	return rec, true, nil
}

func (f *fakeStore) Complete(ctx context.Context, key string, status int, contentType string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completes++
	f.ctxErrs = append(f.ctxErrs, ctx.Err())
	rec := f.records[key]
	rec.Completed, rec.Status, rec.ContentType, rec.Body = true, status, contentType, append([]byte(nil), body...)
	f.records[key] = rec
	return nil
}

func (f *fakeStore) Forget(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgets++
	f.ctxErrs = append(f.ctxErrs, ctx.Err())
	if !f.records[key].Completed {
		delete(f.records, key)
	}
	return nil
}

func (f *fakeStore) Purge(context.Context, time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purges++
	return f.purgeErr
}

// countingHandler answers with the given status and a JSON body echoing the request body, and
// counts how often it ran.
type countingHandler struct {
	calls    int
	status   int // 0: write the body without calling WriteHeader
	lastBody string
}

func (h *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.calls++
	b, _ := io.ReadAll(r.Body)
	h.lastBody = string(b)
	w.Header().Set("Content-Type", "application/json")
	if h.status != 0 {
		w.WriteHeader(h.status)
	}
	_, _ = w.Write([]byte(`{"echo":` + string(b) + `}`))
}

func post(key, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/deposits?x=1", strings.NewReader(body))
	if key != "" {
		r.Header.Set(idempotency.Header, key)
	}
	return r
}

func serve(t *testing.T, store idempotency.Store, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	idempotency.Middleware(store)(h).ServeHTTP(w, r)
	return w
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) httpx.ErrorDetail {
	t.Helper()
	var resp httpx.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || len(resp.Errors) != 1 {
		t.Fatalf("expected one error in the envelope, got %q (%v)", w.Body.String(), err)
	}
	return resp.Errors[0]
}

func TestMiddleware_PassThrough(t *testing.T) {
	t.Run("POST without the header", func(t *testing.T) {
		store, h := newFakeStore(), &countingHandler{status: http.StatusCreated}
		w := serve(t, store, h, post("", `{"a":1}`))
		if w.Code != http.StatusCreated || h.calls != 1 || h.lastBody != `{"a":1}` {
			t.Fatalf("code=%d calls=%d body=%q", w.Code, h.calls, h.lastBody)
		}
		if store.begins+store.purges != 0 {
			t.Fatal("a request without the header must not touch the store")
		}
	})
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method+" with the header", func(t *testing.T) {
			store, h := newFakeStore(), &countingHandler{status: http.StatusOK}
			r := httptest.NewRequest(method, "/api/deposits", nil)
			r.Header.Set(idempotency.Header, "k1")
			serve(t, store, h, r)
			if h.calls != 1 || store.begins+store.purges != 0 {
				t.Fatalf("%s must pass through: calls=%d begins=%d", method, h.calls, store.begins)
			}
		})
	}
}

func TestMiddleware_MalformedKey(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"256 long":  strings.Repeat("k", 256),
		"a space":   "two words",
		"non-ASCII": "klucz-ż",
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			store, h := newFakeStore(), &countingHandler{}
			r := httptest.NewRequest(http.MethodPost, "/api/deposits", strings.NewReader(`{}`))
			r.Header[idempotency.Header] = []string{key} // Set would still send an empty value
			w := serve(t, store, h, r)
			if w.Code != http.StatusBadRequest || h.calls != 0 || store.begins != 0 {
				t.Fatalf("code=%d calls=%d begins=%d", w.Code, h.calls, store.begins)
			}
			d := errorCode(t, w)
			if d.Code != httpx.CodeValidationError || d.Field != idempotency.Header ||
				d.Message != "must be 1-255 printable ASCII characters" {
				t.Fatalf("error = %+v", d)
			}
		})
	}
	t.Run("255 printable characters are accepted", func(t *testing.T) {
		store, h := newFakeStore(), &countingHandler{status: http.StatusOK}
		w := serve(t, store, h, post(strings.Repeat("~", 255), `{}`))
		if w.Code != http.StatusOK || h.calls != 1 {
			t.Fatalf("code=%d calls=%d", w.Code, h.calls)
		}
	})
}

func TestMiddleware_FirstCallStoresSuccessAndReplays(t *testing.T) {
	store, h := newFakeStore(), &countingHandler{status: 0} // implicit 200
	first := serve(t, store, h, post("k1", `{"a":1}`))
	if first.Code != http.StatusOK || h.calls != 1 || h.lastBody != `{"a":1}` {
		t.Fatalf("first: code=%d calls=%d handler saw body %q", first.Code, h.calls, h.lastBody)
	}
	if first.Header().Get(idempotency.ReplayedHeader) != "" {
		t.Fatal("a first call is not a replay")
	}
	rec := store.records["k1"]
	if store.completes != 1 || !rec.Completed || rec.Status != http.StatusOK ||
		rec.ContentType != "application/json" || string(rec.Body) != `{"echo":{"a":1}}` {
		t.Fatalf("stored record = %+v (completes=%d)", rec, store.completes)
	}
	if store.purges != 1 {
		t.Fatalf("expected one lazy purge per keyed request, got %d", store.purges)
	}

	replay := serve(t, store, h, post("k1", `{"a":1}`))
	if h.calls != 1 {
		t.Fatal("a replay must not call the handler")
	}
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() ||
		replay.Header().Get("Content-Type") != "application/json" ||
		replay.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("replay: code=%d body=%q headers=%v", replay.Code, replay.Body.String(), replay.Header())
	}
}

func TestMiddleware_ReplaysStoredStatus(t *testing.T) {
	store, h := newFakeStore(), &countingHandler{status: http.StatusCreated}
	serve(t, store, h, post("k1", `{}`))
	replay := serve(t, store, h, post("k1", `{}`))
	if replay.Code != http.StatusCreated || h.calls != 1 {
		t.Fatalf("replay code=%d calls=%d", replay.Code, h.calls)
	}
}

func TestMiddleware_InProgress409(t *testing.T) {
	store, h := newFakeStore(), &countingHandler{status: http.StatusOK}
	store.records["k1"] = idempotency.Record{RequestHash: requestHash(http.MethodPost, "/api/deposits?x=1", `{}`)}
	w := serve(t, store, h, post("k1", `{}`))
	if w.Code != http.StatusConflict || h.calls != 0 {
		t.Fatalf("code=%d calls=%d", w.Code, h.calls)
	}
	if d := errorCode(t, w); d.Code != httpx.CodeIdempotencyInProgress ||
		d.Message != "A request with this Idempotency-Key is in progress or its outcome is unknown; check the history before retrying" {
		t.Fatalf("error = %+v", d)
	}
}

func TestMiddleware_KeyReused422(t *testing.T) {
	cases := map[string]*http.Request{
		"different body":   post("k1", `{"a":2}`),
		"different path":   httptest.NewRequest(http.MethodPost, "/api/crypto-trades?x=1", strings.NewReader(`{"a":1}`)),
		"different method": httptest.NewRequest(http.MethodPut, "/api/deposits?x=1", strings.NewReader(`{"a":1}`)),
	}
	for name, second := range cases {
		t.Run(name, func(t *testing.T) {
			store, h := newFakeStore(), &countingHandler{status: http.StatusOK}
			serve(t, store, h, post("k1", `{"a":1}`))
			second.Header.Set(idempotency.Header, "k1")
			w := serve(t, store, h, second)
			if w.Code != http.StatusUnprocessableEntity || h.calls != 1 {
				t.Fatalf("code=%d calls=%d", w.Code, h.calls)
			}
			if d := errorCode(t, w); d.Code != httpx.CodeIdempotencyKeyReused ||
				d.Message != "Idempotency-Key was already used for a different request" {
				t.Fatalf("error = %+v", d)
			}
		})
	}
	t.Run("also while the first request is still in progress", func(t *testing.T) {
		store, h := newFakeStore(), &countingHandler{status: http.StatusOK}
		store.records["k1"] = idempotency.Record{RequestHash: requestHash(http.MethodPost, "/api/deposits?x=1", `{"a":1}`)}
		if w := serve(t, store, h, post("k1", `{"a":2}`)); w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("code=%d", w.Code)
		}
	})
}

func TestMiddleware_ClientErrorForgetsKey(t *testing.T) {
	store, h := newFakeStore(), &countingHandler{status: http.StatusBadRequest}
	w := serve(t, store, h, post("k1", `{}`))
	if w.Code != http.StatusBadRequest || store.forgets != 1 || store.completes != 0 {
		t.Fatalf("code=%d forgets=%d completes=%d", w.Code, store.forgets, store.completes)
	}
	if _, kept := store.records["k1"]; kept {
		t.Fatal("a 4xx must free the key")
	}
	// A corrected retry with the same key runs normally.
	h.status = http.StatusOK
	if w := serve(t, store, h, post("k1", `{}`)); w.Code != http.StatusOK || h.calls != 2 {
		t.Fatalf("retry after 4xx: code=%d calls=%d", w.Code, h.calls)
	}
}

func TestMiddleware_ServerErrorKeepsRecord(t *testing.T) {
	store, h := newFakeStore(), &countingHandler{status: http.StatusInternalServerError}
	serve(t, store, h, post("k1", `{}`))
	if store.forgets != 0 || store.completes != 0 {
		t.Fatalf("a 5xx must leave the record IN_PROGRESS: forgets=%d completes=%d", store.forgets, store.completes)
	}
	if w := serve(t, store, h, post("k1", `{}`)); w.Code != http.StatusConflict || h.calls != 1 {
		t.Fatalf("retry after 5xx must be 409 without running: code=%d calls=%d", w.Code, h.calls)
	}
}

func TestMiddleware_PanicPropagatesAndRecordStays(t *testing.T) {
	store := newFakeStore()
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the handler's panic must propagate to chi's Recoverer")
			}
		}()
		serve(t, store, panicking, post("k1", `{}`))
	}()
	rec, kept := store.records["k1"]
	if !kept || rec.Completed || store.forgets != 0 || store.completes != 0 {
		t.Fatalf("record after panic = %+v kept=%v forgets=%d completes=%d", rec, kept, store.forgets, store.completes)
	}
}

func TestMiddleware_CompleteSurvivesCancelledRequestContext(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		store := newFakeStore()
		ctx, cancel := context.WithCancel(context.Background())
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			cancel() // the client went away after the handler committed
		})
		serve(t, store, h, post("k1", `{}`).WithContext(ctx))
		if len(store.ctxErrs) != 1 || store.ctxErrs[0] != nil {
			t.Fatalf("status %d: Complete/Forget must run on a live context, saw %v", status, store.ctxErrs)
		}
	}
}

func TestMiddleware_BodyOverOneMiB413(t *testing.T) {
	store, h := newFakeStore(), &countingHandler{}
	w := serve(t, store, h, post("k1", strings.Repeat("x", 1<<20+1)))
	if w.Code != http.StatusRequestEntityTooLarge || h.calls != 0 || store.begins != 0 {
		t.Fatalf("code=%d calls=%d begins=%d", w.Code, h.calls, store.begins)
	}
	if d := errorCode(t, w); d.Code != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("error = %+v", d)
	}
	t.Run("exactly 1 MiB is accepted", func(t *testing.T) {
		store, h := newFakeStore(), &countingHandler{status: http.StatusOK}
		body := strings.Repeat("x", 1<<20)
		serve(t, store, h, post("k2", body))
		if h.calls != 1 || len(h.lastBody) != len(body) {
			t.Fatalf("calls=%d body length=%d", h.calls, len(h.lastBody))
		}
	})
}

func TestMiddleware_StoreFailures(t *testing.T) {
	t.Run("Begin error is a 500 and the handler does not run", func(t *testing.T) {
		store, h := newFakeStore(), &countingHandler{}
		store.beginErr = errors.New("db down")
		w := serve(t, store, h, post("k1", `{}`))
		if w.Code != http.StatusInternalServerError || h.calls != 0 {
			t.Fatalf("code=%d calls=%d", w.Code, h.calls)
		}
	})
	t.Run("Purge error is ignored", func(t *testing.T) {
		store, h := newFakeStore(), &countingHandler{status: http.StatusOK}
		store.purgeErr = errors.New("purge failed")
		if w := serve(t, store, h, post("k1", `{}`)); w.Code != http.StatusOK || h.calls != 1 {
			t.Fatalf("code=%d calls=%d", w.Code, h.calls)
		}
	})
}

// requestHash is D174 §5.4 step 4 written out independently of the implementation, so tests
// can seed a record "from an earlier request".
func requestHash(method, requestURI, body string) string {
	sum := sha256.Sum256([]byte(method + " " + requestURI + "\n" + body))
	return hex.EncodeToString(sum[:])
}
