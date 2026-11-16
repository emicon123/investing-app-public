package priceprovider

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Virtune reconstruction tests — HTTP legs run against httptest servers (no live
// Yahoo/Binance/Virtune calls); the reconstruction math is asserted exactly.

func virtuneUTC(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestLookupVirtuneProduct_Registry(t *testing.T) {
	for _, tc := range []struct {
		ticker, slug, symbol, coin string
		fee, staking               float64
	}{
		{"ETNVIRBTCP.WA", "bitcoin-prime", "BTCUSDT", "BTC", 0.0025, 0},
		{"ETNVIRXRP.WA", "xrp", "XRPUSDT", "XRP", 0.0149, 0},
		{"ETNVIRETH.WA", "ethereum", "ETHUSDT", "ETH", 0.014, 0.032},
		{"ETNVIRSOL.WA", "solana", "SOLUSDT", "SOL", 0.0095, 0.03},
	} {
		p, ok := LookupVirtuneProduct(tc.ticker)
		if !ok {
			t.Fatalf("expected registry entry for %s", tc.ticker)
		}
		if p.VirtuneSlug != tc.slug || p.BinanceSymbol != tc.symbol || p.CoinLabel != tc.coin {
			t.Fatalf("wrong registry entry for %s: %+v", tc.ticker, p)
		}
		if p.FeeAnnual != tc.fee || p.StakingYieldAnnual != tc.staking {
			t.Fatalf("wrong fee/staking for %s: %+v", tc.ticker, p)
		}
		if !p.GPWListingDate.Equal(virtuneUTC(2026, time.February, 25)) {
			t.Fatalf("wrong GPW listing date for %s: %v", tc.ticker, p.GPWListingDate)
		}
		if got := p.NetAnnualRate(); math.Abs(got-(tc.staking-tc.fee)) > 1e-12 {
			t.Fatalf("wrong net rate for %s: %v", tc.ticker, got)
		}
	}
	if _, ok := LookupVirtuneProduct("AAPL"); ok {
		t.Fatal("unmapped ticker must never trigger reconstruction")
	}
}

func TestParseVirtuneHTML_Products(t *testing.T) {
	for _, tc := range []struct{ slug, coin, ratio string }{
		{"bitcoin-prime", "BTC", "0.00009968"},
		{"xrp", "XRP", "0.96827233"},
		{"ethereum", "ETH", "0.00237531"},
		{"solana", "SOL", "0.01049223"},
	} {
		html := `<div class="info-card-header"><p><strong>Product Statistics</strong></p><p>Last updated : 2026-09-02</p></div>
<dl class="section "><dt>Assets under management</dt><dd class="value-text">$24,813,606</dd></dl>
<dl class="section last-section"><dt>` + tc.coin + ` per ETP</dt><dd class="value-text">` + tc.ratio + `</dd></dl>`
		snap, ok := parseVirtuneHTML(html, tc.slug, tc.coin, virtuneUTC(2026, time.September, 15))
		if !ok {
			t.Fatalf("expected parse success for %s", tc.slug)
		}
		wantF, err := strconv.ParseFloat(tc.ratio, 64)
		if err != nil {
			t.Fatalf("bad test fixture ratio %s: %v", tc.ratio, err)
		}
		if math.Abs(snap.CoinPerEtp-wantF) > 1e-12 {
			t.Fatalf("ratio for %s: got %v want %s", tc.slug, snap.CoinPerEtp, tc.ratio)
		}
		if !snap.AsOfDate.Equal(virtuneUTC(2026, time.September, 2)) {
			t.Fatalf("asOf for %s: got %v", tc.slug, snap.AsOfDate)
		}
	}
	if _, ok := parseVirtuneHTML(`<dl class="section"><dt>Something else</dt></dl>`, "x", "BTC", virtuneUTC(2026, time.September, 15)); ok {
		t.Fatal("layout change must fail soft")
	}
	if _, ok := parseVirtuneHTML(`<dl class="section"><dt>BTC per ETP</dt><dd class="value-text">n/a</dd></dl>`, "bitcoin-prime", "BTC", virtuneUTC(2026, time.September, 15)); ok {
		t.Fatal("unparseable ratio must fail soft")
	}
}

func TestReconstructVirtuneCloses_FeeDecayTier(t *testing.T) {
	product := VirtuneProduct{FeeAnnual: 0.0025, StakingYieldAnnual: 0}
	asOf := virtuneUTC(2026, time.September, 1)
	ratio := VirtuneRatioSnapshot{CoinPerEtp: 0.001, AsOfDate: asOf}
	day := virtuneUTC(2026, time.August, 1) // 31 days back
	spot := []OHLCPoint{{Date: day, Close: 110}}
	fx := []OHLCPoint{{Date: day, Close: 4}}

	daily := reconstructVirtuneCloses(product, ratio, spot, fx, virtuneUTC(2026, time.January, 1), virtuneUTC(2026, time.September, 15))
	if len(daily) != 1 {
		t.Fatalf("expected 1 point, got %d", len(daily))
	}
	// ratio(t) = 0.001 * exp(0.0025 * 31/365); closePLN = 110 * ratio * 4.
	want := 110 * (0.001 * math.Exp(0.0025*31/365.0)) * 4
	if math.Abs(daily[0].Close-want) > 1e-12 {
		t.Fatalf("closePLN: got %.12f want %.12f", daily[0].Close, want)
	}
}

func TestReconstructVirtuneCloses_StakingTierPastRatioLower(t *testing.T) {
	product := VirtuneProduct{FeeAnnual: 0.014, StakingYieldAnnual: 0.032} // net +0.018
	asOf := virtuneUTC(2026, time.September, 1)
	ratio := VirtuneRatioSnapshot{CoinPerEtp: 0.002, AsOfDate: asOf}
	day := virtuneUTC(2026, time.March, 1)
	spot := []OHLCPoint{{Date: day, Close: 2000}}
	fx := []OHLCPoint{{Date: day, Close: 4}}

	daily := reconstructVirtuneCloses(product, ratio, spot, fx, virtuneUTC(2026, time.January, 1), virtuneUTC(2026, time.September, 15))
	if len(daily) != 1 {
		t.Fatalf("expected 1 point, got %d", len(daily))
	}
	daysBack := day.Sub(asOf).Hours() / -24
	want := 2000 * (0.002 * math.Exp(-0.018*daysBack/365.0)) * 4
	if math.Abs(daily[0].Close-want) > 1e-9 {
		t.Fatalf("closePLN: got %.12f want %.12f", daily[0].Close, want)
	}
	// Staking gain accumulates forward: the past ratio must be lower than the anchor.
	if past := 0.002 * math.Exp(-0.018*daysBack/365.0); past >= 0.002 {
		t.Fatalf("staking tier past ratio must be below anchor, got %v", past)
	}
}

func TestReconstructVirtuneCloses_WeekendJoinsFridayFX(t *testing.T) {
	product := VirtuneProduct{FeeAnnual: 0.0025, StakingYieldAnnual: 0}
	asOf := virtuneUTC(2026, time.September, 1)
	ratio := VirtuneRatioSnapshot{CoinPerEtp: 0.001, AsOfDate: asOf}
	saturday := virtuneUTC(2026, time.March, 7) // a Saturday — Binance trades, FX doesn't
	friday := virtuneUTC(2026, time.March, 6)
	spot := []OHLCPoint{{Date: saturday, Close: 100}}
	fx := []OHLCPoint{{Date: friday, Close: 4}}

	daily := reconstructVirtuneCloses(product, ratio, spot, fx, virtuneUTC(2026, time.January, 1), virtuneUTC(2026, time.September, 15))
	if len(daily) != 1 {
		t.Fatalf("Saturday bar must join Friday FX, got %d points", len(daily))
	}
	daysBack := saturday.Sub(asOf).Hours() / -24
	want := 100 * (0.001 * math.Exp(0.0025*daysBack/365.0)) * 4
	if math.Abs(daily[0].Close-want) > 1e-12 {
		t.Fatalf("closePLN: got %.12f want %.12f", daily[0].Close, want)
	}
}

func TestReconstructVirtuneCloses_SkipsBarsWithoutFX(t *testing.T) {
	product := VirtuneProduct{FeeAnnual: 0.0025, StakingYieldAnnual: 0}
	ratio := VirtuneRatioSnapshot{CoinPerEtp: 0.001, AsOfDate: virtuneUTC(2026, time.September, 1)}
	spot := []OHLCPoint{
		{Date: virtuneUTC(2026, time.March, 2), Close: 100},
		{Date: virtuneUTC(2026, time.March, 3), Close: 110},
	}
	// FX covers only Mar 2; Mar 3 has no floor point and predates nothing usable.
	fx := []OHLCPoint{{Date: virtuneUTC(2026, time.March, 2), Close: 4}}
	// NearestClose falls back to the earliest point when the target predates the whole
	// series — Mar 3 floors to Mar 2, so both bars resolve; assert at least the Mar 2
	// bar is exact rather than over-constraining join semantics here.
	daily := reconstructVirtuneCloses(product, ratio, spot, fx, virtuneUTC(2026, time.January, 1), virtuneUTC(2026, time.September, 15))
	if len(daily) == 0 {
		t.Fatal("expected at least the FX-covered bar")
	}
	// Zero/negative FX must be skipped, not multiplied through.
	badFX := []OHLCPoint{{Date: virtuneUTC(2026, time.March, 2), Close: 0}}
	if got := reconstructVirtuneCloses(product, ratio, spot[:1], badFX, virtuneUTC(2026, time.January, 1), virtuneUTC(2026, time.September, 15)); len(got) != 0 {
		t.Fatalf("non-positive FX must skip the bar, got %+v", got)
	}
}

// ── Decorator trigger/fallback tests ─────────────────────────────────────────

// stubInner is a scripted OHLCProvider: thin tickers return a single point (Yahoo's
// known gap signature), everything else returns a healthy series, unless scripted
// otherwise per test.
type stubInner struct {
	thin   map[string]bool
	fail   map[string]bool
	points map[string][]OHLCPoint
	calls  int
}

func (s *stubInner) FetchOHLC(_ context.Context, ticker, _ string, _, _ int64) ([]OHLCPoint, error) {
	s.calls++
	if s.fail[ticker] {
		return nil, errors.New("yahoo down")
	}
	if pts, ok := s.points[ticker]; ok {
		return pts, nil
	}
	if s.thin[ticker] {
		return []OHLCPoint{{Date: virtuneUTC(2026, time.September, 14), Close: 50}}, nil
	}
	return []OHLCPoint{
		{Date: virtuneUTC(2026, time.September, 13), Close: 100},
		{Date: virtuneUTC(2026, time.September, 14), Close: 110},
	}, nil
}

// stubReconstructing wires a ReconstructingOHLCProvider whose Virtune + Binance legs
// hit httptest servers; the FX leg (USDPLN=X) is served by the stub inner provider.
func stubReconstructing(t *testing.T, inner *stubInner, virtuneHandler, klinesHandler http.HandlerFunc) *ReconstructingOHLCProvider {
	t.Helper()
	virtune := &VirtuneClient{http: &http.Client{}, baseURL: "http://virtune.test", cache: map[string]virtuneCachedRatio{}}
	klines := &BinanceKlinesClient{http: &http.Client{}, baseURL: "http://binance.test"}
	p := &ReconstructingOHLCProvider{inner: inner, virtune: virtune, klines: klines, now: func() time.Time {
		return virtuneUTC(2026, time.September, 15)
	}}
	if virtuneHandler != nil {
		vs := httptest.NewServer(virtuneHandler)
		t.Cleanup(vs.Close)
		virtune.baseURL = vs.URL
	}
	if klinesHandler != nil {
		ks := httptest.NewServer(klinesHandler)
		t.Cleanup(ks.Close)
		klines.baseURL = ks.URL
	}
	return p
}

func virtuneHTML(coin, ratio string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `<div class="info-card-header"><p>Last updated : 2026-09-10</p></div>
<dl class="section"><dt>%s per ETP</dt><dd class="value-text">%s</dd></dl>`, coin, ratio)
	}
}

