package marketdatacore

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"investing-app/common-go/decx"
	"investing-app/common-go/httpx"
)

// validTickerPattern is the direct port of MarketDataController.getPrices' inline validation
// regex.
var validTickerPattern = regexp.MustCompile(`^[A-Z0-9.\-]{1,50}$`)

const maxTickersParamLen = 500

// Handler serves GET /market-data/prices, POST /market-data/prices/refresh,
// GET /market-data/fx-rates and POST /market-data/fx-rates/refresh for every app, with the
// exact validation, filtering and status codes of the three per-app copies it replaces (task
// 179). Each app's generated ServerInterface methods forward to it in one line. It writes its
// own JSON wire structs (priceEntryJSON, fxRateEntryJSON): the field names and order of the
// generated DTOs, with float64 numbers on every app — the siblings' generated types are float32,
// which rounded prices and rates to 7 significant digits (finding F3.8).
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler around service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// priceEntryJSON is the wire shape of one price (the generated PriceEntry's JSON names, in its
// alphabetical field order).
type priceEntryJSON struct {
	Currency  string    `json:"currency"`
	FetchedAt time.Time `json:"fetchedAt"`
	Price     float64   `json:"price"`
	PricePln  float64   `json:"pricePln"`
	Ticker    string    `json:"ticker"`
}

// fxRateEntryJSON is the wire shape of one FX rate (the generated FxRateEntry's JSON names, in
// its alphabetical field order).
type fxRateEntryJSON struct {
	FetchedAt    time.Time `json:"fetchedAt"`
	FromCurrency string    `json:"fromCurrency"`
	RateToPln    float64   `json:"rateToPln"`
	Source       string    `json:"source"`
}

// GetPrices implements GET /market-data/prices. Cache-only read, optionally filtered by the
// comma-separated tickers query param (nil = absent); never triggers a live fetch. Edge cases kept
// from the original (ADR-094): a present-but-blank param (e.g. "tickers=") is "no filter", while a
// param containing only commas/whitespace is "not blank" and legitimately filters down to zero
// results — a latent quirk preserved as-is. Main's undocumented "forceRefresh" query param has no
// effect.
func (h *Handler) GetPrices(w http.ResponseWriter, r *http.Request, tickers *string) {
	var requested map[string]bool // nil = no filter
	if tickers != nil {
		raw := *tickers
		if len(raw) > maxTickersParamLen {
			httpx.WriteError(w, httpx.NewValidationError("tickers", "size must be between 0 and 500"))
			return
		}
		segments := splitTickers(raw)
		for _, t := range segments {
			if !validTickerPattern.MatchString(t) {
				httpx.WriteError(w, httpx.NewValidationError("", "Invalid ticker: "+t))
				return
			}
		}
		if strings.TrimSpace(raw) != "" {
			requested = make(map[string]bool, len(segments))
			for _, t := range segments {
				requested[t] = true
			}
		}
	}

	entries, err := h.service.GetAllPriceEntries(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]priceEntryJSON, 0, len(entries))
	for _, e := range entries {
		if requested == nil || requested[e.Ticker] {
			out = append(out, priceEntryJSON{Currency: e.Currency, FetchedAt: e.FetchedAt,
				Price: decx.ToFloat(e.Price), PricePln: decx.ToFloat(e.PricePln), Ticker: e.Ticker})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List(out))
}

// splitTickers splits the tickers param on commas, trimming each segment and dropping empty
// ones (nil for no segment).
func splitTickers(raw string) []string {
	var out []string
	for t := range strings.SplitSeq(raw, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// RefreshPrices implements POST /market-data/prices/refresh. Synchronously refreshes every tracked
// ticker and writes the RefreshResult raw ({total, succeeded, failures}, NOT the {data, meta}
// envelope): that is the shape the frontends read, although main's docs/openapi.yaml documents a
// list envelope for this operation.
func (h *Handler) RefreshPrices(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ForceRefreshAllPrices(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

// GetFxRates implements GET /market-data/fx-rates — the direct port of
// MarketDataController.getFxRates. Cache-only read (PLN=1.0 always first); never triggers a live
// fetch.
func (h *Handler) GetFxRates(w http.ResponseWriter, r *http.Request) {
	entries, err := h.service.GetAllFxRateEntries(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]fxRateEntryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, fxRateEntryJSON{FetchedAt: e.FetchedAt, FromCurrency: e.FromCurrency,
			RateToPln: decx.ToFloat(e.RateToPln), Source: e.Source})
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.List(out))
}

// RefreshFxRates implements POST /market-data/fx-rates/refresh. Always answers 204 regardless of
// per-currency failures. Each app's apiServer forwards the generated refreshFxRates operation to
// it.
func (h *Handler) RefreshFxRates(w http.ResponseWriter, r *http.Request) {
	h.service.ForceRefreshAllFxRates(r.Context())
	w.WriteHeader(http.StatusNoContent)
}
