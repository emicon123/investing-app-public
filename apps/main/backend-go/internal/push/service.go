package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
)

// Sender encrypts and posts one message to a push service (the Adapter seam, ADR-108 §10).
type Sender interface {
	// Send returns the push service's HTTP status (0 with a non-nil err on a network error or
	// timeout). It never logs the endpoint.
	Send(ctx context.Context, sub Subscription, n Notification) (status int, err error)
}

// Queries is the narrow slice of *sqlcgen.Queries this service calls (house convention).
type Queries interface {
	UpsertPushSubscription(ctx context.Context, arg sqlcgen.UpsertPushSubscriptionParams) (sqlcgen.PushSubscription, error)
	DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) error
	DeletePushSubscriptionByID(ctx context.Context, id int64) error
	ListPushSubscriptions(ctx context.Context) ([]sqlcgen.PushSubscription, error)
	ListPushSubscriptionsByUser(ctx context.Context, userEmail string) ([]sqlcgen.PushSubscription, error)
	MarkPushSubscriptionSuccess(ctx context.Context, id int64) error
}

// Service is the Facade every push consumer calls. It owns the endpoint allowlist, the send-result
// policy (ADR-108 §4) and ErrNotConfigured when the VAPID config is incomplete.
type Service struct {
	queries      Queries
	sender       Sender
	cfg          Config
	allowedHosts []string
}

// NewService wires the store, the sender, the VAPID config and the endpoint host allowlist.
func NewService(q Queries, s Sender, cfg Config, allowedHosts []string) *Service {
	return &Service{queries: q, sender: s, cfg: cfg, allowedHosts: allowedHosts}
}

// Config reports whether push is enabled and, if so, the VAPID public key for the browser.
func (s *Service) Config() (enabled bool, publicKey string) {
	if !s.cfg.Enabled() {
		return false, ""
	}
	return true, s.cfg.PublicKey
}

// Key length bounds mirror the OpenAPI PushSubscriptionKeys schema.
const (
	minP256dhLen, maxP256dhLen = 80, 128
	minAuthLen, maxAuthLen     = 16, 64
)

// Subscribe validates and upserts one browser subscription on its endpoint.
func (s *Service) Subscribe(ctx context.Context, in SubscribeInput) (Subscription, error) {
	if !s.cfg.Enabled() {
		return Subscription{}, ErrNotConfigured
	}
	if !endpointAllowed(in.Endpoint, s.allowedHosts) {
		return Subscription{}, unprocessable("endpoint", "endpoint host is not a known push service")
	}
	if n := len(in.P256dh); n < minP256dhLen || n > maxP256dhLen {
		return Subscription{}, unprocessable("keys.p256dh", fmt.Sprintf("length must be between %d and %d", minP256dhLen, maxP256dhLen))
	}
	if n := len(in.Auth); n < minAuthLen || n > maxAuthLen {
		return Subscription{}, unprocessable("keys.auth", fmt.Sprintf("length must be between %d and %d", minAuthLen, maxAuthLen))
	}
	row, err := s.queries.UpsertPushSubscription(ctx, sqlcgen.UpsertPushSubscriptionParams{
		UserEmail: in.UserEmail, Endpoint: in.Endpoint, P256dh: in.P256dh, Auth: in.Auth,
		DeviceLabel: DeviceLabel(in.UserAgent),
	})
	if err != nil {
		return Subscription{}, fmt.Errorf("push: upsert subscription: %w", err)
	}
	slog.Info("push subscription stored", "id", row.ID, "host", endpointHost(row.Endpoint), "device", row.DeviceLabel)
	return toSubscription(row), nil
}

