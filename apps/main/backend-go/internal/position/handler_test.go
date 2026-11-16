package position

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"investing-app/common-go/pgnum"
)

// TestCreatePosition_OmittedPurchaseCurrency_DefaultsToPLN is a regression test for the bug
// reported 2026-09-22: CreatePositionDialog only collects priceCurrency, never
// purchaseCurrency, so the JSON body omits that key on every request. The handler must
// leave the field nil so Service.Create's "nil -> default PLN" logic applies —
// forwarding "" would hit Postgres's currency_enum with SQLSTATE 22P02 and 500 the
// whole dialog. Service-level tests never caught this because they call svc.Create
// directly with PurchaseCurrency left nil, bypassing the handler's JSON-decode mapping.
func TestCreatePosition_OmittedPurchaseCurrency_DefaultsToPLN(t *testing.T) {
	fq := newFakeQueries()
	svc := NewService(fq, nil, newFakePrices())
	h := NewHandler(svc)

	body := []byte(`{
		"ticker": "VRTU.DE", "name": "VanEck Sui ETN", "portfolio": "MAIN", "exchange": "XTB",
		"assetClass": "ETF", "priceCurrency": "EUR",
		"newAsset": {"name": "Sui", "exposure": "CRYPTO", "assetClass": "CRYPTO"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/positions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.CreatePosition(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Subassets []struct {
				PurchaseCurrency string `json:"purchaseCurrency"`
				PriceCurrency    string `json:"priceCurrency"`
			} `json:"subassets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Data.Subassets) != 1 {
		t.Fatalf("expected 1 subasset, got %s", w.Body.String())
	}
	if resp.Data.Subassets[0].PurchaseCurrency != "PLN" {
		t.Fatalf("expected purchaseCurrency to default to PLN, got %q", resp.Data.Subassets[0].PurchaseCurrency)
	}
	if resp.Data.Subassets[0].PriceCurrency != "EUR" {
		t.Fatalf("expected priceCurrency EUR to pass through untouched, got %q", resp.Data.Subassets[0].PriceCurrency)
	}
}

// TestCreatePosition_DuplicateTickerIs409 confirms a Postgres unique-violation on
// positions_uq (ticker, exchange, portfolio) maps to 409 DUPLICATE_ENTRY instead of the
// raw 500 httpx.WriteError's generic fallback used to produce — the bug fixed here. Mirrors
// TestCreateInflationEntry_DuplicateIs409's shape (inflation/handler_test.go).
func TestCreatePosition_DuplicateTickerIs409(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "SUI", "3")
	fq.putSub(aid, "VRTU.DE", "MAIN", "10", "", true)
	svc := NewService(fq, nil, newFakePrices())
	h := NewHandler(svc)

	body := []byte(`{
		"ticker": "VRTU.DE", "name": "VanEck Sui ETN (dup)", "portfolio": "MAIN", "exchange": "XTB",
		"assetClass": "ETF", "priceCurrency": "EUR", "assetId": 1
	}`)
	req := httptest.NewRequest(http.MethodPost, "/positions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.CreatePosition(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var errBody struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if len(errBody.Errors) != 1 || errBody.Errors[0].Code != "DUPLICATE_ENTRY" {
		t.Fatalf("expected DUPLICATE_ENTRY, got %s", w.Body.String())
	}
}

