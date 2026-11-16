package cryptotrades

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/oapi-codegen/runtime/types"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the crypto-trades
// endpoints — the same thin-Facade convention as the other domains (no business logic here; service
// -> openapi type mapping only).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// ListCryptoTrades implements GET /crypto-trades. Paginated (default size 50, no maximum — any size
// passes through), optional ticker/year/exchange/side filters.
func (h *Handler) ListCryptoTrades(w http.ResponseWriter, r *http.Request, params openapi.ListCryptoTradesParams) {
	var exchange, side *string
	if params.Exchange != nil {
		s := string(*params.Exchange)
		exchange = &s
	}
	if params.Side != nil {
		s := string(*params.Side)
		side = &s
	}
	page, err := h.service.List(r.Context(), params.Ticker, params.Year, exchange, side, params.Page, params.Size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(toOpenAPITrades(page.Items), httpx.PageInfo{
		Number: page.Number, Size: page.Size, Total: page.Total,
	}))
}

// CreateCryptoTrade implements POST /crypto-trades. Answers 201 with the {data, meta} envelope.
// Body validation failures answer 422 per docs/openapi.yaml.
//
// NOTE on numerics/enums: the generated request type uses decimal.Decimal (x-go-type, task 182) and
// plain string enums, so an absent numeric decodes as 0 and any string passes binding — presence is
// checked on the raw map first and value rules live in Service.Create (the same split as the other
// create endpoints: a decode step plus service range checks).
func (h *Handler) CreateCryptoTrade(w http.ResponseWriter, r *http.Request) {
	in, appErr := decodeCreateBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	dto, err := h.service.Create(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(toOpenAPITrade(dto)))
}

// GetCryptoTaxSummary implements GET /crypto-trades/tax-summary. Answers 200 with the {data, meta}
// envelope.
func (h *Handler) GetCryptoTaxSummary(w http.ResponseWriter, r *http.Request, params openapi.GetCryptoTaxSummaryParams) {
	summary, err := h.service.TaxSummary(r.Context(), params.Year)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOpenAPITaxSummary(summary, newOpenAPIMeta(r.Context())))
}

// requiredCreateFields mirrors CreateCryptoTradeRequest's @NotNull/@NotBlank members —
// every one except the optional notes must be present (explicit null included: a JSON
// null unmarshals into the zero value, which Service.Create then rejects on value
// rules, keeping "missing" and "null" both at 422 with the field named).
var requiredCreateFields = []string{
	"tradeDate", "ticker", "exchange", "accountType", "side",
	"quantity", "unitPrice", "priceCurrency", "totalAmount", "amountPln",
}

func decodeCreateBody(r *http.Request) (CreateInput, *httpx.AppError) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return CreateInput{}, unprocessable("", "Invalid request body")
	}
	for _, field := range requiredCreateFields {
		if _, ok := raw[field]; !ok {
			return CreateInput{}, unprocessable(field, "must not be null")
		}
	}
	var body openapi.CreateCryptoTradeRequest
	reassembled, _ := json.Marshal(raw)
	if err := json.Unmarshal(reassembled, &body); err != nil {
		return CreateInput{}, unprocessable("", "Invalid request body")
	}
	var notes *string
	if body.Notes != nil {
		n := *body.Notes
		notes = &n
	}
	return CreateInput{
		TradeDate: body.TradeDate.Time, Ticker: body.Ticker,
		Exchange: string(body.Exchange), AccountType: string(body.AccountType),
		Side:          string(body.Side),
		Quantity:      body.Quantity,
		UnitPrice:     body.UnitPrice,
		PriceCurrency: string(body.PriceCurrency),
		TotalAmount:   body.TotalAmount,
		AmountPln:     body.AmountPln,
		Notes:         notes,
	}, nil
}

// ── Service -> openapi type mapping ──────────────────────────────────────────────

func toOpenAPITrade(d TradeDTO) openapi.CryptoTrade {
	id := d.ID
	tradeDate := types.Date{Time: d.TradeDate}
	createdAt := d.CreatedAt.UTC() // rendered as Z (UTC); same instant, byte-identical text
	source := openapi.TradeSource(d.Source)
	return openapi.CryptoTrade{
		Id: &id, TradeDate: tradeDate, Ticker: d.Ticker,
		Exchange: openapi.Exchange(d.Exchange), AccountType: openapi.AccountType(d.AccountType),
		Side:     openapi.TradeSide(d.Side),
		Quantity: d.Quantity.InexactFloat64(), UnitPrice: d.UnitPrice.InexactFloat64(),
		PriceCurrency: openapi.Currency(d.PriceCurrency),
		TotalAmount:   d.TotalAmount.InexactFloat64(), AmountPln: d.AmountPln.InexactFloat64(),
		Source: &source, Notes: d.Notes, CreatedAt: &createdAt,
	}
}

func toOpenAPITrades(dtos []TradeDTO) []openapi.CryptoTrade {
	out := make([]openapi.CryptoTrade, 0, len(dtos))
	for _, d := range dtos {
		out = append(out, toOpenAPITrade(d))
	}
	return out
}

func toOpenAPITaxSummary(s Summary, meta openapi.Meta) openapi.CryptoTaxSummaryResponse {
	byTicker := make([]openapi.CryptoTaxSummaryEntry, 0, len(s.ByTicker))
	for _, t := range s.ByTicker {
		buys := t.TotalBuysPln.InexactFloat64()
		sells := t.TotalSellsPln.InexactFloat64()
		net := t.NetGainPln.InexactFloat64()
		belka := t.EstimatedBelkaTaxPln.InexactFloat64()
		ticker := t.Ticker
		byTicker = append(byTicker, openapi.CryptoTaxSummaryEntry{
			Ticker: &ticker, TotalBuysPln: &buys, TotalSellsPln: &sells,
			NetGainPln: &net, EstimatedBelkaTaxPln: &belka,
		})
	}
	var resp openapi.CryptoTaxSummaryResponse
	resp.Data.Year = s.Year
	resp.Data.TotalBuysPln = s.TotalBuysPln.InexactFloat64()
	resp.Data.TotalSellsPln = s.TotalSellsPln.InexactFloat64()
	resp.Data.NetGainPln = s.NetGainPln.InexactFloat64()
	resp.Data.EstimatedBelkaTaxPln = s.EstimatedBelkaTaxPln.InexactFloat64()
	resp.Data.ByTicker = byTicker
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
