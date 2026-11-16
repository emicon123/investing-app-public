// Package push sends Web Push notifications (RFC 8030 / 8291 / 8292) to the browsers stored in
// push_subscriptions (ADR-108, task 172). Service is the Facade every consumer calls and owns the
// send-result policy; webpushSender is the only file that touches the webpush-go library.
//
// Write ownership: this package writes ONLY push_subscriptions.
//
// Secrets: a subscription's endpoint is a capability URL and p256dh/auth are key material; the VAPID
// private key is a secret. None of them is ever logged or returned in a DTO. Logs carry the row id,
// the endpoint's host, the HTTP status and counts.
package push

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// Urgency is the RFC 8030 Urgency header value.
type Urgency string

const (
	UrgencyLow    Urgency = "low"
	UrgencyNormal Urgency = "normal"
	UrgencyHigh   Urgency = "high"
)

// Notification is one message to show. URL is a relative path ("/news"); Topic is optional.
type Notification struct {
	Title, Body, URL, Tag, Topic string
	Urgency                      Urgency
	TTL                          time.Duration
}

// TestNotification is what POST /push/test-notifications sends (ADR-108 §5).
var TestNotification = Notification{Title: "Powiadomienie testowe",
	Body: "Powiadomienia działają na tym urządzeniu.", URL: "/news", Tag: "push-test",
	Urgency: UrgencyNormal, TTL: 5 * time.Minute}

// Subscription is one stored browser subscription (domain shape of a push_subscriptions row).
type Subscription struct {
	ID            int64
	UserEmail     string
	Endpoint      string
	P256dh        string
	Auth          string
	DeviceLabel   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastSuccessAt *time.Time
}

// SubscribeInput is what the handler passes to Service.Subscribe.
type SubscribeInput struct{ UserEmail, Endpoint, P256dh, Auth, UserAgent string }

// DeliveryReport is the fan-out result; Attempted = Delivered + Pruned + Failed.
type DeliveryReport struct{ Attempted, Delivered, Pruned, Failed int }

// Config holds the VAPID key pair and subject (VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY / VAPID_SUBJECT).
type Config struct{ PublicKey, PrivateKey, Subject string }

// LoadConfig reads the three VAPID_* variables. Empty values mean push is disabled.
func LoadConfig() Config {
	return Config{
		PublicKey:  strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
		PrivateKey: strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
		Subject:    strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),
	}
}

// Enabled reports whether all three values are set and Subject starts with mailto: or https:.
func (c Config) Enabled() bool {
	if strings.TrimSpace(c.PublicKey) == "" || strings.TrimSpace(c.PrivateKey) == "" {
		return false
	}
	s := strings.TrimSpace(c.Subject)
	return (strings.HasPrefix(s, "mailto:") && len(s) > len("mailto:")) ||
		(strings.HasPrefix(s, "https:") && len(s) > len("https:"))
}

// Partial reports a misconfiguration worth one startup WARN: some VAPID value is set, but push is
// still disabled (another value is missing or the subject has no mailto:/https: prefix).
func (c Config) Partial() bool {
	anySet := strings.TrimSpace(c.PublicKey) != "" || strings.TrimSpace(c.PrivateKey) != "" ||
		strings.TrimSpace(c.Subject) != ""
	return anySet && !c.Enabled()
}

// ErrNotConfigured is returned by Subscribe, Broadcast and SendToUser when Config is not Enabled.
var ErrNotConfigured = errors.New("push: not configured")

// CodePushNotConfigured is the ErrorResponse code for a 503 from a push route (OpenAPI PushNotConfigured).
const CodePushNotConfigured = "PUSH_NOT_CONFIGURED"

// DefaultAllowedHosts are the browser push services a subscription endpoint may point at (SSRF
// guard, ADR-108 §4): the host equals one of these or ends in "."+entry.
var DefaultAllowedHosts = []string{"fcm.googleapis.com", "push.services.mozilla.com",
	"push.apple.com", "notify.windows.com"}

// maxEndpointLen mirrors the push_subscriptions_endpoint_https CHECK and the OpenAPI maxLength.
const maxEndpointLen = 2048

// endpointAllowed reports whether raw is an https URL, at most 2048 chars, without userinfo, whose
// host is a DNS name on (or under) one of hosts. IP literals are always rejected.
func endpointAllowed(raw string, hosts []string) bool {
	if raw == "" || len(raw) > maxEndpointLen {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || net.ParseIP(host) != nil {
		return false
	}
	for _, h := range hosts {
		h = strings.ToLower(h)
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// endpointHost returns the endpoint's host for log lines (never the full endpoint).
func endpointHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid"
	}
	return u.Hostname()
}

const (
	unknownDevice  = "Nieznane urządzenie"
	maxDeviceLabel = 100
)

// DeviceLabel derives a short "OS · Browser" label from a User-Agent, e.g. "Android · Chrome".
// It returns "Nieznane urządzenie" when nothing is recognised, and at most 100 characters.
func DeviceLabel(userAgent string) string {
	platform, browser := uaOS(userAgent), uaBrowser(userAgent)
	var label string
	switch {
	case platform != "" && browser != "":
		label = platform + " · " + browser
	case platform != "":
		label = platform
	case browser != "":
		label = browser
	default:
		return unknownDevice
	}
	if utf8.RuneCountInString(label) > maxDeviceLabel {
		label = string([]rune(label)[:maxDeviceLabel])
	}
	return label
}

func uaOS(ua string) string {
	switch {
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"), strings.Contains(ua, "iPod"):
		return "iOS"
	case strings.Contains(ua, "CrOS"):
		return "ChromeOS"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		return "macOS"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	}
	return ""
}

// uaBrowser checks the tokens most specific first: Edge, Opera and Samsung Internet all also
// carry "Chrome/", and every Chromium/iOS browser also carries "Safari/".
func uaBrowser(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "EdgA/"), strings.Contains(ua, "EdgiOS/"):
		return "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		return "Opera"
	case strings.Contains(ua, "SamsungBrowser/"):
		return "Samsung Internet"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		return "Firefox"
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "CriOS/"):
		return "Chrome"
	case strings.Contains(ua, "Safari/"):
		return "Safari"
	}
	return ""
}
