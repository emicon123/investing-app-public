// Package rebalancing is portfolioB's HTTP adapter for the purchase endpoints plus its funding policy
// (funding.go); the calculate/confirm/history service is common-go/siblingcore/rebalancing
// (ADR-113). portfolioB has one book and five account buckets; every buy is split over the four
// IKE/IKZE accounts by the cash waterfall.
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
)

// Handler adapts the siblingcore rebalancing service to the ServerInterface methods
// (Calculate, Confirm, History). Wire numbers are float64 (the generated models are float32).
type Handler struct {
	service *screbalancing.Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *screbalancing.Service) *Handler { return &Handler{service: service} }

// Calculate implements POST /purchases/calculate (no request body: the budget is the account
// cash capped by the monthly contribution).
func (h *Handler) Calculate(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Calculate(r.Context(), siblingcore.WholeBook, nil)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(toWireResult(result)))
}

// wireAccountUnitAlloc is one account's bought units; units decode as a decimal (task 182 /
// F3.4), the wire still takes a JSON number.
type wireAccountUnitAlloc struct {
	AccountType string          `json:"accountType"`
	Units       decimal.Decimal `json:"units"`
}

type wireConfirmItem struct {
	Ticker             string                 `json:"ticker"`
	AccountAllocations []wireAccountUnitAlloc `json:"accountAllocations"`
}

type wireConfirmRequest struct {
	Items []wireConfirmItem `json:"items"`
}

// toConfirmRequest maps the body to the service input: per ticker, the units bought per account.
func (b wireConfirmRequest) toConfirmRequest() screbalancing.ConfirmRequest {
	items := make([]screbalancing.ConfirmItem, 0, len(b.Items))
	for _, item := range b.Items {
		var units siblingcore.Units
		for _, a := range item.AccountAllocations {
			units = units.Add(siblingcore.Bucket(a.AccountType), a.Units)
		}
		items = append(items, screbalancing.ConfirmItem{Ticker: item.Ticker, Units: units})
	}
	return screbalancing.ConfirmRequest{Portfolio: siblingcore.WholeBook, Items: items}
}

// Confirm implements POST /purchases/confirm: the units actually bought per account.
func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	var body wireConfirmRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.service.Confirm(r.Context(), body.toConfirmRequest()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// History implements GET /purchases/history.
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.service.History(r.Context(), siblingcore.WholeBook)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Single(wire.PurchaseHistory{Sessions: wire.FromSessions(sessions)}))
}

// ── Wire types ──────────────────────────────────────────────────────────────

type wireAccountAlloc struct {
	AccountType string  `json:"accountType"`
	AllocPln    float64 `json:"allocPln"`
	UnitsToBuy  float64 `json:"unitsToBuy"`
}

type wireBuyListItem struct {
	Ticker             string             `json:"ticker"`
	Name               string             `json:"name"`
	Exchange           string             `json:"exchange"`
	PurchaseCurrency   string             `json:"purchaseCurrency"`
	AllocatedPln       float64            `json:"allocatedPln"`
	AllocatedNative    float64            `json:"allocatedNative"`
	UnitsToBuy         float64            `json:"unitsToBuy"`
	CurrentPct         float64            `json:"currentPct"`
	TargetPct          float64            `json:"targetPct"`
	DeviationPct       float64            `json:"deviationPct"`
	AccountAllocations []wireAccountAlloc `json:"accountAllocations,omitempty"`
	AvailableTickers   []string           `json:"availableTickers,omitempty"`
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
	CashOnHandPln          float64           `json:"cashOnHandPln"`
	DeployableCashPln      float64           `json:"deployableCashPln"`
	TotalPortfolioValuePln float64           `json:"totalPortfolioValuePln"`
	PricesRefreshedAt      time.Time         `json:"pricesRefreshedAt"`
	BuyList                []wireBuyListItem `json:"buyList"`
	DonutData              []wireDonutSlice  `json:"donutData"`
}

func toWireResult(r screbalancing.Result) wireRebalancingResult {
	buyList := make([]wireBuyListItem, 0, len(r.BuyList))
	for _, item := range r.BuyList {
		allocs := make([]wireAccountAlloc, 0, len(item.AccountAllocations))
		for _, a := range item.AccountAllocations {
			allocs = append(allocs, wireAccountAlloc{AccountType: string(a.Bucket), AllocPln: decx.ToFloat(a.AllocPln), UnitsToBuy: decx.ToFloat(a.UnitsToBuy)})
		}
		buyList = append(buyList, wireBuyListItem{
			Ticker: item.Ticker, Name: item.Name, Exchange: item.Exchange, PurchaseCurrency: item.PurchaseCurrency,
			AllocatedPln: decx.ToFloat(item.AllocatedPln), AllocatedNative: decx.ToFloat(item.AllocatedNative),
			UnitsToBuy: decx.ToFloat(item.UnitsToBuy), CurrentPct: decx.ToFloat(item.CurrentPct),
			TargetPct: decx.ToFloat(item.TargetPct), DeviationPct: decx.ToFloat(item.DeviationPct),
			AccountAllocations: allocs, AvailableTickers: item.AvailableTickers,
		})
	}
	donut := make([]wireDonutSlice, 0, len(r.DonutData))
	for _, d := range r.DonutData {
		donut = append(donut, wireDonutSlice{
			Ticker: d.Ticker, Name: d.Name, CurrentPct: decx.ToFloat(d.CurrentPct), TargetPct: decx.ToFloat(d.TargetPct),
			ValuePln: decx.ToFloat(d.ValuePln), DeviationPct: decx.ToFloat(d.DeviationPct), Status: d.Status,
		})
	}
	cash := 0.0
	if r.CashOnHandPln != nil {
		cash = decx.ToFloat(*r.CashOnHandPln)
	}
	return wireRebalancingResult{
		CashOnHandPln: cash, DeployableCashPln: decx.ToFloat(r.DeployableCashPln),
		TotalPortfolioValuePln: decx.ToFloat(r.TotalPortfolioValuePln), PricesRefreshedAt: r.PricesRefreshedAt,
		BuyList: buyList, DonutData: donut,
	}
}
