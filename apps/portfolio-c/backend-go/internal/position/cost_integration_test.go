package position

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/siblingcore"
	scposition "investing-app/common-go/siblingcore/position"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestPosition_RealPostgres_ManualUnitsEditNeverChangesCost is design 161 §7 I4 (rule 6, D7) for
// portfolioC (task 161a-3): a manual PUT /positions/{id} that edits units leaves total_spent_pln
// untouched - the stored go-live baseline stays exactly as it was, in BOTH portfolios when the
// same instrument (QDV5.DE) is held in each - and the cost changes only through an explicit
// totalSpentPln edit (the reviewed correction path). Real Postgres, no DB mocks.
func TestPosition_RealPostgres_ManualUnitsEditNeverChangesCost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets CASCADE`); err != nil {
		t.Fatal(err)
	}
	prices := &fakePriceSource{prices: map[string]decimal.Decimal{}, rates: map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}}
	svc := scposition.NewService(book.Config, store.New(pool), nil, prices)

	insert := func(portfolio, ticker, units, cost string) int64 {
		var assetID, id int64
		if err := pool.QueryRow(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class)
			VALUES ($1::portfolio_enum, $2::text, $2::text, 'STOCKS', 'ETF') RETURNING id`, portfolio, ticker).Scan(&assetID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency,
				units, total_spent_pln, asset_id, is_primary)
			VALUES ($1::portfolio_enum, $2::text, $2::text, 'XTB', 'ETF', 'STOCKS', 'PLN', $3::numeric, NULLIF($4, '')::numeric, $5, true) RETURNING id`,
			portfolio, ticker, units, cost, assetID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	variable := insert("VARIABLE", "QDV5.DE", "73.5727", "2348.6289")
	fixed := insert("FIXED", "QDV5.DE", "5.3697", "174.0385")
	nullCost := insert("VARIABLE", "ICGA.DE", "121.9191", "")
	cost := func(id int64) string {
		var c *string
		if err := pool.QueryRow(ctx, `SELECT total_spent_pln::text FROM positions WHERE id = $1`, id).Scan(&c); err != nil {
			t.Fatal(err)
		}
		if c == nil {
			return "NULL"
		}
		return *c
	}

	units := decimal.NewFromInt(100)
	if _, err := svc.Update(ctx, variable, scposition.UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{siblingcore.SingleBucket: units}}); err != nil {
		t.Fatalf("Update units on VARIABLE QDV5.DE: %v", err)
	}
	if got := cost(variable); got != "2348.6289" {
		t.Fatalf("VARIABLE cost after a units edit = %s, want unchanged 2348.6289", got)
	}
	if got := cost(fixed); got != "174.0385" {
		t.Fatalf("FIXED cost after the VARIABLE units edit = %s, want unchanged 174.0385", got)
	}
	if _, err := svc.Update(ctx, nullCost, scposition.UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{siblingcore.SingleBucket: units}}); err != nil {
		t.Fatalf("Update units on a NULL-cost row: %v", err)
	}
	if got := cost(nullCost); got != "NULL" {
		t.Fatalf("NULL cost after a units edit = %s, want NULL", got)
	}

	// The explicit correction path still works: only then does the cost change.
	corrected := decimal.RequireFromString("2400")
	if _, err := svc.Update(ctx, variable, scposition.UpdateInput{TotalSpentPln: &corrected}); err != nil {
		t.Fatalf("Update cost on VARIABLE QDV5.DE: %v", err)
	}
	if got := cost(variable); got != "2400.0000" {
		t.Fatalf("VARIABLE cost after an explicit totalSpentPln edit = %s, want 2400.0000", got)
	}
	if got := cost(fixed); got != "174.0385" {
		t.Fatalf("FIXED cost = %s, want unchanged 174.0385", got)
	}
}
