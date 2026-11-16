package httpx

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// RequestIDHeader carries the request id in both directions: an inbound value is reused when it
// is a UUID, and every response echoes the id the server used (task-174 F4.2, D174 §8.2).
const RequestIDHeader = "X-Request-Id"

type requestIDKey struct{}

// RequestID replaces chi's RequestID middleware: it accepts an inbound X-Request-Id only if it
// parses as a non-nil UUID, otherwise generates uuid.New(); stores it in the request context;
// sets the X-Request-Id response header before calling next. The access log, the "unhandled
// error" log and every envelope's meta.requestId use this one id, so the id the UI shows can be
// found in docker logs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.Header.Get(RequestIDHeader))
		if err != nil || id == uuid.Nil {
			id = uuid.New()
		}
		w.Header().Set(RequestIDHeader, id.String())
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// RequestIDFrom returns the id RequestID stored, or uuid.Nil.
func RequestIDFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(requestIDKey{}).(uuid.UUID)
	return id
}

// responseRequestID returns the id RequestID set on w's X-Request-Id header, if any.
func responseRequestID(w http.ResponseWriter) (uuid.UUID, bool) {
	id, err := uuid.Parse(w.Header().Get(RequestIDHeader))
	return id, err == nil && id != uuid.Nil
}

// withRequestID is implemented (value receivers) by every httpx envelope, so WriteJSON can put
// the request's id into meta.requestId without any call-site change.
type withRequestID interface {
	withRequestID(id uuid.UUID) any
}

func (s SingleResponse[T]) withRequestID(id uuid.UUID) any { s.Meta.RequestID = id; return s }
func (l ListResponse[T]) withRequestID(id uuid.UUID) any   { l.Meta.RequestID = id; return l }
func (p PagedResponse[T]) withRequestID(id uuid.UUID) any  { p.Meta.RequestID = id; return p }
func (e ErrorResponse) withRequestID(id uuid.UUID) any     { e.Meta.RequestID = id; return e }
