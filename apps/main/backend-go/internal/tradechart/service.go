package tradechart

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/linearregression"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	AggregateTradesByTicker(ctx context.Context) ([]sqlcgen.AggregateTradesByTickerRow, error)
	ListPositionsForChart(ctx context.Context) ([]sqlcgen.ListPositionsForChartRow, error)
	ListTradesByTicker(ctx context.Context, ticker string) ([]sqlcgen.ListTradesByTickerRow, error)
}

// History is the OHLC history facade this service reads price series (and historical
// FX series for marker conversion) through — satisfied by
// *linearregression.HistoryService, faked in unit tests.
type History interface {
	FetchOHLCByRange(ctx context.Context, yahooTicker, interval string, period1, period2 int64) []linearregression.PricedOHLCPoint
}

// fxTickers mirrors FxRateCacheService.YAHOO_FX_TICKERS — the four PLN pairs. PLN
// itself is the identity (no fetch); anything else has no pair and fails with 503,
// never a silent 1.0 (ADR-057).
var fxTickers = map[string]string{
	"EUR": "EURPLN=X",
	"USD": "USDPLN=X",
	"CHF": "CHFPLN=X",
	"GBP": "GBPPLN=X",
}

// Service is the trade-chart service layer: handler -> Service -> sqlc Queries +
// History (both read-only — this package never writes).
type Service struct {
	queries Queries
	history History
	now     func() time.Time // overridable clock (tests); defaults to time.Now
}

// NewService wires queries and the history facade (explicit constructor wiring,
// ADR-094 §2 — no DI framework).
func NewService(queries Queries, history History) *Service {
	return &Service{queries: queries, history: history, now: time.Now}
}

// ListTickers returns the chart ticker picker — the direct port of
// TradeChartService.listTickers. Built from DISTINCT asset_purchases.ticker joined to
// positions for display metadata; never triggers a Yahoo call. Tickers with no
// matching position (or a NULL yahoo_ticker) land in Excluded with a reason instead
// of causing an error.
func (s *Service) ListTickers(ctx context.Context) (TickersResult, error) {
	aggregates, err := s.queries.AggregateTradesByTicker(ctx)
	if err != nil {
		return TickersResult{}, fmt.Errorf("tradechart: aggregate: %w", err)
	}
	positions, err := s.queries.ListPositionsForChart(ctx)
	if err != nil {
		return TickersResult{}, fmt.Errorf("tradechart: positions: %w", err)
	}
	byTicker := make(map[string]sqlcgen.ListPositionsForChartRow, len(positions))
	for _, p := range positions {
		byTicker[p.Ticker] = p
	}

	result := TickersResult{
		Data:     []TickerEntry{},
		Excluded: []ExcludedTicker{},
	}
	for _, agg := range aggregates {
		pos, ok := byTicker[agg.Ticker]
		if !ok {
			result.Excluded = append(result.Excluded, ExcludedTicker{Ticker: agg.Ticker, Reason: "NO_POSITION_MATCH"})
			continue
		}
		if pos.YahooTicker == nil {
			result.Excluded = append(result.Excluded, ExcludedTicker{Ticker: agg.Ticker, Reason: "MISSING_YAHOO_TICKER"})
			continue
		}
		result.Data = append(result.Data, TickerEntry{
			Ticker: agg.Ticker, Name: pos.Name, YahooTicker: *pos.YahooTicker,
			PriceCurrency: string(pos.PriceCurrency), TradeCount: agg.TradeCount,
			FirstTradeAt: agg.FirstTradeAt.Time, LastTradeAt: agg.LastTradeAt.Time,
		})
	}
	return result, nil
}

