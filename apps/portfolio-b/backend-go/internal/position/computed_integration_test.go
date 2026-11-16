package position

import (
	"context"
	"encoding/json"
	"reflect"
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

// TestPosition_RealPostgres_EntryPriceFieldsOnTheWire is task 161c-1's integration gate for portfolioB:
// real Postgres (migrated by the Flyway CLI image), seeded multi-subasset assets, the service's
// List and the wire mapping — the JSON a client receives.
//   - bitcoin: BTC-USD (USD primary, cost 200 000) + PL.ETNVIRBTCP (cost 100 000): the owner's example
//     -> entry 300 000 PLN = 75 000 USD, equivalentUnits 1, no costless subassets;
//   - gold: costed head + two cost-less wrappers -> both named (ticker ASC after the primary);
//   - untracked: every cost NULL (portfolioB today) -> entry fields omitted, equivalentUnits present.
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

	newAsset := func(key string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO assets (key, name, exposure, asset_class)
			VALUES ($1, $2, 'CRYPTO', 'CRYPTO') RETURNING id`, key, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// sub seeds one subasset: units in one bucket (UnitsTotal sums all five), an optional recorded
	// cost ("" = NULL) and the yahoo ticker the fake price source resolves.
	sub := func(assetID int64, ticker string, primary bool, currency, yahoo, units, cost string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO positions (ticker, name, exchange, asset_class, exposure, price_currency,
				units_inne, total_spent_pln, asset_id, is_primary, yahoo_ticker)
			VALUES ($1::text, $1::text, 'XTB', 'CRYPTO', 'CRYPTO', $2::currency_enum, $3::numeric, NULLIF($4, '')::numeric, $5, $6, $7)`,
			ticker, currency, units, cost, assetID, primary, yahoo); err != nil {
			t.Fatal(err)
		}
	}

	btc := newAsset("bitcoin")
	sub(btc, "BTC-USD", true, "USD", "BTC-USD", "0.5", "200000")
	sub(btc, "PL.ETNVIRBTCP", false, "PLN", "ETN.Y", "5000", "100000")
	gold := newAsset("gold")
	sub(gold, "ETCGLDRMAU.PL", true, "PLN", "ETC.Y", "10", "50000")
	sub(gold, "IGLN.L", false, "PLN", "IGLN.Y", "2", "")
	sub(gold, "4GLD.DE", false, "PLN", "GLD4.Y", "3", "")
	untracked := newAsset("untracked")
	sub(untracked, "AAA", true, "PLN", "AAA.Y", "10", "")
	sub(untracked, "BBB", false, "PLN", "BBB.Y", "4", "")

	svc := scposition.NewService(book.Config, store.New(pool), nil, &fakePriceSource{
		prices: map[string]decimal.Decimal{
			"BTC-USD": decimal.NewFromInt(480000), "ETN.Y": decimal.NewFromInt(48),
			"ETC.Y": decimal.RequireFromString("4000"), "IGLN.Y": decimal.RequireFromString("7500"), "GLD4.Y": decimal.RequireFromString("5000"),
			"AAA.Y": decimal.NewFromInt(100), "BBB.Y": decimal.NewFromInt(50),
		},
		rates: map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1), "USD": decimal.NewFromInt(4)},
	})
	assets, _, err := svc.List(ctx, siblingcore.WholeBook, 0, 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	wire := map[string]map[string]any{}
	byKey := map[string]scposition.AssetDTO{}
	for _, a := range assets {
		byKey[a.Key] = a
		raw, err := json.Marshal(toWireAsset(a))
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
	if len(wire) != 3 {
		t.Fatalf("expected 3 assets, got %d", len(wire))
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
	absent("bitcoin", "costlessSubassets")

	if !reflect.DeepEqual(byKey["gold"].Computed.CostlessSubassets, []string{"4GLD.DE", "IGLN.L"}) {
		t.Fatalf("gold costlessSubassets = %v, want [4GLD.DE IGLN.L]", byKey["gold"].Computed.CostlessSubassets)
	}
	list, ok := wire["gold"]["costlessSubassets"].([]any)
	if !ok || len(list) != 2 || list[0] != "4GLD.DE" || list[1] != "IGLN.L" {
		t.Fatalf("gold wire costlessSubassets = %v", wire["gold"]["costlessSubassets"])
	}
	if got, pln := num("gold", "avgBuyPriceNative"), num("gold", "avgBuyPricePln"); got != pln {
		t.Fatalf("gold has a PLN primary: native %v must equal PLN %v", got, pln)
	}

	absent("untracked", "profitPct", "avgBuyPricePln", "avgBuyPriceNative", "costlessSubassets")
	if got := num("untracked", "equivalentUnits"); got != 12 { // (10*100 + 4*50) / 100
		t.Fatalf("untracked equivalentUnits = %v, want 12", got)
	}
}