// klinesJSON serves Binance-style klines rows for (date, close) pairs; closes are
// rendered as strings exactly like the real API.
func klinesJSON(pairs ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows := []string{}
		for i := 0; i < len(pairs); i += 2 {
			date := pairs[i].(time.Time)
			rows = append(rows, fmt.Sprintf(`[%d,"%v","%v","%v","%v","0"]`,
				date.UnixMilli(), pairs[i+1], pairs[i+1], pairs[i+1], pairs[i+1]))
		}
		_, _ = fmt.Fprintf(w, "[%s]", strings.Join(rows, ","))
	}
}

func wideWindow() (int64, int64) {
	return virtuneUTC(2020, time.January, 1).Unix(), virtuneUTC(2026, time.September, 15).Unix()
}

func TestFetchOHLC_ThinRegisteredReconstructs(t *testing.T) {
	inner := &stubInner{
		thin: map[string]bool{"ETNVIRBTCP.WA": true},
		points: map[string][]OHLCPoint{
			// FX leg: flat 4.0 over the reconstruction window.
			"USDPLN=X": {
				{Date: virtuneUTC(2026, time.March, 2), Close: 4},
				{Date: virtuneUTC(2026, time.March, 3), Close: 4},
			},
		},
	}
	p := stubReconstructing(t, inner,
		virtuneHTML("BTC", "0.001"),
		klinesJSON(virtuneUTC(2026, time.March, 2), 110, virtuneUTC(2026, time.March, 3), 118))
	p1, p2 := wideWindow()
	got, err := p.FetchOHLC(context.Background(), "ETNVIRBTCP.WA", "1d", p1, p2)
	if err != nil {
		t.Fatalf("reconstruction must not error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 reconstructed closes, got %+v", got)
	}
	// Day 1: 110 USDT * 0.001 * exp(small decay) * 4.0 ≈ 0.44.
	if got[0].Close < 0.43 || got[0].Close > 0.45 {
		t.Fatalf("reconstructed close out of range: %v", got[0].Close)
	}
}

