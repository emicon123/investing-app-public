package push

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/auth"
	"investing-app/common-go/httpx"
)

const androidUA = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36"

func handlerAuthConfig() auth.Config {
	return auth.Config{AllowedEmails: []string{"owner@example.com", "other@example.com"},
		JWTSecret: "test-secret-with-enough-length-32b", JWTIssuer: "investing-app", JWTExpiryDays: 1}
}

// call runs one request through the real bearer-JWT middleware, as the router does.
func call(t *testing.T, h http.HandlerFunc, email, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	cfg := handlerAuthConfig()
	token, _, err := auth.IssueBackendJWT(cfg, email)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, "/api/push/x", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", androidUA)
	rec := httptest.NewRecorder()
	auth.RequireAuthWithPrincipal(cfg)(h).ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) (code, field string) {
	t.Helper()
	var body httpx.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 {
		t.Fatalf("not an ErrorResponse: %s", rec.Body.String())
	}
	return body.Errors[0].Code, body.Errors[0].Field
}

func subscribeBody(endpoint string) string {
	return `{"endpoint":"` + endpoint + `","keys":{"p256dh":"` + fakeP256dh + `","auth":"` + fakeAuth + `"},"expirationTime":null}`
}

func TestHandler_Disabled(t *testing.T) {
	h := NewHandler(NewService(nil, &fakeSender{}, Config{}, DefaultAllowedHosts))

	rec := call(t, h.GetPushConfig, "owner@example.com", http.MethodGet, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) == "" {
		t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
	}
	var cfg struct {
		Data map[string]any `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Data["enabled"] != false || cfg.Meta == nil {
		t.Fatalf("config body: %s", rec.Body.String())
	}
	if _, has := cfg.Data["vapidPublicKey"]; has {
		t.Fatalf("disabled config leaks a key field: %s", rec.Body.String())
	}

	for name, fn := range map[string]http.HandlerFunc{"subscribe": h.UpsertPushSubscription, "test": h.SendTestPushNotification} {
		rec := call(t, fn, "owner@example.com", http.MethodPost, subscribeBody(fcm("d")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if code, _ := errorCode(t, rec); code != CodePushNotConfigured {
			t.Fatalf("%s code %s", name, code)
		}
	}
}

func TestMapError(t *testing.T) {
	var appErr *httpx.AppError
	if !errors.As(MapError(ErrNotConfigured), &appErr) || appErr.Status != http.StatusServiceUnavailable ||
		appErr.Code != CodePushNotConfigured {
		t.Fatalf("ErrNotConfigured not mapped to 503: %#v", MapError(ErrNotConfigured))
	}
	other := errors.New("db down")
	if !errors.Is(MapError(other), other) {
		t.Fatal("other errors must pass through")
	}
}

func TestHandler_RealPostgres(t *testing.T) {
	pool := startPool(t)
	ctx := context.Background()
	sender := &fakeSender{}
	enabled := NewHandler(NewService(sqlcgen.New(pool), sender, enabledCfg, DefaultAllowedHosts))
	disabled := NewHandler(NewService(sqlcgen.New(pool), sender, Config{}, DefaultAllowedHosts))

	t.Run("config enabled carries the public key", func(t *testing.T) {
		rec := call(t, enabled.GetPushConfig, "owner@example.com", http.MethodGet, "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"vapidPublicKey":"pub"`) ||
			!strings.Contains(rec.Body.String(), `"enabled":true`) {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("subscribe stores the principal and derived label, never echoes keys", func(t *testing.T) {
		truncate(t, pool)
		rec := call(t, enabled.UpsertPushSubscription, "owner@example.com", http.MethodPost, subscribeBody(fcm("h1")))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, secret := range []string{fcm("h1"), fakeP256dh, fakeAuth, "owner@example.com"} {
			if strings.Contains(body, secret) {
				t.Fatalf("response echoes subscription data: %s", body)
			}
		}
		var resp struct {
			Data struct {
				ID            int64   `json:"id"`
				DeviceLabel   string  `json:"deviceLabel"`
				CreatedAt     string  `json:"createdAt"`
				UpdatedAt     string  `json:"updatedAt"`
				LastSuccessAt *string `json:"lastSuccessAt"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Data.ID == 0 || resp.Data.DeviceLabel != "Android · Chrome" || resp.Data.CreatedAt == "" ||
			resp.Data.UpdatedAt == "" || resp.Data.LastSuccessAt != nil || !strings.HasSuffix(resp.Data.CreatedAt, "Z") {
			t.Fatalf("unexpected data: %s", body)
		}
		var email string
		if err := pool.QueryRow(ctx, `SELECT user_email FROM push_subscriptions WHERE id = $1`, resp.Data.ID).Scan(&email); err != nil {
			t.Fatal(err)
		}
		if email != "owner@example.com" {
			t.Fatalf("user_email = %q", email)
		}
	})

	t.Run("subscribe validation is 422", func(t *testing.T) {
		cases := map[string]struct{ body, field string }{
			"malformed":     {`{"endpoint":`, ""},
			"foreign host":  {subscribeBody("https://evil.example.com/x"), "endpoint"},
			"missing keys":  {`{"endpoint":"` + fcm("k") + `"}`, "keys.p256dh"},
			"wrong type":    {`{"endpoint":42}`, ""},
			"http endpoint": {subscribeBody("http://fcm.googleapis.com/x"), "endpoint"},
		}
		for name, tc := range cases {
			rec := call(t, enabled.UpsertPushSubscription, "owner@example.com", http.MethodPost, tc.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
			}
			if code, field := errorCode(t, rec); code != httpx.CodeValidationError || field != tc.field {
				t.Fatalf("%s: code %s field %q", name, code, field)
			}
		}
	})

	t.Run("test notification reaches only the caller's devices", func(t *testing.T) {
		truncate(t, pool)
		call(t, enabled.UpsertPushSubscription, "owner@example.com", http.MethodPost, subscribeBody(fcm("mine")))
		call(t, enabled.UpsertPushSubscription, "other@example.com", http.MethodPost, subscribeBody(fcm("theirs")))
		sender.reset(nil)
		rec := call(t, enabled.SendTestPushNotification, "owner@example.com", http.MethodPost, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"attempted":1,"delivered":1,"failed":0,"pruned":0`) {
			t.Fatalf("report: %s", rec.Body.String())
		}
		if len(sender.calls) != 1 || sender.calls[0] != fcm("mine") || sender.lastMsg != TestNotification {
			t.Fatalf("sent to %d endpoints, msg %+v", len(sender.calls), sender.lastMsg)
		}
	})

	t.Run("delete is 204 even when disabled, and idempotent", func(t *testing.T) {
		truncate(t, pool)
		call(t, enabled.UpsertPushSubscription, "owner@example.com", http.MethodPost, subscribeBody(fcm("gone")))
		for i := range 2 {
			rec := call(t, disabled.DeletePushSubscription, "owner@example.com", http.MethodDelete, `{"endpoint":"`+fcm("gone")+`"}`)
			if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
				t.Fatalf("delete #%d: %d %s", i, rec.Code, rec.Body.String())
			}
		}
		if n := countRows(t, pool); n != 0 {
			t.Fatalf("row not deleted: %d", n)
		}
		rec := call(t, disabled.DeletePushSubscription, "owner@example.com", http.MethodDelete, `{}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("empty delete body: %d", rec.Code)
		}
	})
}
