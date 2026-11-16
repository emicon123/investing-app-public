// Package rebalancing is portfolioC's HTTP adapter for the purchase endpoints plus its funding policy
// (funding.go); the calculate/confirm/history service is common-go/siblingcore/rebalancing
// (ADR-113). Every call is scoped to one portfolio; the deposit amount is request-scoped.
package rebalancing

import (
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"investing-app/common-go/siblingcore/wire"
	"portfolio-c/backend-go/internal/book"
	"portfolio-c/backend-go/openapi"
)

// Handler adapts the siblingcore rebalancing service to the ServerInterface methods
// (Calculate, Confirm, History). Wire numbers are float64 (the generated models are float32).
type Handler struct {
	service *screbalancing.Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *screbalancing.Service) *Handler { return &Handler{service: service} }

// wireCalculateRequest is POST /purchases/calculate's body; the optional override decodes as a
// decimal (task 182 / F3.4), the wire still takes a JSON number.
type wireCalculateRequest struct {
	Portfolio        string           `json:"portfolio"`
	DepositAmountPln *decimal.Decimal `json:"depositAmountPln,omitempty"`
}

// Calculate implements POST /purchases/calculate — the body carries the portfolio (required) and
// an optional depositAmountPln override.
func (h *Handler) Calculate(w http.ResponseWriter, r *http.Request) {
	var body wireCalculateRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	portfolio, err := book.Config.ResolvePortfolio("portfolio", body.Portfolio)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.service.Calculate(r.Context(), portfolio, body.DepositAmountPln)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toWireResult(result)))
}

// wireConfirmItem / wireConfirmRequest are POST /purchases/confirm's body; every number decodes
// as a decimal (task 182 / F3.4). An absent depositAmountPln is zero, as before.
type wireConfirmItem struct {
	Ticker string          `json:"ticker"`
	Units  decimal.Decimal `json:"units"`
}

type wireConfirmRequest struct {
	Portfolio        string            `json:"portfolio"`
	DepositAmountPln decimal.Decimal   `json:"depositAmountPln"`
	Items            []wireConfirmItem `json:"items"`
}

// toConfirmRequest maps the body to the service input (400 for an unknown portfolio).
func (b wireConfirmRequest) toConfirmRequest() (screbalancing.ConfirmRequest, error) {
	portfolio, err := book.Config.ResolvePortfolio("portfolio", b.Portfolio)
	if err != nil {
		return screbalancing.ConfirmRequest{}, err
	}
	items := make([]screbalancing.ConfirmItem, 0, len(b.Items))
	for _, item := range b.Items {
		items = append(items, screbalancing.ConfirmItem{Ticker: item.Ticker, Units: siblingcore.Units{}.Add(siblingcore.SingleBucket, item.Units)})
	}
	deposit := b.DepositAmountPln
	return screbalancing.ConfirmRequest{Portfolio: portfolio, DepositAmountPln: &deposit, Items: items}, nil
}

// Confirm implements POST /purchases/confirm.
func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	var body wireConfirmRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	req, err := body.toConfirmRequest()
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.service.Confirm(r.Context(), req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// History implements GET /purchases/history?portfolio=... (required).
func (h *Handler) History(w http.ResponseWriter, r *http.Request, params openapi.HistoryParams) {
	portfolio, err := book.Config.ResolvePortfolio("portfolio", string(params.Portfolio))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	sessions, err := h.service.History(r.Context(), portfolio)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(wire.PurchaseHistory{Sessions: wire.FromSessions(sessions)}))
}

// ── Wire types ──────────────────────────────────────────────────────────────

type wireBuyListItem struct {
	Ticker           string   `json:"ticker"`
	Name             string   `json:"name"`
	Exchange         string   `json:"exchange"`
	PurchaseCurrency string   `json:"purchaseCurrency"`
	AllocatedPln     float64  `json:"allocatedPln"`
	AllocatedNative  float64  `json:"allocatedNative"`
	UnitsToBuy       float64  `json:"unitsToBuy"`
	CurrentPct       float64  `json:"currentPct"`
	TargetPct        float64  `json:"targetPct"`
	DeviationPct     float64  `json:"deviationPct"`
	AvailableTickers []string `json:"availableTickers,omitempty"`
}

type wireDonutSlice struct {
	Ticker       string  `json:"ticker"`
	Name         string  `json:"name"`
	CurrentPct   float64 `json:"currentPct"`
	TargetPct    float64 `json:"targetPct"`
	ValuePln     float64 `json:"valuePln"`
	DeviationPct float64 `json:"deviationPct"`
	Status       string  `json:"status"`
}

type wireRebalancingResult struct {
	Portfolio              string            `json:"portfolio"`
	DeployableCashPln      float64           `json:"deployableCashPln"`
	TotalPortfolioValuePln float64           `json:"totalPortfolioValuePln"`
	PricesRefreshedAt      time.Time         `json:"pricesRefreshedAt"`
	BuyList                []wireBuyListItem `json:"buyList"`
	DonutData              []wireDonutSlice  `json:"donutData"`
}

func toWireResult(r screbalancing.Result) wireRebalancingResult {
	buyList := make([]wireBuyListItem, 0, len(r.BuyList))
	for _, item := range r.BuyList {
		buyList = append(buyList, wireBuyListItem{
			Ticker: item.Ticker, Name: item.Name, Exchange: item.Exchange, PurchaseCurrency: item.PurchaseCurrency,
			AllocatedPln: decx.ToFloat(item.AllocatedPln), AllocatedNative: decx.ToFloat(item.AllocatedNative),
			UnitsToBuy: decx.ToFloat(item.UnitsToBuy), CurrentPct: decx.ToFloat(item.CurrentPct),
			TargetPct: decx.ToFloat(item.TargetPct), DeviationPct: decx.ToFloat(item.DeviationPct),
			AvailableTickers: item.AvailableTickers,
		})
	}
	donut := make([]wireDonutSlice, 0, len(r.DonutData))
	for _, d := range r.DonutData {
		donut = append(donut, wireDonutSlice{
			Ticker: d.Ticker, Name: d.Name, CurrentPct: decx.ToFloat(d.CurrentPct), TargetPct: decx.ToFloat(d.TargetPct),
			ValuePln: decx.ToFloat(d.ValuePln), DeviationPct: decx.ToFloat(d.DeviationPct), Status: d.Status,
		})
	}
	return wireRebalancingResult{
		Portfolio: string(r.Portfolio), DeployableCashPln: decx.ToFloat(r.DeployableCashPln),
		TotalPortfolioValuePln: decx.ToFloat(r.TotalPortfolioValuePln), PricesRefreshedAt: r.PricesRefreshedAt,
		BuyList: buyList, DonutData: donut,
	}
}
