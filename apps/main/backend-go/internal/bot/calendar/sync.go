package calendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
)

const (
	defaultThisWeekURL = "https://nfs.faireconomy.media/ff_calendar_thisweek.json"
	defaultNextWeekURL = "https://nfs.faireconomy.media/ff_calendar_nextweek.json"
)

// ForexSource is the Go port of ForexFactorySyncService: fetches and upserts both the
// current-week and next-week Fair Economy calendar feeds. Each feed is fetched and
// upserted independently so a failure on one URL never prevents the other. The
// external_id hash is content-based (event time + title + currency), so upserting
// both overlapping feeds stays idempotent. Fail-soft throughout: every failure mode
// logs at warn and yields 0 for that feed, never an error to the caller.
type ForexSource struct {
	queries      Queries
	httpClient   *http.Client
	thisWeekURL  string
	nextWeekURL  string
	backoffUntil atomic.Int64 // unix seconds; 0 = no backoff
}

// NewForexSource wires the source with constructor-injected feed URLs (the
// ForexFactorySyncServiceIT WireMock URL-override pattern) and a 15s total HTTP
// timeout (Java: 5s connect / 10s read).
func NewForexSource(queries Queries, thisWeekURL, nextWeekURL string) *ForexSource {
	if thisWeekURL == "" {
		thisWeekURL = defaultThisWeekURL
	}
	if nextWeekURL == "" {
		nextWeekURL = defaultNextWeekURL
	}
	if v := strings.TrimSpace(os.Getenv("BOT_FOREXFACTORY_THIS_WEEK_URL")); v != "" {
		thisWeekURL = v
	}
	if v := strings.TrimSpace(os.Getenv("BOT_FOREXFACTORY_NEXT_WEEK_URL")); v != "" {
		nextWeekURL = v
	}
	return &ForexSource{
		queries:     queries,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		thisWeekURL: thisWeekURL,
		nextWeekURL: nextWeekURL,
	}
}

// BackoffUntil exposes the 429-backoff deadline for the watcher's refresh gate.
func (f *ForexSource) BackoffUntil() time.Time {
	return time.Unix(f.backoffUntil.Load(), 0).UTC()
}

// Sync fetches both feeds and returns the total upserted rows.
func (f *ForexSource) Sync(ctx context.Context) int {
	total := f.fetchAndUpsert(ctx, f.thisWeekURL) + f.fetchAndUpsert(ctx, f.nextWeekURL)
	slog.Info("bot calendar sync: forexFactory upserted total", "upserted", total)
	return total
}

func (f *ForexSource) fetchAndUpsert(ctx context.Context, url string) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Warn("bot calendar sync failed: bad request", "url", url, "error", err)
		return 0
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		slog.Warn("bot calendar sync failed", "url", url, "error", err)
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		f.backoffUntil.Store(retryAfter(resp.Header.Get("Retry-After")).Unix())
		slog.Warn("bot calendar sync rate-limited: backing off",
			"url", url, "until", f.BackoffUntil().Format(time.RFC3339))
		return 0
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("bot calendar sync failed: non-2xx", "url", url, "status", resp.StatusCode)
		return 0
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || len(strings.TrimSpace(string(body))) == 0 {
		slog.Warn("bot calendar sync failed: empty body", "url", url, "error", err)
		return 0
	}
	var feed []map[string]any
	if err := json.Unmarshal(body, &feed); err != nil {
		slog.Warn("bot calendar sync failed: non-JSON body", "url", url, "error", err)
		return 0
	}
	return f.upsertFeed(ctx, feed)
}

func retryAfter(header string) time.Time {
	if h := strings.TrimSpace(header); h != "" {
		var secs int64
		if _, err := fmt.Sscanf(h, "%d", &secs); err == nil {
			return time.Now().Add(time.Duration(secs) * time.Second)
		}
		if t, err := time.Parse(time.RFC1123, h); err == nil {
			return t
		}
	}
	return time.Now().Add(5 * time.Minute)
}