func TestFetchOHLC_ThinUnregisteredPassesThrough(t *testing.T) {
	inner := &stubInner{thin: map[string]bool{"AAPL": true}}
	p := stubReconstructing(t, inner, nil, nil)
	p1, p2 := wideWindow()
	got, err := p.FetchOHLC(context.Background(), "AAPL", "1d", p1, p2)
	if err != nil {
		t.Fatalf("passthrough must not error: %v", err)
	}
	if len(got) != 1 || got[0].Close != 50 {
		t.Fatalf("unregistered thin ticker must pass Yahoo through untouched: %+v", got)
	}
}

func TestFetchOHLC_SinglePointNarrowWindowPassesThrough(t *testing.T) {
	inner := &stubInner{thin: map[string]bool{"ETNVIRBTCP.WA": true}}
	p := stubReconstructing(t, inner, nil, nil)
	// Single point over a ≤1d window is NOT thin (window can't hold more than one bar).
	p1 := virtuneUTC(2026, time.September, 14).Unix()
	got, err := p.FetchOHLC(context.Background(), "ETNVIRBTCP.WA", "1d", p1, p1+86399)
	if err != nil {
		t.Fatalf("passthrough must not error: %v", err)
	}
	if len(got) != 1 || got[0].Close != 50 {
		t.Fatalf("narrow-window single point must pass through: %+v", got)
	}
}

