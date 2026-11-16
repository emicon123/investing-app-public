package news

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"investing-app/backend-go/internal/push"
	"investing-app/common-go/httpx"
)

var oct27 = time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)

func TestReportNotification_Copy(t *testing.T) {
	cases := []struct {
		name    string
		in      ReportOutcome
		title   string
		body    string
		urgency push.Urgency
	}{
		{"ready", ReportOutcome{Outcome: OutcomeReady, NewsDate: oct27, ItemCount: new(7)},
			"Raport z wiadomościami gotowy", "Nowe wiadomości: 7 (2026-10-27)", push.UrgencyNormal},
		{"no news", ReportOutcome{Outcome: OutcomeNoNews, NewsDate: oct27},
			"Raport z wiadomościami gotowy", "Brak istotnych wiadomości na dziś (2026-10-27)", push.UrgencyLow},
		{"no assets", ReportOutcome{Outcome: OutcomeNoAssets, NewsDate: oct27},
			"Raport z wiadomościami gotowy", "Brak aktywów do analizy (2026-10-27)", push.UrgencyLow},
		{"failed", ReportOutcome{Outcome: OutcomeFailed, NewsDate: oct27},
			"Raport z wiadomościami — błąd",
			"Nie udało się wygenerować raportu (2026-10-27). Szczegóły: infra/news/news-research.log na Pi.",
			push.UrgencyHigh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := ReportNotification(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			want := push.Notification{Title: tc.title, Body: tc.body, URL: "/news", Tag: "news-report",
				Topic: "news-report", Urgency: tc.urgency, TTL: 12 * time.Hour}
			if n != want {
				t.Fatalf("got %+v\nwant %+v", n, want)
			}
		})
	}
}

func TestReportNotification_Invalid(t *testing.T) {
	cases := []struct {
		name  string
		in    ReportOutcome
		field string
	}{
		{"ready without count", ReportOutcome{Outcome: OutcomeReady, NewsDate: oct27}, "itemCount"},
		{"ready with zero", ReportOutcome{Outcome: OutcomeReady, NewsDate: oct27, ItemCount: new(0)}, "itemCount"},
		{"no news with count", ReportOutcome{Outcome: OutcomeNoNews, NewsDate: oct27, ItemCount: new(3)}, "itemCount"},
		{"failed with count", ReportOutcome{Outcome: OutcomeFailed, NewsDate: oct27, ItemCount: new(1)}, "itemCount"},
		{"unknown outcome", ReportOutcome{Outcome: "MAYBE", NewsDate: oct27}, "outcome"},
		{"missing date", ReportOutcome{Outcome: OutcomeNoNews}, "newsDate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReportNotification(tc.in)
			var appErr *httpx.AppError
			if !errors.As(err, &appErr) || appErr.Status != http.StatusUnprocessableEntity || appErr.Field != tc.field {
				t.Fatalf("expected 422 on %s, got %#v", tc.field, err)
			}
		})
	}
}

type fakeNotifier struct {
	got   []push.Notification
	rep   push.DeliveryReport
	err   error
	calls int
}

func (f *fakeNotifier) Broadcast(_ context.Context, n push.Notification) (push.DeliveryReport, error) {
	f.calls++
	f.got = append(f.got, n)
	return f.rep, f.err
}

