package trade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"investing-app/backend-go/internal/push"
)

// Notifier is the one push capability live execution needs (DIP, ADR-108/ADR-114): *push.Service
// satisfies it, wired in cmd/server; trade imports only push's value types, as news does. A nil
// Notifier means push is not wired: alerts then degrade to the ERROR log.
type Notifier interface {
	Broadcast(ctx context.Context, n push.Notification) (push.DeliveryReport, error)
}

const (
	alertURL = "/bot"
	alertTTL = 6 * time.Hour
)

// Alert copy (D191 §12, Polish UI). Every body carries the OKX ids so the owner can find the
// order in the OKX UI. The tag dedupes repeats on the phone.

func unprotectedAlert(ref string, p entryPlan) push.Notification {
	return push.Notification{
		Title: "PILNE: pozycja BTC bez stop-lossa",
		Body: fmt.Sprintf("Zlecenie %s (%s, %s kontr.) jest otwarte bez SL i nie udało się go zamknąć. Zamknij je ręcznie w OKX.",
			ref, p.Decision.Side, p.Contracts),
		URL: alertURL, Tag: "bot-exec-" + ref, Urgency: push.UrgencyHigh, TTL: alertTTL,
	}
}

func unknownAlert(ref string) push.Notification {
	return push.Notification{
		Title: "PILNE: nieznany stan zlecenia BTC",
		Body: fmt.Sprintf("Nie wiadomo, czy zlecenie %s zostało wykonane lub zabezpieczone. Sprawdź pozycję i zlecenia SL w OKX.",
			ref),
		URL: alertURL, Tag: "bot-exec-" + ref, Urgency: push.UrgencyHigh, TTL: alertTTL,
	}
}

func placedUnrecordedAlert(ref, algoID string, p entryPlan) push.Notification {
	return push.Notification{
		Title: "Bot: transakcja bez zapisu w aplikacji",
		Body: fmt.Sprintf("Pozycja %s ma SL (%s, zlecenie %s), ale nie zapisała się w bazie. Sprawdź /bot.",
			ref, p.Trigger.String(), algoID),
		URL: alertURL, Tag: "bot-exec-" + ref, Urgency: push.UrgencyNormal, TTL: alertTTL,
	}
}

func emergencyClosedAlert(ref string) push.Notification {
	return push.Notification{
		Title: "Bot: awaryjne zamknięcie pozycji",
		Body: fmt.Sprintf("Nie udało się ustawić SL; pozycja %s została zamknięta po cenie rynkowej.",
			ref),
		URL: alertURL, Tag: "bot-exec-" + ref, Urgency: push.UrgencyNormal, TTL: alertTTL,
	}
}

func blockedOpenTradeAlert(tradeID int64) push.Notification {
	return push.Notification{
		Title: "Bot nie handluje: otwarta transakcja",
		Body: fmt.Sprintf("Transakcja #%d jest w bazie oznaczona jako otwarta. Zamknij ją (PATCH /bot/trades/%d/close), jeśli pozycji już nie ma.",
			tradeID, tradeID),
		URL: alertURL, Tag: fmt.Sprintf("bot-open-%d", tradeID), Urgency: push.UrgencyNormal, TTL: alertTTL,
	}
}

// alert delivers one notification on its own context: detached from shutdown and from the
// (possibly exhausted) commit budget, bounded by alertBudget. It never panics and never returns
// an error: a failed alert degrades to a log line, because the exchange-side stop-loss and the
// call site's own ERROR log are the real protection. When the DB is down Broadcast cannot read
// the subscriptions; the result is the same log line.
func (e *LiveExecutionService) alert(parent context.Context, n push.Notification) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("live execution alert panicked", "tag", n.Tag, "panic", fmt.Sprint(r))
		}
	}()
	if e.notifier == nil {
		slog.Warn("live execution alert not delivered: no notifier wired", "tag", n.Tag, "title", n.Title)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), e.alertBudget)
	defer cancel()
	rep, err := e.notifier.Broadcast(ctx, n)
	switch {
	case errors.Is(err, push.ErrNotConfigured):
		slog.Warn("live execution alert not delivered: web push not configured", "tag", n.Tag, "title", n.Title)
	case err != nil:
		slog.Error("live execution alert delivery failed", "tag", n.Tag, "title", n.Title, "error", err)
	default:
		slog.Info("live execution alert sent", "tag", n.Tag,
			"attempted", rep.Attempted, "delivered", rep.Delivered, "failed", rep.Failed)
	}
}
