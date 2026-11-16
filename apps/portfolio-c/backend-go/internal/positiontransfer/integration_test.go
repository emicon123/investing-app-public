package positiontransfer

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/pgtx"
	scposition "investing-app/common-go/siblingcore/position"
	sctransfer "investing-app/common-go/siblingcore/transfer"
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestExecuteTransfer_RealPostgres exercises the full sold+buy write path against portfolioC's own
// migrated database — never mock the database.
func TestExecuteTransfer_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets CASCADE`); err != nil {
		t.Fatalf("truncate seeded positions: %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct)
		VALUES ('VARIABLE', 'IT-SOLD', 'Sold ETF', 'STOCKS', 'ETF', 50),
		       ('VARIABLE', 'IT-BUY', 'Buy ETF', 'STOCKS', 'ETF', 50)`); err != nil {
		t.Fatalf("seed assets: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, units, asset_id, is_primary)
		VALUES ('VARIABLE', 'IT-SOLD', 'Sold ETF', 'XTB', 'ETF', 'STOCKS', 'PLN', 100, (SELECT id FROM assets WHERE key='IT-SOLD' AND portfolio='VARIABLE'), true)`); err != nil {
		t.Fatalf("seed sold position: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, units, asset_id, is_primary)
		VALUES ('VARIABLE', 'IT-BUY', 'Buy ETF', 'XTB', 'ETF', 'STOCKS', 'PLN', 0, (SELECT id FROM assets WHERE key='IT-BUY' AND portfolio='VARIABLE'), true)`); err != nil {
		t.Fatalf("seed buy position: %v", err)
	}

	svc := sctransfer.NewService(book.Config, pgtx.Runner(pool, store.Bind))

	result, err := svc.ExecuteTransfer(ctx, sctransfer.Request{
		Portfolio: "VARIABLE",
		Sold:      sctransfer.Leg{Ticker: "IT-SOLD", Units: dec("20"), PricePln: dec("15")},
		Buys:      []sctransfer.Leg{{Ticker: "IT-BUY", Units: dec("30"), PricePln: dec("10")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TransferID.String() == "" {
		t.Fatal("expected a non-empty transfer id")
	}

	var soldUnits, buyUnits string
	if err := pool.QueryRow(ctx, `SELECT units::text FROM positions WHERE ticker = 'IT-SOLD' AND portfolio = 'VARIABLE'`).Scan(&soldUnits); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT units::text FROM positions WHERE ticker = 'IT-BUY' AND portfolio = 'VARIABLE'`).Scan(&buyUnits); err != nil {
		t.Fatal(err)
	}
	if dec(soldUnits).Cmp(dec("80")) != 0 {
		t.Fatalf("expected IT-SOLD units=80, got %s", soldUnits)
	}
	if dec(buyUnits).Cmp(dec("30")) != 0 {
		t.Fatalf("expected IT-BUY units=30, got %s", buyUnits)
	}

	var transferRowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM position_transfers WHERE transfer_id = $1`, result.TransferID).Scan(&transferRowCount); err != nil {
		t.Fatal(err)
	}
	if transferRowCount != 2 {
		t.Fatalf("expected 2 audit rows (1 sold + 1 buy) sharing transfer_id, got %d", transferRowCount)
	}
}

// TestMoneyWrites_Serialised_RealPostgres proves ADR-111 Decision 1 for portfolioC (task 177 B6):
// with the composition root's SerializedRunner wiring, overlapping money writes on one position
// run one after the other and each reads the state the previous one committed.
func TestMoneyWrites_Serialised_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	// Wired exactly like cmd/server (positionTransferTransact / positionTransact).
	transfers := sctransfer.NewService(book.Config, pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, store.Bind))
	positions := scposition.NewService(book.Config, store.New(pool), pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, store.Bind), store.New(pool))

	sell60 := sctransfer.Request{
		Portfolio: "VARIABLE",
		Sold:      sctransfer.Leg{Ticker: "RACE-SOLD", Units: dec("60"), PricePln: dec("10")},
		Buys:      []sctransfer.Leg{{Ticker: "RACE-BUY", Units: dec("6"), PricePln: dec("100")}},
	}
	reset := func(t *testing.T) int64 {
		t.Helper()
		if _, err := pool.Exec(ctx, `DELETE FROM position_transfers WHERE ticker LIKE 'RACE-%';
			DELETE FROM positions WHERE ticker LIKE 'RACE-%'; DELETE FROM assets WHERE key LIKE 'RACE-%'`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct)
			VALUES ('VARIABLE', 'RACE-SOLD', 'Sold ETF', 'STOCKS', 'ETF', 0), ('VARIABLE', 'RACE-BUY', 'Buy ETF', 'STOCKS', 'ETF', 0)`); err != nil {
			t.Fatal(err)
		}
		var soldID int64
		if err := pool.QueryRow(ctx, `INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, units, asset_id, is_primary)
			VALUES ('VARIABLE', 'RACE-SOLD', 'Sold ETF', 'XTB', 'ETF', 'STOCKS', 'PLN', 100, (SELECT id FROM assets WHERE key='RACE-SOLD' AND portfolio='VARIABLE'), true)
			RETURNING id`).Scan(&soldID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, units, asset_id, is_primary)
			VALUES ('VARIABLE', 'RACE-BUY', 'Buy ETF', 'XTB', 'ETF', 'STOCKS', 'PLN', 0, (SELECT id FROM assets WHERE key='RACE-BUY' AND portfolio='VARIABLE'), true)`); err != nil {
			t.Fatal(err)
		}
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
	transfer := func() error { _, err := transfers.ExecuteTransfer(ctx, sell60); return err }

	t.Run("two transfers each selling 60% of one position: exactly one applies", func(t *testing.T) {
		soldID := reset(t)
		var succeeded, insufficient int
		for _, err := range testsupport.RaceWhilePositionLocked(ctx, t, pool, soldID, transfer, transfer) {
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
			t.Fatalf("expected one success and one insufficient-units 400, got %d / %d", succeeded, insufficient)
		}
		if got := dec(scanText(t, `SELECT units::text FROM positions WHERE id = $1`, soldID)); got.Cmp(dec("40")) != 0 {
			t.Fatalf("sold units = %s, want 40 (base - 60%%)", got)
		}
		if got := dec(scanText(t, `SELECT units::text FROM positions WHERE ticker = 'RACE-BUY'`)); got.Cmp(dec("6")) != 0 {
			t.Fatalf("bought units = %s, want 6 (one buy leg)", got)
		}
		if got := scanText(t, `SELECT count(*)::text FROM position_transfers WHERE ticker LIKE 'RACE-%'`); got != "2" {
			t.Fatalf("position_transfers rows = %s, want exactly one transfer's 2 legs", got)
		}
	})

	// portfolioC has one units column, so the edit changes another field; the full-row overwrite
	// is what used to undo the committed transfer.
	t.Run("a PUT /positions/{id} edit racing a transfer keeps both writes", func(t *testing.T) {
		soldID := reset(t)
		name := "Renamed ETF"
		edit := func() error { _, err := positions.Update(ctx, soldID, scposition.UpdateInput{Name: &name}); return err }
		for i, err := range testsupport.RaceWhilePositionLocked(ctx, t, pool, soldID, transfer, edit) {
			if err != nil {
				t.Fatalf("writer %d: %v", i, err)
			}
		}
		if got := dec(scanText(t, `SELECT units::text FROM positions WHERE id = $1`, soldID)); got.Cmp(dec("40")) != 0 {
			t.Fatalf("units = %s, want 40: the edit overwrote the committed transfer", got)
		}
		if got := scanText(t, `SELECT name FROM positions WHERE id = $1`, soldID); got != name {
			t.Fatalf("name = %q, want %q", got, name)
		}
	})
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
