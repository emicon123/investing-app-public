package linearregression

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// History/fallback tests — HTTP legs run against httptest servers (no live Yahoo/Binance
// calls in unit tests); the reconstruction math is asserted exactly.

func TestParseYahooOHLC_SkipsNullCloseAndFallsBack(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	resp := yahooChartResponse{}
	resp.Chart.Result = append(resp.Chart.Result, struct {
		Timestamp  []int64 `json:"timestamp"`
		Indicators struct {
			Quote []struct {
				Open  []*float64 `json:"open"`
				Close []*float64 `json:"close"`
				High  []*float64 `json:"high"`
				Low   []*float64 `json:"low"`
			} `json:"quote"`
		} `json:"indicators"`
	}{Timestamp: []int64{1704067200, 1704153600, 1704240000}})
	q := struct {
		Open  []*float64 `json:"open"`
		Close []*float64 `json:"close"`
		High  []*float64 `json:"high"`
		Low   []*float64 `json:"low"`
	}{Open: []*float64{f(100), nil, f(102)}, Close: []*float64{f(101), nil, f(103)},
		High: []*float64{nil, f(200), f(104)}, Low: []*float64{f(99), f(199), nil}}
	resp.Chart.Result[0].Indicators.Quote = append(resp.Chart.Result[0].Indicators.Quote, q)

	pts := parseYahooOHLC(resp)
	if len(pts) != 2 {
		t.Fatalf("expected 2 points (null close skipped), got %d", len(pts))
	}
	if pts[0].Close != 101 || pts[0].High != 101 || pts[0].Low != 99 {
		t.Fatalf("open/high/low must fall back to close: %+v", pts[0])
	}
	if pts[1].Low != 103 {
		t.Fatalf("low must fall back to close: %+v", pts[1])
	}
	if !pts[0].Date.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dates are UTC calendar dates: %v", pts[0].Date)
	}
}

func TestResample_LastBarPerBucket(t *testing.T) {
	// Mon 2026-01-05 .. Sun 2026-01-11 (one ISO week) + Mon 2026-01-12.
	mk := func(day, close int) PricedOHLCPoint {
		return PricedOHLCPoint{Date: utcDate(2026, time.January, day), Close: float64(close), Source: PriceSourceYahoo}
	}
	daily := []PricedOHLCPoint{mk(5, 1), mk(6, 2), mk(11, 3), mk(12, 4)}
	weekly := resample(daily, "1wk")
	if len(weekly) != 2 || weekly[0].Close != 3 || weekly[1].Close != 4 {
		t.Fatalf("weekly must keep last bar per ISO week: %+v", weekly)
	}
	monthly := resample(daily, "1mo")
	if len(monthly) != 1 || monthly[0].Close != 4 {
		t.Fatalf("monthly must keep last bar per calendar month: %+v", monthly)
	}
	if got := resample(daily, "1d"); len(got) != 4 {
		t.Fatalf("daily must pass through: %+v", got)
	}
}

func TestReconstructDaily_RatioFormula(t *testing.T) {
	product := VirtuneProduct{
		FeeAnnual: 0.0025, StakingYieldAnnual: 0,
		ConfidenceTier: PriceSourceReconstructedPrecise,
	}
	asOf := utcDate(2026, time.September, 1)
	ratio := RatioSnapshot{CoinPerEtp: 0.001, AsOfDate: asOf}
	day := utcDate(2026, time.August, 1) // 31 days back
	spot := []OHLCPoint{{Date: day, Open: 100, Close: 110, High: 115, Low: 99}}
	fx := []OHLCPoint{{Date: day, Open: 4, Close: 4, High: 4, Low: 4}}

	daily := reconstructDaily(product, ratio, spot, fx, utcDate(2026, time.January, 1), utcDate(2026, time.September, 15))
	if len(daily) != 1 {
		t.Fatalf("expected 1 point, got %d", len(daily))
	}
	// ratio(t) = 0.001 * exp(0.0025 * 31/365); closePLN = 110 * ratio * 4.
	want := 110 * (0.001 * math.Exp(0.0025*31/365.0)) * 4
	if math.Abs(daily[0].Close-want) > 1e-12 {
		t.Fatalf("closePLN: got %.12f want %.12f", daily[0].Close, want)
	}
	if daily[0].Source != PriceSourceReconstructedPrecise {
		t.Fatalf("wrong source: %v", daily[0].Source)
	}
}

func TestNearestByDate_TieGoesEarlier(t *testing.T) {
	series := []OHLCPoint{
		{Date: utcDate(2026, time.January, 1), Close: 1},
		{Date: utcDate(2026, time.January, 3), Close: 3},
	}
	// Jan 2 is equidistant — the earlier point must win.
	best, ok := nearestOHLCByDate(series, utcDate(2026, time.January, 2))
	if !ok || best.Close != 1 {
		t.Fatalf("tie must resolve earlier: %+v", best)
	}
	best, _ = nearestOHLCByDate(series, utcDate(2026, time.January, 4))
	if best.Close != 3 {
		t.Fatalf("nearest must be Jan 3: %+v", best)
	}
}

// stubRatio is a fixed RatioSource (the Virtune scrape itself is tested in
// common-go/priceprovider); ok=false is "no ratio available".
type stubRatio struct {
	snap RatioSnapshot
	ok   bool
}

func (s stubRatio) FetchCurrentRatio(context.Context, string, string) (RatioSnapshot, bool) {
	return s.snap, s.ok
}

