package rebalancingcalc

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/testsupport"
)

type intTestPrices struct {
	prices map[string]decimal.Decimal
	rates  map[string]decimal.Decimal
}

func (p intTestPrices) PricePlnByYahooTicker(_ context.Context) (map[string]decimal.Decimal, error) {
	return p.prices, nil
}
func (p intTestPrices) AllRatesToPln(_ context.Context) (map[string]decimal.Decimal, error) {
	return p.rates, nil
}

// TestCalculate_RealPostgresRoundTrip exercises Calculate (RebalancingService port) against a
// real postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod —
// the "never mock the database" gate. Confirms the read-only Queries interface actually
// satisfies structural typing against *sqlcgen.Queries, and that the read-only cross-package
// dependencies (settings, cash_positions, macro_state) resolve correctly against real Flyway
// seed data.
func TestCalculate_RealPostgresRoundTrip(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, cash_positions CASCADE`); err != nil {
		t.Fatal(err)
	}
	// V1 seeds retirement.monthly_contribution_pln=1000 and V33 seeds
	// rebalancing.macro_derisk_enabled=true — disable the gate for this simple round trip
	// (macro_state is empty here, which the gate already treats gracefully as full risk-on, but
	// disabling makes the assertion below unconditional rather than order-dependent on that
	// null-safety path also being exercised correctly, which is covered separately by
	// macroderisk-focused unit tests).
	if _, err := pool.Exec(ctx, `UPDATE settings SET value = 'false' WHERE key = 'rebalancing.macro_derisk_enabled'`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	prices := intTestPrices{
		prices: map[string]decimal.Decimal{"A.YAHOO": decimal.NewFromInt(100), "B.YAHOO": decimal.NewFromInt(50)},
		rates:  map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)},
	}

	mustInsertPosition(ctx, t, pool, "A", "MAIN", "60", "A.YAHOO")
	mustInsertPosition(ctx, t, pool, "B", "MAIN", "40", "B.YAHOO")

	cashOverride := decimal.NewFromInt(1050)
	result, err := Calculate(ctx, queries, prices, PortfolioMAIN, &cashOverride)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if len(result.BuyList) != 2 {
		t.Fatalf("expected 2 buy-list items, got %+v", result.BuyList)
	}
	sum := decimal.Zero
	for _, item := range result.BuyList {
		if item.AllocatedPln.Sign() < 0 {
			t.Fatalf("manual-sell-only invariant violated: negative allocation for %s: %s", item.Ticker, item.AllocatedPln)
		}
		sum = sum.Add(item.AllocatedPln)
	}
	if !sum.Equal(result.DeployableCashPln) {
		t.Fatalf("expected sum(allocatedPln)=%s to equal deployableCashPln, got %s", result.DeployableCashPln, sum)
	}
}

func mustInsertPosition(ctx context.Context, t *testing.T, pool *pgxpool.Pool, ticker, portfolio, targetPct, yahooTicker string) {
	t.Helper()
	// Task 156: every fixture is its own single-subasset asset (key = ticker) carrying the
	// target, with the subasset as its primary.
	_, err := pool.Exec(ctx, `
		WITH a AS (
			INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct)
			VALUES ($2::portfolio_enum, $1::text, $1::text, 'CASH', 'ETF', $3) RETURNING id)
		INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency,
			purchase_currency, units_ike, units_ikze, units_regular_eur,
			units_regular_usd, units_regular_pln, asset_id, is_primary, yahoo_ticker, company_backed)
		SELECT $1::text, $1::text, $2::portfolio_enum, 'XTB', 'ETF', 'CASH', 'PLN', 'PLN', 0, 0, 0, 0, 0, a.id, true, $4, true FROM a`,
		ticker, portfolio, pgnum.FromDecimal(decimal.RequireFromString(targetPct)), yahooTicker)
	if err != nil {
		t.Fatalf("insert fixture position %s: %v", ticker, err)
	}
}

// TestCalculate_GroupedAssets_RealPostgres (task 156): with grouped assets in real Postgres the
// value fold sums each asset's subassets (one entry per asset, keyed by its primary ticker), the
// buy list carries exactly ONE item per asset (never one per subasset row), currentPct reflects
// the folded value, and the macro de-risk map holds one interpolated target per MAIN asset's
// primary (both regime tables read from the assets row, summing to 100).
func TestCalculate_GroupedAssets_RealPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, cash_positions CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE settings SET value = 'false' WHERE key = 'rebalancing.macro_derisk_enabled'`); err != nil {
		t.Fatal(err)
	}

	// Asset G = primary G1 + non-primary G2 (targets 60 on / 20 off); asset H = single H1 (40 / 80).
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO assets (portfolio, key, name, exposure, asset_class, target_allocation_pct, target_allocation_pct_risk_off) VALUES
		('MAIN', 'g', 'G', 'CRYPTO', 'ETF', 60, 20), ('MAIN', 'h', 'H', 'CRYPTO', 'ETF', 40, 80)`)
	mustExec(`INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency, purchase_currency,
			units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln, asset_id, is_primary, yahoo_ticker, company_backed)
		SELECT v.ticker, v.ticker, 'MAIN', 'XTB', 'ETF', 'CRYPTO', 'PLN', 'PLN', 0, 0, 0, 0, v.units, a.id, v.is_primary, v.yahoo, true
		FROM (VALUES ('G1', 'g', true, 1, 'G1.Y'), ('G2', 'g', false, 2, 'G2.Y'), ('H1', 'h', true, 1, 'H1.Y')) AS v(ticker, akey, is_primary, units, yahoo)
		JOIN assets a ON a.key = v.akey AND a.portfolio = 'MAIN'`)

	queries := sqlcgen.New(pool)
	prices := intTestPrices{
		prices: map[string]decimal.Decimal{"G1.Y": decimal.NewFromInt(100), "G2.Y": decimal.NewFromInt(50), "H1.Y": decimal.NewFromInt(100)},
		rates:  map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)},
	}

	// Gate disabled: targets 60/40 straight from the assets rows.
	cash := decimal.NewFromInt(2000)
	res, err := Calculate(ctx, queries, prices, PortfolioMAIN, &cash)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if !res.TotalPortfolioValuePln.Equal(decimal.NewFromInt(300)) { // G1 100 + G2 2x50 + H1 100
		t.Fatalf("expected total 300 (subasset values folded once), got %s", res.TotalPortfolioValuePln)
	}
	if len(res.BuyList) != 2 {
		t.Fatalf("expected exactly one buy-list item per asset, got %+v", res.BuyList)
	}
	seen := map[string]BuyListItem{}
	for _, it := range res.BuyList {
		seen[it.Ticker] = it
	}
	g, okG := seen["G1"]
	if _, hasSub := seen["G2"]; hasSub || !okG || seen["H1"].Ticker != "H1" {
		t.Fatalf("expected items keyed by the primaries G1 and H1 only, got %v", seen)
	}
	if !g.CurrentPct.Equal(decimal.RequireFromString("66.6667")) { // (100 + 100) / 300
		t.Fatalf("expected G's currentPct = folded 200/300 = 66.6667, got %s", g.CurrentPct)
	}
	if !g.TargetPct.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("expected G's target 60 from the assets row, got %s", g.TargetPct)
	}

	// Gate enabled, no macro_state row (= full risk-on): one override per MAIN asset, keyed by the
	// primary ticker, equal to the asset's risk-on target and summing to 100.
	mustExec(`UPDATE settings SET value = 'true' WHERE key = 'rebalancing.macro_derisk_enabled'`)
	overrides, err := LoadMainTargetOverrides(ctx, queries)
	if err != nil {
		t.Fatalf("LoadMainTargetOverrides: %v", err)
	}
	if len(overrides) != 2 || !overrides["G1"].Equal(decimal.NewFromInt(60)) || !overrides["H1"].Equal(decimal.NewFromInt(40)) {
		t.Fatalf("expected {G1:60, H1:40} (primaries only), got %v", overrides)
	}
	// Risk-off side of the same rows (blend 0) via the pure interpolation: 20 + 80 = 100.
	rows, err := queries.ListAllPositions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assetRows, err := queries.ListAllAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	all, err := DecodeAllPositions(rows, assetRows)
	if err != nil {
		t.Fatal(err)
	}
	zero := decimal.Zero
	off := computeOverrides(mainPrimaries(all), &zero)
	if len(off) != 2 || !off["G1"].Equal(decimal.NewFromInt(20)) || !off["H1"].Equal(decimal.NewFromInt(80)) {
		t.Fatalf("expected the risk-off table {G1:20, H1:80}, got %v", off)
	}
}
