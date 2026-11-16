package push

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/testsupport"
)

// Fake keys and endpoints only: these strings have the right shape but are not real key material.
var (
	fakeP256dh = "B" + strings.Repeat("x", 86)
	fakeAuth   = strings.Repeat("y", 22)
	enabledCfg = Config{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:owner@example.com"}
)

func fcm(id string) string { return "https://fcm.googleapis.com/fcm/send/" + id }

type sendResult struct {
	status int
	err    error
}

// fakeSender is scripted per endpoint (default 201) and records every call.
type fakeSender struct {
	mu      sync.Mutex
	script  map[string]sendResult
	calls   []string
	lastMsg Notification
}

func (f *fakeSender) Send(_ context.Context, sub Subscription, n Notification) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sub.Endpoint)
	f.lastMsg = n
	if r, ok := f.script[sub.Endpoint]; ok {
		return r.status, r.err
	}
	return http.StatusCreated, nil
}

func (f *fakeSender) reset(script map[string]sendResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script, f.calls = script, nil
}

func startPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	pool, err := pgxpool.New(context.Background(), pg.ConnString)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `TRUNCATE push_subscriptions`); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM push_subscriptions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func subscribe(t *testing.T, svc *Service, email, endpoint string) Subscription {
	t.Helper()
	sub, err := svc.Subscribe(context.Background(), SubscribeInput{UserEmail: email, Endpoint: endpoint,
		P256dh: fakeP256dh, Auth: fakeAuth, UserAgent: "Mozilla/5.0 (Linux; Android 14) Chrome/130.0 Mobile Safari/537.36"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return sub
}

func TestService_RealPostgres(t *testing.T) {
	pool := startPool(t)
	ctx := context.Background()
	sender := &fakeSender{}
	svc := NewService(sqlcgen.New(pool), sender, enabledCfg, DefaultAllowedHosts)
	disabled := NewService(sqlcgen.New(pool), sender, Config{}, DefaultAllowedHosts)

	t.Run("config", func(t *testing.T) {
		if on, key := svc.Config(); !on || key != "pub" {
			t.Fatalf("enabled config = %v %q", on, key)
		}
		if on, key := disabled.Config(); on || key != "" {
			t.Fatalf("disabled config = %v %q", on, key)
		}
	})

	t.Run("subscribe upserts on endpoint", func(t *testing.T) {
		truncate(t, pool)
		first := subscribe(t, svc, "a@example.com", fcm("one"))
		if first.DeviceLabel != "Android · Chrome" || first.LastSuccessAt != nil || first.ID == 0 {
			t.Fatalf("unexpected first row: %+v", first)
		}
		second, err := svc.Subscribe(ctx, SubscribeInput{UserEmail: "b@example.com", Endpoint: fcm("one"),
			P256dh: "B" + strings.Repeat("z", 86), Auth: strings.Repeat("w", 22), UserAgent: ""})
		if err != nil {
			t.Fatal(err)
		}
		if second.ID != first.ID || second.UserEmail != "b@example.com" || second.P256dh[1] != 'z' ||
			second.DeviceLabel != "Nieznane urządzenie" || second.UpdatedAt.Before(first.UpdatedAt) {
			t.Fatalf("upsert did not refresh in place: %+v", second)
		}
		if n := countRows(t, pool); n != 1 {
			t.Fatalf("expected one row, got %d", n)
		}
	})

	t.Run("subscribe validation is 422", func(t *testing.T) {
		truncate(t, pool)
		cases := []struct {
			name, endpoint, p256dh, auth, field string
		}{
			{"http endpoint", "http://fcm.googleapis.com/x", fakeP256dh, fakeAuth, "endpoint"},
			{"foreign host", "https://fcm.googleapis.com.evil.com/x", fakeP256dh, fakeAuth, "endpoint"},
			{"ip literal", "https://10.0.0.1/x", fakeP256dh, fakeAuth, "endpoint"},
			{"empty endpoint", "", fakeP256dh, fakeAuth, "endpoint"},
			{"short p256dh", fcm("v"), "B123", fakeAuth, "keys.p256dh"},
			{"long p256dh", fcm("v"), strings.Repeat("x", 129), fakeAuth, "keys.p256dh"},
			{"short auth", fcm("v"), fakeP256dh, "abc", "keys.auth"},
			{"long auth", fcm("v"), fakeP256dh, strings.Repeat("y", 65), "keys.auth"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := svc.Subscribe(ctx, SubscribeInput{UserEmail: "a@example.com", Endpoint: tc.endpoint,
					P256dh: tc.p256dh, Auth: tc.auth})
				var appErr *httpx.AppError
				if !errors.As(err, &appErr) || appErr.Status != http.StatusUnprocessableEntity ||
					appErr.Code != httpx.CodeValidationError || appErr.Field != tc.field {
					t.Fatalf("expected 422 on %s, got %#v", tc.field, err)
				}
			})
		}
		if n := countRows(t, pool); n != 0 {
			t.Fatalf("rejected input stored %d rows", n)
		}
	})

	t.Run("disabled: not configured, unsubscribe still works", func(t *testing.T) {
		truncate(t, pool)
		subscribe(t, svc, "a@example.com", fcm("keep"))
		if _, err := disabled.Subscribe(ctx, SubscribeInput{UserEmail: "a@example.com", Endpoint: fcm("x"),
			P256dh: fakeP256dh, Auth: fakeAuth}); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("Subscribe: %v", err)
		}
		if _, err := disabled.Broadcast(ctx, TestNotification); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("Broadcast: %v", err)
		}
		if _, err := disabled.SendToUser(ctx, "a@example.com", TestNotification); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("SendToUser: %v", err)
		}
		if err := disabled.Unsubscribe(ctx, fcm("keep")); err != nil {
			t.Fatalf("Unsubscribe: %v", err)
		}
		if err := disabled.Unsubscribe(ctx, fcm("never-stored")); err != nil {
			t.Fatalf("Unsubscribe unknown: %v", err)
		}
		if n := countRows(t, pool); n != 0 {
			t.Fatalf("expected the row gone, got %d", n)
		}
	})

	t.Run("broadcast reaches every row, send to user only the caller's", func(t *testing.T) {
		truncate(t, pool)
		subscribe(t, svc, "a@example.com", fcm("a1"))
		subscribe(t, svc, "a@example.com", fcm("a2"))
		subscribe(t, svc, "b@example.com", fcm("b1"))

		sender.reset(nil)
		rep, err := svc.Broadcast(ctx, TestNotification)
		if err != nil {
			t.Fatal(err)
		}
		if rep != (DeliveryReport{Attempted: 3, Delivered: 3}) || len(sender.calls) != 3 {
			t.Fatalf("broadcast report %+v calls %d", rep, len(sender.calls))
		}

		sender.reset(nil)
		rep, err = svc.SendToUser(ctx, "b@example.com", TestNotification)
		if err != nil {
			t.Fatal(err)
		}
		if rep != (DeliveryReport{Attempted: 1, Delivered: 1}) || len(sender.calls) != 1 || sender.calls[0] != fcm("b1") {
			t.Fatalf("send to user report %+v calls %v", rep, len(sender.calls))
		}
		if sender.lastMsg.Title != "Powiadomienie testowe" {
			t.Fatalf("unexpected notification %+v", sender.lastMsg)
		}

		sender.reset(nil)
		rep, err = svc.SendToUser(ctx, "nobody@example.com", TestNotification)
		if err != nil || rep != (DeliveryReport{}) {
			t.Fatalf("no subscriptions: %+v %v", rep, err)
		}
	})

	t.Run("result policy", func(t *testing.T) {
		truncate(t, pool)
		cases := map[string]sendResult{
			fcm("ok200"):   {status: 200},
			fcm("ok201"):   {status: 201},
			fcm("gone404"): {status: 404},
			fcm("gone410"): {status: 410},
			fcm("busy429"): {status: 429},
			fcm("err500"):  {status: 503},
			fcm("auth403"): {status: 403},
			fcm("bad400"):  {status: 400},
			fcm("net"):     {status: 0, err: errors.New("dial tcp: i/o timeout")},
		}
		for ep := range cases {
			subscribe(t, svc, "a@example.com", ep)
		}
		sender.reset(cases)
		rep, err := svc.Broadcast(ctx, TestNotification)
		if err != nil {
			t.Fatal(err)
		}
		want := DeliveryReport{Attempted: 9, Delivered: 2, Pruned: 2, Failed: 5}
		if rep != want {
			t.Fatalf("report %+v, want %+v", rep, want)
		}
		rows, err := pool.Query(ctx, `SELECT endpoint, last_success_at IS NOT NULL FROM push_subscriptions`)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for rows.Next() {
			var ep string
			var ok bool
			if err := rows.Scan(&ep, &ok); err != nil {
				t.Fatal(err)
			}
			got[ep] = ok
		}
		rows.Close()
		if len(got) != 7 {
			t.Fatalf("expected 7 rows kept (404/410 pruned), got %d", len(got))
		}
		for ep, ok := range got {
			wantSuccess := ep == fcm("ok200") || ep == fcm("ok201")
			if ok != wantSuccess {
				t.Fatalf("last_success_at set=%v for %s", ok, ep[len(ep)-6:])
			}
		}
		if _, still := got[fcm("gone404")]; still {
			t.Fatal("404 row not pruned")
		}
	})

	t.Run("list error surfaces", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := svc.Broadcast(cctx, TestNotification); err == nil {
			t.Fatal("expected an error on a cancelled context")
		}
	})
}
