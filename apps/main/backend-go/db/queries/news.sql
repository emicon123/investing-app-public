-- Phase 5 news queries — NewsController + NewsService port.
--
-- Write ownership (doc 13 §6.3): asset_news is written ONLY here (nightly host cron
-- via POST /news/ingest, upsert on (ticker, news_date)). The positions read below is
-- read-only (MAIN / primary-subasset / company-backed scope for GET /news/assets) — zero
-- writes outside asset_news, by construction: this file contains no INSERT/UPDATE/
-- DELETE touching any other table.

-- name: NewsByDate :many
SELECT id, ticker, asset_name, news_date, headline, summary, sentiment, sources,
    created_at, updated_at
FROM asset_news
WHERE news_date = $1::date
ORDER BY ticker ASC;

-- name: MaxNewsDate :one
SELECT MAX(news_date)::date AS max_date
FROM asset_news;

-- name: DistinctNewsDates :many
SELECT DISTINCT news_date
FROM asset_news
ORDER BY news_date DESC;

-- name: UpsertAssetNews :one
INSERT INTO asset_news (ticker, asset_name, news_date, headline, summary, sentiment, sources)
VALUES (sqlc.arg(ticker)::text, sqlc.arg(asset_name)::text, sqlc.arg(news_date)::date,
    sqlc.arg(headline)::text, sqlc.arg(summary)::text, sqlc.narg(sentiment)::text,
    sqlc.arg(sources)::jsonb)
ON CONFLICT (ticker, news_date)
DO UPDATE SET asset_name = EXCLUDED.asset_name,
              headline = EXCLUDED.headline,
              summary = EXCLUDED.summary,
              sentiment = EXCLUDED.sentiment,
              sources = EXCLUDED.sources
RETURNING id;

-- name: NewsHistoryForTickers :many
SELECT ticker, asset_name, news_date, headline, summary
FROM asset_news
WHERE ticker = ANY(sqlc.arg(tickers)::text[])
  AND news_date >= sqlc.arg(since)::date
ORDER BY ticker ASC, news_date DESC;

-- name: PositionsForNewsResearch :many
SELECT ticker, name
FROM positions
WHERE is_primary
  AND deleted_at IS NULL
  AND portfolio = 'MAIN'
  AND company_backed = TRUE
ORDER BY ticker ASC;
