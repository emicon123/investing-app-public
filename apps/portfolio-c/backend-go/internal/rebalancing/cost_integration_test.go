package rebalancing

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/pgtx"
	"investing-app/common-go/siblingcore"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

type posSpec struct {
	portfolio string
	ticker    string
	yahoo     string
	units     string
	cost      *string // nil = NULL total_spent_pln
}

// seedSingleton inserts one asset of a portfolio with a single (primary) subasset - portfolioC's shape:
// one asset per instrument per portfolio.
func seedSingleton(ctx context.Context, t *testing.T, pool *pgxpool.Pool, s posSpec) int64 {
	t.Helper()
	var assetID, id int64
	if err := pool.QueryRow(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class)
		VALUES ($1::portfolio_enum, $2::text, $2::text, 'STOCKS', 'ETF') RETURNING id`, s.portfolio, s.ticker).Scan(&assetID); err != nil {
		t.Fatalf("insert asset %s/%s: %v", s.portfolio, s.ticker, err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, yahoo_ticker,
			units, total_spent_pln, asset_id, is_primary)
		VALUES ($1::portfolio_enum, $2::text, $2::text, 'XTB', 'ETF', 'STOCKS', 'PLN', $3::text, $4::numeric, $5::numeric, $6, true) RETURNING id`,
		s.portfolio, s.ticker, s.yahoo, s.units, s.cost, assetID).Scan(&id); err != nil {
		t.Fatalf("insert position %s/%s: %v", s.portfolio, s.ticker, err)
	}
	return id
}

func costText(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64) string {
	t.Helper()
	var c *string
	if err := pool.QueryRow(ctx, `SELECT total_spent_pln::text FROM positions WHERE id = $1`, id).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c == nil {
		return "NULL"
	}
	return *c
}

func unitsText(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64) decimal.Decimal {
	t.Helper()
	var u string
	if err := pool.QueryRow(ctx, `SELECT units::text FROM positions WHERE id = $1`, id).Scan(&u); err != nil {
		t.Fatal(err)
	}
	return decimal.RequireFromString(u)
}

