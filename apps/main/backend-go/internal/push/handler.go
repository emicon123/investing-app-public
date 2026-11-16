package push

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/auth"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the four `push` operations of the generated ServerInterface. All of
// them sit behind the bearer-JWT branch of smartAuth; the caller's email is the subscription owner.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// MapError turns ErrNotConfigured into the 503 PUSH_NOT_CONFIGURED envelope (OpenAPI
// PushNotConfigured); every other error passes through to httpx.WriteError unchanged.
func MapError(err error) error {
	if errors.Is(err, ErrNotConfigured) {
		return &httpx.AppError{Status: http.StatusServiceUnavailable, Code: CodePushNotConfigured,
			Message: "Web Push is not configured (VAPID keys missing)"}
	}
	return err
}

// GetPushConfig implements GET /push/config: 200 {enabled:false} without a key when unconfigured.
func (h *Handler) GetPushConfig(w http.ResponseWriter, r *http.Request) {
	enabled, key := h.service.Config()
	data := openapi.PushConfig{Enabled: enabled}
	if enabled {
		data.VapidPublicKey = &key
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.PushConfigResponse{Data: data, Meta: newOpenAPIMeta(r.Context())})
}

// UpsertPushSubscription implements POST /push/subscriptions. The owner is the JWT principal and
// the device label comes from User-Agent; neither is read from the body.
func (h *Handler) UpsertPushSubscription(w http.ResponseWriter, r *http.Request) {
	email, ok := auth.EmailFromContext(r.Context())
	if !ok || email == "" {
		httpx.WriteError(w, httpx.NewUnauthorizedError("missing authenticated principal"))
		return
	}
	var body openapi.UpsertPushSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, unprocessable("", "Invalid request body"))
		return
	}
	sub, err := h.service.Subscribe(r.Context(), SubscribeInput{UserEmail: email, Endpoint: body.Endpoint,
		P256dh: body.Keys.P256dh, Auth: body.Keys.Auth, UserAgent: r.UserAgent()})
	if err != nil {
		httpx.WriteError(w, MapError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, openapi.PushSubscriptionResponse{Data: toInfo(sub), Meta: newOpenAPIMeta(r.Context())})
}

// DeletePushSubscription implements DELETE /push/subscriptions (endpoint in the JSON body, never
// in the URL). 204 for known and unknown endpoints alike, also when push is not configured.
func (h *Handler) DeletePushSubscription(w http.ResponseWriter, r *http.Request) {
	var body openapi.DeletePushSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, unprocessable("", "Invalid request body"))
		return
	}
	if err := h.service.Unsubscribe(r.Context(), body.Endpoint); err != nil {
		httpx.WriteError(w, MapError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SendTestPushNotification implements POST /push/test-notifications: the fixed test message to
// the caller's own subscriptions only.
func (h *Handler) SendTestPushNotification(w http.ResponseWriter, r *http.Request) {
	email, ok := auth.EmailFromContext(r.Context())
	if !ok || email == "" {
		httpx.WriteError(w, httpx.NewUnauthorizedError("missing authenticated principal"))
		return
	}
	rep, err := h.service.SendToUser(r.Context(), email, TestNotification)
	if err != nil {
		httpx.WriteError(w, MapError(err))
		return
	}
	WriteReport(r.Context(), w, rep)
}

// WriteReport writes a DeliveryReport as the 200 PushDeliveryReportResponse envelope (shared with
// POST /news/notifications).
func WriteReport(ctx context.Context, w http.ResponseWriter, rep DeliveryReport) {
	httpx.WriteJSON(w, http.StatusOK, openapi.PushDeliveryReportResponse{
		Data: openapi.PushDeliveryReport{Attempted: rep.Attempted, Delivered: rep.Delivered,
			Pruned: rep.Pruned, Failed: rep.Failed},
		Meta: newOpenAPIMeta(ctx),
	})
}

// toInfo maps the domain Subscription to the DTO: no endpoint, keys or owner ever leave the server.
func toInfo(s Subscription) openapi.PushSubscriptionInfo {
	id, label := s.ID, s.DeviceLabel
	created, updated := s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	info := openapi.PushSubscriptionInfo{Id: &id, DeviceLabel: &label, CreatedAt: &created, UpdatedAt: &updated}
	if s.LastSuccessAt != nil {
		last := s.LastSuccessAt.UTC()
		info.LastSuccessAt = &last
	}
	return info
}

// newOpenAPIMeta builds the generated openapi.Meta for the response shapes that carry their own
// meta field instead of an httpx envelope; its requestId is the request's X-Request-Id
// (httpx.NewMetaFrom), the id the access and error logs carry (task 182 / F4.2).
func newOpenAPIMeta(ctx context.Context) openapi.Meta {
	m := httpx.NewMetaFrom(ctx)
	id := types.UUID(m.RequestID)
	ts := m.Timestamp
	return openapi.Meta{RequestId: &id, Timestamp: &ts}
}
