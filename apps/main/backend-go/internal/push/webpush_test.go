package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"investing-app/backend-go/db/sqlcgen"
)

// stubHost resolves to the httptest server; the httptest certificate covers *.example.com.
const stubHost = "push.example.com"

// pushStub stands in for a browser push service: the path's last segment is the status it answers.
type pushStub struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
	body [][]byte
}

func newPushStub(t *testing.T) *pushStub {
	t.Helper()
	p := &pushStub{}
	p.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.reqs, p.body = append(p.reqs, r), append(p.body, b)
		p.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/404"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/410"):
			w.WriteHeader(http.StatusGone)
		case strings.HasSuffix(r.URL.Path, "/429"):
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusCreated)
		}
		_, _ = io.WriteString(w, "stub body")
	}))
	t.Cleanup(p.srv.Close)
	return p
}

// client trusts the stub's certificate and dials the stub for every host.
func (p *pushStub) client() *http.Client {
	c := p.srv.Client()
	tr := c.Transport.(*http.Transport).Clone()
	addr := p.srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	c.Transport = tr
	c.Timeout = 5 * time.Second
	return c
}

func (p *pushStub) endpoint(status string) string {
	return "https://" + stubHost + "/wp/" + status
}

// throwawayConfig generates a VAPID pair for this test only. Never a real key.
func throwawayConfig(t *testing.T) Config {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return Config{PublicKey: pub, PrivateKey: priv, Subject: "mailto:owner@example.com"}
}

// browserKeys generates a throwaway browser subscription key pair (p256dh, auth).
func browserKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(secret)
}

func TestPayload(t *testing.T) {
	n := Notification{Title: "T", Body: "B", URL: "/news", Tag: "news-report", Topic: "news-report",
		Urgency: UrgencyHigh, TTL: time.Hour}
	b, err := payload(n)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"title": "T", "body": "B", "url": "/news", "tag": "news-report"}
	if len(got) != len(want) {
		t.Fatalf("payload keys %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("payload[%s] = %v, want %v", k, got[k], v)
		}
	}
}

