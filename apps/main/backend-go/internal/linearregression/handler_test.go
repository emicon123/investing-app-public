package linearregression

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"investing-app/backend-go/openapi"
)

// Handler tests assert the wire contract: the RAW flat RegressionResponse (no {data}
// envelope — ticker/slope/dataPoints/meta at top level), 400 on bad interval, and the
// shared error envelope.

func TestGetLinearRegression_FlatShape(t *testing.T) {
	h := NewHandler(NewService(&stubHistoryFetcher{points: stubPoints(3)}))
	interval := "wk"
	req := httptest.NewRequest(http.MethodGet, "/api/market-data/linear-regression?ticker=BTC-USD", nil)
	rec := httptest.NewRecorder()
	h.GetLinearRegression(rec, req, openapi.GetLinearRegressionParams{Ticker: "BTC-USD", Interval: &interval})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Ticker     string  `json:"ticker"`
		Interval   string  `json:"interval"`
		Slope      float64 `json:"slope"`
		Intercept  float64 `json:"intercept"`
		StdDev     float64 `json:"stdDev"`
		DataPoints []struct {
			Count     int      `json:"count"`
			Close     *float64 `json:"close"`
			Trendline *float64 `json:"trendline"`
			Sd1plus   *float64 `json:"sd1plus"`
			Sd4minus  *float64 `json:"sd4minus"`
		} `json:"dataPoints"`
		PriceSource *string `json:"priceSource"`
		Meta        struct {
			RequestID string `json:"requestId"`
			Timestamp string `json:"timestamp"`
		} `json:"meta"`
		// Must NOT be enveloped.
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data != nil {
		t.Fatalf("response must be flat, got data envelope: %s", rec.Body.String())
	}
	if body.Ticker != "BTC-USD" || body.Interval != "wk" {
		t.Fatalf("unexpected header: %+v", body)
	}
	if len(body.DataPoints) != 3 || body.DataPoints[0].Count != 1 {
		t.Fatalf("expected 3 1-indexed points, got %+v", body.DataPoints)
	}
	p := body.DataPoints[0]
	if p.Close == nil || p.Trendline == nil || p.Sd1plus == nil || p.Sd4minus == nil {
		t.Fatalf("history rows need actuals + bands: %+v", p)
	}
	if body.PriceSource == nil || *body.PriceSource != "YAHOO" {
		t.Fatalf("expected YAHOO priceSource, got %v", body.PriceSource)
	}
	if body.Meta.RequestID == "" || body.Meta.Timestamp == "" {
		t.Fatalf("expected meta, got %+v", body.Meta)
	}
}

func TestGetLinearRegression_BadIntervalIs400(t *testing.T) {
	h := NewHandler(NewService(&stubHistoryFetcher{points: stubPoints(3)}))
	interval := "fortnight"
	req := httptest.NewRequest(http.MethodGet, "/api/market-data/linear-regression", nil)
	rec := httptest.NewRecorder()
	h.GetLinearRegression(rec, req, openapi.GetLinearRegressionParams{Ticker: "BTC-USD", Interval: &interval})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil ||
		len(errBody.Errors) != 1 || errBody.Errors[0].Code != "VALIDATION_ERROR" ||
		errBody.Errors[0].Message != "interval must be d, wk, or mo; got: fortnight" {
		t.Fatalf("expected literal interval message, got %s", rec.Body.String())
	}
}

func TestGetLinearRegression_EmptyHistoryIs400(t *testing.T) {
	h := NewHandler(NewService(&stubHistoryFetcher{}))
	interval := "wk"
	req := httptest.NewRequest(http.MethodGet, "/api/market-data/linear-regression", nil)
	rec := httptest.NewRecorder()
	h.GetLinearRegression(rec, req, openapi.GetLinearRegressionParams{Ticker: "NOPE", Interval: &interval})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
