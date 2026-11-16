package auth

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"investing-app/common-go/httpx"
)

// invalidTokenMessage is the one fixed 401 body text for a bearer token that was presented but
// rejected (F3.7): the validation error — signature, expiry, issuer, allowlist — never reaches
// the wire, it goes to the log.
const invalidTokenMessage = "Invalid or expired token"

// captureLogs routes slog's default logger into a buffer for the duration of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// signClaims signs arbitrary claims HS256 with secret, for tokens IssueBackendJWT would never
// produce (expired, no exp).
func signClaims(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) httpx.ErrorDetail {
	t.Helper()
	var body httpx.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, rec.Body.String())
	}
	if len(body.Errors) != 1 {
		t.Fatalf("expected exactly one error detail, got %v", body.Errors)
	}
	return body.Errors[0]
}

// rejectedTokenCases are bearer tokens that must all answer the same fixed 401 text while the
// log carries the specific reason.
func rejectedTokenCases(t *testing.T, cfg Config) []struct {
	name      string
	token     string
	logDetail string // a substring only the log line may contain
} {
	t.Helper()
	otherSecretCfg := cfg
	otherSecretCfg.JWTSecret = "a-completely-different-secret-value-32c"
	wrongSig, _, err := IssueBackendJWT(otherSecretCfg, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	notAllowed, _, err := IssueBackendJWT(cfg, "mallory@example.com")
	if err != nil {
		t.Fatal(err)
	}
	expired := signClaims(t, cfg.JWTSecret, jwt.MapClaims{
		"iss":   cfg.JWTIssuer,
		"sub":   "alice@example.com",
		"email": "alice@example.com",
		"exp":   time.Now().Add(-time.Hour).Unix(),
	})
	return []struct {
		name      string
		token     string
		logDetail string
	}{
		{"wrong signature", wrongSig, "signature is invalid"},
		{"expired", expired, "token is expired"},
		{"email not in allowlist", notAllowed, "mallory@example.com"},
		{"garbage", "garbage", "auth: invalid token"},
	}
}

func assertRejectedWithFixedBody(t *testing.T, rec *httptest.ResponseRecorder, logs, logDetail string) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	detail := decodeError(t, rec)
	if detail.Code != httpx.CodeUnauthorized {
		t.Errorf("expected code %q, got %q", httpx.CodeUnauthorized, detail.Code)
	}
	if detail.Message != invalidTokenMessage {
		t.Errorf("expected message exactly %q, got %q", invalidTokenMessage, detail.Message)
	}
	if strings.Contains(rec.Body.String(), logDetail) {
		t.Errorf("response body leaks validation detail %q: %s", logDetail, rec.Body.String())
	}
	if !strings.Contains(logs, "auth: token rejected") {
		t.Errorf("expected an %q log line, got: %s", "auth: token rejected", logs)
	}
	if !strings.Contains(logs, logDetail) {
		t.Errorf("expected the log line to carry %q, got: %s", logDetail, logs)
	}
}

func TestRequireAuth_RejectedTokenAnswersFixedMessageAndLogsDetail(t *testing.T) {
	cfg := testConfig()
	for _, c := range rejectedTokenCases(t, cfg) {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLogs(t)
			h := RequireAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler should not be reached")
			}))
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+c.token)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assertRejectedWithFixedBody(t, rec, logs.String(), c.logDetail)
		})
	}
}

func TestRequireAuthWithPrincipal_RejectedTokenAnswersFixedMessage(t *testing.T) {
	cfg := testConfig()
	for _, c := range rejectedTokenCases(t, cfg) {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLogs(t)
			h := RequireAuthWithPrincipal(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler should not be reached")
			}))
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer "+c.token)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assertRejectedWithFixedBody(t, rec, logs.String(), c.logDetail)
		})
	}
}

// The no-token case keeps its own text: it tells the caller what to send, leaks nothing.
func TestRequireAuth_MissingTokenKeepsBearerMissingMessage(t *testing.T) {
	h := RequireAuth(testConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if got := decodeError(t, rec).Message; got != "Bearer token missing or invalid" {
		t.Errorf("unexpected message %q", got)
	}
}

// RequireAuthOrServiceToken never echoed the validation error, but a rejected bearer that
// finds no service token to fall back on must now be diagnosable from the log too.
func TestRequireAuthOrServiceToken_RejectedBearerLogsDetail(t *testing.T) {
	cfg := testConfig()
	svcCfg := ServiceTokenConfig{Token: "machine-secret"}
	for _, c := range rejectedTokenCases(t, cfg) {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLogs(t)
			h := RequireAuthOrServiceToken(cfg, svcCfg)(okHandler())
			req := httptest.NewRequest(http.MethodGet, "/api/bot/calendar", nil)
			req.Header.Set("Authorization", "Bearer "+c.token)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rec.Code)
			}
			if strings.Contains(rec.Body.String(), c.logDetail) {
				t.Errorf("response body leaks validation detail %q: %s", c.logDetail, rec.Body.String())
			}
			if !strings.Contains(logs.String(), "auth: token rejected") || !strings.Contains(logs.String(), c.logDetail) {
				t.Errorf("expected the rejection detail %q in the log, got: %s", c.logDetail, logs.String())
			}
		})
	}
}
