package purchasesession

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/openapi"
	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
)

// Handler adapts Service to the oapi-codegen ServerInterface methods for the purchase-session
// endpoints.
//
// NOTE (residual spec/wire gaps, documented not fixed): the legacy Java wire shape exceeded the
// spec in a few fields no current frontend code path reads — item.sessionId, item.pricePlnAtPlan,
// and the spec's own pkoCHF breakdown slot (which was never populated). If a future UI needs any of
// them, extend docs/openapi.yaml additively and regenerate (go generate ./openapi/) rather than
// hand-editing generated code. The frontend-consumed fields (session budgets/plannedAt, summary
// itemCount/totalActualPln, account limits, item priceCurrency/isMigrationBuy, breakdown PLN
// legs/rates) are in the spec and mapped below.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

const defaultPageSize = 20

// CreatePurchaseSession implements POST /purchase-sessions.
func (h *Handler) CreatePurchaseSession(w http.ResponseWriter, r *http.Request) {
	in, appErr := decodeCreateSessionBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	detail, err := h.service.CreateSession(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, httpx.Single(toOpenAPISession(detail)))
}

// GetActivePurchaseSession implements GET /purchase-sessions/active.
func (h *Handler) GetActivePurchaseSession(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.GetActiveSession(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPISession(detail)))
}

// PatchSessionItems implements PATCH /purchase-sessions/{id}/items.
func (h *Handler) PatchSessionItems(w http.ResponseWriter, r *http.Request, id int64) {
	entries, appErr := decodePatchItemsBody(r)
	if appErr != nil {
		httpx.WriteError(w, appErr)
		return
	}
	detail, err := h.service.UpdateSessionItems(r.Context(), id, entries)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toOpenAPISession(detail)))
}

