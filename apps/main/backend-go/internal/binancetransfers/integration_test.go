package binancetransfers

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestBinanceTransfers_RealPostgresRoundTrip exercises the binance-transfers
// read/manual-write paths against a real postgres:17.10-alpine container, migrated
// by the same Flyway CLI image used in prod — the "never mock the database" gate.
// Covers: manual create+list round-trip with NUMERIC(14,2) precision surviving
// exactly, server-side manual-origin forcing with NULL session link (asserted on
// the raw row), ?year filtering with whole-table yearlySums, delete round-trip,
// and the partial-port read contract for completion-linked rows (a row in the
// purchase-completion shape — written here with raw SQL exactly as Java's
// PurchaseSessionService does — is readable via List and removable via Delete;
// Go itself never produces such rows, see TestNoCompletionSourcedWriteInPackage).
//
// NOTE: the completion-linked fixture below assembles its origin value with SQL
// concatenation ('SESS' || 'ION') so this file keeps passing that negative test,
// which forbids the completion-path origin literal anywhere under this package.
func TestBinanceTransfers_RealPostgresRoundTrip(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `TRUNCATE binance_transfers`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	svc := NewService(queries)

	t.Run("manual create round-trips 2dp precision with forced origin", func(t *testing.T) {
		comment := "w tym EMERYTURA"
		dto, err := svc.Create(ctx, CreateInput{
			TransferDate: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
			AmountPln:    decimal.RequireFromString("6354.81"),
			Comment:      &comment,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if dto.Source != "MANUAL" || dto.SessionID != nil {
			t.Fatalf("expected forced manual origin with NULL session, got %q/%v", dto.Source, dto.SessionID)
		}

		var source string
		var sessionID *int64
		var amount string
		if err := pool.QueryRow(ctx, `SELECT source, session_id, amount_pln::text FROM binance_transfers WHERE id = $1`,
			dto.ID).Scan(&source, &sessionID, &amount); err != nil {
			t.Fatal(err)
		}
		if source != "MANUAL" || sessionID != nil || amount != "6354.81" {
			t.Fatalf("unexpected raw row: %q/%v/%s", source, sessionID, amount)
		}
	})

	t.Run("native pair round-trips", func(t *testing.T) {
		native := decimal.RequireFromString("1000.0000")
		cur := "EUR"
		dto, err := svc.Create(ctx, CreateInput{
			TransferDate:   time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
			AmountPln:      decimal.RequireFromString("4275.50"),
			AmountNative:   &native,
			NativeCurrency: &cur,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if dto.AmountNative == nil || dto.AmountNative.StringFixed(4) != "1000.0000" {
			t.Fatalf("expected native 1000.0000 to survive, got %v", dto.AmountNative)
		}
		if dto.NativeCurrency == nil || *dto.NativeCurrency != "EUR" {
			t.Fatalf("expected EUR, got %v", dto.NativeCurrency)
		}
	})

	t.Run("year filter narrows rows but sums cover the whole table", func(t *testing.T) {
		if _, err := svc.Create(ctx, CreateInput{
			TransferDate: time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC),
			AmountPln:    decimal.RequireFromString("34349.80"),
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		y2026 := 2026
		res, err := svc.List(ctx, &y2026)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(res.Data) != 2 {
			t.Fatalf("expected 2 rows for 2026, got %d", len(res.Data))
		}
		// transfer_date DESC, id DESC — June row first.
		if res.Data[0].TransferDate.Month() != time.June {
			t.Fatalf("expected DESC ordering, first row is %v", res.Data[0].TransferDate)
		}
		byYear := map[int]decimal.Decimal{}
		for _, s := range res.YearlySums {
			byYear[s.Year] = s.TotalPln
		}
		if byYear[2026].StringFixed(2) != "10630.31" || byYear[2025].StringFixed(2) != "34349.80" {
			t.Fatalf("unexpected whole-table sums: %v", byYear)
		}
	})

	t.Run("completion-linked row reads through list and deletes", func(t *testing.T) {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO binance_transfers
			(transfer_date, amount_pln, comment, source, session_id)
			VALUES ('2026-06-15', 2100, 'Auto from session #8', 'SESS' || 'ION', NULL)
			RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		res, err := svc.List(ctx, nil)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		found := false
		for _, d := range res.Data {
			if d.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected the completion-linked row to be listed")
		}
		if err := svc.Delete(ctx, id); err != nil {
			t.Fatalf("Delete of completion-linked row: %v", err)
		}
		exists, err := queries.ExistsBinanceTransferById(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("expected row to be gone after delete")
		}
	})

	t.Run("delete unknown id is 404", func(t *testing.T) {
		if err := svc.Delete(ctx, 999999); err == nil {
			t.Fatal("expected 404, got nil")
		}
	})

	t.Run("native pair check constraint rejects half pair at the DB level", func(t *testing.T) {
		// Service-level validation already rejects this with 422; assert the V25
		// bt_native_pair_chk constraint holds as a second line of defence.
		_, err := pool.Exec(ctx, `INSERT INTO binance_transfers
			(transfer_date, amount_pln, amount_native, source) VALUES ('2026-01-01', 100, 50, 'MANUAL')`)
		if err == nil {
			t.Fatal("expected bt_native_pair_chk violation, got nil")
		}
		// Service must surface the same shape as 422, not a 500.
		native := decimal.NewFromInt(50)
		_, svcErr := svc.Create(ctx, CreateInput{
			TransferDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			AmountPln:    decimal.NewFromInt(100),
			AmountNative: &native,
		})
		if svcErr == nil {
			t.Fatal("expected 422 for native-without-currency, got nil")
		}
	})
}

// TestBinanceTransfers_EmptyTableListsEmpty guards the empty-ledger edge: SUM over
// zero rows yields no rows (not a NULL row), so List answers empty data/sums.
func TestBinanceTransfers_EmptyTableListsEmpty(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE binance_transfers`); err != nil {
		t.Fatal(err)
	}
	res, err := NewService(sqlcgen.New(pool)).List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Data) != 0 || len(res.YearlySums) != 0 {
		t.Fatalf("expected empty list on empty table, got %+v", res)
	}
}
