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
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

// TestPosition_RealPostgres_ManualUnitsEditNeverChangesCost is design 161 §7 I4 (rule 6, D7) for
// portfolioB (task 161a-3): a manual PUT /positions/{id} that edits units leaves total_spent_pln
// untouched - a recorded cost stays exactly as it was, a NULL cost stays NULL - and the cost
// changes only through an explicit totalSpentPln edit (the reviewed correction path). Real
// Postgres, no DB mocks.
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

	var assetID int64
	if err := pool.QueryRow(ctx, `INSERT INTO assets (key, name, exposure, asset_class)
		VALUES ('bitcoin', 'Bitcoin', 'CRYPTO', 'CRYPTO') RETURNING id`).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	insert := func(ticker string, primary bool, units, cost string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency,
				units_inne, total_spent_pln, asset_id, is_primary)
			VALUES ($1::text, $1::text, 'XTB', 'CRYPTO', 'CRYPTO', 'PLN', $2::numeric, NULLIF($3, '')::numeric, $4, $5) RETURNING id`,
			ticker, units, cost, assetID, primary).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	head := insert("HEAD", true, "10", "1000")
	sibling := insert("SIB", false, "10", "")
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

	units := decimal.NewFromInt(25)
	if _, err := svc.Update(ctx, head, scposition.UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{book.Inne: units}}); err != nil {
		t.Fatalf("Update units on the head: %v", err)
	}
	if got := cost(head); got != "1000.0000" {
		t.Fatalf("head cost after a units edit = %s, want unchanged 1000.0000", got)
	}
	if _, err := svc.Update(ctx, sibling, scposition.UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{book.Inne: units}}); err != nil {
		t.Fatalf("Update units on the sibling: %v", err)
	}
	if got := cost(sibling); got != "NULL" {
		t.Fatalf("sibling cost after a units edit = %s, want NULL", got)
	}
	if got := cost(head); got != "1000.0000" {
		t.Fatalf("head cost after the sibling's units edit = %s, want unchanged 1000.0000", got)
	}

	// The explicit correction path still works: only then does the cost change.
	corrected := decimal.NewFromInt(1200)
	if _, err := svc.Update(ctx, head, scposition.UpdateInput{TotalSpentPln: &corrected}); err != nil {
		t.Fatalf("Update cost on the head: %v", err)
	}
	if got := cost(head); got != "1200.0000" {
		t.Fatalf("head cost after an explicit totalSpentPln edit = %s, want 1200.0000", got)
	}
}