// stubHistory routes the Yahoo/Binance HTTP legs to httptest servers.
func stubHistory(t *testing.T, yahooHandler, binanceHandler http.HandlerFunc, virtune RatioSource) *HistoryService {
	t.Helper()
	yahoo := &YahooChartClient{http: &http.Client{}, chartBase: "http://yahoo.test", userAgent: "test"}
	binance := &BinanceKlinesClient{http: &http.Client{}, baseURL: "http://binance.test"}
	if virtune == nil {
		virtune = stubRatio{}
	}
	svc := NewHistoryService(yahoo, virtune, binance)
	if yahooHandler != nil {
		ys := httptest.NewServer(yahooHandler)
		t.Cleanup(ys.Close)
		yahoo.chartBase = ys.URL
	}
	if binanceHandler != nil {
		bs := httptest.NewServer(binanceHandler)
		t.Cleanup(bs.Close)
		binance.baseURL = bs.URL
	}
	return svc
}

func yahooChartJSON(timestamps []int64, closes []float64) string {
	ts, cs := "", ""
	for i, v := range timestamps {
		if i > 0 {
			ts += ","
			cs += ","
		}
		ts += fmt.Sprint(v)
		cs += fmt.Sprint(closes[i])
	}
	return fmt.Sprintf(`{"chart":{"result":[{"timestamp":[%s],"indicators":{"quote":[{"open":[%s],"close":[%s],"high":[%s],"low":[%s]}]}}]}}`, ts, cs, cs, cs, cs)
}

func TestFetchOHLCByRange_PassesThroughHealthyYahoo(t *testing.T) {
	var calls int
	svc := stubHistory(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = fmt.Fprint(w, yahooChartJSON(
			[]int64{utcDate(2026, 1, 6).Unix(), utcDate(2026, 1, 13).Unix()},
			[]float64{100, 110}))
	}, nil, nil)
	pts := svc.FetchOHLCByRange(context.Background(), "BTC-USD", "1wk",
		utcDate(2020, 1, 1).Unix(), utcDate(2026, 9, 15).Unix())
	if len(pts) != 2 || pts[0].Source != PriceSourceYahoo {
		t.Fatalf("expected 2 YAHOO points, got %+v", pts)
	}
	if calls != 1 {
		t.Fatalf("healthy Yahoo must cost exactly one call, got %d", calls)
	}
}

func TestFetchOHLCByRange_VirtuneFallbackReconstructs(t *testing.T) {
	yahooCalls := 0
	svc := stubHistory(t,
		func(w http.ResponseWriter, r *http.Request) {
			yahooCalls++
			if strings.Contains(r.URL.Path, "USDPLN") {
				// USDPLN=X FX leg: flat 4.0.
				_, _ = fmt.Fprint(w, yahooChartJSON(
					[]int64{utcDate(2026, 3, 2).Unix(), utcDate(2026, 3, 3).Unix()},
					[]float64{4, 4}))
				return
			}
			// Thin Yahoo result for the ETP itself.
			_, _ = fmt.Fprint(w, yahooChartJSON([]int64{utcDate(2026, 3, 2).Unix()}, []float64{50}))
		},
		func(w http.ResponseWriter, r *http.Request) {
			// Binance daily klines for two days.
			_, _ = fmt.Fprint(w, `[[`+fmt.Sprint(utcDate(2026, 3, 2).UnixMilli())+`,"100","115","99","110"],`+
				`[`+fmt.Sprint(utcDate(2026, 3, 3).UnixMilli())+`,"110","120","109","118"]]`)
		},
		// The scraped page carried no "Last updated" line, so the ratio is as of today.
		stubRatio{snap: RatioSnapshot{CoinPerEtp: 0.001, AsOfDate: WarsawTodayAt(time.Now())}, ok: true})
	pts := svc.FetchOHLCByRange(context.Background(), "ETNVIRBTCP.WA", "1d",
		utcDate(2020, 1, 1).Unix(), utcDate(2026, 9, 15).Unix())
	if len(pts) != 2 {
		t.Fatalf("expected 2 reconstructed points, got %+v", pts)
	}
	for _, p := range pts {
		if p.Source != PriceSourceReconstructedPrecise {
			t.Fatalf("expected reconstructed source, got %+v", p)
		}
	}
	// Day 1: 110 USDT * 0.001 * exp(small decay) * 4.0 ≈ 0.44.
	if pts[0].Close < 0.43 || pts[0].Close > 0.45 {
		t.Fatalf("reconstructed close out of range: %v", pts[0].Close)
	}
	if yahooCalls < 2 {
		t.Fatalf("expected ETP + FX yahoo calls, got %d", yahooCalls)
	}
}

func TestFetchOHLCByRange_UnmappedTickerFallsBackToYahoo(t *testing.T) {
	svc := stubHistory(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, yahooChartJSON([]int64{utcDate(2026, 3, 2).Unix()}, []float64{50}))
	}, nil, nil)
	// Single point over a short window is NOT thin (window can't hold more than one bar).
	pts := svc.FetchOHLCByRange(context.Background(), "AAPL", "1d",
		utcDate(2026, 3, 2).Unix(), utcDate(2026, 3, 2).Add(86399*time.Second).Unix())
	if len(pts) != 1 || pts[0].Source != PriceSourceYahoo {
		t.Fatalf("expected Yahoo fallback passthrough, got %+v", pts)
	}
}
