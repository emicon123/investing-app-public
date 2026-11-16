package position_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/siblingcore"
	scposition "investing-app/common-go/siblingcore/position"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"investing-app/common-go/siblingcore/refdata"
	"investing-app/common-go/testsupport"
	"portfolio-b/backend-go/db/sqlcgen"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/rebalancing"
	"portfolio-b/backend-go/internal/store"
)

// softDeletePrices is a cache-only price source satisfying both position.PriceSource and
// rebalancing.PriceSource (this is an external test package, so the in-package fakes are not
// visible).
type softDeletePrices struct{}

func (softDeletePrices) PricePlnByYahooTicker(context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{"VVMX.DE": decimal.NewFromInt(50), "KEEP.WA": decimal.NewFromInt(10)}, nil
}
func (softDeletePrices) AllRatesToPln(context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1), "EUR": decimal.RequireFromString("4.3")}, nil
}
func (softDeletePrices) FetchedAtByYahooTicker(context.Context) (map[string]time.Time, error) {
	return map[string]time.Time{}, nil
}

// TestSoftDelete_RealPostgres is task 173's acceptance regression (portfolioB VVMX, asset 22): an
// asset with position_transfers + purchase_lots rows deletes with 204 semantics, history stays
// intact, the asset disappears from every current-holdings read, and re-adding the ticker
// restores the same rows (design docs/architecture/diagrams/173-asset-soft-delete.md §5.2/§6).
func TestSoftDelete_RealPostgres(t *testing.T) {
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets CASCADE`); err != nil {
		t.Fatal(err)
	}

	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, store.Bind)
	svc := scposition.NewService(book.Config, store.New(pool), transact, softDeletePrices{})
	str := func(s string) *string { return &s }
	dec := func(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

	// KEEP (target 100) with two subassets; VVMX (target 40, so Σ = 140 while VVMX is live)
	// with a primary and a sibling.
	keep, err := svc.Create(ctx, scposition.CreateInput{
		Ticker: "KEEP", Name: "Keep", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
		YahooTicker: str("KEEP.WA"),
		NewAsset:    &scposition.NewAssetInput{Name: "Keep", Exposure: "STOCKS", AssetClass: "ETF", TargetAllocationPct: dec("100")},
	})
	if err != nil {
		t.Fatal(err)
	}
	keep2, err := svc.Create(ctx, scposition.CreateInput{
		Ticker: "KEEP-2", Name: "Keep 2", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN", AssetID: &keep.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	vv, err := svc.Create(ctx, scposition.CreateInput{
		Ticker: "VVMX", Name: "VanEck Rare Earth", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "EUR",
		YahooTicker: str("VVMX.DE"), SortOrder: func() *int { v := 3; return &v }(),
		NewAsset: &scposition.NewAssetInput{Name: "VVMX", Exposure: "STOCKS", AssetClass: "ETF", TargetAllocationPct: dec("40")},
	})
	if err != nil {
		t.Fatal(err)
	}
	vvID, vvSubID := vv.ID, vv.Subassets[0].ID
	vvSib, err := svc.Create(ctx, scposition.CreateInput{
		Ticker: "VVMX-B", Name: "VVMX second", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "EUR", AssetID: &vvID,
	})
	if err != nil {
		t.Fatal(err)
	}
	vvSibID := vvSib.Subassets[1].ID
	if _, err := svc.Update(ctx, vvSubID, scposition.UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{book.IkzeMama: *dec("18")}}); err != nil {
		t.Fatal(err)
	}
	// History: a transfer VVMX -> KEEP (both legs) and a purchase lot of VVMX.
	if _, err := pool.Exec(ctx, `INSERT INTO position_transfers (transfer_id, ticker, account_type, units_delta, price_pln, total_pln) VALUES
		('7f41cca4-0000-4000-8000-000000000001', 'VVMX', 'IKZE_MAMA', -18, 50, -900),
		('7f41cca4-0000-4000-8000-000000000001', 'KEEP', 'IKZE_MAMA', 90, 10, 900)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_lots (session_id, purchased_at, ticker, units, price_pln, cost_pln)
		VALUES ('11111111-0000-4000-8000-000000000001', '2026-01-15', 'VVMX', 18, 40, 720)`); err != nil {
		t.Fatal(err)
	}
	countHistory := func() (transfers, lots int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM position_transfers`).Scan(&transfers); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_lots`).Scan(&lots); err != nil {
			t.Fatal(err)
		}
		return transfers, lots
	}
	testsupport.AssertAssetInvariants(t, ctx, pool)

	t.Run("(c) deleting a primary with live siblings is 409 and writes nothing", func(t *testing.T) {
		err := svc.Delete(ctx, vvSubID)
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != 409 {
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

	t.Run("(a) delete an asset with transfers and lots", func(t *testing.T) {
		if err := svc.DeleteAsset(ctx, vvID); err != nil {
			t.Fatalf("DeleteAsset must succeed despite history FKs: %v", err)
		}
		if tr, lots := countHistory(); tr != 2 || lots != 1 {
			t.Fatalf("history must stay intact, got %d transfer legs / %d lots", tr, lots)
		}
		list, _, err := svc.List(ctx, siblingcore.WholeBook, 0, 100)
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
			t.Fatalf("calculator input (ListAllPositions/ListAllAssets) must exclude VVMX, got %d rows / %d assets", len(rows), len(assets))
		}
		tracked, err := refdata.New(pool).ListTrackedTickers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, tr := range tracked {
			if tr.YahooTicker == "VVMX.DE" {
				t.Fatal("price refresh must not track a deleted ticker")
			}
		}
		snap, err := store.New(pool).ListSubassets(ctx, siblingcore.WholeBook)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range snap {
			if r.Ticker == "VVMX" || r.Ticker == "VVMX-B" {
				t.Fatal("historia snapshot input (the live subassets) must not include a deleted ticker")
			}
		}
		var appErr *httpx.AppError
		if _, err := svc.Get(ctx, vvSubID); !errors.As(err, &appErr) || appErr.Status != 404 {
			t.Fatalf("GET /positions/{deleted sub} must be 404, got %v", err)
		}
		if err := svc.DeleteAsset(ctx, vvID); !errors.As(err, &appErr) || appErr.Status != 404 {
			t.Fatalf("a second DELETE must be 404, got %v", err)
		}
		// Σ guard: KEEP alone is 100; the deleted VVMX (40) must not count.
		if _, err := svc.SetPrimary(ctx, keep.ID, keep2.Subassets[1].ID); err != nil {
			t.Fatalf("Σ guard must ignore the deleted asset: %v", err)
		}
		testsupport.AssertAssetInvariants(t, ctx, pool)
	})

	t.Run("(d) purchase history still names the deleted asset", func(t *testing.T) {
		hist, err := screbalancing.NewService(book.Config, store.New(pool), pgtx.Runner(pool, store.Bind), softDeletePrices{}, rebalancing.NewFunding(), nil).History(ctx, siblingcore.WholeBook)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, s := range hist {
			for _, l := range s.Lots {
				if l.Ticker == "VVMX" {
					found = l.Name != nil && *l.Name == "VanEck Rare Earth" && l.PriceAvailable
				}
			}
		}
		if !found {
			t.Fatalf("expected the VVMX lot with its name and price, got %+v", hist)
		}
	})

	t.Run("(b) re-creating the ticker restores the same rows", func(t *testing.T) {
		dto, err := svc.Create(ctx, scposition.CreateInput{
			Ticker: "VVMX", Name: "VanEck renamed", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "EUR",
			NewAsset: &scposition.NewAssetInput{Name: "VVMX again", Exposure: "STOCKS", AssetClass: "ETF"},
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
		sub := dto.Subassets[0]
		if sub.Name != "VanEck renamed" || !sub.Units.Of(book.IkzeMama).Equal(decimal.NewFromInt(18)) {
			t.Fatalf("new name, kept units expected, got %+v", sub)
		}
		var yahoo string
		var sortOrder int
		var sibDeleted bool
		if err := pool.QueryRow(ctx, `SELECT yahoo_ticker, sort_order FROM positions WHERE id = $1 AND deleted_at IS NULL`, vvSubID).Scan(&yahoo, &sortOrder); err != nil {
			t.Fatal(err)
		}
		if yahoo != "VVMX.DE" || sortOrder != 3 {
			t.Fatalf("omitted optional settings must be kept, got yahoo=%s sort=%d", yahoo, sortOrder)
		}
		if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM positions WHERE id = $1`, vvSibID).Scan(&sibDeleted); err != nil {
			t.Fatal(err)
		}
		if !sibDeleted {
			t.Fatal("the deleted sibling must stay deleted until re-added")
		}
		if tr, lots := countHistory(); tr != 2 || lots != 1 {
			t.Fatalf("history must stay intact, got %d / %d", tr, lots)
		}
		testsupport.AssertAssetInvariants(t, ctx, pool)

		// a live duplicate stays 409 DUPLICATE_ENTRY
		_, err = svc.Create(ctx, scposition.CreateInput{
			Ticker: "VVMX", Name: "x", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "EUR",
			NewAsset: &scposition.NewAssetInput{Name: "x", Exposure: "STOCKS", AssetClass: "ETF"},
		})
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Code != httpx.CodeDuplicateEntry {
			t.Fatalf("expected 409 DUPLICATE_ENTRY, got %v", err)
		}
	})
}