// CompletePurchaseSession implements POST /purchase-sessions/{id}/complete.
func (h *Handler) CompletePurchaseSession(w http.ResponseWriter, r *http.Request, id int64) {
	result, err := h.service.CompleteSession(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	resp := openapi.PurchaseSessionCompleteResponse{Meta: newOpenAPIMeta(r.Context())}
	resp.Data.SessionId = result.SessionID
	resp.Data.Status = openapi.SessionStatus(result.Status)
	resp.Data.PurchasesWritten = result.PurchasesWritten
	resp.Data.UpdatedPositionIds = result.UpdatedPositionIds
	if resp.Data.UpdatedPositionIds == nil {
		resp.Data.UpdatedPositionIds = []int64{}
	}
	resp.Data.DepositsCreated = result.DepositsCreated
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// DeletePurchaseSession implements DELETE /purchase-sessions/{id}.
func (h *Handler) DeletePurchaseSession(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.service.DeleteSession(r.Context(), id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListPurchaseSessions implements GET /purchase-sessions.
func (h *Handler) ListPurchaseSessions(w http.ResponseWriter, r *http.Request, params openapi.ListPurchaseSessionsParams) {
	page := 0
	if params.Page != nil {
		page = *params.Page
	}
	size := defaultPageSize
	if params.Size != nil {
		size = *params.Size
	}
	items, total, err := h.service.ListSessions(r.Context(), page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	data := make([]openapi.PurchaseSession, 0, len(items))
	for _, it := range items {
		data = append(data, toOpenAPISummary(it))
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Paged(data, httpx.PageInfo{Number: page, Size: size, Total: total}))
}

func toOpenAPISession(d SessionDetail) openapi.PurchaseSession {
	id := d.ID
	status := openapi.SessionStatus(d.Status)
	portfolios := openapi.PortfoliosIncluded(d.PortfoliosIncluded)
	plannedAt := d.PlannedAt.UTC()
	retBudget := decx.ToFloat(d.RetirementBudgetPln)
	mainBudget := decx.ToFloat(d.MainBudgetPln)
	out := openapi.PurchaseSession{
		Id: &id, Status: &status, PortfoliosIncluded: &portfolios, Notes: d.Notes,
		DoneAt: d.DoneAt, PlannedAt: &plannedAt,
		RetirementBudgetPln: &retBudget, MainBudgetPln: &mainBudget,
	}
	items := make([]openapi.PurchaseSessionItemPlanned, 0, len(d.Items))
	for _, it := range d.Items {
		items = append(items, toOpenAPIItem(it))
	}
	out.Items = &items
	breakdown := toOpenAPIBreakdown(d.CurrencyBreakdown)
	out.CurrencyBreakdown = &breakdown
	retLimits := toOpenAPILimits(d.RetirementLimitsRemaining)
	out.RetirementLimitsRemaining = &retLimits
	mainLimits := toOpenAPILimits(d.MainLimitsRemaining)
	out.MainLimitsRemaining = &mainLimits
	return out
}

// toOpenAPISummary maps a SessionSummary onto the same generated PurchaseSession type
// ListPurchaseSessions' schema uses (per docs/openapi.yaml — the list endpoint reuses the full
// PurchaseSession schema, not a distinct summary type). items/currencyBreakdown stay nil, since
// PurchaseSessionSummary never loads them (mirrors PurchaseSessionMapper.entityToSummary).
func toOpenAPISummary(s SessionSummary) openapi.PurchaseSession {
	id := s.ID
	status := openapi.SessionStatus(s.Status)
	portfolios := openapi.PortfoliosIncluded(s.PortfoliosIncluded)
	plannedAt := s.PlannedAt.UTC()
	retBudget := decx.ToFloat(s.RetirementBudgetPln)
	mainBudget := decx.ToFloat(s.MainBudgetPln)
	count := s.ItemCount
	out := openapi.PurchaseSession{
		Id: &id, Status: &status, PortfoliosIncluded: &portfolios, DoneAt: s.DoneAt,
		PlannedAt: &plannedAt, RetirementBudgetPln: &retBudget, MainBudgetPln: &mainBudget,
		ItemCount: &count,
	}
	if s.TotalActualPln != nil {
		v := decx.ToFloat(*s.TotalActualPln)
		out.TotalActualPln = &v
	}
	return out
}

func toOpenAPIItem(it *SessionItem) openapi.PurchaseSessionItemPlanned {
	accountType := openapi.AccountType(it.AccountType)
	portfolio := openapi.Portfolio(it.Portfolio)
	out := openapi.PurchaseSessionItemPlanned{
		Id: &it.ID, Ticker: &it.Ticker, Portfolio: &portfolio, AccountType: &accountType,
		IsMigrationSell: &it.IsMigrationSell, IsMigrationBuy: &it.IsMigrationBuy,
	}
	if it.PriceCurrency != "" {
		pc := openapi.Currency(it.PriceCurrency)
		out.PriceCurrency = &pc
	}
	if it.Exchange != "" {
		ex := openapi.Exchange(it.Exchange)
		out.Exchange = &ex
	}
	if it.PlannedUnits != nil {
		v := decx.ToFloat(*it.PlannedUnits)
		out.PlannedUnits = &v
	}
	if it.PlannedValuePln != nil {
		v := decx.ToFloat(*it.PlannedValuePln)
		out.PlannedValuePln = &v
	}
	if it.ActualUnits != nil {
		v := decx.ToFloat(*it.ActualUnits)
		out.ActualUnits = &v
	}
	if it.ActualValuePln != nil {
		v := decx.ToFloat(*it.ActualValuePln)
		out.ActualValuePln = &v
	}
	return out
}

func toOpenAPILimits(l AccountLimits) openapi.PurchaseSessionAccountLimits {
	ikeUsed, ikeRem := decx.ToFloat(l.IkeUsedPln), decx.ToFloat(l.IkeRemainingPln)
	ikzeUsed, ikzeRem := decx.ToFloat(l.IkzeUsedPln), decx.ToFloat(l.IkzeRemainingPln)
	ikeLim, ikzeLim := decx.ToFloat(l.IkeAnnualLimitPln), decx.ToFloat(l.IkzeAnnualLimitPln)
	return openapi.PurchaseSessionAccountLimits{
		IkeUsedPln: &ikeUsed, IkeRemainingPln: &ikeRem, IkzeUsedPln: &ikzeUsed,
		IkzeRemainingPln: &ikzeRem, IkeAnnualLimitPln: &ikeLim, IkzeAnnualLimitPln: &ikzeLim,
	}
}

func toOpenAPIBreakdown(b CurrencyBreakdown) openapi.PurchaseSessionCurrencyBreakdown {
	xtbPln, xtbEur, xtbUsd, binanceEur := decx.ToFloat(b.XtbPln), decx.ToFloat(b.XtbEur), decx.ToFloat(b.XtbUsd), decx.ToFloat(b.BinanceEur)
	xtbEurPln, xtbUsdPln, binanceEurPln := decx.ToFloat(b.XtbEurPln), decx.ToFloat(b.XtbUsdPln), decx.ToFloat(b.BinanceEurPln)
	usdRate, eurRate := decx.ToFloat(b.UsdPlnRate), decx.ToFloat(b.EurPlnRate)
	return openapi.PurchaseSessionCurrencyBreakdown{
		XtbPln: &xtbPln, XtbEur: &xtbEur, XtbUsd: &xtbUsd, BinanceEur: &binanceEur,
		XtbEurPln: &xtbEurPln, XtbUsdPln: &xtbUsdPln, BinanceEurPln: &binanceEurPln,
		UsdPlnRate: &usdRate, EurPlnRate: &eurRate,
	}
}

// decodeCreateSessionBody maps POST /purchase-sessions' body to the service input;
// cashOverridePln is a decimal on the generated type (x-go-type, task 182 / F3.4).
func decodeCreateSessionBody(r *http.Request) (CreateSessionInput, *httpx.AppError) {
	var body openapi.CreatePurchaseSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return CreateSessionInput{}, unprocessable("Invalid request body")
	}
	return CreateSessionInput{PortfoliosIncluded: string(body.PortfoliosIncluded),
		CashOverridePln: body.CashOverridePln, Notes: body.Notes}, nil
}

// patchSessionItemsRequest mirrors openapi.PatchSessionItemsRequest with the actuals as
// decimals (task 182 / F3.4). PatchSessionItemsRequest is not on D174 §8.2's x-go-type list, so
// it is decoded through this hand-written struct instead (A-Q5 fallback); absent or null stays nil.
type patchSessionItemsRequest struct {
	Items []struct {
		ID             int64            `json:"id"`
		ActualUnits    *decimal.Decimal `json:"actualUnits"`
		ActualValuePln *decimal.Decimal `json:"actualValuePln"`
	} `json:"items"`
}

// decodePatchItemsBody maps PATCH /purchase-sessions/{id}/items' body to the service input.
func decodePatchItemsBody(r *http.Request) ([]UpdateItemEntry, *httpx.AppError) {
	var body patchSessionItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, unprocessable("Invalid request body")
	}
	entries := make([]UpdateItemEntry, 0, len(body.Items))
	for _, it := range body.Items {
		entries = append(entries, UpdateItemEntry{ID: it.ID, ActualUnits: it.ActualUnits, ActualValuePln: it.ActualValuePln})
	}
	return entries, nil
}

func unprocessable(message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Message: message}
}

// newOpenAPIMeta builds the generated openapi.Meta for the response shapes that carry their own
// meta field instead of an httpx envelope; its requestId is the request's X-Request-Id
// (httpx.NewMetaFrom), the id the access and error logs carry (task 182 / F4.2).
func newOpenAPIMeta(ctx context.Context) openapi.Meta {
	m := httpx.NewMetaFrom(ctx)
	id := types.UUID(m.RequestID)
	return openapi.Meta{RequestId: &id, Timestamp: &m.Timestamp}
}
