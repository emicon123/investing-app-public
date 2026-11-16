// Package httpx holds the chi router helpers and response-envelope types shared by every backend-go
// domain package, so all three backends answer with byte-for-byte the same wire shape (the OpenAPI
// {data, meta} / ErrorResponse envelopes).
package httpx

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Meta is the direct equivalent of pl.shared.dto.common.Meta — a fresh request id + timestamp
// attached to every response envelope.
type Meta struct {
	RequestID uuid.UUID `json:"requestId"`
	Timestamp time.Time `json:"timestamp"`
}

// NewMeta mirrors Meta.create() — a fresh random request id and the current instant. WriteJSON
// replaces the random id with the request's X-Request-Id (see RequestID) for httpx envelopes.
func NewMeta() Meta {
	return Meta{RequestID: uuid.New(), Timestamp: time.Now().UTC()}
}

// NewMetaFrom is NewMeta carrying the request id RequestID stored in ctx — for envelopes WriteJSON
// cannot rewrite (the generated openapi.Meta types). Without a request id it falls back to
// NewMeta's random one.
func NewMetaFrom(ctx context.Context) Meta {
	m := NewMeta()
	if id := RequestIDFrom(ctx); id != uuid.Nil {
		m.RequestID = id
	}
	return m
}

// PageInfo is the direct equivalent of pl.shared.dto.common.PageInfo.
type PageInfo struct {
	Number int   `json:"number"`
	Size   int   `json:"size"`
	Total  int64 `json:"total"`
}

// SingleResponse is the direct equivalent of pl.shared.dto.common.SingleResponse<T> —
// the {data, meta} envelope used by every single-resource endpoint.
type SingleResponse[T any] struct {
	Data T    `json:"data"`
	Meta Meta `json:"meta"`
}

// Single mirrors Envelopes.single(data).
func Single[T any](data T) SingleResponse[T] {
	return SingleResponse[T]{Data: data, Meta: NewMeta()}
}

// ListResponse is the direct equivalent of pl.shared.dto.common.ListResponse<T> —
// the {data, meta} envelope used by non-paginated collection endpoints.
type ListResponse[T any] struct {
	Data []T  `json:"data"`
	Meta Meta `json:"meta"`
}

// List mirrors Envelopes.list(items). A nil slice is normalized to an empty JSON array,
// never `null`, matching Jackson's default List serialization.
func List[T any](items []T) ListResponse[T] {
	if items == nil {
		items = []T{}
	}
	return ListResponse[T]{Data: items, Meta: NewMeta()}
}

// PagedResponse is the direct equivalent of pl.shared.dto.common.PagedResponse<T> —
// the {data, meta, page} envelope used by paginated collection endpoints.
type PagedResponse[T any] struct {
	Data []T      `json:"data"`
	Meta Meta     `json:"meta"`
	Page PageInfo `json:"page"`
}

// Paged mirrors Envelopes.paged(items, pageInfo).
func Paged[T any](items []T, page PageInfo) PagedResponse[T] {
	if items == nil {
		items = []T{}
	}
	return PagedResponse[T]{Data: items, Meta: NewMeta(), Page: page}
}