func TestSubscriber(t *testing.T) {
	cases := map[string]string{
		"mailto:owner@example.com": "owner@example.com",
		"https://example.com":      "https://example.com",
	}
	for in, want := range cases {
		if got := subscriber(in); got != want {
			t.Fatalf("subscriber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWebPushSender_Headers(t *testing.T) {
	stub := newPushStub(t)
	cfg := throwawayConfig(t)
	p256dh, auth := browserKeys(t)
	sender := NewWebPushSender(cfg, stub.client())

	n := Notification{Title: "Raport", Body: "x", URL: "/news", Tag: "news-report", Topic: "news-report",
		Urgency: UrgencyLow, TTL: 12 * time.Hour}
	status, err := sender.Send(context.Background(), Subscription{ID: 1, Endpoint: stub.endpoint("201"),
		P256dh: p256dh, Auth: auth}, n)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("Send = %d, %v", status, err)
	}
	if len(stub.reqs) != 1 {
		t.Fatalf("stub saw %d requests", len(stub.reqs))
	}
	h := stub.reqs[0].Header
	if got := h.Get("Content-Encoding"); got != "aes128gcm" {
		t.Errorf("Content-Encoding = %q", got)
	}
	if got := h.Get("Authorization"); !strings.HasPrefix(got, "vapid t=") || !strings.HasSuffix(got, ", k="+cfg.PublicKey) {
		t.Errorf("Authorization header has the wrong shape")
	}
	if got := h.Get("TTL"); got != "43200" {
		t.Errorf("TTL = %q", got)
	}
	if got := h.Get("Urgency"); got != "low" {
		t.Errorf("Urgency = %q", got)
	}
	if got := h.Get("Topic"); got != "news-report" {
		t.Errorf("Topic = %q", got)
	}
	if strings.Contains(string(stub.body[0]), "Raport") {
		t.Error("payload sent in clear text")
	}

	// The VAPID JWT subject carries mailto: exactly once.
	tok := strings.TrimSuffix(strings.TrimPrefix(h.Get("Authorization"), "vapid t="), ", k="+cfg.PublicKey)
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts", len(parts))
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var c struct{ Sub, Aud string }
	if err := json.Unmarshal(claims, &c); err != nil {
		t.Fatal(err)
	}
	if c.Sub != "mailto:owner@example.com" || c.Aud != "https://"+stubHost {
		t.Errorf("JWT claims sub=%q aud=%q", c.Sub, c.Aud)
	}
}

func TestWebPushSender_OmitsEmptyTopic(t *testing.T) {
	stub := newPushStub(t)
	p256dh, auth := browserKeys(t)
	sender := NewWebPushSender(throwawayConfig(t), stub.client())
	if _, err := sender.Send(context.Background(), Subscription{Endpoint: stub.endpoint("201"), P256dh: p256dh, Auth: auth},
		TestNotification); err != nil {
		t.Fatal(err)
	}
	h := stub.reqs[0].Header
	if _, ok := h["Topic"]; ok {
		t.Error("Topic header sent for a notification without a topic")
	}
	if h.Get("TTL") != "300" || h.Get("Urgency") != "normal" {
		t.Errorf("TTL=%q Urgency=%q", h.Get("TTL"), h.Get("Urgency"))
	}
}

func TestWebPushSender_NetworkErrorHidesEndpoint(t *testing.T) {
	p256dh, auth := browserKeys(t)
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Err: io.ErrUnexpectedEOF}
		}}}
	secretPath := "secret-capability-path"
	status, err := NewWebPushSender(throwawayConfig(t), client).Send(context.Background(),
		Subscription{Endpoint: "https://" + stubHost + "/" + secretPath, P256dh: p256dh, Auth: auth}, TestNotification)
	if err == nil || status != 0 {
		t.Fatalf("expected a network error, got %d %v", status, err)
	}
	if strings.Contains(err.Error(), secretPath) || strings.Contains(err.Error(), stubHost) {
		t.Fatalf("error text leaks the endpoint: %v", err)
	}
}

// TestWebPushSender_ServicePolicy drives push.Service end to end through the real adapter:
// 201 delivered, 404 and 410 pruned (row gone), 429 failed (row kept).
func TestWebPushSender_ServicePolicy(t *testing.T) {
	pool := startPool(t)
	ctx := context.Background()
	stub := newPushStub(t)
	cfg := throwawayConfig(t)
	svc := NewService(sqlcgen.New(pool), NewWebPushSender(cfg, stub.client()), cfg, []string{"example.com"})

	statuses := []string{"201", "404", "410", "429"}
	for _, s := range statuses {
		p256dh, auth := browserKeys(t)
		if _, err := svc.Subscribe(ctx, SubscribeInput{UserEmail: "a@example.com", Endpoint: stub.endpoint(s),
			P256dh: p256dh, Auth: auth}); err != nil {
			t.Fatalf("Subscribe %s: %v", s, err)
		}
	}
	rep, err := svc.Broadcast(ctx, TestNotification)
	if err != nil {
		t.Fatal(err)
	}
	if want := (DeliveryReport{Attempted: 4, Delivered: 1, Pruned: 2, Failed: 1}); rep != want {
		t.Fatalf("report %+v, want %+v", rep, want)
	}
	rows, err := pool.Query(ctx, `SELECT endpoint, last_success_at IS NOT NULL FROM push_subscriptions ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var ep string
		var ok bool
		if err := rows.Scan(&ep, &ok); err != nil {
			t.Fatal(err)
		}
		got[ep] = ok
	}
	if len(got) != 2 || !got[stub.endpoint("201")] {
		t.Fatalf("kept rows %v: want 201 (delivered) and 429 (failed)", got)
	}
	if ok, kept := got[stub.endpoint("429")]; !kept || ok {
		t.Fatalf("429 row kept=%v success=%v", kept, ok)
	}
}
