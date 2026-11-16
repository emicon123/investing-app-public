package news

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	NewsByDate(ctx context.Context, newsDate pgtype.Date) ([]sqlcgen.AssetNews, error)
	MaxNewsDate(ctx context.Context) (pgtype.Date, error)
	DistinctNewsDates(ctx context.Context) ([]pgtype.Date, error)
	UpsertAssetNews(ctx context.Context, arg sqlcgen.UpsertAssetNewsParams) (int64, error)
	NewsHistoryForTickers(ctx context.Context, arg sqlcgen.NewsHistoryForTickersParams) ([]sqlcgen.NewsHistoryForTickersRow, error)
	PositionsForNewsResearch(ctx context.Context) ([]sqlcgen.PositionsForNewsResearchRow, error)
}

// Service is the news service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries             Queries
	historyLookbackDays int
	notifier            Notifier
}

// NewService wires queries with the history-lookback window (app.news.history-
// lookback-days, default 7 — the single source of truth for both the recentHistory
// window and the ingest dedup-guard window, mirroring NewsProperties) and the push
// notifier for NotifyReport (ADR-108; nil means push is not configured).
func NewService(queries Queries, historyLookbackDays int, notifier Notifier) *Service {
	if historyLookbackDays <= 0 {
		historyLookbackDays = 7
	}
	return &Service{queries: queries, historyLookbackDays: historyLookbackDays, notifier: notifier}
}

// List returns news for the given date, or for the latest date that has any news when
// date is nil — the direct port of NewsService.listNews. Empty when no news exists.
func (s *Service) List(ctx context.Context, date *time.Time) (ListResult, error) {
	target := date
	if target == nil {
		max, err := s.queries.MaxNewsDate(ctx)
		if err != nil {
			return ListResult{}, fmt.Errorf("news: max date: %w", err)
		}
		if !max.Valid {
			return ListResult{Data: []ItemDTO{}}, nil
		}
		t := max.Time
		target = &t
	}
	rows, err := s.queries.NewsByDate(ctx, pgtype.Date{Time: *target, Valid: true})
	if err != nil {
		return ListResult{}, fmt.Errorf("news: list: %w", err)
	}
	data := make([]ItemDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := toDTO(r)
		if err != nil {
			return ListResult{}, fmt.Errorf("news: decode row %d: %w", r.ID, err)
		}
		data = append(data, dto)
	}
	return ListResult{Data: data}, nil
}

// Dates returns distinct news dates, most recent first — the direct port of
// NewsService.listDates (feeds the web history picker).
func (s *Service) Dates(ctx context.Context) ([]time.Time, error) {
	rows, err := s.queries.DistinctNewsDates(ctx)
	if err != nil {
		return nil, fmt.Errorf("news: dates: %w", err)
	}
	out := make([]time.Time, 0, len(rows))
	for _, d := range rows {
		if d.Valid {
			out = append(out, d.Time)
		}
	}
	return out, nil
}

