package position

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
	"investing-app/common-go/testsupport"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/internal/store"
)

// TestPosition_RealPostgres exercises the asset/subasset CRUD + set-primary path against
// portfolioC's own migrated database (real Postgres via testcontainers-go + the same Flyway CLI
// image used in prod) — never mock the database. TRUNCATEs `positions` and `assets` right
// after connecting, so the test's own fixtures start from an empty book and the Σ==100
// invariant is checked against them only.
func TestPosition_RealPostgres(t *testing.T) {
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

	prices := &fakePriceSource{prices: map[string]decimal.Decimal{}, rates: map[string]decimal.Decimal{"PLN": decimal.RequireFromString("1")}}
	// The production transaction runner (cmd/server/main.go's positionTransact), so the
	// set-primary atomicity assertions run against a real BEGIN/COMMIT/ROLLBACK.
	transact := pgtx.Runner(pool, store.Bind)
	svc := scposition.NewService(book.Config, store.New(pool), transact, prices)

	var assetID, subID int64
	t.Run("create under a new asset", func(t *testing.T) {
		half := decimal.RequireFromString("100")
		dto, err := svc.Create(ctx, scposition.CreateInput{
			Portfolio: "VARIABLE", Ticker: "IT-TEST", Name: "Integration Test ETF", Exchange: "XTB", AssetClass: "ETF",
			Exposure: "STOCKS", PriceCurrency: "PLN",
			NewAsset: &scposition.NewAssetInput{Name: "Integration Test ETF", Exposure: "STOCKS", AssetClass: "ETF", TargetAllocationPct: &half},
		})
		if err != nil {
			t.Fatal(err)
		}
		assetID = dto.ID
		if dto.Key != "IT-TEST" || dto.Portfolio != "VARIABLE" {
			t.Fatalf("unexpected create result: %+v", dto)
		}
		if len(dto.Subassets) != 1 || !dto.Subassets[0].IsPrimary {
			t.Fatalf("expected one primary subasset, got %+v", dto.Subassets)
		}
		subID = dto.Subassets[0].ID
		if !dto.Subassets[0].Units.Of(siblingcore.SingleBucket).IsZero() {
			t.Fatalf("expected zero units, got %s", dto.Subassets[0].Units.Of(siblingcore.SingleBucket))
		}
		if dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(half) {
			t.Fatalf("expected asset target 100, got %+v", dto.TargetAllocationPct)
		}

		got, err := svc.Get(ctx, subID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Key != "IT-TEST" {
			t.Fatalf("expected IT-TEST, got %s", got.Key)
		}
	})

	t.Run("update writes units", func(t *testing.T) {
		newUnits := decimal.RequireFromString("12.5")
		dto, err := svc.Update(ctx, subID, scposition.UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{siblingcore.SingleBucket: newUnits}})
		if err != nil {
			t.Fatal(err)
		}
		if len(dto.Subassets) != 1 || !dto.Subassets[0].Units.Of(siblingcore.SingleBucket).Equal(newUnits) {
			t.Fatalf("expected units=12.5, got %+v", dto.Subassets)
		}
	})

	t.Run("same ticker in the other portfolio is independent", func(t *testing.T) {
		half := decimal.RequireFromString("100")
		dto, err := svc.Create(ctx, scposition.CreateInput{
			Portfolio: "FIXED", Ticker: "IT-TEST", Name: "Same ticker, other portfolio", Exchange: "XTB", AssetClass: "ETF",
			Exposure: "STOCKS", PriceCurrency: "PLN",
			NewAsset: &scposition.NewAssetInput{Name: "Same ticker, other portfolio", Exposure: "STOCKS", AssetClass: "ETF", TargetAllocationPct: &half},
		})
		if err != nil {
			t.Fatal(err)
		}
		if dto.ID == assetID {
			t.Fatal("expected a distinct asset row for the other portfolio")
		}
		variableList, _, err := svc.List(ctx, "VARIABLE", 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range variableList {
			if item.Key == "IT-TEST" && !item.Subassets[0].Units.Of(siblingcore.SingleBucket).Equal(decimal.RequireFromString("12.5")) {
				t.Fatalf("expected VARIABLE's IT-TEST to be unaffected by creating FIXED's, got %+v", item.Subassets)
			}
		}
	})

	t.Run("attach a second subasset then set-primary flips", func(t *testing.T) {
		attached, err := svc.Create(ctx, scposition.CreateInput{
			Portfolio: "VARIABLE", Ticker: "IT-TEST-2", Name: "Second leg", Exchange: "XTB", AssetClass: "ETF",
			Exposure: "STOCKS", PriceCurrency: "PLN", AssetID: &assetID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(attached.Subassets) != 2 || attached.Subassets[0].ID != subID {
			t.Fatalf("expected primary-first ordering, got %+v", attached.Subassets)
		}
		flipped, err := svc.SetPrimary(ctx, assetID, attached.Subassets[1].ID)
		if err != nil {
			t.Fatal(err)
		}
		if flipped.Subassets[0].Ticker != "IT-TEST-2" || !flipped.Subassets[0].IsPrimary {
			t.Fatalf("expected IT-TEST-2 primary-first, got %+v", flipped.Subassets)
		}
		// Targets never move: still on the asset, still 50.
		if flipped.TargetAllocationPct == nil || !flipped.TargetAllocationPct.Equal(decimal.RequireFromString("100")) {
			t.Fatalf("expected asset target untouched at 100, got %+v", flipped.TargetAllocationPct)
		}
	})

	t.Run("set-primary is atomic and never touches cost basis, units or targets", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE positions SET total_spent_pln = 1000.5, avg_buy_price = 42.25 WHERE asset_id = $1`, assetID); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			var out string
			if err := pool.QueryRow(ctx, `
				SELECT string_agg(ticker || ':' || units || ':' || total_spent_pln || ':' || avg_buy_price, ',' ORDER BY ticker)
				FROM positions WHERE asset_id = $1`, assetID).Scan(&out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		before := snapshot()

		// A subasset of a DIFFERENT asset fails inside the transaction: the "clear old
		// primary" half must roll back (one primary still, the same one).
		var otherSub int64
		if err := pool.QueryRow(ctx, `
			WITH a AS (INSERT INTO assets (portfolio, key, name, exposure, asset_class) VALUES ('VARIABLE', 'IT-OTHER', 'other', 'STOCKS', 'ETF') RETURNING id)
			INSERT INTO positions (portfolio, ticker, name, exchange, asset_class, exposure, price_currency, asset_id, is_primary)
			SELECT 'VARIABLE', 'IT-OTHER', 'other', 'XTB', 'ETF', 'STOCKS', 'PLN', id, true FROM a RETURNING id`).Scan(&otherSub); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetPrimary(ctx, assetID, otherSub); err == nil {
			t.Fatal("expected a foreign subasset to be rejected")
		}
		var primaries int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM positions WHERE asset_id = $1 AND is_primary`, assetID).Scan(&primaries); err != nil {
			t.Fatal(err)
		}
		if primaries != 1 || snapshot() != before {
			t.Fatalf("a rejected flip must roll back completely: %d primaries, snapshot %q -> %q", primaries, before, snapshot())
		}
		if _, err := pool.Exec(ctx, `DELETE FROM assets WHERE key = 'IT-OTHER'`); err != nil {
			t.Fatal(err)
		}

		// A real flip (IT-TEST-2 is currently primary; flip back to IT-TEST) changes only the
		// is_primary flags: every unit/cost figure is byte-identical.
		if _, err := svc.SetPrimary(ctx, assetID, subID); err != nil {
			t.Fatal(err)
		}
		if snapshot() != before {
			t.Fatalf("set-primary moved data: %q -> %q", before, snapshot())
		}
		var primaryTicker string
		if err := pool.QueryRow(ctx, `SELECT ticker FROM positions WHERE asset_id = $1 AND is_primary`, assetID).Scan(&primaryTicker); err != nil || primaryTicker != "IT-TEST" {
			t.Fatalf("expected IT-TEST primary again, got %q (%v)", primaryTicker, err)
		}
	})

	t.Run("re-home moves a subasset's value between two assets and rolls nothing else", func(t *testing.T) {
		// IT-TEST is primary at this point of the sequence: attach a fresh non-primary sibling
		// (the row to move) and a second asset as the destination.
		sib, err := svc.Create(ctx, scposition.CreateInput{
			Portfolio: "VARIABLE", Ticker: "IT-REHOME", Name: "rehome me", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
			AssetID: &assetID,
		})
		if err != nil {
			t.Fatal(err)
		}
		var sibID int64
		for _, sd := range sib.Subassets {
			if sd.Ticker == "IT-REHOME" {
				sibID = sd.ID
			}
		}
		dest, err := svc.Create(ctx, scposition.CreateInput{
			Portfolio: "VARIABLE", Ticker: "IT-DEST", Name: "dest", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
			NewAsset: &scposition.NewAssetInput{Name: "dest", Exposure: "STOCKS", AssetClass: "ETF"},
		})
		if err != nil {
			t.Fatal(err)
		}
		other, err := svc.Create(ctx, scposition.CreateInput{
			Portfolio: "FIXED", Ticker: "IT-OTHER-PF", Name: "other portfolio", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
			NewAsset: &scposition.NewAssetInput{Name: "other portfolio", Exposure: "STOCKS", AssetClass: "ETF"},
		})
		if err != nil {
			t.Fatal(err)
		}
		pricesLocal := &fakePriceSource{prices: map[string]decimal.Decimal{"IT-REHOME.Y": decimal.RequireFromString("10")}, rates: map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}}
		if _, err := pool.Exec(ctx, `UPDATE positions SET yahoo_ticker = 'IT-REHOME.Y', units = 4, total_spent_pln = 33, avg_buy_price = 8 WHERE id = $1`, sibID); err != nil {
			t.Fatal(err)
		}
		local := scposition.NewService(book.Config, store.New(pool), transact, pricesLocal)
		// valueOf is the Σ of an asset's priced subassets — the value the asset folds to.
		valueOf := func(id int64) string {
			items, _, err := local.List(ctx, "VARIABLE", 0, 50)
			if err != nil {
				t.Fatal(err)
			}
			sum := decimal.Zero
			for _, a := range items {
				if a.ID == id {
					for _, sd := range a.Subassets {
						if sd.ValuePln != nil {
							sum = sum.Add(*sd.ValuePln)
						}
					}
				}
			}
			return sum.String()
		}
		if src, dst := valueOf(assetID), valueOf(dest.ID); src != "40" || dst != "0" {
			t.Fatalf("precondition: source Σ=40 and destination Σ=0, got %s / %s", src, dst)
		}

		// The other portfolio's asset is refused by the service (400) and nothing moves.
		var appErr *httpx.AppError
		if _, err := local.Update(ctx, sibID, scposition.UpdateInput{AssetID: &other.ID}); !errors.As(err, &appErr) || appErr.Status != 400 {
			t.Fatalf("expected 400 for a cross-portfolio asset, got %v", err)
		}

		moved, err := local.Update(ctx, sibID, scposition.UpdateInput{AssetID: &dest.ID})
		if err != nil {
			t.Fatal(err)
		}
		if moved.ID != dest.ID {
			t.Fatalf("expected the response to be the destination asset, got %d", moved.ID)
		}
		var isPrimary bool
		var spent, avg string
		if err := pool.QueryRow(ctx, `SELECT is_primary, total_spent_pln::text, avg_buy_price::text FROM positions WHERE id = $1`, sibID).Scan(&isPrimary, &spent, &avg); err != nil {
			t.Fatal(err)
		}
		if isPrimary || !decimal.RequireFromString(spent).Equal(decimal.NewFromInt(33)) || !decimal.RequireFromString(avg).Equal(decimal.NewFromInt(8)) {
			t.Fatalf("re-home must leave is_primary=false and the cost data untouched, got primary=%v spent=%s avg=%s", isPrimary, spent, avg)
		}
		// The 40 PLN (4 units x 10) left the source asset's Σ and joined the destination's.
		if got := valueOf(dest.ID); got != "40" {
			t.Fatalf("destination Σ: got %s want 40", got)
		}
		if got := valueOf(assetID); got != "0" {
			t.Fatalf("source Σ must drop to 0 after the move, got %s", got)
		}
		// Rules against the real FK: a primary cannot move; an unknown asset is a 404.
		if _, err := local.Update(ctx, dest.Subassets[0].ID, scposition.UpdateInput{AssetID: &assetID}); !errors.As(err, &appErr) || appErr.Status != 409 {
			t.Fatalf("expected 409 moving a primary, got %v", err)
		}
		unknown := int64(987654)
		if _, err := local.Update(ctx, sibID, scposition.UpdateInput{AssetID: &unknown}); !errors.As(err, &appErr) || appErr.Status != 404 {
			t.Fatalf("expected 404 for an unknown asset, got %v", err)
		}
		// An edit that omits assetId keeps the row where it is (real full-row overwrite).
		units := decimal.RequireFromString("5")
		if _, err := local.Update(ctx, sibID, scposition.UpdateInput{Units: map[siblingcore.Bucket]decimal.Decimal{siblingcore.SingleBucket: units}}); err != nil {
			t.Fatal(err)
		}
		var homedAt int64
		if err := pool.QueryRow(ctx, `SELECT asset_id FROM positions WHERE id = $1`, sibID).Scan(&homedAt); err != nil || homedAt != dest.ID {
			t.Fatalf("an edit without assetId must keep the row under the destination, got asset %d (%v)", homedAt, err)
		}
		// Clean up so later subtests see the sequence they expect.
		if err := local.DeleteAsset(ctx, dest.ID); err != nil {
			t.Fatal(err)
		}
		if err := local.DeleteAsset(ctx, other.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("update asset persists target and name; a partial edit keeps the rest", func(t *testing.T) {
		target, name := decimal.RequireFromString("80"), "Renamed asset"
		dto, err := svc.UpdateAsset(ctx, assetID, scposition.UpdateAssetInput{TargetAllocationPct: setDec(target), Name: &name})
		if err != nil {
			t.Fatal(err)
		}
		if dto.Name != name || dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(target) {
			t.Fatalf("unexpected DTO %+v", dto)
		}
		again := "Renamed twice"
		dto, err = svc.UpdateAsset(ctx, assetID, scposition.UpdateAssetInput{Name: &again})
		if err != nil {
			t.Fatal(err)
		}
		if !dto.TargetAllocationPct.Equal(target) {
			t.Fatalf("a name-only edit must keep the target, got %s", dto.TargetAllocationPct)
		}
		// null clears (NULL = no target), then 100 restores it for the later subtests.
		cleared, err := svc.UpdateAsset(ctx, assetID, scposition.UpdateAssetInput{TargetAllocationPct: httpx.Optional[decimal.Decimal]{Set: true}})
		if err != nil || cleared.TargetAllocationPct != nil {
			t.Fatalf("explicit null must clear the target, got %+v (%v)", cleared.TargetAllocationPct, err)
		}
		hundred := decimal.RequireFromString("100")
		if _, err := svc.UpdateAsset(ctx, assetID, scposition.UpdateAssetInput{TargetAllocationPct: setDec(hundred)}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("delete primary with siblings is 409", func(t *testing.T) {
		data, _, err := svc.List(ctx, "VARIABLE", 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		var primaryID int64
		for _, a := range data {
			if a.ID == assetID {
				primaryID = a.Subassets[0].ID
			}
		}
		if err := svc.Delete(ctx, primaryID); err == nil {
			t.Fatal("expected a 409 deleting a primary that still has siblings")
		}
	})

	t.Run("delete: non-primary first, then the last subasset removes the asset row", func(t *testing.T) {
		// IT-TEST is primary again after the set-primary subtest; IT-TEST-2 is the sibling.
		var siblingID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM positions WHERE ticker = 'IT-TEST-2' AND portfolio = 'VARIABLE'`).Scan(&siblingID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(ctx, siblingID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(ctx, subID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Get(ctx, subID); err == nil {
			t.Fatal("expected 404 after delete")
		}
		var assets int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM assets WHERE id = $1 AND deleted_at IS NULL`, assetID).Scan(&assets); err != nil {
			t.Fatal(err)
		}
		if assets != 0 {
			t.Fatal("expected the asset soft-deleted together with its last subasset (task 173)")
		}
	})

	t.Run("delete asset cascades to every subasset", func(t *testing.T) {
		created, err := svc.Create(ctx, scposition.CreateInput{
			Portfolio: "VARIABLE", Ticker: "IT-CASCADE", Name: "c", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
			NewAsset: &scposition.NewAssetInput{Name: "c", Exposure: "STOCKS", AssetClass: "ETF"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Create(ctx, scposition.CreateInput{
			Portfolio: "VARIABLE", Ticker: "IT-CASCADE-2", Name: "c2", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
			AssetID: &created.ID,
		}); err != nil {
			t.Fatal(err)
		}
		if err := svc.DeleteAsset(ctx, created.ID); err != nil {
			t.Fatal(err)
		}
		var left int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM positions WHERE ticker LIKE 'IT-CASCADE%' AND deleted_at IS NULL`).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Fatalf("expected DELETE /assets to soft-delete every subasset (task 173), %d live subassets remain", left)
		}
	})

	t.Run("duplicate ticker in the same portfolio is a unique violation mapped to 409", func(t *testing.T) {
		in := scposition.CreateInput{
			Portfolio: "VARIABLE", Ticker: "IT-DUP", Name: "d", Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN",
			NewAsset: &scposition.NewAssetInput{Name: "d", Exposure: "STOCKS", AssetClass: "ETF"},
		}
		if _, err := svc.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
		_, err := svc.Create(ctx, in)
		var appErr *httpx.AppError
		if !errors.As(err, &appErr) || appErr.Status != 409 {
			t.Fatalf("expected 409, got %v", err)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM assets WHERE key = 'IT-DUP' AND portfolio = 'VARIABLE'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("expected exactly one IT-DUP asset, got %d (%v)", n, err)
		}
	})
}

func setDec(d decimal.Decimal) httpx.Optional[decimal.Decimal] {
	return httpx.Optional[decimal.Decimal]{Set: true, Value: &d}
}
