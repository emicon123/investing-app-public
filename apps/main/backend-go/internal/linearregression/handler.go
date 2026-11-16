package linearregression

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface method for
// LinearRegressionController's endpoint — the same thin-Facade convention as prior
// phases (no business logic here; service -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// GetLinearRegression implements GET /market-data/linear-regression. Answers 200 with the RAW flat
// RegressionResponse (ticker/interval/slope/intercept/stdDev/dataPoints/priceSource/meta at top
// level — NOT wrapped in a {data} envelope; the spec's fixed-2026-09-15 shape). Errors: bad
// interval or empty history answer 400 VALIDATION_ERROR; a negative `since` simply means no
// extrapolation, never an error.
func (h *Handler) GetLinearRegression(w http.ResponseWriter, r *http.Request, params openapi.GetLinearRegressionParams) {
	interval := "wk"
	if params.Interval != nil {
		interval = *params.Interval
	}
	since := 0
	if params.Since != nil {
		since = *params.Since
	}
	result, err := h.service.Compute(r.Context(), params.Ticker, interval,
		openapiDateToTime(params.FromDate), openapiDateToTime(params.ToDate), since)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOpenAPIResult(result, newOpenAPIMeta(r.Context())))
}

func openapiDateToTime(d *types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	return &t
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────

// floatPtr maps a computed double to the response's optional fields. NaN/±Inf arise
// only in the degenerate single-point-history edge (n=1: SimpleRegression yields NaN
// slope/intercept/stdDev, which Jackson serializes as non-standard `NaN` tokens) — Go's
// encoding/json cannot emit those, so they are omitted instead of breaking the body.
// Slope/intercept/stdDev themselves are required floats: NaN there is coerced to 0 for
// the same reason (documented divergence, pathological edge only — real histories
// always carry dozens of points).
func floatPtr(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func toOpenAPIResult(res Result, meta openapi.Meta) openapi.RegressionResponse {
	points := make([]openapi.RegressionDataPoint, 0, len(res.DataPoints))
	for _, p := range res.DataPoints {
		date := types.Date{Time: p.Date}
		count := p.Count
		trendline := p.Trendline
		lnTrendline := p.LnTrendline
		points = append(points, openapi.RegressionDataPoint{
			Date: &date, Count: &count,
			Close: p.Close, High: p.High, Low: p.Low, LogN: p.LogN,
			Trendline: &trendline, LnTrendline: &lnTrendline, Residual: p.Residual,
			Sd1plus: floatPtr(p.Sd1Plus), Sd2plus: floatPtr(p.Sd2Plus),
			Sd1minus: floatPtr(p.Sd1Minus), Sd2minus: floatPtr(p.Sd2Minus),
			Sd3plus: floatPtr(p.Sd3Plus), Sd3minus: floatPtr(p.Sd3Minus),
			Sd4plus: floatPtr(p.Sd4Plus), Sd4minus: floatPtr(p.Sd4Minus),
		})
	}
	slope := res.Slope
	if math.IsNaN(slope) || math.IsInf(slope, 0) {
		slope = 0
	}
	intercept := res.Intercept
	if math.IsNaN(intercept) || math.IsInf(intercept, 0) {
		intercept = 0
	}
	stdDev := res.StdDev
	if math.IsNaN(stdDev) || math.IsInf(stdDev, 0) {
		stdDev = 0
	}
	priceSource := openapi.PriceSource(res.PriceSource)
	return openapi.RegressionResponse{
		Ticker: res.Ticker, Interval: res.Interval,
		Slope: slope, Intercept: intercept, StdDev: stdDev,
		DataPoints: points, PriceSource: &priceSource,
		Meta: meta,
	}
}

// newOpenAPIMeta builds the generated openapi.Meta for the response shapes that carry their own
// meta field instead of an httpx envelope; its requestId is the request's X-Request-Id
// (httpx.NewMetaFrom), the id the access and error logs carry (task 182 / F4.2).
func newOpenAPIMeta(ctx context.Context) openapi.Meta {
	m := httpx.NewMetaFrom(ctx)
	id := types.UUID(m.RequestID)
	ts := m.Timestamp
	return openapi.Meta{RequestId: &id, Timestamp: &ts}
}