// GetChart returns the raw price series plus one marker per asset_purchases row. Errors: bad
// interval -> 400, unknown ticker or no trade history -> 404, NULL yahoo_ticker or empty price
// history -> 400, unavailable historical FX for a non-PLN currency -> 503 (never a 1.0 default).
func (s *Service) GetChart(ctx context.Context, ticker, interval string, fromDate, toDate *time.Time) (ChartData, error) {
	if err := linearregression.ValidateInterval(interval); err != nil {
		return ChartData{}, err
	}

	positions, err := s.queries.ListPositionsForChart(ctx)
	if err != nil {
		return ChartData{}, fmt.Errorf("tradechart: positions: %w", err)
	}
	var position *sqlcgen.ListPositionsForChartRow
	for i, p := range positions {
		if p.Ticker == ticker {
			position = &positions[i]
			break
		}
	}
	if position == nil {
		return ChartData{}, httpx.NewNotFoundError("Position not found for ticker: " + ticker)
	}

	trades, err := s.queries.ListTradesByTicker(ctx, ticker)
	if err != nil {
		return ChartData{}, fmt.Errorf("tradechart: trades: %w", err)
	}
	if len(trades) == 0 {
		return ChartData{}, httpx.NewNotFoundError("No trade history found for ticker: " + ticker)
	}
	if position.YahooTicker == nil {
		return ChartData{}, httpx.NewValidationError("", "yahoo_ticker is not set for ticker: "+ticker)
	}

	ohlc := s.fetchOHLC(ctx, *position.YahooTicker, interval, fromDate, toDate)
	if len(ohlc) == 0 {
		return ChartData{}, httpx.NewValidationError("", "No data found for ticker: "+ticker)
	}

	priceSeries := make([]PricePoint, 0, len(ohlc))
	for _, p := range ohlc {
		if fromDate != nil && p.Date.Before(*fromDate) {
			continue
		}
		if toDate != nil && p.Date.After(*toDate) {
			continue
		}
		priceSeries = append(priceSeries, PricePoint{
			Date: p.Date, Open: p.Open, Close: p.Close, High: p.High, Low: p.Low,
			PriceSource: string(p.Source),
		})
	}

	filtered := trades[:0:0]
	for _, t := range trades {
		day := t.PurchasedAt.Time
		if fromDate != nil && day.Before(*fromDate) {
			continue
		}
		if toDate != nil && day.After(*toDate) {
			continue
		}
		filtered = append(filtered, t)
	}

	markers, err := s.buildMarkers(ctx, filtered, string(position.PriceCurrency), fromDate, toDate)
	if err != nil {
		return ChartData{}, err
	}

	return ChartData{
		Ticker: ticker, YahooTicker: *position.YahooTicker,
		PriceCurrency: string(position.PriceCurrency), Interval: interval,
		PriceSeries: priceSeries, Trades: markers,
	}, nil
}

// fetchOHLC ports TradeChartService.fetchOHLC's window policy: an explicit
// period1/period2 range for every interval (never range=max — see
// linearregression.Service.fetchOHLC for why). Daily defaults to 5 years, weekly and
// monthly to 15 years (comfortably covering this app's ~2-year history without ever
// touching the broken shorthand).
func (s *Service) fetchOHLC(ctx context.Context, yahooTicker, interval string, fromDate, toDate *time.Time) []linearregression.PricedOHLCPoint {
	yahooInterval := "1" + interval
	today := linearregression.WarsawTodayAt(s.now())
	defaultFrom := today.AddDate(-5, 0, 0)
	if interval != "d" {
		defaultFrom = today.AddDate(-15, 0, 0)
	}
	from := defaultFrom
	if fromDate != nil {
		from = *fromDate
	}
	to := today
	if toDate != nil {
		to = *toDate
	}
	p1 := from.Unix()
	p2 := to.Add(23*time.Hour + 59*time.Minute + 59*time.Second).Unix()
	return s.history.FetchOHLCByRange(ctx, yahooTicker, yahooInterval, p1, p2)
}