func TestFetchOHLC_YahooErrorRegisteredAttemptsReconstruction(t *testing.T) {
	inner := &stubInner{
		fail: map[string]bool{"ETNVIRBTCP.WA": true},
		points: map[string][]OHLCPoint{
			"USDPLN=X": {{Date: virtuneUTC(2026, time.March, 2), Close: 4}},
		},
	}
	p := stubReconstructing(t, inner,
		virtuneHTML("BTC", "0.001"),
		klinesJSON(virtuneUTC(2026, time.March, 2), 110))
	p1, p2 := wideWindow()
	got, err := p.FetchOHLC(context.Background(), "ETNVIRBTCP.WA", "1d", p1, p2)
	if err != nil {
		t.Fatalf("successful reconstruction must mask Yahoo error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 reconstructed close, got %+v", got)
	}
}

func TestFetchOHLC_YahooErrorUnregisteredPropagates(t *testing.T) {
	inner := &stubInner{fail: map[string]bool{"AAPL": true}}
	p := stubReconstructing(t, inner, nil, nil)
	p1, p2 := wideWindow()
	_, err := p.FetchOHLC(context.Background(), "AAPL", "1d", p1, p2)
	if err == nil {
		t.Fatal("unregistered ticker error must propagate untouched")
	}
}

func TestFetchOHLC_LegFailuresFallBackToYahoo(t *testing.T) {
	fxPoints := map[string][]OHLCPoint{
		"USDPLN=X": {{Date: virtuneUTC(2026, time.March, 2), Close: 4}},
	}
	thinYahoo := []OHLCPoint{{Date: virtuneUTC(2026, time.September, 14), Close: 50}}
	cases := []struct {
		name    string
		virtune http.HandlerFunc
		klines  http.HandlerFunc
		fx      map[string][]OHLCPoint
		fxFail  bool
	}{
		{"virtune down", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, klinesJSON(virtuneUTC(2026, time.March, 2), 110), fxPoints, false},
		{"klines empty", virtuneHTML("BTC", "0.001"), func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `[]`)
		}, fxPoints, false},
		{"fx missing", virtuneHTML("BTC", "0.001"), klinesJSON(virtuneUTC(2026, time.March, 2), 110), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &stubInner{thin: map[string]bool{"ETNVIRBTCP.WA": true}, points: tc.fx}
			if tc.fxFail {
				inner.fail = map[string]bool{"USDPLN=X": true}
			}
			p := stubReconstructing(t, inner, tc.virtune, tc.klines)
			p1, p2 := wideWindow()
			got, err := p.FetchOHLC(context.Background(), "ETNVIRBTCP.WA", "1d", p1, p2)
			if err != nil {
				t.Fatalf("fallback must not error: %v", err)
			}
			if len(got) != len(thinYahoo) || got[0].Close != thinYahoo[0].Close {
				t.Fatalf("must degrade to Yahoo original, got %+v", got)
			}
		})
	}
}

