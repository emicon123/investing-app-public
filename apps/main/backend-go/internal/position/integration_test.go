package position

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgtx"
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
func (p intTestPrices) RateToPln(_ context.Context, currency string) (decimal.Decimal, error) {
	if v, ok := p.rates[currency]; ok {
		return v, nil
	}
	return decimal.NewFromInt(1), nil
}

// TestPosition_RealPostgresRoundTrip exercises asset CRUD + set-primary against a real
// postgres:17.10-alpine container, migrated by the same Flyway CLI image used in prod — the
// "never mock the database" gate. Covers: create (inline newAsset)/get/update/delete
// round-trip with NUMERIC precision, computed-field degradation when a price is genuinely
// unavailable, and the set-primary boolean flip (task 156, replaces promote) committing
// atomically via its own transact.
func TestPosition_RealPostgresRoundTrip(t *testing.T) {
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

	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
	prices := intTestPrices{
		prices: map[string]decimal.Decimal{"A.YAHOO": decimal.NewFromInt(100)},
		rates:  map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)},
	}
	svc := NewService(queries, transact, prices)

	var assetID, subassetID int64

	t.Run("Create then Get round-trips", func(t *testing.T) {
		yahoo := "A.YAHOO"
		target := decimal.NewFromInt(100)
		dto, err := svc.Create(ctx, CreateInput{
			Ticker: "A", Name: "Asset A", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF",
			PriceCurrency: "PLN",
			NewAsset:      &NewAssetInput{Name: "Asset A", Exposure: "CRYPTO", AssetClass: "ETF", TargetAllocationPct: &target},
			YahooTicker:   &yahoo,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		assetID = dto.ID
		if len(dto.Subassets) != 1 || !dto.Subassets[0].IsPrimary {
			t.Fatalf("expected 1 trivially-primary subasset, got %+v", dto.Subassets)
		}
		subassetID = dto.Subassets[0].ID
		got, err := svc.Get(ctx, subassetID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Key != "A" || got.Portfolio != "MAIN" {
			t.Fatalf("unexpected dto: %+v", got)
		}
		if got.TargetAllocationPct == nil || !got.TargetAllocationPct.Equal(decimal.NewFromInt(100)) {
			t.Fatalf("expected asset target 100, got %v", got.TargetAllocationPct)
		}
		if got.Computed.PricePln == nil || !got.Computed.PricePln.Equal(decimal.NewFromInt(100)) {
			t.Fatalf("expected computed pricePln=100, got %+v", got.Computed)
		}
	})

	t.Run("Update applies partial fields only", func(t *testing.T) {
		units := decimal.NewFromInt(5)
		got, err := svc.Update(ctx, subassetID, UpdateInput{UnitsRegularPln: &units})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if len(got.Subassets) != 1 || !got.Subassets[0].UnitsRegularPln.Equal(decimal.NewFromInt(5)) {
			t.Fatalf("expected unitsRegularPln=5, got %+v", got.Subassets)
		}
		if got.TargetAllocationPct == nil || !got.TargetAllocationPct.Equal(decimal.NewFromInt(100)) {
			t.Fatalf("expected targetAllocationPct untouched at 100, got %v", got.TargetAllocationPct)
		}
	})

	t.Run("SetPrimary flips boolean atomically", func(t *testing.T) {
		subID := mustInsertSubasset(ctx, t, pool, assetID, "A-SUB", "MAIN", false)
		flipped, err := svc.SetPrimary(ctx, assetID, subID)
		if err != nil {
			t.Fatalf("SetPrimary: %v", err)
		}
		if flipped.Subassets[0].Ticker != "A-SUB" || !flipped.Subassets[0].IsPrimary {
			t.Fatalf("expected A-SUB first and primary, got %+v", flipped.Subassets)
		}
		var oldPrimary bool
		if err := pool.QueryRow(ctx, `SELECT is_primary FROM positions WHERE id = $1`, subassetID).
			Scan(&oldPrimary); err != nil {
			t.Fatal(err)
		}
		if oldPrimary {
			t.Fatal("expected old primary demoted")
		}
	})

	t.Run("Delete primary with siblings is 409, then whole asset", func(t *testing.T) {
		// subassetID is now non-primary with a sibling — deleting the CURRENT primary fails.
		var primaryID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM positions WHERE asset_id = $1 AND is_primary`, assetID).Scan(&primaryID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(ctx, primaryID); err == nil {
			t.Fatal("expected 409 deleting a primary with siblings")
		}
		// delete both subassets one at a time (non-primary first, then the last)...
		var ids []int64
		rows, err := pool.Query(ctx, `SELECT id FROM positions WHERE asset_id = $1 ORDER BY is_primary`, assetID)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if err := svc.Delete(ctx, id); err != nil {
				t.Fatalf("Delete %d: %v", id, err)
			}
		}
		// Task 173 (ADR-109): soft delete — rows stay (deleted_at set), live reads no longer see them.
		var remaining, softDeleted int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE deleted_at IS NULL), COUNT(*) FILTER (WHERE deleted_at IS NOT NULL) FROM positions`).Scan(&remaining, &softDeleted); err != nil {
			t.Fatal(err)
		}
		if remaining != 0 || softDeleted != len(ids) {
			t.Fatalf("expected every subasset soft-deleted and kept, got %d live / %d soft-deleted", remaining, softDeleted)
		}
		var assetsLeft int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM assets WHERE deleted_at IS NULL`).Scan(&assetsLeft); err != nil {
			t.Fatal(err)
		}
		if assetsLeft != 0 {
			t.Fatalf("expected asset soft-deleted with its last subasset, got %d live", assetsLeft)
		}
		for _, id := range ids {
			if _, err := svc.Get(ctx, id); err == nil {
				t.Fatalf("GET of soft-deleted subasset %d must be 404", id)
			}
		}
	})
}

func mustInsertSubasset(ctx context.Context, t *testing.T, pool *pgxpool.Pool, assetID int64, ticker, portfolio string, primary bool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO positions (ticker, name, portfolio, exchange, asset_class, exposure, price_currency,
			purchase_currency, units_ike, units_ikze, units_regular_eur, units_regular_usd, units_regular_pln,
			asset_id, is_primary, company_backed)
		VALUES ($1, $1, $2, 'XTB', 'ETF', 'CRYPTO', 'PLN', 'PLN', 0, 0, 0, 0, 0, $3, $4, true)
		RETURNING id`,
		ticker, portfolio, assetID, primary).Scan(&id)
	if err != nil {
		t.Fatalf("insert fixture subasset %s: %v", ticker, err)
	}
	return id
}

// TestPosition_RealPostgres_AssetSemantics covers the task-156 semantics that hinge on real
// constraints: tri-state target clearing through NUMERIC NULL, the set-primary Σ == 100
// tripwire rolling the flip back, attach-to-asset with the partial-UNIQUE primary index, the
// (key, portfolio) UNIQUE -> 409, and DELETE /assets cascading through the FK.
func TestPosition_RealPostgres_AssetSemantics(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets CASCADE`); err != nil {
		t.Fatal(err)
	}
	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
	svc := NewService(queries, transact, intTestPrices{
		prices: map[string]decimal.Decimal{}, rates: map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)},
	})

	create := func(ticker string, target string, in func(*CreateInput)) AssetDTO {
		t.Helper()
		ci := CreateInput{Ticker: ticker, Name: ticker, Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN"}
		if target != "" {
			v := decimal.RequireFromString(target)
			ci.NewAsset = &NewAssetInput{Name: ticker + " asset", Exposure: "CRYPTO", AssetClass: "ETF", TargetAllocationPct: &v}
		}
		if in != nil {
			in(&ci)
		}
		dto, err := svc.Create(ctx, ci)
		if err != nil {
			t.Fatalf("Create %s: %v", ticker, err)
		}
		return dto
	}

	a := create("A", "60", nil)
	b := create("B", "30", nil) // book sums to 90: NOT 100
	attached := create("A-WRAP", "", func(ci *CreateInput) { ci.AssetID = &a.ID })
	if len(attached.Subassets) != 2 || attached.Subassets[1].IsPrimary {
		t.Fatalf("expected A-WRAP attached as a non-primary second subasset, got %+v", attached.Subassets)
	}
	wrapID := attached.Subassets[1].ID

	t.Run("a second primary and a duplicate key are rejected by the real constraints", func(t *testing.T) {
		yes := true
		_, err := svc.Create(ctx, CreateInput{Ticker: "A-2P", Name: "x", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN", AssetID: &a.ID, IsPrimary: &yes})
		if err == nil {
			t.Fatal("expected 409 for a second primary")
		}
		_, err = svc.Create(ctx, CreateInput{Ticker: "A", Name: "dup", Portfolio: "MAIN", Exchange: "BINANCE", AssetClass: "ETF", PriceCurrency: "PLN",
			NewAsset: &NewAssetInput{Name: "dup", Exposure: "CRYPTO", AssetClass: "ETF"}})
		if err == nil {
			t.Fatal("expected 409 for a duplicate (key, portfolio)")
		}
	})

	t.Run("target edits are range-only and null clears through NUMERIC NULL", func(t *testing.T) {
		if _, err := svc.UpdateAsset(ctx, b.ID, UpdateAssetInput{TargetAllocationPct: setDec(decimal.NewFromInt(20))}); err != nil {
			t.Fatalf("a Σ != 100 edit must succeed: %v", err)
		}
		cleared, err := svc.UpdateAsset(ctx, b.ID, UpdateAssetInput{TargetAllocationPct: nullDec()})
		if err != nil || cleared.TargetAllocationPct != nil {
			t.Fatalf("null must clear the stored target, got %v (%v)", cleared.TargetAllocationPct, err)
		}
		var isNull bool
		if err := pool.QueryRow(ctx, `SELECT target_allocation_pct IS NULL FROM assets WHERE id = $1`, b.ID).Scan(&isNull); err != nil || !isNull {
			t.Fatalf("expected SQL NULL, got isNull=%v err=%v", isNull, err)
		}
		if _, err := svc.UpdateAsset(ctx, b.ID, UpdateAssetInput{TargetAllocationPct: setDec(decimal.NewFromInt(40))}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("set-primary tripwire rolls back; a complete book flips", func(t *testing.T) {
		if _, err := svc.SetPrimary(ctx, a.ID, wrapID); err == nil { // 60 + 40 = 100 -> passes
			// book is complete here, so the flip must commit; flip it back below
			var primaryID int64
			if err := pool.QueryRow(ctx, `SELECT id FROM positions WHERE asset_id = $1 AND is_primary`, a.ID).Scan(&primaryID); err != nil || primaryID != wrapID {
				t.Fatalf("expected the wrapper to be primary after the flip, got %d (%v)", primaryID, err)
			}
		} else {
			t.Fatalf("SetPrimary on a Σ=100 book: %v", err)
		}
		// break the book (sum 60 + 30 = 90) and try to flip back
		if _, err := svc.UpdateAsset(ctx, b.ID, UpdateAssetInput{TargetAllocationPct: setDec(decimal.NewFromInt(30))}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetPrimary(ctx, a.ID, a.Subassets[0].ID); err == nil {
			t.Fatal("expected the Σ tripwire to reject the flip")
		}
		var primaryID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM positions WHERE asset_id = $1 AND is_primary`, a.ID).Scan(&primaryID); err != nil || primaryID != wrapID {
			t.Fatalf("a rejected flip must leave exactly the previous primary, got %d (%v)", primaryID, err)
		}
	})

	t.Run("DeleteAsset cascades to every subasset", func(t *testing.T) {
		if err := svc.DeleteAsset(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		// Task 173 (ADR-109): the cascade is a soft delete — both rows kept with deleted_at set.
		var left, kept int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE deleted_at IS NULL), COUNT(*) FROM positions WHERE ticker IN ('A','A-WRAP')`).Scan(&left, &kept); err != nil || left != 0 || kept != 2 {
			t.Fatalf("expected DeleteAsset to soft-delete both subassets and keep the rows, %d live of %d (%v)", left, kept, err)
		}
		if err := svc.DeleteAsset(ctx, a.ID); err == nil {
			t.Fatal("a second DeleteAsset of a soft-deleted asset must be 404")
		}
	})
}

// TestPosition_RealPostgres_ReHome (design §13): re-homing a non-primary subasset moves its
// value from one asset's Σ to the other's through the real FK, and only asset_id changes.
func TestPosition_RealPostgres_ReHome(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets CASCADE`); err != nil {
		t.Fatal(err)
	}
	queries := sqlcgen.New(pool)
	svc := NewService(queries, nil, intTestPrices{
		prices: map[string]decimal.Decimal{"XP.Y": decimal.NewFromInt(10), "XS.Y": decimal.NewFromInt(10), "YP.Y": decimal.NewFromInt(10)},
		rates:  map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)},
	})

	newAsset := func(ticker, yahoo string, units string, target int64) AssetDTO {
		t.Helper()
		y := yahoo
		tg := decimal.NewFromInt(target)
		dto, err := svc.Create(ctx, CreateInput{Ticker: ticker, Name: ticker, Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN",
			YahooTicker: &y, NewAsset: &NewAssetInput{Name: ticker, Exposure: "CRYPTO", AssetClass: "ETF", TargetAllocationPct: &tg}})
		if err != nil {
			t.Fatal(err)
		}
		u := decimal.RequireFromString(units)
		if _, err := svc.Update(ctx, dto.Subassets[0].ID, UpdateInput{UnitsRegularPln: &u}); err != nil {
			t.Fatal(err)
		}
		return dto
	}
	x := newAsset("XP", "XP.Y", "10", 60) // value 100
	yA := newAsset("YP", "YP.Y", "5", 40) // value 50
	ys := "XS.Y"
	xsDTO, err := svc.Create(ctx, CreateInput{Ticker: "XS", Name: "XS", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN", YahooTicker: &ys, AssetID: &x.ID})
	if err != nil {
		t.Fatal(err)
	}
	var xsID int64
	for _, s := range xsDTO.Subassets {
		if s.Ticker == "XS" {
			xsID = s.ID
		}
	}
	u3 := decimal.NewFromInt(3)
	if _, err := svc.Update(ctx, xsID, UpdateInput{UnitsRegularPln: &u3}); err != nil {
		t.Fatal(err)
	}

	valueOf := func(id int64) decimal.Decimal {
		t.Helper()
		got, err := svc.Get(ctx, id)
		if err != nil || got.Computed.ValuePln == nil {
			t.Fatalf("Get %d: %v (%+v)", id, err, got.Computed)
		}
		return *got.Computed.ValuePln
	}
	if v := valueOf(x.Subassets[0].ID); !v.Equal(decimal.NewFromInt(130)) {
		t.Fatalf("expected asset X = 100 + 30, got %s", v)
	}
	if v := valueOf(yA.Subassets[0].ID); !v.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("expected asset Y = 50, got %s", v)
	}

	moved, err := svc.Update(ctx, xsID, UpdateInput{AssetID: &yA.ID})
	if err != nil {
		t.Fatalf("re-home: %v", err)
	}
	if moved.ID != yA.ID {
		t.Fatalf("expected the response to be the new parent (Y=%d), got %d", yA.ID, moved.ID)
	}
	if v := valueOf(x.Subassets[0].ID); !v.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("expected X to lose the moved subasset's value (100), got %s", v)
	}
	if v := valueOf(yA.Subassets[0].ID); !v.Equal(decimal.NewFromInt(80)) {
		t.Fatalf("expected Y to gain it (50 + 30), got %s", v)
	}
	var isPrimary bool
	var units string
	if err := pool.QueryRow(ctx, `SELECT is_primary, units_regular_pln::text FROM positions WHERE id = $1`, xsID).Scan(&isPrimary, &units); err != nil || isPrimary || units != "3.00000000" {
		t.Fatalf("the moved subasset must stay non-primary with its units, got primary=%v units=%s err=%v", isPrimary, units, err)
	}
	// a primary cannot be re-homed (409), through the real service path
	if _, err := svc.Update(ctx, x.Subassets[0].ID, UpdateInput{AssetID: &yA.ID}); err == nil {
		t.Fatal("expected 409 re-homing a primary")
	}
}