func (f *ForexSource) upsertFeed(ctx context.Context, feed []map[string]any) int {
	upserted := 0
	for _, n := range feed {
		title, _ := n["title"].(string)
		if strings.TrimSpace(title) == "" {
			continue
		}
		currency, _ := n["currency"].(string)
		country, _ := n["country"].(string)
		if strings.TrimSpace(currency) == "" && strings.TrimSpace(country) != "" {
			currency = country
		}
		if strings.TrimSpace(currency) == "" {
			currency = "USD"
		}
		impact := "LOW"
		if raw, _ := n["impact"].(string); raw != "" {
			if upper := strings.ToUpper(strings.TrimSpace(raw)); ValidImpacts[upper] {
				impact = upper
			}
		}
		dateStr, _ := n["date"].(string)
		timeStr, _ := n["time"].(string)
		eventTime := parseFeedTime(dateStr, timeStr)
		if eventTime.IsZero() {
			continue
		}
		forecast := emptyToNull(stringField(n, "forecast"))
		previous := emptyToNull(stringField(n, "previous"))
		actual := emptyToNull(stringField(n, "actual"))
		sum := sha256.Sum256([]byte(eventTime.UTC().Format(time.RFC3339) + "|" + title + "|" + currency))
		externalID := hex.EncodeToString(sum[:])[:32]
		if _, err := f.queries.UpsertCalendarEvent(ctx, sqlcgen.UpsertCalendarEventParams{
			EventTime:  pgtype.Timestamptz{Time: eventTime, Valid: true},
			Currency:   currency,
			Country:    emptyToNull(country),
			EventName:  title,
			Impact:     impact,
			Forecast:   forecast,
			Previous:   previous,
			Actual:     actual,
			Source:     "FAIRECONOMY",
			ExternalID: externalID,
		}); err != nil {
			slog.Warn("bot calendar sync: upsert failed", "externalId", externalID, "error", err)
			continue
		}
		upserted++
	}
	return upserted
}

func stringField(n map[string]any, key string) string {
	s, _ := n[key].(string)
	return s
}

func emptyToNull(s string) *string {
	t := strings.TrimSpace(s)
	if t == "" || t == "-" {
		return nil
	}
	return &t
}

// parseFeedTime ports ForexFactorySyncService.parseTime: ISO-offset datetimes parse
// directly; otherwise "MM-dd-yyyy HH:mm" in America/New_York ("All Day"/"Tentative"/
// blank time → 00:00); last resort Instant.parse. Zero time = unparseable = skip row.
func parseFeedTime(date, tim string) time.Time {
	if strings.TrimSpace(date) == "" {
		return time.Time{}
	}
	if strings.Contains(date, "T") {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(date)); err == nil {
			return t.UTC()
		}
	}
	t := strings.TrimSpace(tim)
	if t == "" || strings.EqualFold(t, "All Day") || strings.EqualFold(t, "Tentative") {
		t = "00:00"
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		ny = time.UTC
	}
	if local, err := time.ParseInLocation("01-02-2006 15:04", strings.TrimSpace(date)+" "+t, ny); err == nil {
		return local.UTC()
	}
	if instant, err := time.Parse(time.RFC3339, strings.TrimSpace(date)); err == nil {
		return instant.UTC()
	}
	return time.Time{}
}

// Orchestrator is the Go port of BotCalendarSyncOrchestrator: ForexFactory first,
// then TradingEconomics PPI-YoY enrichment, then BLS baseline capture — never in
// parallel, never reordered. TE and BLS are explicit no-op stubs (documented gap —
// Java remains their owner until a follow-up ports them); the sequence is preserved
// so slotting the real sources in later needs no reordering.
type Orchestrator struct {
	forex *ForexSource
}

// NewOrchestrator wires the orchestrator around the ForexFactory source.
func NewOrchestrator(forex *ForexSource) *Orchestrator { return &Orchestrator{forex: forex} }

// SyncAll runs every source in order and returns the summed upsert count (BLS
// baseline writes no calendar rows, so it is deliberately not folded into the
// total). Never fails: all sources are fail-soft internally.
func (o *Orchestrator) SyncAll(ctx context.Context) int {
	ffUpserted := 0
	if o.forex != nil {
		ffUpserted = o.forex.Sync(ctx)
	}
	teUpserted := syncTradingEconomicsStub()
	captureBLSBaselineStub()
	total := ffUpserted + teUpserted
	slog.Info("bot calendar sync orchestrator: sources done",
		"forexFactory", ffUpserted, "tradingEconomics", teUpserted, "total", total)
	return total
}

// BackoffUntil passes through the ForexFactory 429-backoff deadline for the
// watcher's refresh gate (TradingEconomics has no equivalent backoff — ADR-091 keeps
// its volume trivially low by construction).
func (o *Orchestrator) BackoffUntil() time.Time {
	if o.forex == nil {
		return time.Time{}
	}
	return o.forex.BackoffUntil()
}

func syncTradingEconomicsStub() int {
	slog.Warn("bot calendar sync: TradingEconomics PPI-YoY enrichment not yet ported in Go; Java remains its owner until the follow-up")
	return 0
}

func captureBLSBaselineStub() {
	slog.Warn("bot calendar sync: BLS baseline capture not yet ported in Go; Java remains its owner until the follow-up")
}
