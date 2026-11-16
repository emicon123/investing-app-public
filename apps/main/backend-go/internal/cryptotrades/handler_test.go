package cryptotrades

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"investing-app/backend-go/db/sqlcgen"

	"investing-app/backend-go/openapi"
)

// Handler tests assert the wire contract: envelope shapes ({data,meta} /
// {data,meta,page}), status codes per docs/openapi.yaml (201 create, 422 validation —
// NOT Java's 400), and the shared error envelopes.

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return body
}

func seedHandler() *Handler {
	fq := newFakeQueries()
	fq.insert(fakeRow("BTC-USD", 2025, sqlcgen.TradeSideEnumBUY, "2530"))
	fq.insert(fakeRow("ETH-USD", 2025, sqlcgen.TradeSideEnumSELL, "1200"))
	return NewHandler(NewService(fq))
}

func TestListCryptoTrades_PagedEnvelope(t *testing.T) {
	h := seedHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/crypto-trades", nil)
	rec := httptest.NewRecorder()
	h.ListCryptoTrades(rec, req, openapi.ListCryptoTradesParams{})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data []json.RawMessage
	if err := json.Unmarshal(body["data"], &data); err != nil || len(data) != 2 {
		t.Fatalf("expected 2 data items, got %s", rec.Body.String())
	}
	var page struct {
		Number int   `json:"number"`
		Size   int   `json:"size"`
		Total  int64 `json:"total"`
	}
	if err := json.Unmarshal(body["page"], &page); err != nil {
		t.Fatalf("expected page object, got %s", rec.Body.String())
	}
	if page.Number != 0 || page.Size != 50 || page.Total != 2 {
		t.Fatalf("expected default page 0/50 total 2, got %+v", page)
	}
	var first struct {
		Ticker    string  `json:"ticker"`
		Source    string  `json:"source"`
		AmountPln float64 `json:"amountPln"`
	}
	if err := json.Unmarshal(data[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.Ticker != "BTC-USD" || first.Source != "MANUAL" || first.AmountPln != 2530 {
		t.Fatalf("unexpected first row: %s", string(data[0]))
	}
}

func TestListCryptoTrades_InvalidQueryIs400(t *testing.T) {
	h := seedHandler()
	exchange := openapi.Exchange("KRAKEN")
	req := httptest.NewRequest(http.MethodGet, "/api/crypto-trades", nil)
	rec := httptest.NewRecorder()
	h.ListCryptoTrades(rec, req, openapi.ListCryptoTradesParams{Exchange: &exchange})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil ||
		len(errBody.Errors) != 1 || errBody.Errors[0].Code != "VALIDATION_ERROR" {
		t.Fatalf("expected VALIDATION_ERROR envelope, got %s", rec.Body.String())
	}
}

func TestCreateCryptoTrade_Returns201Manual(t *testing.T) {
	h := NewHandler(NewService(newFakeQueries()))
	reqBody := `{"tradeDate":"2026-06-01","ticker":"BTC-USD","exchange":"BINANCE",` +
		`"accountType":"REGULAR","side":"BUY","quantity":0.01,"unitPrice":65000,` +
		`"priceCurrency":"USD","totalAmount":650,"amountPln":2530,"notes":"Monthly DCA buy"}`
	req := httptest.NewRequest(http.MethodPost, "/api/crypto-trades", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	h.CreateCryptoTrade(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data struct {
		Source string `json:"source"`
		Ticker string `json:"ticker"`
	}
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data.Source != "MANUAL" || data.Ticker != "BTC-USD" {
		t.Fatalf("unexpected created row: %s", string(body["data"]))
	}
	if _, ok := body["meta"]; !ok {
		t.Fatalf("expected meta envelope, got %s", rec.Body.String())
	}
}

func TestCreateCryptoTrade_MissingFieldIs422(t *testing.T) {
	h := NewHandler(NewService(newFakeQueries()))
	// amountPln absent entirely.
	reqBody := `{"tradeDate":"2026-06-01","ticker":"BTC-USD","exchange":"BINANCE",` +
		`"accountType":"REGULAR","side":"BUY","quantity":0.01,"unitPrice":65000,` +
		`"priceCurrency":"USD","totalAmount":650}`
	req := httptest.NewRequest(http.MethodPost, "/api/crypto-trades", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	h.CreateCryptoTrade(rec, req)

	if rec.Code != 422 {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Errors []struct {
			Code  string `json:"code"`
			Field string `json:"field"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil ||
		len(errBody.Errors) != 1 || errBody.Errors[0].Code != "VALIDATION_ERROR" ||
		errBody.Errors[0].Field != "amountPln" {
		t.Fatalf("expected 422 VALIDATION_ERROR on amountPln, got %s", rec.Body.String())
	}
}

func TestCreateCryptoTrade_InvalidEnumIs422(t *testing.T) {
	h := NewHandler(NewService(newFakeQueries()))
	reqBody := `{"tradeDate":"2026-06-01","ticker":"BTC-USD","exchange":"KRAKEN",` +
		`"accountType":"REGULAR","side":"BUY","quantity":0.01,"unitPrice":65000,` +
		`"priceCurrency":"USD","totalAmount":650,"amountPln":2530}`
	req := httptest.NewRequest(http.MethodPost, "/api/crypto-trades", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	h.CreateCryptoTrade(rec, req)

	if rec.Code != 422 {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetCryptoTaxSummary_SingleEnvelope(t *testing.T) {
	h := seedHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/crypto-trades/tax-summary?year=2025", nil)
	rec := httptest.NewRecorder()
	h.GetCryptoTaxSummary(rec, req, openapi.GetCryptoTaxSummaryParams{Year: 2025})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	var data struct {
		Year                 int     `json:"year"`
		TotalBuysPln         float64 `json:"totalBuysPln"`
		TotalSellsPln        float64 `json:"totalSellsPln"`
		NetGainPln           float64 `json:"netGainPln"`
		EstimatedBelkaTaxPln float64 `json:"estimatedBelkaTaxPln"`
		ByTicker             []struct {
			Ticker string `json:"ticker"`
		} `json:"byTicker"`
	}
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatal(err)
	}
	// Fixture: BTC buy 2530, ETH sell 1200 -> buys 2530, sells 1200, net -1330, belka 0.
	if data.Year != 2025 || data.TotalBuysPln != 2530 || data.TotalSellsPln != 1200 ||
		data.NetGainPln != -1330 || data.EstimatedBelkaTaxPln != 0 {
		t.Fatalf("unexpected summary: %+v", data)
	}
	if len(data.ByTicker) != 2 || data.ByTicker[0].Ticker != "BTC-USD" {
		t.Fatalf("expected 2 sorted tickers, got %+v", data.ByTicker)
	}
}
