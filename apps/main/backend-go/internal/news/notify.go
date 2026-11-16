package news

import (
	"context"
	"fmt"
	"time"

	"investing-app/backend-go/internal/push"
)

// Notifier is the one push capability the news use case needs (DIP, ADR-108 pattern table):
// *push.Service satisfies it, wired in cmd/server. news imports only push's value types.
type Notifier interface {
	Broadcast(ctx context.Context, n push.Notification) (push.DeliveryReport, error)
}

// Outcome is the morning job's result, reported by infra/news/news-research.sh.
type Outcome string

const (
	OutcomeReady    Outcome = "READY"
	OutcomeNoNews   Outcome = "NO_NEWS"
	OutcomeNoAssets Outcome = "NO_ASSETS"
	OutcomeFailed   Outcome = "FAILED"
)

// ReportOutcome is the structured body of POST /news/notifications. ItemCount is required (>= 1)
// for READY and must be absent otherwise.
type ReportOutcome struct {
	Outcome   Outcome
	NewsDate  time.Time
	ItemCount *int
}

const (
	reportTag   = "news-report"
	reportURL   = "/news"
	reportTTL   = 12 * time.Hour
	titleReady  = "Raport z wiadomościami gotowy"
	titleFailed = "Raport z wiadomościami — błąd"
	dateLayout  = "2006-01-02"
)

// ReportNotification validates the outcome/itemCount pair and returns the ADR-108 §5 copy. It is
// pure: a count and a date only, never headlines, tickers or failure detail.
func ReportNotification(o ReportOutcome) (push.Notification, error) {
	if o.NewsDate.IsZero() {
		return push.Notification{}, unprocessable("newsDate", "must not be null")
	}
	date := o.NewsDate.Format(dateLayout)
	n := push.Notification{Title: titleReady, URL: reportURL, Tag: reportTag, Topic: reportTag, TTL: reportTTL}
	switch o.Outcome {
	case OutcomeReady:
		if o.ItemCount == nil || *o.ItemCount < 1 {
			return push.Notification{}, unprocessable("itemCount", "must be at least 1 when outcome is READY")
		}
		n.Body, n.Urgency = fmt.Sprintf("Nowe wiadomości: %d (%s)", *o.ItemCount, date), push.UrgencyNormal
		return n, nil
	case OutcomeNoNews, OutcomeNoAssets, OutcomeFailed:
		if o.ItemCount != nil {
			return push.Notification{}, unprocessable("itemCount", "must be omitted unless outcome is READY")
		}
	default:
		return push.Notification{}, unprocessable("outcome", "must be one of READY, NO_NEWS, NO_ASSETS, FAILED")
	}
	switch o.Outcome {
	case OutcomeNoNews:
		n.Body, n.Urgency = fmt.Sprintf("Brak istotnych wiadomości na dziś (%s)", date), push.UrgencyLow
	case OutcomeNoAssets:
		n.Body, n.Urgency = fmt.Sprintf("Brak aktywów do analizy (%s)", date), push.UrgencyLow
	default: // OutcomeFailed
		n.Title, n.Urgency = titleFailed, push.UrgencyHigh
		n.Body = fmt.Sprintf("Nie udało się wygenerować raportu (%s). Szczegóły: infra/news/news-research.log na Pi.", date)
	}
	return n, nil
}

// NotifyReport turns the outcome into the fixed copy and fans it out to every subscribed device.
// Push-service failures are counted in the report; only validation, ErrNotConfigured and database
// errors come back as errors.
func (s *Service) NotifyReport(ctx context.Context, o ReportOutcome) (push.DeliveryReport, error) {
	n, err := ReportNotification(o)
	if err != nil {
		return push.DeliveryReport{}, err
	}
	if s.notifier == nil {
		return push.DeliveryReport{}, push.ErrNotConfigured
	}
	return s.notifier.Broadcast(ctx, n)
}