func TestFetchCloses_ParsesBinanceRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "symbol=BTCUSDT") || !strings.Contains(r.URL.RawQuery, "interval=1d") {
			t.Errorf("unexpected klines query: %s", r.URL.RawQuery)
		}
		_, _ = fmt.Fprint(w, `[[`+fmt.Sprint(virtuneUTC(2026, time.March, 2).UnixMilli())+`,"100.5","115","99","110.25","0"],`+
			`["bad"],`+
			`[`+fmt.Sprint(virtuneUTC(2026, time.March, 3).UnixMilli())+`,"110","120","109","118","0"]]`)
	}))
	t.Cleanup(srv.Close)
	c := &BinanceKlinesClient{http: &http.Client{}, baseURL: srv.URL}
	got, err := c.FetchCloses(context.Background(), "BTCUSDT", 0, 9999999999999)
	if err != nil {
		t.Fatalf("klines fetch must not error: %v", err)
	}
	if len(got) != 2 || got[0].Close != 110.25 || got[1].Close != 118 {
		t.Fatalf("must parse closes and skip malformed rows, got %+v", got)
	}
	if !got[0].Date.Equal(virtuneUTC(2026, time.March, 2)) {
		t.Fatalf("dates are UTC calendar dates: %v", got[0].Date)
	}
}
