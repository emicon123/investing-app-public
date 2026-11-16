package news

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// TestNews_RealPostgresRoundTrip exercises the news read/upsert paths against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod
// — the "never mock the database" gate. Covers: ingest+list round-trip with JSONB
// sources surviving exactly, (ticker, news_date) upsert refreshing in place, dates
// listing most-recent-first, the UNIQUE(ticker, news_date) constraint as a second line
// of defence, and the positions read-only scope (a companion row and a RETIREMENT row
// are invisible to Assets while the MAIN company-backed row is returned).
func TestNews_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE asset_news`); err != nil {
		t.Fatal(err)
	}

	svc := NewService(sqlcgen.New(pool), 7, nil)
	day := warsawToday()

	t.Run("ingest then list round-trips sources jsonb", func(t *testing.T) {
		sent := "POSITIVE"
		n, err := svc.Ingest(ctx, []IngestItem{{
			Ticker: "MSTR", AssetName: "MicroStrategy", Headline: "Adds BTC",
			Summary: "Disclosure.", Sentiment: &sent,
			Sources:  []SourceDTO{{Title: "Watcher Guru", URL: "https://watcher.guru/news/x"}},
			NewsDate: day,
		}})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected upserted=1, got %d", n)
		}
		res, err := svc.List(ctx, nil)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(res.Data) != 1 {
			t.Fatalf("expected 1 row, got %+v", res.Data)
		}
		got := res.Data[0]
		if got.Ticker != "MSTR" || got.Sentiment == nil || *got.Sentiment != "POSITIVE" {
			t.Fatalf("unexpected row: %+v", got)
		}
		if len(got.Sources) != 1 || got.Sources[0].URL != "https://watcher.guru/news/x" {
			t.Fatalf("sources did not survive: %+v", got.Sources)
		}
		var raw string
		if err := pool.QueryRow(ctx, `SELECT sources::text FROM asset_news WHERE ticker='MSTR'`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		// jsonb normalizes key order alphabetically — compare semantically.
		var decoded []map[string]string
		if err := jsonUnmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded) != 1 || decoded[0]["title"] != "Watcher Guru" ||
			decoded[0]["url"] != "https://watcher.guru/news/x" {
			t.Fatalf("unexpected raw jsonb: %s", raw)
		}
	})

	t.Run("same-day re-ingest refreshes instead of duplicating", func(t *testing.T) {
		n, err := svc.Ingest(ctx, []IngestItem{ingestItem("MSTR", "Refreshed headline", day)})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected upserted=1, got %d", n)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_news`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected still 1 row, got %d", count)
		}
	})

	t.Run("unique constraint rejects raw duplicates", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO asset_news (ticker, asset_name, news_date, headline, summary)
			VALUES ('MSTR', 'MicroStrategy', $1, 'dup', 'dup')`, day.Format("2006-01-02"))
		if err == nil {
			t.Fatal("expected uq_asset_news_ticker_date violation, got nil")
		}
	})

	t.Run("dates most recent first", func(t *testing.T) {
		// UTC calendar arithmetic: day is UTC midnight shown in Warsaw time, so AddDate in that zone
		// shifts by an hour across a DST change and no longer equals the stored DATE's instant.
		older := day.UTC().AddDate(0, 0, -2)
		if _, err := svc.Ingest(ctx, []IngestItem{ingestItem("BTC-USD", "Old", older)}); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		dates, err := svc.Dates(ctx)
		if err != nil {
			t.Fatalf("Dates: %v", err)
		}
		if len(dates) != 2 || !dates[0].Equal(day) || !dates[1].Equal(older) {
			t.Fatalf("expected [day older] desc, got %v", dates)
		}
	})

	t.Run("assets scope is read-only main primary-subasset company-backed", func(t *testing.T) {
		// Minimal rows: the scope columns only (task 156: one asset per scoped ticker, SCOPE-COMP
		// a non-primary subasset of SCOPE-MAIN's asset).
		if _, err := pool.Exec(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class) VALUES
			('MAIN', 'scope-main', 'Scope Main', 'EQUITY', 'STOCK'),
			('RETIREMENT', 'SCOPE-RET', 'Scope Retirement', 'EQUITY', 'STOCK'),
			('MAIN', 'SCOPE-NOCOMP', 'Scope No Company', 'EQUITY', 'STOCK')`); err != nil {
			t.Fatalf("seed assets: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO positions
			(ticker, name, portfolio, exchange, asset_class, exposure, price_currency, purchase_currency,
			 units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
			 asset_id, is_primary, company_backed)
			SELECT v.ticker, v.name, a.portfolio, 'XTB', 'STOCK', 'EQUITY', 'USD', 'PLN',
			       0, 0, 0, 0, 0, a.id, v.is_primary, v.backed
			FROM (VALUES
				('SCOPE-MAIN', 'Scope Main', 'scope-main', TRUE, TRUE),
				('SCOPE-COMP', 'Scope Companion', 'scope-main', FALSE, TRUE),
				('SCOPE-RET', 'Scope Retirement', 'SCOPE-RET', TRUE, TRUE),
				('SCOPE-NOCOMP', 'Scope No Company', 'SCOPE-NOCOMP', TRUE, FALSE)
			) AS v(ticker, name, akey, is_primary, backed)
			JOIN assets a ON a.key = v.akey`); err != nil {
			t.Fatalf("seed positions: %v", err)
		}
		assets, err := svc.Assets(ctx)
		if err != nil {
			t.Fatalf("Assets: %v", err)
		}
		// Seeded MAIN/company-backed test row must be present; non-primary subasset, retirement
		// and non-company rows must be invisible. (The shared fixture DB may hold
		// other positions from seed migrations — assert presence/absence, not exact
		// membership.)
		seen := map[string]bool{}
		for _, a := range assets {
			seen[a.Ticker] = true
		}
		if !seen["SCOPE-MAIN"] {
			t.Fatalf("expected SCOPE-MAIN in scope, got %v", seen)
		}
		for _, excluded := range []string{"SCOPE-COMP", "SCOPE-RET", "SCOPE-NOCOMP"} {
			if seen[excluded] {
				t.Fatalf("expected %s out of scope, got %v", excluded, seen)
			}
		}
		var assetNewsCount int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_news`).Scan(&assetNewsCount); err != nil {
			t.Fatal(err)
		}
		if assetNewsCount != 2 {
			t.Fatalf("assets read must not write asset_news, count=%d", assetNewsCount)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM assets WHERE key IN ('scope-main','SCOPE-RET','SCOPE-NOCOMP')`); err != nil {
			t.Fatal(err) // FK cascade removes the SCOPE-* subassets
		}
	})
}
