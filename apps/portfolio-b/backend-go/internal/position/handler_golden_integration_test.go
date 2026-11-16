package position

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/pgtx"
	scposition "investing-app/common-go/siblingcore/position"
	"investing-app/common-go/testsupport"
	"investing-app/common-go/testsupport/golden"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
	"portfolio-b/backend-go/openapi"
)

// TestPositionHandler_Golden pins the wire bytes of every position endpoint (task 180 wave 0)
// before the service moves into common-go/siblingcore: the handler is built exactly as
// cmd/server/main.go builds it and driven with httptest, and each response is compared with
// testdata/golden/position_<case>.json (volatile ids/timestamps masked). Re-record with -update.
func TestPositionHandler_Golden(t *testing.T) {
	pool := goldenPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE positions, assets CASCADE;
		INSERT INTO price_cache (ticker, price, currency, price_pln) VALUES
			('GA1.WA', 10, 'PLN', 10), ('GA2.DE', 5, 'EUR', 21.5), ('GB1.WA', 50, 'PLN', 50);
		INSERT INTO fx_rate_cache (from_currency, rate_to_pln, source) VALUES ('EUR', 4.3, 'TEST')`); err != nil {
		t.Fatal(err)
	}
	h := goldenPositionHandler(pool)

	check := func(name string, status int, body []byte) {
		t.Helper()
		golden.Response(t, "position_"+name, status, body)
	}
	idOf := func(body []byte) (assetID int64, subIDs []int64) {
		t.Helper()
		var env struct {
			Data struct {
				ID        int64 `json:"id"`
				Subassets []struct {
					ID int64 `json:"id"`
				} `json:"subassets"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatal(err)
		}
		for _, s := range env.Data.Subassets {
			subIDs = append(subIDs, s.ID)
		}
		return env.Data.ID, subIDs
	}
	create := func(body string) (int, []byte) {
		return golden.Call(h.CreatePosition, http.MethodPost, "/api/positions", body)
	}

	st, b := create(`{"ticker":"GA-1","name":"Gold A","exchange":"XTB","assetClass":"ETF","exposure":"METALS","priceCurrency":"PLN","yahooTicker":"GA1.WA","sortOrder":1,
		"newAsset":{"name":"Gold","exposure":"METALS","assetClass":"ETF","targetAllocationPct":60,"targetAllocationPctRiskOff":70}}`)
	check("create_new_asset", st, b)
	assetA, subs := idOf(b)
	ga1 := subs[0]

	st, b = create(fmt.Sprintf(`{"ticker":"GA-2","name":"Gold A EUR","exchange":"XTB","assetClass":"ETF","exposure":"METALS","priceCurrency":"EUR","yahooTicker":"GA2.DE","assetId":%d}`, assetA))
	check("create_existing_asset", st, b)
	_, subs = idOf(b)
	ga2 := subs[1]

	st, b = create(`{"ticker":"GB-1","name":"Bonds B","exchange":"XTB","assetClass":"ETF","exposure":"STOCKS","priceCurrency":"PLN","yahooTicker":"GB1.WA","sortOrder":2,
		"newAsset":{"name":"Bonds","exposure":"STOCKS","assetClass":"ETF","targetAllocationPct":40}}`)
	check("create_second_asset", st, b)
	assetB, subsB := idOf(b)

	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdatePosition(w, r, ga1) }, http.MethodPut, "/", `{"unitsIkeTata":10,"unitsIkzeMama":1.5,"unitsInne":2,"totalSpentPln":500,"groupName":"Złoto"}`)
	check("update_units", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdatePosition(w, r, ga2) }, http.MethodPut, "/", `{"unitsIkeMama":3,"totalSpentPln":250}`)
	check("update_units_second", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdatePosition(w, r, ga1) }, http.MethodPut, "/", `{"unitsIkeTata":-1}`)
	check("update_negative_units", st, b)

	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdateAsset(w, r, assetA) }, http.MethodPut, "/", `{"targetAllocationPct":55,"name":"Gold (all)"}`)
	check("update_asset_targets", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.SetAssetPrimary(w, r, assetA) }, http.MethodPost, "/", fmt.Sprintf(`{"subassetId":%d}`, ga2))
	check("set_primary_sum_not_100", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdateAsset(w, r, assetA) }, http.MethodPut, "/", `{"targetAllocationPct":60,"targetAllocationPctRiskOff":null}`)
	check("update_asset_clear_riskoff", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.SetAssetPrimary(w, r, assetA) }, http.MethodPost, "/", fmt.Sprintf(`{"subassetId":%d}`, ga2))
	check("set_primary", st, b)

	page0 := int32(0)
	size := int32(20)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) {
		h.ListPositions(w, r, openapi.ListPositionsParams{Pageable: openapi.Pageable{Page: &page0, Size: &size}})
	}, http.MethodGet, "/", "")
	check("list", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.GetPosition(w, r, ga1) }, http.MethodGet, "/", "")
	check("get", st, b)

	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.GetPosition(w, r, 999999) }, http.MethodGet, "/", "")
	check("get_unknown_404", st, b)
	st, b = create(`{"ticker":"GA-1","name":"dup","exchange":"XTB","assetClass":"ETF","exposure":"METALS","priceCurrency":"PLN","newAsset":{"name":"dup","exposure":"METALS","assetClass":"ETF"}}`)
	check("create_duplicate_409", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdatePosition(w, r, ga2) }, http.MethodPut, "/", fmt.Sprintf(`{"assetId":%d}`, assetB))
	check("rehome_primary_409", st, b)

	st, b = create(`{"ticker":"NEW-X","name":"x","exchange":"XTB","assetClass":"ETF","exposure":"METALS","priceCurrency":"PLN","assetId":999999}`)
	check("create_attach_unknown_asset_404", st, b)
	st, b = create(`{"ticker":"NEW-Y","name":"y","exchange":"XTB","assetClass":"ETF","exposure":"METALS","priceCurrency":"PLN"}`)
	check("create_without_asset_400", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.DeletePosition(w, r, ga2) }, http.MethodDelete, "/", "")
	check("delete_primary_with_siblings_409", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.SetAssetPrimary(w, r, assetA) }, http.MethodPost, "/", fmt.Sprintf(`{"subassetId":%d}`, subsB[0]))
	check("set_primary_foreign_subasset_400", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdatePosition(w, r, 999999) }, http.MethodPut, "/", `{"name":"x"}`)
	check("update_unknown_404", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdateAsset(w, r, 999999) }, http.MethodPut, "/", `{"name":"x"}`)
	check("update_asset_unknown_404", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.UpdateAsset(w, r, assetA) }, http.MethodPut, "/", `{"targetAllocationPct":101}`)
	check("update_asset_target_out_of_range_400", st, b)
	zero := int32(0)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) {
		h.ListPositions(w, r, openapi.ListPositionsParams{Pageable: openapi.Pageable{Page: &page0, Size: &zero}})
	}, http.MethodGet, "/", "")
	check("list_size_zero_400", st, b)
	one := int32(1)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) {
		h.ListPositions(w, r, openapi.ListPositionsParams{Pageable: openapi.Pageable{Page: &one, Size: &one}})
	}, http.MethodGet, "/", "")
	check("list_page_two", st, b)

	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.DeletePosition(w, r, ga1) }, http.MethodDelete, "/", "")
	check("delete_subasset", st, b)
	st, b = create(fmt.Sprintf(`{"ticker":"GA-1","name":"Gold A back","exchange":"XTB","assetClass":"ETF","exposure":"METALS","priceCurrency":"PLN","assetId":%d}`, assetA))
	check("create_restore_soft_deleted", st, b)

	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.DeleteAsset(w, r, assetB) }, http.MethodDelete, "/", "")
	check("delete_asset", st, b)
	st, b = golden.Call(func(w http.ResponseWriter, r *http.Request) { h.DeleteAsset(w, r, assetB) }, http.MethodDelete, "/", "")
	check("delete_asset_again_404", st, b)
}

// goldenPositionHandler wires the handler exactly as cmd/server/main.go does.
func goldenPositionHandler(pool *pgxpool.Pool) *Handler {
	q := store.New(pool)
	money := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, store.Bind)
	return NewHandler(scposition.NewService(book.Config, q, money, q))
}

func goldenPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
