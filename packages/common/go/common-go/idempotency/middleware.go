package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"investing-app/common-go/httpx"
)

const (
	// maxBodyBytes caps the request body the middleware buffers to hash and replay (1 MiB).
	maxBodyBytes = 1 << 20
	// retention is how long a record is kept (ADR-111: 24 h); older ones are purged lazily.
	retention = 24 * time.Hour
	// recordTimeout bounds Complete/Forget after the handler returned.
	recordTimeout = 5 * time.Second

	codePayloadTooLarge = "PAYLOAD_TOO_LARGE"

	msgMalformedKey = "must be 1-255 printable ASCII characters"
	msgKeyReused    = "Idempotency-Key was already used for a different request"
	msgInProgress   = "A request with this Idempotency-Key is in progress or its outcome is unknown; check the history before retrying"
	msgTooLarge     = "request body must not exceed 1 MiB"
	msgUnreadable   = "could not read the request body"
)

// Middleware enforces Idempotency-Key on any request that carries the header (methods other
// than GET/HEAD/OPTIONS); requests without it pass through untouched. Semantics: ADR-111 §2,
// algorithm D174 §5.4:
//
//   - malformed key → 400 VALIDATION_ERROR (field Idempotency-Key); body over 1 MiB → 413;
//   - new key → the handler runs; a 2xx answer is stored for replay, a 4xx frees the key
//     (nothing was written), a 5xx or a panic keeps it IN_PROGRESS (outcome unknown);
//   - same key and request, COMPLETED → the stored answer, with Idempotent-Replayed: true;
//   - same key, IN_PROGRESS → 409 IDEMPOTENCY_IN_PROGRESS;
//   - same key, different method, URI or body → 422 IDEMPOTENCY_KEY_REUSED.
//
// It never recovers a panic: the panic reaches chi's Recoverer and the record stays.
func Middleware(store Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			values := r.Header[Header] // Header is already in canonical form
			if len(values) == 0 || isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			key := values[0]
			if !validKey(key) {
				httpx.WriteError(w, httpx.NewValidationError(Header, msgMalformedKey))
				return
			}
			body, err := bufferBody(w, r)
			if err != nil {
				httpx.WriteError(w, err)
				return
			}
			hash := requestHash(r.Method, r.URL.RequestURI(), body)

			if err := store.Purge(r.Context(), time.Now().Add(-retention)); err != nil {
				slog.Warn("idempotency: purge of expired keys failed", "error", err)
			}
			rec, created, err := store.Begin(r.Context(), key, hash)
			if err != nil {
				httpx.WriteError(w, err)
				return
			}
			if !created {
				answerExisting(w, rec, hash)
				return
			}

			cw := &capturingWriter{ResponseWriter: w}
			next.ServeHTTP(cw, r)
			recordOutcome(r.Context(), store, key, cw)
		})
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// validKey: 1–255 bytes, each printable ASCII (0x21–0x7E, so no space).
func validKey(key string) bool {
	if len(key) < 1 || len(key) > 255 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

// bufferBody reads the whole body (at most maxBodyBytes) and puts it back on r for the handler.
func bufferBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, &httpx.AppError{Status: http.StatusRequestEntityTooLarge, Code: codePayloadTooLarge, Message: msgTooLarge}
		}
		return nil, httpx.NewValidationError("", msgUnreadable)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// requestHash = hex(sha256(METHOD SP request-URI NL body)) (D174 §5.4 step 4).
func requestHash(method, requestURI string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method + " " + requestURI + "\n"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// answerExisting answers a request whose key already has a record, without running the handler.
func answerExisting(w http.ResponseWriter, rec Record, hash string) {
	switch {
	case rec.RequestHash != hash:
		httpx.WriteError(w, &httpx.AppError{
			Status: http.StatusUnprocessableEntity, Code: httpx.CodeIdempotencyKeyReused, Message: msgKeyReused,
		})
	case rec.Completed:
		if rec.ContentType != "" {
			w.Header().Set("Content-Type", rec.ContentType)
		}
		w.Header().Set(ReplayedHeader, "true")
		w.WriteHeader(rec.Status)
		_, _ = w.Write(rec.Body)
	default:
		httpx.WriteError(w, httpx.NewConflictError(httpx.CodeIdempotencyInProgress, msgInProgress))
	}
}

// recordOutcome stores a 2xx answer, frees the key after a 4xx and keeps it after anything
// else. It runs on a context detached from the request's cancellation: the handler may have
// committed just before the client went away, and that answer must still be recorded.
func recordOutcome(reqCtx context.Context, store Store, key string, cw *capturingWriter) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), recordTimeout)
	defer cancel()
	status := cw.finalStatus()
	var err error
	switch {
	case status >= 200 && status < 300:
		err = store.Complete(ctx, key, status, cw.finalContentType(), cw.body.Bytes())
	case status >= 400 && status < 500:
		err = store.Forget(ctx, key)
	default:
		return // 5xx: the outcome is unknown, the record stays IN_PROGRESS (ADR-111)
	}
	if err != nil {
		slog.Error("idempotency: recording the outcome failed; the key stays IN_PROGRESS",
			"status", status, "error", err)
	}
}

// capturingWriter forwards everything to the real writer and keeps a copy of the status,
// the Content-Type sent with it and the body.
type capturingWriter struct {
	http.ResponseWriter
	status      int
	contentType string
	body        bytes.Buffer
}

func (c *capturingWriter) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
		c.contentType = c.Header().Get("Content-Type")
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *capturingWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.WriteHeader(http.StatusOK)
	}
	c.body.Write(b)
	return c.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer.
func (c *capturingWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// finalStatus is what net/http sent: a handler that never wrote answered 200.
func (c *capturingWriter) finalStatus() int {
	if c.status == 0 {
		return http.StatusOK
	}
	return c.status
}

func (c *capturingWriter) finalContentType() string {
	if c.status == 0 {
		return c.Header().Get("Content-Type")
	}
	return c.contentType
}
