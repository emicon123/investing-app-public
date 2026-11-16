package admin

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"investing-app/common-go/priceprovider"
	"portfolio-c/backend-go/db/sqlcgen"
)

// recordingOHLC fails every fetch and remembers the order of the tickers asked for.
type recordingOHLC struct {
	mu    sync.Mutex
	asked []string
}

func (r *recordingOHLC) FetchOHLC(_ context.Context, ticker, _ string, _, _ int64) ([]priceprovider.OHLCPoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, ticker)
	return nil, errors.New("boom")
}

// The FX series are fetched, and their warnings reported, in a fixed order (EUR, then USD), not in
// Go map order (task 187 B5b). Repeated so a map-order implementation cannot pass by luck.
func TestFetchFxSeries_FixedOrderEURThenUSD(t *testing.T) {
	positions := []sqlcgen.ListBackfillPositionsRow{{PriceCurrency: "USD"}, {PriceCurrency: "EUR"}, {PriceCurrency: "PLN"}}
	for range 50 {
		rec := &recordingOHLC{}
		fxMap, warnings := (&Service{ohlc: rec}).fetchFxSeries(context.Background(), positions, 0, 1)

		if want := []string{"EURPLN=X", "USDPLN=X"}; !slices.Equal(rec.asked, want) {
			t.Fatalf("fetch order = %v, want %v", rec.asked, want)
		}
		if want := []string{"no yahoo EURPLN=X: boom", "no yahoo USDPLN=X: boom"}; !slices.Equal(warnings, want) {
			t.Fatalf("warnings = %v, want %v", warnings, want)
		}
		if len(fxMap) != 2 || fxMap["EURPLN=X"] != nil || fxMap["USDPLN=X"] != nil {
			t.Fatalf("fxMap = %v, want both keys with nil series", fxMap)
		}
	}
}

func TestFetchFxSeries_OnlyNeededCurrencies(t *testing.T) {
	rec := &recordingOHLC{}
	fxMap, warnings := (&Service{ohlc: rec}).fetchFxSeries(context.Background(), []sqlcgen.ListBackfillPositionsRow{{PriceCurrency: "PLN"}}, 0, 1)
	if len(rec.asked) != 0 || len(fxMap) != 0 || len(warnings) != 0 {
		t.Fatalf("PLN-only positions need no FX: asked=%v fxMap=%v warnings=%v", rec.asked, fxMap, warnings)
	}
}