// Assets returns MAIN-portfolio, non-companion, company-backed positions with up to
// historyLookbackDays of each asset's own past news (most-recent-first) — the direct
// port of NewsService.assetsToResearch, including the single batched history query
// (not N+1) grouped by ticker.
func (s *Service) Assets(ctx context.Context) ([]AssetDTO, error) {
	positions, err := s.queries.PositionsForNewsResearch(ctx)
	if err != nil {
		return nil, fmt.Errorf("news: assets scope: %w", err)
	}
	if len(positions) == 0 {
		return []AssetDTO{}, nil
	}
	tickers := make([]string, 0, len(positions))
	for _, p := range positions {
		tickers = append(tickers, p.Ticker)
	}
	since := warsawToday().AddDate(0, 0, -s.historyLookbackDays)
	historyRows, err := s.queries.NewsHistoryForTickers(ctx, sqlcgen.NewsHistoryForTickersParams{
		Tickers: tickers,
		Since:   pgtype.Date{Time: since, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("news: assets history: %w", err)
	}
	byTicker := make(map[string][]HistoryItemDTO, len(positions))
	for _, h := range historyRows {
		byTicker[h.Ticker] = append(byTicker[h.Ticker], HistoryItemDTO{
			NewsDate: h.NewsDate.Time,
			Headline: h.Headline,
			Summary:  h.Summary,
		})
	}
	out := make([]AssetDTO, 0, len(positions))
	for _, p := range positions {
		history := byTicker[p.Ticker]
		if history == nil {
			history = []HistoryItemDTO{}
		}
		out = append(out, AssetDTO{Ticker: p.Ticker, AssetName: p.Name, RecentHistory: history})
	}
	return out, nil
}

// Ingest upserts a batch of researched items on (ticker, newsDate) — the direct port
// of NewsService.ingest, including the defensive exact-normalized-headline dedup
// backstop over [today-lookback, today-1] (same-day re-runs are handled by the upsert
// itself). Skipped duplicates are logged, not counted. Returns rows inserted/updated.
func (s *Service) Ingest(ctx context.Context, items []IngestItem) (int, error) {
	if len(items) == 0 {
		return 0, unprocessable("items", "must not be empty")
	}
	for i := range items {
		if err := validateItem(&items[i]); err != nil {
			return 0, err
		}
	}
	today := warsawToday()
	windowStart := today.AddDate(0, 0, -s.historyLookbackDays)
	windowEnd := today.AddDate(0, 0, -1)

	tickers := make([]string, 0, len(items))
	for _, it := range items {
		tickers = append(tickers, it.Ticker)
	}
	recent := map[string]map[string]bool{}
	if !windowEnd.Before(windowStart) {
		rows, err := s.queries.NewsHistoryForTickers(ctx, sqlcgen.NewsHistoryForTickersParams{
			Tickers: tickers,
			Since:   pgtype.Date{Time: windowStart, Valid: true},
		})
		if err != nil {
			return 0, fmt.Errorf("news: ingest dedup window: %w", err)
		}
		for _, r := range rows {
			if r.NewsDate.Time.After(windowEnd) {
				continue
			}
			m := recent[r.Ticker]
			if m == nil {
				m = map[string]bool{}
				recent[r.Ticker] = m
			}
			m[normalizeHeadline(r.Headline)] = true
		}
	}

	upserted := 0
	for _, it := range items {
		if recent[it.Ticker][normalizeHeadline(it.Headline)] {
			continue
		}
		sourcesJSON, err := marshalSources(it.Sources)
		if err != nil {
			return 0, fmt.Errorf("news: encode sources: %w", err)
		}
		if _, err := s.queries.UpsertAssetNews(ctx, sqlcgen.UpsertAssetNewsParams{
			Ticker:    it.Ticker,
			AssetName: it.AssetName,
			NewsDate:  pgtype.Date{Time: it.NewsDate, Valid: true},
			Headline:  it.Headline,
			Summary:   it.Summary,
			Sentiment: it.Sentiment,
			Sources:   sourcesJSON,
		}); err != nil {
			return 0, fmt.Errorf("news: upsert %s: %w", it.Ticker, err)
		}
		upserted++
	}
	return upserted, nil
}

func validateItem(it *IngestItem) error {
	if strings.TrimSpace(it.Ticker) == "" {
		return unprocessable("ticker", "must not be blank")
	}
	if len(it.Ticker) > 20 {
		return unprocessable("ticker", "size must be between 0 and 20")
	}
	if strings.TrimSpace(it.AssetName) == "" {
		return unprocessable("assetName", "must not be blank")
	}
	if len(it.AssetName) > 200 {
		return unprocessable("assetName", "size must be between 0 and 200")
	}
	if strings.TrimSpace(it.Headline) == "" {
		return unprocessable("headline", "must not be blank")
	}
	if len(it.Headline) > 300 {
		return unprocessable("headline", "size must be between 0 and 300")
	}
	if strings.TrimSpace(it.Summary) == "" {
		return unprocessable("summary", "must not be blank")
	}
	if it.Sentiment != nil {
		switch *it.Sentiment {
		case "POSITIVE", "NEUTRAL", "NEGATIVE":
		default:
			return unprocessable("sentiment", "must be one of POSITIVE, NEUTRAL, NEGATIVE")
		}
	}
	for i, src := range it.Sources {
		if strings.TrimSpace(src.Title) == "" {
			return unprocessable(fmt.Sprintf("sources[%d].title", i), "must not be blank")
		}
		if len(src.Title) > 300 {
			return unprocessable(fmt.Sprintf("sources[%d].title", i), "size must be between 0 and 300")
		}
		if strings.TrimSpace(src.URL) == "" {
			return unprocessable(fmt.Sprintf("sources[%d].url", i), "must not be blank")
		}
	}
	if it.NewsDate.IsZero() {
		return unprocessable("newsDate", "must not be null")
	}
	return nil
}

func normalizeHeadline(headline string) string {
	return strings.Join(strings.Fields(strings.ToLower(headline)), " ")
}

func marshalSources(sources []SourceDTO) ([]byte, error) {
	if sources == nil {
		return []byte("[]"), nil
	}
	type wire struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	out := make([]wire, 0, len(sources))
	for _, s := range sources {
		out = append(out, wire(s))
	}
	return json.Marshal(out)
}

func toDTO(r sqlcgen.AssetNews) (ItemDTO, error) {
	var sources []SourceDTO
	if len(r.Sources) > 0 {
		var wire []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		}
		if err := json.Unmarshal(r.Sources, &wire); err != nil {
			return ItemDTO{}, err
		}
		for _, w := range wire {
			sources = append(sources, SourceDTO{Title: w.Title, URL: w.URL})
		}
	}
	if sources == nil {
		sources = []SourceDTO{}
	}
	var sentiment *string
	if r.Sentiment != nil {
		s := *r.Sentiment
		sentiment = &s
	}
	return ItemDTO{
		ID: r.ID, Ticker: r.Ticker, AssetName: r.AssetName,
		Headline: r.Headline, Summary: r.Summary, Sentiment: sentiment,
		Sources: sources, NewsDate: r.NewsDate.Time,
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
	}, nil
}

func warsawToday() time.Time {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Truncate(24 * time.Hour)
}

// unprocessable mirrors httpx.NewValidationError's envelope/code with the 422 status
// docs/openapi.yaml documents for POST /news/ingest validation failures.
func unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}
