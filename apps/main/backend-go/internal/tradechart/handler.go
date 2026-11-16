package tradechart

import (
	"context"
	"net/http"
	"time"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for
// TradeChartController's endpoints — the same thin-Facade convention as prior phases
// (no business logic here; service -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// GetTradeChartTickers implements GET /market-data/trade-chart/tickers — the direct
// port of TradeChartController.tickers. Answers 200 with the {data, excluded, meta}
// envelope; never triggers a price fetch.
func (h *Handler) GetTradeChartTickers(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListTickers(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOpenAPITickers(result, newOpenAPIMeta(r.Context())))
}

// GetTradeChart implements GET /market-data/trade-chart. Answers 200 with the {data, meta} envelope
// (raw price series plus one marker per asset_purchases row). Errors come from Service.GetChart
// (400/404/503 — see service.go).
func (h *Handler) GetTradeChart(w http.ResponseWriter, r *http.Request, params openapi.GetTradeChartParams) {
	interval := "wk" // default interval
	if params.Interval != nil {
		interval = *params.Interval
	}
	chart, err := h.service.GetChart(r.Context(), params.Ticker, interval,
		openapiDateToTime(params.FromDate), openapiDateToTime(params.ToDate))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOpenAPIChart(chart, newOpenAPIMeta(r.Context())))
}

func openapiDateToTime(d *types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	return &t
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────

func toOpenAPITickers(res TickersResult, meta openapi.Meta) openapi.TradeChartTickersResponse {
	data := make([]openapi.TradeChartTickerEntry, 0, len(res.Data))
	for _, e := range res.Data {
		ticker, name, yahoo, tradeCount := e.Ticker, e.Name, e.YahooTicker, e.TradeCount
		first, last := e.FirstTradeAt.UTC(), e.LastTradeAt.UTC()
		currency := openapi.Currency(e.PriceCurrency)
		data = append(data, openapi.TradeChartTickerEntry{
			Ticker: &ticker, Name: &name, YahooTicker: &yahoo,
			PriceCurrency: &currency, TradeCount: &tradeCount,
			FirstTradeAt: &first, LastTradeAt: &last,
		})
	}
	excluded := make([]openapi.ExcludedTradeChartTicker, 0, len(res.Excluded))
	for _, x := range res.Excluded {
		excluded = append(excluded, openapi.ExcludedTradeChartTicker{
			Ticker: x.Ticker, Reason: openapi.ExcludedTradeChartTickerReason(x.Reason),
		})
	}
	return openapi.TradeChartTickersResponse{
		Data: data, Excluded: excluded, Meta: meta,
	}
}

func toOpenAPIChart(c ChartData, meta openapi.Meta) openapi.TradeChartResponse {
	ticker, yahoo, interval := c.Ticker, c.YahooTicker, c.Interval
	currency := openapi.Currency(c.PriceCurrency)
	series := make([]openapi.TradeChartPricePoint, 0, len(c.PriceSeries))
	for _, p := range c.PriceSeries {
		date := types.Date{Time: p.Date}
		open, close, high, low := p.Open, p.Close, p.High, p.Low
		source := openapi.PriceSource(p.PriceSource)
		series = append(series, openapi.TradeChartPricePoint{
			Date: &date, Open: &open, Close: &close, High: &high, Low: &low,
			PriceSource: &source,
		})
	}
	markers := make([]openapi.TradeChartMarker, 0, len(c.Trades))
	for _, m := range c.Trades {
		id, purchasedAt := m.ID, m.PurchasedAt.UTC()
		tradeType := openapi.TradeType(m.TradeType)
		portfolio := openapi.Portfolio(m.Portfolio)
		accountType := openapi.AccountType(m.AccountType)
		units, pricePln := m.UnitsDelta.InexactFloat64(), m.PricePln.InexactFloat64()
		priceNative, total := m.PriceNative.InexactFloat64(), m.TotalPln.InexactFloat64()
		markers = append(markers, openapi.TradeChartMarker{
			Id: &id, PurchasedAt: &purchasedAt, TradeType: &tradeType,
			Portfolio: &portfolio, AccountType: &accountType,
			UnitsDelta: &units, PricePln: &pricePln,
			PriceNative: &priceNative, TotalPln: &total,
			SessionId: m.SessionID,
		})
	}
	var resp openapi.TradeChartResponse
	resp.Data.Ticker = &ticker
	resp.Data.YahooTicker = &yahoo
	resp.Data.PriceCurrency = &currency
	resp.Data.Interval = &interval
	resp.Data.PriceSeries = &series
	resp.Data.Trades = &markers
	resp.Meta = meta
	return resp
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
