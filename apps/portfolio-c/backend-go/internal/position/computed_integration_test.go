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
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestPosition_RealPostgres_EntryPriceFieldsOnTheWire is task 161c-1's integration gate for portfolioC:
// real Postgres (migrated by the Flyway CLI image), seeded assets in BOTH portfolios, the service's
// List and the wire mapping — the JSON a client receives. Same-ticker rows in different portfolios
// are different holdings and must never mix:
//   - VARIABLE gold: 4GLD.DE (EUR primary, cost 6 000) + IGLN.L (PLN, no cost) -> entry 400 PLN =
//     100 EUR at EURPLN 4, equivalentUnits 15, costlessSubassets [IGLN.L];
//   - FIXED gold: the SAME ticker 4GLD.DE, every cost NULL -> entry fields omitted, equivalentUnits 4;
//   - FIXED sxrv: one costed row at break-even -> entry = price, native = price / fx, no costless.
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
			VALUES ($1::portfolio_enum, $2::text, $2::text, 'STOCKS', 'ETF') RETURNING id`, portfolio, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// sub seeds one subasset: units, an optional recorded cost ("" = NULL) and the yahoo ticker the
	// fake price source resolves.
	sub := func(assetID int64, portfolio, ticker string, primary bool, currency, yahoo, units, cost string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency,
				units, total_spent_pln, asset_id, is_primary, yahoo_ticker)
			VALUES ($1::portfolio_enum, $2::text, $2::text, 'XTB', 'ETF', 'STOCKS', $3::currency_enum, $4::numeric, NULLIF($5, '')::numeric, $6, $7, $8)`,
			portfolio, ticker, currency, units, cost, assetID, primary, yahoo); err != nil {
			t.Fatal(err)
		}
	}

	varGold := newAsset("VARIABLE", "gold")
	sub(varGold, "VARIABLE", "4GLD.DE", true, "EUR", "GLD.Y", "10", "6000")
	sub(varGold, "VARIABLE", "IGLN.L", false, "PLN", "IGLN.Y", "5", "")
	fixGold := newAsset("FIXED", "gold")
	sub(fixGold, "FIXED", "4GLD.DE", true, "EUR", "GLD.Y", "4", "")
	fixSxrv := newAsset("FIXED", "sxrv")
	sub(fixSxrv, "FIXED", "SXRV.DE", true, "EUR", "SXRV.Y", "2", "1600")

	svc := scposition.NewService(book.Config, store.New(pool), nil, &fakePriceSource{
		prices: map[string]decimal.Decimal{
			"GLD.Y": decimal.NewFromInt(480), "IGLN.Y": decimal.NewFromInt(480), "SXRV.Y": decimal.NewFromInt(800),
		},
		rates: map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1), "EUR": decimal.NewFromInt(4)},
	})

	wireOf := func(portfolio string) (map[string]map[string]any, map[string]scposition.AssetDTO) {
		t.Helper()
		assets, _, err := svc.List(ctx, siblingcore.Portfolio(portfolio), 0, 50)
		if err != nil {
			t.Fatalf("List %s: %v", portfolio, err)
		}
		wire := map[string]map[string]any{}
		byKey := map[string]scposition.AssetDTO{}
		for _, a := range assets {
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
			wire[a.Key], byKey[a.Key] = decoded.Computed, a
		}
		return wire, byKey
	}
	varWire, varAssets := wireOf("VARIABLE")
	fixWire, _ := wireOf("FIXED")
	if len(varWire) != 1 || len(fixWire) != 2 {
		t.Fatalf("expected 1 VARIABLE and 2 FIXED assets, got %d / %d", len(varWire), len(fixWire))
	}
	num := func(wire map[string]map[string]any, key, field string) float64 {
		t.Helper()
		v, ok := wire[key][field].(float64)
		if !ok {
			t.Fatalf("%s: computed.%s missing or not a number in %v", key, field, wire[key])
		}
		return v
	}
	absent := func(wire map[string]map[string]any, key string, fields ...string) {
		t.Helper()
		for _, f := range fields {
			if v, present := wire[key][f]; present {
				t.Fatalf("%s: computed.%s must be omitted, got %v", key, f, v)
			}
		}
	}

	// VARIABLE gold — costed head + a cost-less wrapper.
	if got := num(varWire, "gold", "avgBuyPricePln"); got != 400 {
		t.Fatalf("VARIABLE gold avgBuyPricePln = %v, want 400", got)
	}
	if got := num(varWire, "gold", "avgBuyPriceNative"); got != 100 {
		t.Fatalf("VARIABLE gold avgBuyPriceNative = %v, want 100 (400 PLN / EURPLN 4)", got)
	}
	if got := num(varWire, "gold", "equivalentUnits"); got != 15 {
		t.Fatalf("VARIABLE gold equivalentUnits = %v, want 15", got)
	}
	if got := num(varWire, "gold", "profitPct"); got != 0.2 {
		t.Fatalf("VARIABLE gold profitPct = %v, want 0.2", got)
	}
	if !reflect.DeepEqual(varAssets["gold"].Computed.CostlessSubassets, []string{"IGLN.L"}) {
		t.Fatalf("VARIABLE gold costlessSubassets = %v, want [IGLN.L]", varAssets["gold"].Computed.CostlessSubassets)
	}
	if list, ok := varWire["gold"]["costlessSubassets"].([]any); !ok || len(list) != 1 || list[0] != "IGLN.L" {
		t.Fatalf("VARIABLE gold wire costlessSubassets = %v", varWire["gold"]["costlessSubassets"])
	}

	// FIXED gold — the SAME ticker, its own holding with no cost: nothing from VARIABLE leaks in.
	absent(fixWire, "gold", "profitPct", "avgBuyPricePln", "avgBuyPriceNative", "costlessSubassets")
	if got := num(fixWire, "gold", "equivalentUnits"); got != 4 {
		t.Fatalf("FIXED gold equivalentUnits = %v, want 4", got)
	}

	// FIXED sxrv — a single costed row at break-even: entry = today's price.
	if got := num(fixWire, "sxrv", "avgBuyPricePln"); got != 800 {
		t.Fatalf("FIXED sxrv avgBuyPricePln = %v, want 800", got)
	}
	if got := num(fixWire, "sxrv", "avgBuyPriceNative"); got != 200 {
		t.Fatalf("FIXED sxrv avgBuyPriceNative = %v, want 200 (800 PLN / EURPLN 4)", got)
	}
	absent(fixWire, "sxrv", "costlessSubassets")
}
