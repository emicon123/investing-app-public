package position

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestPosition_RealPostgres_EntryPriceFieldsOnTheWire is task 161c-1's integration gate: real
// Postgres (migrated by the Flyway CLI image), seeded multi-subasset assets, the service's List and
// the OpenAPI mapping — the JSON a client receives.
//   - bitcoin (MAIN): BTC-USD (USD primary, cost 200 000) + PL.ETNVIRBTCP (cost 100 000): the owner's
//     example -> entry 300 000 PLN = 75 000 USD, equivalentUnits 1, no costless subassets;
//   - gold (MAIN): head cost + two cost-less wrappers -> both named, primary first then ticker ASC;
//   - retirement (RETIREMENT): the same rows as bitcoin -> the same profit/entry/native/equivalentUnits
//     and no costless subassets (ADR-104: RETIREMENT computes exactly like MAIN);
//   - untracked (MAIN, all NULL cost — the portfolioB shape): entry fields omitted, equivalentUnits there.
func TestPosition_RealPostgres_EntryPriceFieldsOnTheWire(t *testing.T) {
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

	newAsset := func(portfolio, key string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO assets (portfolio, key, name, exposure, asset_class)
			VALUES ($1::portfolio_enum, $2, $3, 'CRYPTO', 'CRYPTO') RETURNING id`, portfolio, key, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// sub seeds one subasset: units in the PLN bucket (UnitsTotal sums every bucket), an optional
	// recorded cost ("" = NULL) and the yahoo ticker the fake price source resolves.
	sub := func(assetID int64, ticker, portfolio string, primary bool, currency, yahoo, units, cost string) {
		t.Helper()
		id := mustInsertSubasset(ctx, t, pool, assetID, ticker, portfolio, primary)
		if _, err := pool.Exec(ctx, `UPDATE positions SET price_currency = $2::currency_enum, yahoo_ticker = $3,
			units_regular_pln = $4::numeric, total_spent_pln = NULLIF($5, '')::numeric WHERE id = $1`,
			id, currency, yahoo, units, cost); err != nil {
			t.Fatal(err)
		}
	}

	btc := newAsset("MAIN", "bitcoin")
	sub(btc, "BTC-USD", "MAIN", true, "USD", "BTC-USD", "0.5", "200000")
	sub(btc, "PL.ETNVIRBTCP", "MAIN", false, "PLN", "ETN.Y", "5000", "100000")
	gold := newAsset("MAIN", "gold")
	sub(gold, "ETCGLDRMAU.PL", "MAIN", true, "PLN", "ETC.Y", "10", "50000")
	sub(gold, "IGLN.L", "MAIN", false, "PLN", "IGLN.Y", "2", "")
	sub(gold, "4GLD.DE", "MAIN", false, "PLN", "GLD4.Y", "3", "")
	ret := newAsset("RETIREMENT", "retirement")
	sub(ret, "BTC-USD", "RETIREMENT", true, "USD", "BTC-USD", "0.5", "200000")
	sub(ret, "PL.ETNVIRBTCP", "RETIREMENT", false, "PLN", "ETN.Y", "5000", "100000")
	untracked := newAsset("MAIN", "untracked")
	sub(untracked, "AAA", "MAIN", true, "PLN", "AAA.Y", "10", "")
	sub(untracked, "BBB", "MAIN", false, "PLN", "BBB.Y", "4", "")

	svc := NewService(sqlcgen.New(pool), nil, intTestPrices{
		prices: map[string]decimal.Decimal{
			"BTC-USD": decimal.NewFromInt(480000), "ETN.Y": decimal.NewFromInt(48),
			"ETC.Y": decimal.RequireFromString("4000"), "IGLN.Y": decimal.RequireFromString("7500"), "GLD4.Y": decimal.RequireFromString("5000"),
			"AAA.Y": decimal.NewFromInt(100), "BBB.Y": decimal.NewFromInt(50),
		},
		rates: map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1), "USD": decimal.NewFromInt(4)},
	})
	assets, _, err := svc.List(ctx, "", 0, 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	wire := map[string]map[string]any{} // asset key -> the JSON `computed` object a client receives
	byKey := map[string]AssetDTO{}
	for _, a := range assets {
		byKey[a.Key] = a
		raw, err := json.Marshal(toOpenAPIAsset(a))
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Computed map[string]any `json:"computed"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		wire[a.Key] = decoded.Computed
	}
	if len(wire) != 4 {
		t.Fatalf("expected 4 assets, got %d", len(wire))
	}
	num := func(key, field string) float64 {
		t.Helper()
		v, ok := wire[key][field].(float64)
		if !ok {
			t.Fatalf("%s: computed.%s missing or not a number in %v", key, field, wire[key])
		}
		return v
	}
	absent := func(key string, fields ...string) {
		t.Helper()
		for _, f := range fields {
			if v, present := wire[key][f]; present {
				t.Fatalf("%s: computed.%s must be omitted, got %v", key, f, v)
			}
		}
	}

	// bitcoin — the owner's example on real rows.
	if got := num("bitcoin", "avgBuyPricePln"); got != 300000 {
		t.Fatalf("bitcoin avgBuyPricePln = %v, want 300000", got)
	}
	if got := num("bitcoin", "avgBuyPriceNative"); got != 75000 {
		t.Fatalf("bitcoin avgBuyPriceNative = %v, want 75000 (300000 PLN / USDPLN 4)", got)
	}
	if got := num("bitcoin", "equivalentUnits"); got != 1 {
		t.Fatalf("bitcoin equivalentUnits = %v, want 1", got)
	}
	if got := num("bitcoin", "profitPct"); got != 0.6 {
		t.Fatalf("bitcoin profitPct = %v, want 0.6", got)
	}
	absent("bitcoin", "costlessSubassets") // both rows carry cost

	// gold — the cost-less wrappers are named (primary first is the primary itself, costed; then ticker ASC).
	if !reflect.DeepEqual(byKey["gold"].Computed.CostlessSubassets, []string{"4GLD.DE", "IGLN.L"}) {
		t.Fatalf("gold costlessSubassets = %v, want [4GLD.DE IGLN.L]", byKey["gold"].Computed.CostlessSubassets)
	}
	list, ok := wire["gold"]["costlessSubassets"].([]any)
	if !ok || len(list) != 2 || list[0] != "4GLD.DE" || list[1] != "IGLN.L" {
		t.Fatalf("gold wire costlessSubassets = %v", wire["gold"]["costlessSubassets"])
	}
	if got := num("gold", "avgBuyPriceNative"); got != num("gold", "avgBuyPricePln") {
		t.Fatalf("gold has a PLN primary: native %v must equal PLN %v", got, num("gold", "avgBuyPricePln"))
	}

	// RETIREMENT — ADR-104: no portfolio branch, so the same rows as bitcoin give the same figures.
	for _, field := range []string{"profitPct", "avgBuyPricePln", "avgBuyPriceNative", "equivalentUnits"} {
		if got, want := num("retirement", field), num("bitcoin", field); got != want {
			t.Fatalf("retirement %s = %v, want the MAIN-shaped %v", field, got, want)
		}
	}
	if got := num("retirement", "avgBuyPriceNative"); got != 75000 {
		t.Fatalf("retirement avgBuyPriceNative = %v, want 75000", got)
	}
	absent("retirement", "costlessSubassets") // both rows carry cost

	// untracked (every cost NULL) — entry fields omitted, equivalentUnits (needs no cost) present.
	absent("untracked", "profitPct", "avgBuyPricePln", "avgBuyPriceNative", "costlessSubassets")
	if got := num("untracked", "equivalentUnits"); got != 12 { // (10*100 + 4*50) / 100
		t.Fatalf("untracked equivalentUnits = %v, want 12", got)
	}
}
