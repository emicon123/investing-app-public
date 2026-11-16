package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// webpushSender is the Adapter over webpush-go (ADR-108 §1): the only file that imports it.
// It encrypts with RFC 8291 aes128gcm, signs the VAPID JWT and posts to the subscription endpoint.
type webpushSender struct {
	publicKey  string
	privateKey string
	subscriber string
	client     *http.Client
}

// NewWebPushSender builds the production Sender. client should carry a 5 s Timeout.
func NewWebPushSender(cfg Config, client *http.Client) Sender {
	return &webpushSender{publicKey: cfg.PublicKey, privateKey: cfg.PrivateKey,
		subscriber: subscriber(cfg.Subject), client: client}
}

// Send posts one encrypted message and returns the push service's HTTP status. A transport error
// returns status 0 and an error whose text never contains the endpoint.
func (w *webpushSender) Send(ctx context.Context, sub Subscription, n Notification) (int, error) {
	body, err := payload(n)
	if err != nil {
		return 0, err
	}
	resp, err := webpush.SendNotificationWithContext(ctx, body,
		&webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}},
		&webpush.Options{
			HTTPClient:      w.client,
			Subscriber:      w.subscriber,
			Topic:           n.Topic,
			TTL:             int(n.TTL.Seconds()),
			Urgency:         webpush.Urgency(n.Urgency),
			VAPIDPublicKey:  w.publicKey,
			VAPIDPrivateKey: w.privateKey,
		})
	if err != nil {
		return 0, stripURL(err)
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

// payload is the JSON the service worker reads: {title, body, url, tag} (design doc §9).
func payload(n Notification) ([]byte, error) {
	return json.Marshal(struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		URL   string `json:"url"`
		Tag   string `json:"tag"`
	}{n.Title, n.Body, n.URL, n.Tag})
}

// subscriber strips a leading "mailto:": webpush-go adds it itself unless the value is https:.
func subscriber(subject string) string {
	return strings.TrimPrefix(strings.TrimSpace(subject), "mailto:")
}

// stripURL drops the *url.Error wrapper, whose text embeds the endpoint (a capability URL).
func stripURL(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("push: %s request: %w", ue.Op, ue.Err)
	}
	return fmt.Errorf("push: send: %w", err)
}