// TestConfirm_AssetLevelCost_RealPostgres is design 161 §7 I1 (+ rule 7) for portfolioC's purchase
// confirm (task 161a-3): a buy adds exactly the PLN paid to a tracked asset (the stored go-live
// baseline stays as it is), leaves an asset with no recorded cost NULL, initializes an empty
// asset, never crosses portfolios (QDV5.DE is held in both), and a failure after the writes rolls
// units AND cost back. Real Postgres, no DB mocks.
func TestConfirm_AssetLevelCost_RealPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	// The confirm reads price_cache inside its serialised transaction (ADR-111), so the prices
	// are seeded there.
	if _, err := pool.Exec(ctx, `INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES
		('4GLD.DE', 300, 'PLN', 300), ('QDV5.DE', 50, 'PLN', 50), ('NULLCOST.Y', 20, 'PLN', 20), ('EMPTY.Y', 20, 'PLN', 20)`); err != nil {
		t.Fatal(err)
	}
	q := store.New(pool)
	svc := screbalancing.NewService(book.Config, q, pgtx.Runner(pool, store.Bind), q, NewFunding(), nil)
	confirm := func(portfolio string, deposit decimal.Decimal, ticker string, units decimal.Decimal) error {
		return svc.Confirm(ctx, screbalancing.ConfirmRequest{
			Portfolio: siblingcore.Portfolio(portfolio), DepositAmountPln: &deposit,
			Items: []screbalancing.ConfirmItem{{Ticker: ticker, Units: siblingcore.Units{}.Add(siblingcore.SingleBucket, units)}},
		})
	}
	reset := func(t *testing.T) {
		t.Helper()
		if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, purchase_lots CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	deposit := decimal.NewFromInt(1000)

	t.Run("buy on a tracked asset adds exactly the paid amount; the go-live baseline stays as it is", func(t *testing.T) {
		reset(t)
		gld := seedSingleton(ctx, t, pool, posSpec{"VARIABLE", "4GLD.DE", "4GLD.DE", "13.6647", new("1500.21")})
		if err := confirm("VARIABLE", deposit, "4GLD.DE", dec("2")); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got := costText(ctx, t, pool, gld); got != "2100.2100" {
			t.Fatalf("cost = %s, want 2100.2100 (1500.21 + 2 x 300)", got)
		}
		if got := unitsText(ctx, t, pool, gld); !got.Equal(dec("15.6647")) {
			t.Fatalf("units = %s, want 15.6647", got)
		}
	})

	t.Run("the same instrument in the other portfolio is not touched", func(t *testing.T) {
		reset(t)
		variable := seedSingleton(ctx, t, pool, posSpec{"VARIABLE", "QDV5.DE", "QDV5.DE", "73.5727", new("2348.6289")})
		fixed := seedSingleton(ctx, t, pool, posSpec{"FIXED", "QDV5.DE", "QDV5.DE", "5.3697", new("174.0385")})
		if err := confirm("FIXED", deposit, "QDV5.DE", dec("1")); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got := costText(ctx, t, pool, fixed); got != "224.0385" {
			t.Fatalf("FIXED cost = %s, want 224.0385 (174.0385 + 1 x 50)", got)
		}
		if got := costText(ctx, t, pool, variable); got != "2348.6289" {
			t.Fatalf("VARIABLE cost = %s, want the untouched 2348.6289", got)
		}
	})

	t.Run("buy on an asset with no recorded cost that holds units leaves it NULL", func(t *testing.T) {
		reset(t)
		id := seedSingleton(ctx, t, pool, posSpec{"VARIABLE", "NULLCOST", "NULLCOST.Y", "10", nil})
		if err := confirm("VARIABLE", deposit, "NULLCOST", dec("4")); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got := costText(ctx, t, pool, id); got != "NULL" {
			t.Fatalf("cost = %s, want NULL (rule 7: no cost invented)", got)
		}
		if got := unitsText(ctx, t, pool, id); !got.Equal(dec("14")) {
			t.Fatalf("units = %s, want 14 (units still applied)", got)
		}
	})

	t.Run("buy on an empty asset initializes the cost", func(t *testing.T) {
		reset(t)
		id := seedSingleton(ctx, t, pool, posSpec{"VARIABLE", "EMPTY", "EMPTY.Y", "0", nil})
		if err := confirm("VARIABLE", deposit, "EMPTY", dec("10")); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got := costText(ctx, t, pool, id); got != "200.0000" {
			t.Fatalf("cost = %s, want 200.0000 (10 x 20)", got)
		}
	})

	t.Run("a buy with no price writes units but no cost", func(t *testing.T) {
		reset(t)
		id := seedSingleton(ctx, t, pool, posSpec{"VARIABLE", "NOPRICE", "NOPRICE.Y", "5", new("100")})
		if err := confirm("VARIABLE", deposit, "NOPRICE", dec("10")); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if got := costText(ctx, t, pool, id); got != "100.0000" {
			t.Fatalf("cost = %s, want the untouched 100.0000 (no derivable paid amount)", got)
		}
	})

	t.Run("a failure after the writes rolls back units and cost", func(t *testing.T) {
		reset(t)
		id := seedSingleton(ctx, t, pool, posSpec{"VARIABLE", "4GLD.DE", "4GLD.DE", "13.6647", new("1500.21")})
		// The deposit insert is the last write; a NUMERIC(15,2) overflow fails it AFTER the position
		// delta (units + cost) and the lot were written.
		overflow := decimal.RequireFromString("1e20")
		if err := confirm("VARIABLE", overflow, "4GLD.DE", dec("2")); err == nil {
			t.Fatal("expected the oversized deposit to fail the confirm")
		}
		if got := costText(ctx, t, pool, id); got != "1500.2100" {
			t.Fatalf("cost = %s, want the rolled-back 1500.2100", got)
		}
		if got := unitsText(ctx, t, pool, id); !got.Equal(dec("13.6647")) {
			t.Fatalf("units = %s, want the rolled-back 13.6647", got)
		}
		var lots int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM purchase_lots`).Scan(&lots); err != nil {
			t.Fatal(err)
		}
		if lots != 0 {
			t.Fatalf("purchase_lots rows = %d, want 0 after the rollback", lots)
		}
	})
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
