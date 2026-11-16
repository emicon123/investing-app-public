package push

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestConfigEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"all set mailto", Config{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:owner@example.com"}, true},
		{"all set https", Config{PublicKey: "pub", PrivateKey: "priv", Subject: "https://example.com"}, true},
		{"nothing set", Config{}, false},
		{"missing public", Config{PrivateKey: "priv", Subject: "mailto:a@b.c"}, false},
		{"missing private", Config{PublicKey: "pub", Subject: "mailto:a@b.c"}, false},
		{"missing subject", Config{PublicKey: "pub", PrivateKey: "priv"}, false},
		{"bare email subject", Config{PublicKey: "pub", PrivateKey: "priv", Subject: "owner@example.com"}, false},
		{"http subject", Config{PublicKey: "pub", PrivateKey: "priv", Subject: "http://example.com"}, false},
		{"whitespace only", Config{PublicKey: " ", PrivateKey: "priv", Subject: "mailto:a@b.c"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Enabled(); got != tc.want {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfigPartial(t *testing.T) {
	if (Config{}).Partial() {
		t.Fatal("empty config is not partial")
	}
	if !(Config{PublicKey: "pub"}).Partial() {
		t.Fatal("one var set is partial")
	}
	if (Config{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:a@b.c"}).Partial() {
		t.Fatal("fully enabled config is not partial")
	}
	if !(Config{PublicKey: "pub", PrivateKey: "priv", Subject: "a@b.c"}).Partial() {
		t.Fatal("all set with a bad subject counts as partial (WARN at startup)")
	}
}

func TestEndpointAllowed(t *testing.T) {
	cases := []struct {
		endpoint string
		want     bool
	}{
		{"https://fcm.googleapis.com/fcm/send/abc", true},
		{"https://updates.push.services.mozilla.com/wpush/v2/abc", true},
		{"https://web.push.apple.com/QGx", true},
		{"https://wns2-db5p.notify.windows.com/w/?token=x", true},
		{"https://FCM.googleapis.com/fcm/send/abc", true},
		{"https://fcm.googleapis.com:443/fcm/send/abc", true},
		{"http://fcm.googleapis.com/fcm/send/abc", false},
		{"https://fcm.googleapis.com.evil.com/x", false},
		{"https://evilfcm.googleapis.com/x", false},
		{"https://googleapis.com/x", false},
		{"https://142.250.1.1/fcm/send/x", false},
		{"https://[2001:db8::1]/x", false},
		{"https://user@fcm.googleapis.com/x", false},
		{"fcm.googleapis.com/fcm/send/abc", false},
		{"", false},
		{"https:///x", false},
		{"https://fcm.googleapis.com/" + strings.Repeat("a", 2048), false},
	}
	for _, tc := range cases {
		t.Run(tc.endpoint[:min(len(tc.endpoint), 60)], func(t *testing.T) {
			if got := endpointAllowed(tc.endpoint, DefaultAllowedHosts); got != tc.want {
				t.Fatalf("endpointAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDeviceLabel(t *testing.T) {
	cases := []struct {
		name, ua, want string
	}{
		{"empty", "", "Nieznane urządzenie"},
		{"android chrome", "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36", "Android · Chrome"},
		{"android samsung", "Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36", "Android · Samsung Internet"},
		{"android firefox", "Mozilla/5.0 (Android 14; Mobile; rv:131.0) Gecko/131.0 Firefox/131.0", "Android · Firefox"},
		{"iphone safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1", "iOS · Safari"},
		{"iphone chrome", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/130.0 Mobile/15E148 Safari/604.1", "iOS · Chrome"},
		{"windows edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0", "Windows · Edge"},
		{"mac safari", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15", "macOS · Safari"},
		{"linux firefox", "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", "Linux · Firefox"},
		{"windows opera", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 OPR/115.0.0.0", "Windows · Opera"},
		{"unknown agent", "curl/8.5.0", "Nieznane urządzenie"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeviceLabel(tc.ua); got != tc.want {
				t.Fatalf("DeviceLabel = %q, want %q", got, tc.want)
			}
		})
	}
	if got := DeviceLabel(strings.Repeat("ż", 500)); utf8.RuneCountInString(got) > 100 || !utf8.ValidString(got) {
		t.Fatalf("label must be valid UTF-8 and at most 100 chars, got %d", utf8.RuneCountInString(got))
	}
}
