package rebalancing

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/position"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// TestRebalancingTransfer_RealPostgresRoundTrip exercises ExecuteTransfer's full
// write-ownership cluster (positions/asset_purchases/deposits) against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod — the
// "never mock the database" gate. Also proves the whole transfer commits/rolls back atomically
// (a single transact.Begin/Commit spanning validation + every write, architect-output §4).
func TestRebalancingTransfer_RealPostgresRoundTrip(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, asset_purchases CASCADE`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
	svc := NewService(queries, transact)

	soldID := mustInsertPosition(ctx, t, pool, "SOLD", "MAIN", "REGULAR-holding", 100)
	buyID := mustInsertPosition(ctx, t, pool, "BOUGHT", "MAIN", "IKE-holding", 0)

	result, err := svc.ExecuteTransfer(ctx, TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "REGULAR", Units: decimal.NewFromInt(40), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "IKE", Units: decimal.NewFromInt(4), PricePln: decimal.NewFromInt(100)}},
	})
	if err != nil {
		t.Fatalf("ExecuteTransfer: %v", err)
	}
	if len(result.AffectedTickers) != 2 {
		t.Fatalf("expected 2 affected tickers, got %v", result.AffectedTickers)
	}

	var soldUnits, boughtUnits string
	if err := pool.QueryRow(ctx, `SELECT units_regular_pln FROM positions WHERE id = $1`, soldID).Scan(&soldUnits); err != nil {
		t.Fatal(err)
	}
	if soldUnits != "60.00000000" {
		t.Fatalf("expected sold position left with 60 units, got %s", soldUnits)
	}
	if err := pool.QueryRow(ctx, `SELECT units_ike FROM positions WHERE id = $1`, buyID).Scan(&boughtUnits); err != nil {
		t.Fatal(err)
	}
	if boughtUnits != "4.00000000" {
		t.Fatalf("expected bought position with 4 IKE units, got %s", boughtUnits)
	}

	var purchaseCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_purchases WHERE session_id IS NULL`).Scan(&purchaseCount); err != nil {
		t.Fatal(err)
	}
	if purchaseCount != 2 {
		t.Fatalf("expected 2 session_id=NULL audit rows (ad-hoc transfer legs), got %d", purchaseCount)
	}

	// ADR-054/059: REGULAR->IKE must auto-create exactly one INTERNAL_TRANSFER deposit.
	var depositCount int
	var capitalSource string
	if err := pool.QueryRow(ctx, `SELECT COUNT(*), MIN(capital_source::text) FROM deposits`).Scan(&depositCount, &capitalSource); err != nil {
		t.Fatal(err)
	}
	if depositCount != 1 {
		t.Fatalf("expected exactly 1 auto-deposit, got %d", depositCount)
	}
	if capitalSource != "INTERNAL_TRANSFER" {
		t.Fatalf("expected INTERNAL_TRANSFER capital_source, got %s", capitalSource)
	}
}

// TestRebalancingTransfer_InsufficientUnits_RollsBackNothingWritten proves a rejected transfer
// (insufficient units) never partially applies — the whole transact rolls back.
func TestRebalancingTransfer_InsufficientUnits_RollsBackNothingWritten(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, asset_purchases CASCADE`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
	svc := NewService(queries, transact)

	soldID := mustInsertPosition(ctx, t, pool, "SOLD", "MAIN", "REGULAR-holding", 10)
	_ = mustInsertPosition(ctx, t, pool, "BOUGHT", "MAIN", "IKE-holding", 0)

	_, err = svc.ExecuteTransfer(ctx, TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "REGULAR", Units: decimal.NewFromInt(999), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "IKE", Units: decimal.NewFromInt(4), PricePln: decimal.NewFromInt(100)}},
	})
	if err == nil {
		t.Fatal("expected an error for insufficient units")
	}

	var soldUnits string
	if err := pool.QueryRow(ctx, `SELECT units_regular_pln FROM positions WHERE id = $1`, soldID).Scan(&soldUnits); err != nil {
		t.Fatal(err)
	}
	if soldUnits != "10.00000000" {
		t.Fatalf("expected sold position untouched at 10 (rolled back), got %s", soldUnits)
	}
	var purchaseCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM asset_purchases`).Scan(&purchaseCount); err != nil {
		t.Fatal(err)
	}
	if purchaseCount != 0 {
		t.Fatalf("expected zero audit rows (rolled back), got %d", purchaseCount)
	}
}

func mustInsertPosition(ctx context.Context, t *testing.T, pool *pgxpool.Pool, ticker, portfolio, label string, regularPln int64) int64 {
	t.Helper()
	var id int64
	// Task 156: each fixture is its own single-subasset asset (key = ticker), primary.
	err := pool.QueryRow(ctx, `
		WITH a AS (
			INSERT INTO assets (portfolio, key, name, exposure, asset_class)
			VALUES ($3::portfolio_enum, $1, $2, 'CASH', 'ETF') RETURNING id)
		INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency,
			purchase_currency, units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
			asset_id, is_primary, company_backed)
		SELECT $1, $2, $3::portfolio_enum, 'XTB', 'ETF', 'CASH', 'PLN', 'PLN', 0, 0, 0, 0, $4, a.id, true, true FROM a
		RETURNING id`,
		ticker, label, portfolio, pgnum.FromDecimal(decimal.NewFromInt(regularPln))).Scan(&id)
	if err != nil {
		t.Fatalf("insert fixture position %s: %v", ticker, err)
	}
	return id
}