// Unsubscribe forgets the subscription with this endpoint. Idempotent, and it works even when push
// is not configured so a device can always clean up.
func (s *Service) Unsubscribe(ctx context.Context, endpoint string) error {
	if endpoint == "" || len(endpoint) > maxEndpointLen {
		return unprocessable("endpoint", fmt.Sprintf("must be 1 to %d characters", maxEndpointLen))
	}
	if err := s.queries.DeletePushSubscriptionByEndpoint(ctx, endpoint); err != nil {
		return fmt.Errorf("push: delete subscription: %w", err)
	}
	return nil
}

// Broadcast sends n to every stored subscription.
func (s *Service) Broadcast(ctx context.Context, n Notification) (DeliveryReport, error) {
	if !s.cfg.Enabled() {
		return DeliveryReport{}, ErrNotConfigured
	}
	rows, err := s.queries.ListPushSubscriptions(ctx)
	if err != nil {
		return DeliveryReport{}, fmt.Errorf("push: list subscriptions: %w", err)
	}
	return s.sendAll(ctx, rows, n), nil
}

// SendToUser sends n only to the subscriptions owned by email.
func (s *Service) SendToUser(ctx context.Context, email string, n Notification) (DeliveryReport, error) {
	if !s.cfg.Enabled() {
		return DeliveryReport{}, ErrNotConfigured
	}
	rows, err := s.queries.ListPushSubscriptionsByUser(ctx, email)
	if err != nil {
		return DeliveryReport{}, fmt.Errorf("push: list subscriptions by user: %w", err)
	}
	return s.sendAll(ctx, rows, n), nil
}

// sendAll sends sequentially (a handful of devices) and applies the ADR-108 §4 result policy:
// 2xx marks success, 404/410 deletes the row, anything else keeps it and counts as failed.
func (s *Service) sendAll(ctx context.Context, rows []sqlcgen.PushSubscription, n Notification) DeliveryReport {
	var rep DeliveryReport
	for _, row := range rows {
		rep.Attempted++
		sub := toSubscription(row)
		host := endpointHost(sub.Endpoint)
		status, err := s.sender.Send(ctx, sub, n)
		switch {
		case err != nil:
			rep.Failed++
			slog.Warn("push send failed", "id", sub.ID, "host", host, "error", errKind(err))
		case status >= 200 && status < 300:
			rep.Delivered++
			if err := s.queries.MarkPushSubscriptionSuccess(ctx, sub.ID); err != nil {
				slog.Error("push mark success failed", "id", sub.ID, "error", err)
			}
		case status == http.StatusNotFound || status == http.StatusGone:
			if err := s.queries.DeletePushSubscriptionByID(ctx, sub.ID); err != nil {
				rep.Failed++
				slog.Error("push prune failed", "id", sub.ID, "status", status, "error", err)
				continue
			}
			rep.Pruned++
			slog.Info("push subscription pruned", "id", sub.ID, "host", host, "status", status)
		case status == http.StatusTooManyRequests || status >= 500 ||
			status == http.StatusUnauthorized || status == http.StatusForbidden:
			rep.Failed++
			slog.Warn("push send rejected", "id", sub.ID, "host", host, "status", status)
		default:
			rep.Failed++
			slog.Error("push send rejected", "id", sub.ID, "host", host, "status", status)
		}
	}
	slog.Info("push fan-out done", "tag", n.Tag, "attempted", rep.Attempted, "delivered", rep.Delivered,
		"pruned", rep.Pruned, "failed", rep.Failed)
	return rep
}

// errKind reduces a send error to a category: the error text of a failed HTTP call embeds the
// endpoint URL, which must never be logged.
func errKind(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "network"
	}
}

func toSubscription(r sqlcgen.PushSubscription) Subscription {
	return Subscription{
		ID: r.ID, UserEmail: r.UserEmail, Endpoint: r.Endpoint, P256dh: r.P256dh, Auth: r.Auth,
		DeviceLabel: r.DeviceLabel, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
		LastSuccessAt: timePtr(r.LastSuccessAt),
	}
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: http.StatusUnprocessableEntity, Code: httpx.CodeValidationError, Field: field, Message: message}
}
