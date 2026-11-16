package deposit

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

type intTestPrices struct{}

func (intTestPrices) PricePlnByYahooTicker(_ context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{}, nil
}
func (intTestPrices) AllRatesToPln(_ context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1), "CHF": decimal.NewFromInt(1)}, nil
}
func (intTestPrices) RateToPln(_ context.Context, _ string) (decimal.Decimal, error) {
	return decimal.NewFromInt(1), nil
}

// TestDeposit_RealPostgresRoundTrip exercises DepositService/AccountCapUsageService/XirrService
// against a real postgres:17.10-alpine container, migrated by the same Flyway CLI image used in
// prod — the "never mock the database" gate. Covers: manual create round-trip with the IKE/IKZE
// PLN-currency guard, account-cap-usage against the real V9-seeded annual limits, the
// upsert-adjustment round trip, and a basic XIRR computation over real EXTERNAL deposits.
func TestDeposit_RealPostgresRoundTrip(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `TRUNCATE deposits, account_cap_adjustment, positions, assets CASCADE`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	svc := NewService(queries, intTestPrices{})

	t.Run("Create rejects non-PLN currency for IKE", func(t *testing.T) {
		_, err := svc.Create(ctx, CreateInput{
			DepositDate: time.Now(), Amount: decimal.NewFromInt(100), Currency: "EUR",
			Exchange: "XTB", AccountType: "IKE", Portfolio: "RETIREMENT",
		})
		if err == nil {
			t.Fatal("expected an error for non-PLN IKE deposit")
		}
	})

	var depositID int64
	t.Run("Create + List round-trips with EXTERNAL capital source", func(t *testing.T) {
		dto, err := svc.Create(ctx, CreateInput{
			DepositDate: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), Amount: decimal.RequireFromString("7000.1234"),
			Currency: "PLN", Exchange: "XTB", AccountType: "IKE", Portfolio: "RETIREMENT",
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		depositID = dto.ID
		if dto.Amount.StringFixed(4) != "7000.1234" {
			t.Fatalf("expected 4dp precision to survive, got %s", dto.Amount)
		}

		items, total, err := svc.List(ctx, ListFilter{}, 0, 20)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 1 || len(items) != 1 {
			t.Fatalf("expected 1 deposit, got total=%d items=%d", total, len(items))
		}
	})

	t.Run("GetAccountCapUsage reflects the real V9-seeded annual limit", func(t *testing.T) {
		items, err := svc.GetAccountCapUsage(ctx, 2026)
		if err != nil {
			t.Fatalf("GetAccountCapUsage: %v", err)
		}
		var ikeItem *AccountCapUsageItem
		for i := range items {
			if items[i].AccountType == "IKE" {
				ikeItem = &items[i]
			}
		}
		if ikeItem == nil {
			t.Fatal("expected an IKE cap usage item")
		}
		if !ikeItem.AnnualCapPln.Equal(decimal.RequireFromString("23472.00")) {
			t.Fatalf("expected the real V9-seeded IKE annual cap (23472.00), got %s", ikeItem.AnnualCapPln)
		}
		if !ikeItem.TotalDepositedPln.Equal(decimal.RequireFromString("7000.1234")) {
			t.Fatalf("expected totalDepositedPln to reflect the just-created deposit, got %s", ikeItem.TotalDepositedPln)
		}
	})

	t.Run("UpsertAccountCapAdjustment round-trips to the exact typed remaining value", func(t *testing.T) {
		item, err := svc.UpsertAccountCapAdjustment(ctx, "IKE", 2026, decimal.NewFromInt(5000))
		if err != nil {
			t.Fatalf("UpsertAccountCapAdjustment: %v", err)
		}
		// NOTE: the just-created deposit's real amount is 7000.1234 (4dp, NUMERIC(18,4) per the
		// deposits schema), but account_cap_adjustment.adjustment_pln is NUMERIC(12,2) (only
		// 2dp) — the exact same real-schema precision Java's own AccountCapAdjustment entity
		// has. Solving adjustmentPln = remainingPln - annualCap + totalDeposited =
		// 5000 - 23472 + 7000.1234 = -11471.8766 and storing it truncates/rounds to 2dp
		// (-11471.88), so the re-computed remaining is 4999.9966, not an exact 5000 — a genuine
		// database-precision effect, not a Go port bug (Java would show the identical figure
		// against the identical schema).
		if !item.RemainingPln.Equal(decimal.NewFromInt(5000)) {
			t.Fatalf("expected remaining=5000 (the exact typed value, before the 2dp storage round-trip), got %s", item.RemainingPln)
		}
		items, err := svc.GetAccountCapUsage(ctx, 2026)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if it.AccountType == "IKE" && !it.RemainingPln.Equal(decimal.RequireFromString("4999.9966")) {
				t.Fatalf("expected re-computed remaining=4999.9966 (2dp adjustment-column rounding of -11471.8766), got %s", it.RemainingPln)
			}
		}
	})

	t.Run("ComputeXirr over a single EXTERNAL deposit with a known current value", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `TRUNCATE deposits`); err != nil {
			t.Fatal(err)
		}
		// One RETIREMENT position with price_currency=CHF — Java's own getNativePricePln checks
		// yahoo_ticker != null/blank FIRST, UNCONDITIONALLY, even for the CHF special case (a
		// CHF position with no ticker at all would fail loudly too, exactly like any other
		// missing price — "never value-at-zero"). A placeholder ticker is still required; the
		// CHF branch then ignores its actual value and reads fxRates[CHF] instead of doing a
		// real price-cache lookup — matching how real CHF savings positions ("PKO CHF
		// savings", ADR-050) are modeled.
		yahoo := "CHFPLN=X"
		if _, err := pool.Exec(ctx, `
			WITH a AS (
				INSERT INTO assets (portfolio, key, name, exposure, asset_class)
				VALUES ('RETIREMENT', 'CASHLIKE', 'Cashlike', 'CASH', 'CURRENCY') RETURNING id)
			INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency,
				purchase_currency, units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
				asset_id, is_primary, yahoo_ticker, company_backed)
			SELECT 'CASHLIKE', 'Cashlike', 'RETIREMENT', 'XTB', 'CURRENCY', 'CASH', 'CHF', 'PLN', 0, 0, 0, 0, 10, a.id, true, $1, false FROM a`, yahoo); err != nil {
			t.Fatal(err)
		}
		oneYearAgo := time.Now().AddDate(-1, 0, 0)
		if _, err := pool.Exec(ctx, `
			INSERT INTO deposits (deposit_date, amount, currency, exchange, account_type, portfolio, capital_source)
			VALUES ($1, 10, 'PLN', 'XTB', 'REGULAR', 'RETIREMENT', 'EXTERNAL')`, oneYearAgo); err != nil {
			t.Fatal(err)
		}
		result, err := svc.ComputeXirr(ctx, "RETIREMENT")
		if err != nil {
			t.Fatalf("ComputeXirr: %v", err)
		}
		if !result.TotalInvestedPln.Equal(decimal.NewFromInt(10)) {
			t.Fatalf("expected totalInvestedPln=10, got %s", result.TotalInvestedPln)
		}
		if !result.CurrentValuePln.Equal(decimal.NewFromInt(10)) {
			t.Fatalf("expected currentValuePln=10 (10 units * rate 1), got %s", result.CurrentValuePln)
		}
	})

	_ = depositID
}
