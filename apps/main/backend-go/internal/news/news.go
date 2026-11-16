// Package news serves daily AI-researched asset news (ADR-041; ported from the Java NewsService,
// ADR-094). This service never calls an LLM itself — a host cron job does the research and pushes
// results via Ingest; this package is persistence + read-side shaping only.
//
// Write ownership (doc 13 §6.3): asset_news is written ONLY by Ingest (upsert on
// (ticker, news_date)). The positions read in Assets is read-only (MAIN-portfolio,
// non-companion, company-backed scope) — zero writes outside asset_news.
package news

import (
	"time"
)

// SourceDTO is one news source link. Amounts ride as decimal only where the domain
// has numerics — news has none; this struct exists so the service layer never touches
// openapi or sqlc types (doc 13's layering rule).
type SourceDTO struct {
	Title string
	URL   string
}

// ItemDTO is one asset_news row in service-layer shape.
type ItemDTO struct {
	ID        int64
	Ticker    string
	AssetName string
	Headline  string
	Summary   string
	Sentiment *string
	Sources   []SourceDTO
	NewsDate  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HistoryItemDTO is one past day's headline for the /news/assets recentHistory window
// (ADR-079) — deliberately omits sentiment/sources to keep the payload small.
type HistoryItemDTO struct {
	NewsDate time.Time
	Headline string
	Summary  string
}

// AssetDTO is one MAIN-portfolio asset the nightly cron should research tonight.
type AssetDTO struct {
	Ticker        string
	AssetName     string
	RecentHistory []HistoryItemDTO
}

// IngestItem is one validated ingest request item.
type IngestItem struct {
	Ticker    string
	AssetName string
	Headline  string
	Summary   string
	Sentiment *string
	Sources   []SourceDTO
	NewsDate  time.Time
}

// ListResult is one listNews result.
type ListResult struct {
	Data []ItemDTO
}