// TestCreatePosition_TopLevelTargetIs400 confirms targets are rejected on the subasset
// endpoint (task 156) — they live on the asset (newAsset inline, or PUT /assets/{id}).
func TestCreatePosition_TopLevelTargetIs400(t *testing.T) {
	fq := newFakeQueries()
	svc := NewService(fq, nil, newFakePrices())
	h := NewHandler(svc)

	body := []byte(`{
		"ticker": "X", "name": "X", "portfolio": "MAIN", "exchange": "XTB",
		"assetClass": "ETF", "priceCurrency": "PLN", "targetAllocationPct": 5,
		"newAsset": {"name": "X", "exposure": "CRYPTO", "assetClass": "CRYPTO"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/positions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.CreatePosition(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdatePosition_AppliesNameAssetClassAndYahooTicker exercises the handler's
// JSON-decode path (which service-level tests bypass) to make sure subasset-owned
// fields reach the service and surface nested in the parent asset response.
func TestUpdatePosition_AppliesNameAssetClassAndYahooTicker(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "SUI", "3")
	fq.putSub(aid, "VRTU.DE", "MAIN", "10", "", true)
	svc := NewService(fq, nil, newFakePrices())
	h := NewHandler(svc)

	body := []byte(`{"name": "Sui (VanEck ETN)", "assetClass": "ETF", "yahooTicker": "VRTU.DE"}`)
	req := httptest.NewRequest(http.MethodPut, "/positions/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.UpdatePosition(w, req, 1)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Subassets []struct {
				Name        string  `json:"name"`
				AssetClass  string  `json:"assetClass"`
				YahooTicker *string `json:"yahooTicker"`
			} `json:"subassets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, w.Body.String())
	}
	if len(resp.Data.Subassets) != 1 {
		t.Fatalf("expected 1 subasset, got %s", w.Body.String())
	}
	sub := resp.Data.Subassets[0]
	if sub.Name != "Sui (VanEck ETN)" {
		t.Fatalf("expected name to be updated, got %q", sub.Name)
	}
	if sub.AssetClass != "ETF" {
		t.Fatalf("expected assetClass to be updated, got %q", sub.AssetClass)
	}
	if sub.YahooTicker == nil || *sub.YahooTicker != "VRTU.DE" {
		t.Fatalf("expected yahooTicker to be updated, got %v", sub.YahooTicker)
	}
}