func TestNotifyReport(t *testing.T) {
	ctx := context.Background()

	t.Run("broadcasts the copy and returns the report", func(t *testing.T) {
		fn := &fakeNotifier{rep: push.DeliveryReport{Attempted: 2, Delivered: 1, Pruned: 1}}
		svc := NewService(newFakeQueries(), 7, fn)
		rep, err := svc.NotifyReport(ctx, ReportOutcome{Outcome: OutcomeReady, NewsDate: oct27, ItemCount: new(4)})
		if err != nil {
			t.Fatal(err)
		}
		if rep != fn.rep || fn.calls != 1 || fn.got[0].Body != "Nowe wiadomości: 4 (2026-10-27)" {
			t.Fatalf("rep %+v calls %d got %+v", rep, fn.calls, fn.got)
		}
	})

	t.Run("invalid outcome never reaches the notifier", func(t *testing.T) {
		fn := &fakeNotifier{}
		svc := NewService(newFakeQueries(), 7, fn)
		if _, err := svc.NotifyReport(ctx, ReportOutcome{Outcome: OutcomeReady, NewsDate: oct27}); err == nil {
			t.Fatal("expected a validation error")
		}
		if fn.calls != 0 {
			t.Fatalf("notifier called %d times", fn.calls)
		}
	})

	t.Run("notifier error passes through", func(t *testing.T) {
		fn := &fakeNotifier{err: push.ErrNotConfigured}
		svc := NewService(newFakeQueries(), 7, fn)
		if _, err := svc.NotifyReport(ctx, ReportOutcome{Outcome: OutcomeFailed, NewsDate: oct27}); !errors.Is(err, push.ErrNotConfigured) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("no notifier is not configured", func(t *testing.T) {
		svc := NewService(newFakeQueries(), 7, nil)
		if _, err := svc.NotifyReport(ctx, ReportOutcome{Outcome: OutcomeFailed, NewsDate: oct27}); !errors.Is(err, push.ErrNotConfigured) {
			t.Fatalf("got %v", err)
		}
	})
}

func notifyRequest(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/news/notifications", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.NotifyNewsReport(rec, req)
	return rec
}

func TestNotifyNewsReportHandler(t *testing.T) {
	t.Run("200 with the delivery report", func(t *testing.T) {
		fn := &fakeNotifier{rep: push.DeliveryReport{Attempted: 2, Delivered: 1, Pruned: 1}}
		h := NewHandler(NewService(newFakeQueries(), 7, fn))
		rec := notifyRequest(t, h, `{"outcome":"READY","newsDate":"2026-10-27","itemCount":7}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"data":{"attempted":2,"delivered":1,"failed":0,"pruned":1}`) ||
			!strings.Contains(rec.Body.String(), `"meta":{`) {
			t.Fatalf("body %s", rec.Body.String())
		}
		if fn.got[0].Body != "Nowe wiadomości: 7 (2026-10-27)" {
			t.Fatalf("copy %q", fn.got[0].Body)
		}
	})

	t.Run("422 cases", func(t *testing.T) {
		h := NewHandler(NewService(newFakeQueries(), 7, &fakeNotifier{}))
		cases := map[string]struct{ body, field string }{
			"malformed":          {`{"outcome":`, ""},
			"bad date":           {`{"outcome":"FAILED","newsDate":"27.10.2026"}`, ""},
			"missing date":       {`{"outcome":"FAILED"}`, "newsDate"},
			"unknown outcome":    {`{"outcome":"MAYBE","newsDate":"2026-10-27"}`, "outcome"},
			"ready without":      {`{"outcome":"READY","newsDate":"2026-10-27"}`, "itemCount"},
			"no news with count": {`{"outcome":"NO_NEWS","newsDate":"2026-10-27","itemCount":2}`, "itemCount"},
		}
		for name, tc := range cases {
			rec := notifyRequest(t, h, tc.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
			}
			var body httpx.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 ||
				body.Errors[0].Code != httpx.CodeValidationError || body.Errors[0].Field != tc.field {
				t.Fatalf("%s: %s", name, rec.Body.String())
			}
		}
	})

	t.Run("503 when push is not configured", func(t *testing.T) {
		h := NewHandler(NewService(newFakeQueries(), 7, &fakeNotifier{err: push.ErrNotConfigured}))
		rec := notifyRequest(t, h, `{"outcome":"NO_ASSETS","newsDate":"2026-10-27"}`)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), push.CodePushNotConfigured) {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
}