// buildMarkers ports TradeChartService.buildMarkers: one marker per trade, price_pln
// converted to the position's native priceCurrency via a nearest-date historical FX
// lookup. PLN tickers skip the FX fetch entirely; non-PLN tickers fetch one daily FX
// series bounded to the same effective window and convert every trade through it.
// Never defaults a missing rate to 1.0 — 503 instead.
func (s *Service) buildMarkers(ctx context.Context, trades []sqlcgen.ListTradesByTickerRow, priceCurrency string, fromDate, toDate *time.Time) ([]Marker, error) {
	if priceCurrency == "PLN" {
		markers := make([]Marker, 0, len(trades))
		for _, t := range trades {
			pricePln, err := pgnum.ToDecimal(t.PricePln)
			if err != nil {
				return nil, fmt.Errorf("tradechart: decode price_pln for trade %d: %w", t.ID, err)
			}
			m, err := toMarker(t, pricePln)
			if err != nil {
				return nil, err
			}
			markers = append(markers, m)
		}
		return markers, nil
	}

	fxTicker, ok := fxTickers[priceCurrency]
	if !ok {
		return nil, httpx.NewServiceUnavailableError("No FX pair configured for currency: " + priceCurrency)
	}
	today := linearregression.WarsawTodayAt(s.now())
	effectiveFrom := today.AddDate(-5, 0, 0)
	if fromDate != nil {
		effectiveFrom = *fromDate
	}
	effectiveTo := today
	if toDate != nil {
		effectiveTo = *toDate
	}
	p1 := effectiveFrom.Unix()
	p2 := effectiveTo.Add(23*time.Hour + 59*time.Minute + 59*time.Second).Unix()
	fxSeries := s.history.FetchOHLCByRange(ctx, fxTicker, "1d", p1, p2)
	if len(fxSeries) == 0 {
		return nil, httpx.NewServiceUnavailableError("FX rate history unavailable for currency: " + priceCurrency)
	}

	markers := make([]Marker, 0, len(trades))
	for _, t := range trades {
		nearest, ok := linearregression.NearestPricedByDate(fxSeries, t.PurchasedAt.Time)
		if !ok || nearest.Close <= 0 {
			return nil, httpx.NewServiceUnavailableError(
				"FX rate unavailable for currency: " + priceCurrency + " near " + t.PurchasedAt.Time.Format("2006-01-02"))
		}
		pricePln, err := pgnum.ToDecimal(t.PricePln)
		if err != nil {
			return nil, fmt.Errorf("tradechart: decode price_pln for trade %d: %w", t.ID, err)
		}
		// priceNative = pricePln / fx, scale 8 HALF_UP (BigDecimal.divide(fx, 8,
		// HALF_UP); shopspring Round is half-away, identical on these positives).
		priceNative := pricePln.Div(decimal.NewFromFloat(nearest.Close)).Round(8)
		m, err := toMarker(t, priceNative)
		if err != nil {
			return nil, err
		}
		markers = append(markers, m)
	}
	return markers, nil
}

// toMarker ports TradeChartService.toMarker 1:1 (session_id NULL -> nil sessionId).
func toMarker(t sqlcgen.ListTradesByTickerRow, priceNative decimal.Decimal) (Marker, error) {
	unitsDelta, err := pgnum.ToDecimal(t.UnitsDelta)
	if err != nil {
		return Marker{}, fmt.Errorf("tradechart: decode units_delta for trade %d: %w", t.ID, err)
	}
	pricePln, err := pgnum.ToDecimal(t.PricePln)
	if err != nil {
		return Marker{}, fmt.Errorf("tradechart: decode price_pln for trade %d: %w", t.ID, err)
	}
	totalPln, err := pgnum.ToDecimal(t.TotalPln)
	if err != nil {
		return Marker{}, fmt.Errorf("tradechart: decode total_pln for trade %d: %w", t.ID, err)
	}
	return Marker{
		ID: t.ID, PurchasedAt: t.PurchasedAt.Time,
		TradeType: string(t.TradeType), Portfolio: string(t.Portfolio),
		AccountType: string(t.AccountType),
		UnitsDelta:  unitsDelta, PricePln: pricePln, PriceNative: priceNative,
		TotalPln: totalPln, SessionID: t.SessionID,
	}, nil
}
