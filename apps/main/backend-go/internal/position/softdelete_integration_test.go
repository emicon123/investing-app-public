package position_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/position"
	"investing-app/backend-go/internal/purchasesession"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// softDeletePrices is a cache-only price source satisfying position.PriceSource and
// purchasesession.PriceSource (this is an external test package — the in-package fakes are not
// visible, and purchasesession imports position, so only an external package can use both).
type softDeletePrices struct{}

func (softDeletePrices) PricePlnByYahooTicker(context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{"VVMX.DE": decimal.NewFromInt(50), "KEEP.WA": decimal.NewFromInt(10)}, nil
}
func (softDeletePrices) AllRatesToPln(context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1), "EUR": decimal.RequireFromString("4.3"), "USD": decimal.NewFromInt(4)}, nil
}
func (softDeletePrices) RateToPln(_ context.Context, currency string) (decimal.Decimal, error) {
	if currency == "EUR" {
		return decimal.RequireFromString("4.3"), nil
	}
	return decimal.NewFromInt(4), nil
}

// TestSoftDelete_RealPostgres is task 173's soft-delete regression for main (design
// docs/architecture/diagrams/173-asset-soft-delete.md §5.1/§6): deleting an asset with history
// keeps every history row, hides it from every current-holdings read, the purchase-session
// detail still resolves it, and re-adding the natural key restores the same rows.
func TestSoftDelete_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets, asset_purchases, purchase_sessions, purchase_session_items CASCADE`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) position.Queries { return queries.WithTx(tx) })
	svc := position.NewService(queries, transact, softDeletePrices{})
	str := func(s string) *string { return &s }
	dec := func(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }
	no := false

	keep, err := svc.Create(ctx, position.CreateInput{
		Ticker: "KEEP", Name: "Keep", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN",
		YahooTicker: str("KEEP.WA"),
		NewAsset:    &position.NewAssetInput{Name: "Keep", Exposure: "EQUITY", AssetClass: "ETF", TargetAllocationPct: dec("100")},
	})
	if err != nil {
		t.Fatal(err)
	}
	keep2, err := svc.Create(ctx, position.CreateInput{
		Ticker: "KEEP-2", Name: "Keep 2", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN", AssetID: &keep.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	vv, err := svc.Create(ctx, position.CreateInput{
		Ticker: "VVMX", Name: "VanEck Rare Earth", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "EUR",
		YahooTicker: str("VVMX.DE"), PurchaseCurrency: str("EUR"), CompanyBacked: &no,
		NewAsset: &position.NewAssetInput{Name: "VVMX", Exposure: "EQUITY", AssetClass: "ETF", TargetAllocationPct: dec("40")},
	})
	if err != nil {
		t.Fatal(err)
	}
	vvID, vvSubID := vv.ID, vv.Subassets[0].ID
	vvSib, err := svc.Create(ctx, position.CreateInput{
		Ticker: "VVMX-B", Name: "VVMX second", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "EUR", AssetID: &vvID,
	})
	if err != nil {
		t.Fatal(err)
	}
	vvSibID := vvSib.Subassets[1].ID
	if _, err := svc.Update(ctx, vvSubID, position.UpdateInput{UnitsRegularEur: dec("18")}); err != nil {
		t.Fatal(err)
	}
	// History: a planned session item and an asset_purchases row for VVMX.
	var sessionID int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_sessions DEFAULT VALUES RETURNING id`).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_session_items
		(session_id, portfolio, ticker, account_type, planned_units, planned_value_pln, price_pln_at_plan, is_migration_sell, is_migration_buy)
		VALUES ($1, 'MAIN', 'VVMX', 'REGULAR', 2, 100, 50, false, false)`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO asset_purchases (session_id, portfolio, ticker, account_type, units_delta, price_pln, total_pln, trade_type)
		VALUES ($1, 'MAIN', 'VVMX', 'REGULAR', 18, 40, 720, 'DCA_BUY')`, sessionID); err != nil {
		t.Fatal(err)
	}
	countHistory := func() (purchases, items int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM asset_purchases`).Scan(&purchases); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_session_items`).Scan(&items); err != nil {
			t.Fatal(err)
		}
		return purchases, items
	}
	testsupport.AssertAssetInvariants(t, ctx, pool)

	t.Run("(c) deleting a primary with live siblings is 409 and writes nothing", func(t *testing.T) {
		var appErr *httpx.AppError
		if err := svc.Delete(ctx, vvSubID); !errors.As(err, &appErr) || appErr.Status != 409 {
			t.Fatalf("expected 409, got %v", err)
		}
		var deleted int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM positions WHERE deleted_at IS NOT NULL`).Scan(&deleted); err != nil {
			t.Fatal(err)
		}
		if deleted != 0 {
			t.Fatalf("a rejected delete must write nothing, %d rows deleted", deleted)
		}
		testsupport.AssertAssetInvariants(t, ctx, pool)
	})

	t.Run("(a) delete an asset with history", func(t *testing.T) {
		if err := svc.DeleteAsset(ctx, vvID); err != nil {
			t.Fatal(err)
		}
		if p, i := countHistory(); p != 1 || i != 1 {
			t.Fatalf("history must stay intact, got %d purchases / %d items", p, i)
		}
		list, _, err := svc.List(ctx, "MAIN", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != keep.ID {
			t.Fatalf("GET /positions must show only KEEP, got %+v", list)
		}
		rows, err := queries.ListAllPositions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		assets, err := queries.ListAllAssets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || len(assets) != 1 {
			t.Fatalf("calculator input must exclude VVMX, got %d rows / %d assets", len(rows), len(assets))
		}
		tracked, err := queries.GetTrackedTickers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, tr := range tracked {
			if tr.YahooTicker != nil && *tr.YahooTicker == "VVMX.DE" {
				t.Fatal("price refresh must not track a deleted ticker")
			}
		}
		snap, err := queries.ListHistoriaPositions(ctx, sqlcgen.PortfolioEnumMAIN)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range snap {
			if r.Ticker == "VVMX" || r.Ticker == "VVMX-B" {
				t.Fatal("historia snapshot input must not include a deleted ticker")
			}
		}
		var appErr *httpx.AppError
		if _, err := svc.Get(ctx, vvSubID); !errors.As(err, &appErr) || appErr.Status != 404 {
			t.Fatalf("GET /positions/{deleted sub} must be 404, got %v", err)
		}
		if err := svc.DeleteAsset(ctx, vvID); !errors.As(err, &appErr) || appErr.Status != 404 {
			t.Fatalf("a second DELETE must be 404, got %v", err)
		}
		if _, err := svc.SetPrimary(ctx, keep.ID, keep2.Subassets[1].ID); err != nil {
			t.Fatalf("Σ guard must ignore the deleted asset: %v", err)
		}
		testsupport.AssertAssetInvariants(t, ctx, pool)
	})

	t.Run("(d) session detail still resolves the deleted asset", func(t *testing.T) {
		ps := purchasesession.NewService(queries, nil, nil, softDeletePrices{})
		detail, err := ps.GetActiveSession(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.Items) != 1 || detail.Items[0].Ticker != "VVMX" ||
			detail.Items[0].PriceCurrency != "EUR" || detail.Items[0].Exchange != "XTB" {
			t.Fatalf("expected the VVMX item enriched from the deleted position, got %+v", detail.Items)
		}
	})

	t.Run("(b) re-creating the natural key restores the same rows", func(t *testing.T) {
		dto, err := svc.Create(ctx, position.CreateInput{
			Ticker: "VVMX", Name: "VanEck renamed", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "EUR",
			NewAsset: &position.NewAssetInput{Name: "VVMX again", Exposure: "EQUITY", AssetClass: "ETF"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if dto.ID != vvID || len(dto.Subassets) != 1 || dto.Subassets[0].ID != vvSubID || !dto.Subassets[0].IsPrimary {
			t.Fatalf("expected the same ids restored with only VVMX live, got %+v", dto)
		}
		if dto.Name != "VVMX again" || dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(decimal.NewFromInt(40)) {
			t.Fatalf("new name, kept target expected, got %+v", dto)
		}
		if sub := dto.Subassets[0]; sub.Name != "VanEck renamed" || !sub.UnitsRegularEur.Equal(decimal.NewFromInt(18)) {
			t.Fatalf("new name, kept units expected, got %+v", sub)
		}
		var yahoo, purchaseCurrency string
		var companyBacked, sibDeleted bool
		if err := pool.QueryRow(ctx, `SELECT yahoo_ticker, purchase_currency::text, company_backed FROM positions WHERE id = $1 AND deleted_at IS NULL`,
			vvSubID).Scan(&yahoo, &purchaseCurrency, &companyBacked); err != nil {
			t.Fatal(err)
		}
		if yahoo != "VVMX.DE" || purchaseCurrency != "EUR" || companyBacked {
			t.Fatalf("omitted optional settings must be kept, got %s/%s/%v", yahoo, purchaseCurrency, companyBacked)
		}
		if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM positions WHERE id = $1`, vvSibID).Scan(&sibDeleted); err != nil {
			t.Fatal(err)
		}
		if !sibDeleted {
			t.Fatal("the deleted sibling must stay deleted until re-added")
		}
		if p, i := countHistory(); p != 1 || i != 1 {
			t.Fatalf("history must stay intact, got %d / %d", p, i)
		}
		testsupport.AssertAssetInvariants(t, ctx, pool)

		_, err = svc.Create(ctx, position.CreateInput{
			Ticker: "VVMX", Name: "x", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "EUR",
			NewAsset: &position.NewAssetInput{Name: "x", Exposure: "EQUITY", AssetClass: "ETF"},
		})
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Code != httpx.CodeDuplicateEntry {
			t.Fatalf("expected 409 DUPLICATE_ENTRY, got %v", err)
		}
	})
}
