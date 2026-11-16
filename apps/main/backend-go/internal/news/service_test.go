package news

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/backend-go/db/sqlcgen"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as prior
// phases (business logic table-tested here; real Postgres covered by
// integration_test.go).
type fakeQueries struct {
	rows      []sqlcgen.AssetNews
	positions []sqlcgen.PositionsForNewsResearchRow
	nextID    int64
}

func newFakeQueries() *fakeQueries { return &fakeQueries{nextID: 1} }

func (f *fakeQueries) NewsByDate(_ context.Context, newsDate pgtype.Date) ([]sqlcgen.AssetNews, error) {
	var out []sqlcgen.AssetNews
	for _, r := range f.rows {
		if r.NewsDate.Time.Equal(newsDate.Time) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ticker < out[j].Ticker })
	return out, nil
}

func (f *fakeQueries) MaxNewsDate(_ context.Context) (pgtype.Date, error) {
	if len(f.rows) == 0 {
		return pgtype.Date{}, nil
	}
	max := f.rows[0].NewsDate.Time
	for _, r := range f.rows[1:] {
		if r.NewsDate.Time.After(max) {
			max = r.NewsDate.Time
		}
	}
	return pgtype.Date{Time: max, Valid: true}, nil
}

func (f *fakeQueries) DistinctNewsDates(_ context.Context) ([]pgtype.Date, error) {
	seen := map[time.Time]bool{}
	var out []pgtype.Date
	for _, r := range f.rows {
		if !seen[r.NewsDate.Time] {
			seen[r.NewsDate.Time] = true
			out = append(out, r.NewsDate)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

func (f *fakeQueries) UpsertAssetNews(_ context.Context, arg sqlcgen.UpsertAssetNewsParams) (int64, error) {
	for i, r := range f.rows {
		if r.Ticker == arg.Ticker && r.NewsDate.Time.Equal(arg.NewsDate.Time) {
			f.rows[i].AssetName = arg.AssetName
			f.rows[i].Headline = arg.Headline
			f.rows[i].Summary = arg.Summary
			f.rows[i].Sentiment = arg.Sentiment
			f.rows[i].Sources = arg.Sources
			return r.ID, nil
		}
	}
	id := f.nextID
	f.nextID++
	f.rows = append(f.rows, sqlcgen.AssetNews{
		ID: id, Ticker: arg.Ticker, AssetName: arg.AssetName,
		NewsDate: arg.NewsDate, Headline: arg.Headline, Summary: arg.Summary,
		Sentiment: arg.Sentiment, Sources: arg.Sources,
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	})
	return id, nil
}

func (f *fakeQueries) NewsHistoryForTickers(_ context.Context, arg sqlcgen.NewsHistoryForTickersParams) ([]sqlcgen.NewsHistoryForTickersRow, error) {
	want := map[string]bool{}
	for _, t := range arg.Tickers {
		want[t] = true
	}
	var out []sqlcgen.NewsHistoryForTickersRow
	for _, r := range f.rows {
		if want[r.Ticker] && !r.NewsDate.Time.Before(arg.Since.Time) {
			out = append(out, sqlcgen.NewsHistoryForTickersRow{
				Ticker: r.Ticker, AssetName: r.AssetName, NewsDate: r.NewsDate,
				Headline: r.Headline, Summary: r.Summary,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ticker != out[j].Ticker {
			return out[i].Ticker < out[j].Ticker
		}
		return out[i].NewsDate.Time.After(out[j].NewsDate.Time)
	})
	return out, nil
}

func (f *fakeQueries) PositionsForNewsResearch(_ context.Context) ([]sqlcgen.PositionsForNewsResearchRow, error) {
	return f.positions, nil
}

func testService(f *fakeQueries) *Service { return NewService(f, 7, nil) }

func ingestItem(ticker, headline string, date time.Time) IngestItem {
	return IngestItem{
		Ticker: ticker, AssetName: "Asset " + ticker,
		Headline: headline, Summary: "Summary " + headline,
		NewsDate: date,
	}
}

func TestList_DefaultsToLatestDate(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	d1 := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	if _, err := svc.Ingest(ctx, []IngestItem{ingestItem("B", "h-b", d1), ingestItem("A", "h-a", d2)}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Data) != 1 || res.Data[0].Ticker != "A" {
		t.Fatalf("expected latest-date row A, got %+v", res.Data)
	}
	explicit, err := svc.List(ctx, &d1)
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Data) != 1 || explicit.Data[0].Ticker != "B" {
		t.Fatalf("expected date-scoped row B, got %+v", explicit.Data)
	}
}

func TestList_EmptyWhenNoNews(t *testing.T) {
	res, err := testService(newFakeQueries()).List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Data) != 0 {
		t.Fatalf("expected empty, got %+v", res.Data)
	}
}

func TestIngest_UpsertSameDayRefreshes(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	day := warsawToday()
	first := ingestItem("MSTR", "First headline", day)
	if _, err := svc.Ingest(ctx, []IngestItem{first}); err != nil {
		t.Fatal(err)
	}
	second := ingestItem("MSTR", "Updated headline", day)
	n, err := svc.Ingest(ctx, []IngestItem{second})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(f.rows) != 1 || f.rows[0].Headline != "Updated headline" {
		t.Fatalf("expected same-day upsert to refresh, got n=%d rows=%+v", n, f.rows)
	}
}

func TestIngest_DedupSkipsExactRepeatWithinWindow(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	yesterday := warsawToday().AddDate(0, 0, -1)
	if _, err := svc.Ingest(ctx, []IngestItem{ingestItem("BTC-USD", "  Big  Rally  ", yesterday)}); err != nil {
		t.Fatal(err)
	}
	// Same normalized headline today (different spacing/case) must be skipped, not counted.
	n, err := svc.Ingest(ctx, []IngestItem{ingestItem("BTC-USD", "big rally", warsawToday())})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(f.rows) != 1 {
		t.Fatalf("expected dedup skip (n=0, 1 row), got n=%d rows=%d", n, len(f.rows))
	}
	// A genuinely reworded follow-up is intentionally NOT caught.
	n, err = svc.Ingest(ctx, []IngestItem{ingestItem("BTC-USD", "Big rally continues into Asia", warsawToday())})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected reworded follow-up to persist, got n=%d", n)
	}
}

func TestIngest_Validation(t *testing.T) {
	svc := testService(newFakeQueries())
	ctx := context.Background()
	cases := []struct {
		name  string
		items []IngestItem
		field string
	}{
		{"empty batch", nil, "items"},
		{"blank ticker", []IngestItem{func() IngestItem { it := ingestItem("x", "h", warsawToday()); it.Ticker = " "; return it }()}, "ticker"},
		{"ticker too long", []IngestItem{func() IngestItem {
			it := ingestItem("x", "h", warsawToday())
			it.Ticker = strings.Repeat("A", 21)
			return it
		}()}, "ticker"},
		{"blank headline", []IngestItem{func() IngestItem { it := ingestItem("x", "h", warsawToday()); it.Headline = ""; return it }()}, "headline"},
		{"bad sentiment", []IngestItem{func() IngestItem {
			it := ingestItem("x", "h", warsawToday())
			s := "BULLISH"
			it.Sentiment = &s
			return it
		}()}, "sentiment"},
		{"missing date", []IngestItem{func() IngestItem { it := ingestItem("x", "h", warsawToday()); it.NewsDate = time.Time{}; return it }()}, "newsDate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Ingest(ctx, c.items)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestAssets_BatchedHistoryGroupedByTicker(t *testing.T) {
	f := newFakeQueries()
	f.positions = []sqlcgen.PositionsForNewsResearchRow{
		{Ticker: "BTC-USD", Name: "Bitcoin"},
		{Ticker: "MSTR", Name: "MicroStrategy"},
	}
	svc := testService(f)
	ctx := context.Background()
	old := warsawToday().AddDate(0, 0, -3)
	if _, err := svc.Ingest(ctx, []IngestItem{ingestItem("BTC-USD", "Old story", old)}); err != nil {
		t.Fatal(err)
	}
	assets, err := svc.Assets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %+v", assets)
	}
	if len(assets[0].RecentHistory) != 1 || assets[0].RecentHistory[0].Headline != "Old story" {
		t.Fatalf("expected BTC history slice, got %+v", assets[0])
	}
	if len(assets[1].RecentHistory) != 0 {
		t.Fatalf("expected empty history array (never null), got %+v", assets[1])
	}
}