// serialisedPrices is the cache-only price source position.Service needs for its response
// (no price is required for the money-write assertions).
type serialisedPrices struct{}

func (serialisedPrices) PricePlnByYahooTicker(context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{}, nil
}
func (serialisedPrices) AllRatesToPln(context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}, nil
}
func (serialisedPrices) RateToPln(context.Context, string) (decimal.Decimal, error) {
	return decimal.NewFromInt(1), nil
}

// TestMoneyWrites_Serialised_RealPostgres proves ADR-111 Decision 1 for main (task 177 B5):
// with the composition root's SerializedRunner wiring, overlapping money writes on one position
// run one after the other and each reads the state the previous one committed.
func TestMoneyWrites_Serialised_RealPostgres(t *testing.T) {
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

	// Wired exactly like cmd/server newRebalancingService / newPositionService.
	queries := sqlcgen.New(pool)
	transfers := NewService(queries, pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey,
		func(tx pgx.Tx) Queries { return queries.WithTx(tx) }))
	positions := position.NewService(queries, pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey,
		func(tx pgx.Tx) position.Queries { return queries.WithTx(tx) }), serialisedPrices{})

	sell60 := TransferRequest{
		Portfolio: "MAIN",
		Sold:      Leg{Ticker: "SOLD", AccountType: "REGULAR", Units: decimal.NewFromInt(60), PricePln: decimal.NewFromInt(10)},
		Buys:      []Leg{{Ticker: "BOUGHT", AccountType: "REGULAR", Units: decimal.NewFromInt(6), PricePln: decimal.NewFromInt(100)}},
	}
	reset := func(t *testing.T) int64 {
		t.Helper()
		if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, deposits, asset_purchases CASCADE`); err != nil {
			t.Fatal(err)
		}
		soldID := mustInsertPosition(ctx, t, pool, "SOLD", "MAIN", "REGULAR-holding", 100)
		mustInsertPosition(ctx, t, pool, "BOUGHT", "MAIN", "REGULAR-target", 0)
		return soldID
	}
	scanText := func(t *testing.T, query string, args ...any) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, query, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	t.Run("two transfers each selling 60% of one position: exactly one applies", func(t *testing.T) {
		soldID := reset(t)
		transfer := func() error { _, err := transfers.ExecuteTransfer(ctx, sell60); return err }
		errs := testsupport.RaceWhilePositionLocked(ctx, t, pool, soldID, transfer, transfer)

		var succeeded, insufficient int
		for _, err := range errs {
			var appErr *httpx.AppError
			switch {
			case err == nil:
				succeeded++
			case errors.As(err, &appErr) && appErr.Status == http.StatusBadRequest &&
				strings.Contains(appErr.Message, "Insufficient units"):
				insufficient++
			default:
				t.Fatalf("unexpected transfer error: %v", err)
			}
		}
		if succeeded != 1 || insufficient != 1 {
			t.Fatalf("expected one success and one insufficient-units 400, got %d / %d (%v)", succeeded, insufficient, errs)
		}
		if got := scanText(t, `SELECT units_regular_pln::text FROM positions WHERE id = $1`, soldID); got != "40.00000000" {
			t.Fatalf("sold units = %s, want 40 (base - 60%%)", got)
		}
		if got := scanText(t, `SELECT units_regular_pln::text FROM positions WHERE ticker = 'BOUGHT'`); got != "6.00000000" {
			t.Fatalf("bought units = %s, want 6 (one buy leg)", got)
		}
		if got := scanText(t, `SELECT count(*)::text FROM asset_purchases`); got != "2" {
			t.Fatalf("asset_purchases rows = %s, want exactly one transfer's 2 legs", got)
		}
	})

	t.Run("a PUT /positions/{id} edit racing a transfer keeps both writes", func(t *testing.T) {
		soldID := reset(t)
		ike := decimal.NewFromInt(5)
		transfer := func() error { _, err := transfers.ExecuteTransfer(ctx, sell60); return err }
		edit := func() error {
			_, err := positions.Update(ctx, soldID, position.UpdateInput{UnitsIke: &ike})
			return err
		}
		for i, err := range testsupport.RaceWhilePositionLocked(ctx, t, pool, soldID, transfer, edit) {
			if err != nil {
				t.Fatalf("writer %d: %v", i, err)
			}
		}
		if got := scanText(t, `SELECT units_regular_pln::text FROM positions WHERE id = $1`, soldID); got != "40.00000000" {
			t.Fatalf("units_regular_pln = %s, want 40: the edit overwrote the committed transfer", got)
		}
		if got := scanText(t, `SELECT units_ike::text FROM positions WHERE id = $1`, soldID); got != "5.00000000" {
			t.Fatalf("units_ike = %s, want 5: the transfer overwrote the committed edit", got)
		}
	})
}