// TestUpdatePosition_TargetIs400 confirms target edits are rejected on the subasset
// endpoint (task 156) — PUT /assets/{id} is the only writer of targets.
func TestUpdatePosition_TargetIs400(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "SUI", "3")
	fq.putSub(aid, "VRTU.DE", "MAIN", "10", "", true)
	svc := NewService(fq, nil, newFakePrices())
	h := NewHandler(svc)

	body := []byte(`{"targetAllocationPct": 5}`)
	req := httptest.NewRequest(http.MethodPut, "/positions/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.UpdatePosition(w, req, 1)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func doJSON(h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func decodeAssetResp(t *testing.T, w *httptest.ResponseRecorder) (target, riskOff *float64, name string) {
	t.Helper()
	var resp struct {
		Data struct {
			Name                       string   `json:"name"`
			TargetAllocationPct        *float64 `json:"targetAllocationPct"`
			TargetAllocationPctRiskOff *float64 `json:"targetAllocationPctRiskOff"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, w.Body.String())
	}
	return resp.Data.TargetAllocationPct, resp.Data.TargetAllocationPctRiskOff, resp.Data.Name
}

// TestUpdateAsset_Handler_EditsTargets_NullClears_AbsentKeeps is the wire-level tri-state:
// JSON null CLEARS a target, an absent key leaves it alone, 0 is an explicit zero, and an
// out-of-range value is a 400 that writes nothing. No Σ == 100 guard on this path.
func TestUpdateAsset_Handler_EditsTargets_NullClears_AbsentKeeps(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "30")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	h := NewHandler(NewService(fq, nil, newFakePrices()))
	call := func(body string) *httptest.ResponseRecorder {
		return doJSON(func(w http.ResponseWriter, r *http.Request) { h.UpdateAsset(w, r, aid) }, http.MethodPut, "/assets/1", body)
	}

	w := call(`{"name": "Renamed"}`) // absent targets -> untouched
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if tgt, _, name := decodeAssetResp(t, w); tgt == nil || *tgt != 30 || name != "Renamed" {
		t.Fatalf("absent target must stay 30 and name change, got %v %q", tgt, name)
	}
	w = call(`{"targetAllocationPct": null, "targetAllocationPctRiskOff": 12.5}`)
	if tgt, off, _ := decodeAssetResp(t, w); tgt != nil || off == nil || *off != 12.5 {
		t.Fatalf("null must clear the target, risk-off 12.5 must set, got %v %v", tgt, off)
	}
	w = call(`{"targetAllocationPct": 0}`)
	if tgt, off, _ := decodeAssetResp(t, w); tgt == nil || *tgt != 0 || off == nil || *off != 12.5 {
		t.Fatalf("0 is an explicit zero target and must not touch the risk-off, got %v %v", tgt, off)
	}
	if w := call(`{"targetAllocationPct": 100.5}`); w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range target, got %d: %s", w.Code, w.Body.String())
	}
	if w := call(`not json`); w.Code != 422 {
		t.Fatalf("expected 422 for a malformed body, got %d", w.Code)
	}
	if w := doJSON(func(w http.ResponseWriter, r *http.Request) { h.UpdateAsset(w, r, 99) }, http.MethodPut, "/assets/99", `{"name":"x"}`); w.Code != 404 {
		t.Fatalf("expected 404 for an unknown asset, got %d", w.Code)
	}
}

func TestSetAssetPrimary_Handler_StatusMapping(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "100")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	fq.putSub(aid, "SUB", "MAIN", "4", "", false)
	other := fq.putAsset("MAIN", "OTHER", "0")
	fq.putSub(other, "OTHER", "MAIN", "1", "", true)
	h := NewHandler(NewService(fq, nil, newFakePrices()))
	call := func(id int64, body string) *httptest.ResponseRecorder {
		return doJSON(func(w http.ResponseWriter, r *http.Request) { h.SetAssetPrimary(w, r, id) }, http.MethodPost, "/assets/1/primary", body)
	}
	if w := call(aid, `{"subassetId": 2}`); w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !fq.rows[2].IsPrimary || fq.rows[1].IsPrimary {
		t.Fatal("expected the flip to commit")
	}
	if w := call(aid, `{"subassetId": 3}`); w.Code != 400 { // OTHER's subasset
		t.Fatalf("expected 400 for a subasset of another asset, got %d", w.Code)
	}
	if w := call(999, `{"subassetId": 2}`); w.Code != 404 {
		t.Fatalf("expected 404 for an unknown asset, got %d", w.Code)
	}
}

func TestDelete_Handlers_StatusMapping(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "100")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	fq.putSub(aid, "SUB", "MAIN", "4", "", false)
	h := NewHandler(NewService(fq, nil, newFakePrices()))

	w := httptest.NewRecorder()
	h.DeletePosition(w, httptest.NewRequest(http.MethodDelete, "/positions/1", nil), 1)
	if w.Code != http.StatusConflict {
		t.Fatalf("deleting a primary with siblings must be 409, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.DeletePosition(w, httptest.NewRequest(http.MethodDelete, "/positions/2", nil), 2)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.DeleteAsset(w, httptest.NewRequest(http.MethodDelete, "/assets/1", nil), aid)
	if w.Code != http.StatusNoContent || fq.liveRows() != 0 {
		t.Fatalf("expected 204 and cascade, got %d, %d rows left", w.Code, fq.liveRows())
	}
	w = httptest.NewRecorder()
	h.DeleteAsset(w, httptest.NewRequest(http.MethodDelete, "/assets/1", nil), aid)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestUpdatePosition_Handler_AssetIDReHome: assetId decodes into UpdateInput (the moved
// subasset comes back under the NEW parent), and 404 / 409 / 400 map to the right statuses.
func TestUpdatePosition_Handler_AssetIDReHome(t *testing.T) {
	svc, fq, _, b, r := rehomeFixture()
	h := NewHandler(svc)
	call := func(id int64, body string) *httptest.ResponseRecorder {
		return doJSON(func(w http.ResponseWriter, r *http.Request) { h.UpdatePosition(w, r, id) }, http.MethodPut, "/positions/x", body)
	}

	if w := call(1, `{"assetId": 3}`); w.Code != http.StatusConflict { // primary
		t.Fatalf("expected 409 for a primary, got %d: %s", w.Code, w.Body.String())
	}
	if w := call(2, `{"assetId": 999}`); w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown asset, got %d: %s", w.Code, w.Body.String())
	}
	body := `{"assetId": ` + itoa(r) + `}`
	w := call(2, body)
	if w.Code != http.StatusBadRequest || !bytes.Contains(w.Body.Bytes(), []byte(`"field":"assetId"`)) {
		t.Fatalf("expected 400 on field assetId, got %d: %s", w.Code, w.Body.String())
	}
	w = call(2, `{"assetId": `+itoa(b)+`, "name": "A2 moved"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			ID        int64 `json:"id"`
			Subassets []struct {
				Ticker  string `json:"ticker"`
				Name    string `json:"name"`
				AssetID int64  `json:"assetId"`
			} `json:"subassets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.ID != b || fq.rows[2].AssetID != b {
		t.Fatalf("expected the response to be the new parent asset %d, got %d", b, resp.Data.ID)
	}
	moved := false
	for _, s := range resp.Data.Subassets {
		if s.Ticker == "A2" && s.Name == "A2 moved" && s.AssetID == b {
			moved = true
		}
	}
	if !moved {
		t.Fatalf("expected A2 (renamed, assetId=%d) nested under the new parent, got %s", b, w.Body.String())
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// TestUpdatePosition_MoneyFieldsReachTheStoreExactly pins task 182 B5 (F3.4): request numbers
// are decoded as decimals, so 18 significant digits survive (float64 keeps ~17) and a typed PLN
// amount is stored as typed — absent fields still leave the stored value alone.
func TestUpdatePosition_MoneyFieldsReachTheStoreExactly(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "BTC", "10")
	id := fq.putSub(aid, "BTC-USD", "MAIN", "2.5", "", true)
	h := NewHandler(NewService(fq, nil, newFakePrices()))

	body := []byte(`{"unitsIke":0.123456789012345678,"unitsIkze":12345678.123456789012345678,` +
		`"totalSpentPln":1234567.8912,"avgBuyPrice":0.000000012345678901}`)
	req := httptest.NewRequest(http.MethodPut, "/positions/"+strconv.FormatInt(id, 10), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.UpdatePosition(w, req, id)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	row := fq.rows[id]
	for name, c := range map[string]struct {
		got  pgtype.Numeric
		want string
	}{
		"unitsIke":        {row.UnitsIke, "0.123456789012345678"},
		"unitsIkze":       {row.UnitsIkze, "12345678.123456789012345678"},
		"totalSpentPln":   {row.TotalSpentPln, "1234567.8912"},
		"avgBuyPrice":     {row.AvgBuyPrice, "0.000000012345678901"},
		"unitsRegularPln": {row.UnitsRegularPln, "2.5"}, // absent: unchanged
	} {
		got, err := pgnum.ToDecimal(c.got)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.String() != c.want {
			t.Errorf("%s stored %s, want exactly %s", name, got.String(), c.want)
		}
	}
}

// TestCreatePosition_NewAssetTargetsReachTheStoreExactly: the newAsset targets are decimals too.
func TestCreatePosition_NewAssetTargetsReachTheStoreExactly(t *testing.T) {
	fq := newFakeQueries()
	h := NewHandler(NewService(fq, nil, newFakePrices()))
	body := []byte(`{"ticker":"VRTU.DE","name":"Sui","portfolio":"MAIN","exchange":"XTB","assetClass":"ETF",` +
		`"priceCurrency":"EUR","newAsset":{"name":"Sui","exposure":"CRYPTO","assetClass":"CRYPTO",` +
		`"targetAllocationPct":2.123456789012345678,"targetAllocationPctRiskOff":0.1}}`)
	req := httptest.NewRequest(http.MethodPost, "/positions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.CreatePosition(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(fq.assets) != 1 {
		t.Fatalf("expected one asset, got %d", len(fq.assets))
	}
	for _, a := range fq.assets {
		target, _ := pgnum.ToDecimal(a.TargetAllocationPct)
		riskOff, _ := pgnum.ToDecimal(a.TargetAllocationPctRiskOff)
		if target.String() != "2.123456789012345678" || riskOff.String() != "0.1" {
			t.Errorf("targets stored %s / %s, want 2.123456789012345678 / 0.1", target, riskOff)
		}
	}
}
