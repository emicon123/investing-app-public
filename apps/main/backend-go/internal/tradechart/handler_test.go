package tradechart

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/linearregression"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as prior
// phases (business logic table-tested here; real Postgres covered by
// integration_test.go).
type fakeQueries struct {
	aggregates []sqlcgen.AggregateTradesByTickerRow
	positions  []sqlcgen.ListPositionsForChartRow
	trades     map[string][]sqlcgen.ListTradesByTickerRow
}

func (f *fakeQueries) AggregateTradesByTicker(_ context.Context) ([]sqlcgen.AggregateTradesByTickerRow, error) {
	return f.aggregates, nil
}

func (f *fakeQueries) ListPositionsForChart(_ context.Context) ([]sqlcgen.ListPositionsForChartRow, error) {
	return f.positions, nil
}

func (f *fakeQueries) ListTradesByTicker(_ context.Context, ticker string) ([]sqlcgen.ListTradesByTickerRow, error) {
	return f.trades[ticker], nil
}

// fakeHistory returns a canned OHLC series regardless of the requested window —
// enough for the service's filtering/marker logic (price-fetch edge cases belong to
// linearregression's own tests).
type fakeHistory struct {
	points []linearregression.PricedOHLCPoint
}

func (f *fakeHistory) FetchOHLCByRange(_ context.Context, _, _ string, _, _ int64) []linearregression.PricedOHLCPoint {
	return f.points
}

func ts(y int, m time.Month, d int) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

func TestListTickers_SplitsDataAndExcluded(t *testing.T) {
	yahoo := "BTC-USD"
	fq := &fakeQueries{
		aggregates: []sqlcgen.AggregateTradesByTickerRow{
			{Ticker: "BTC", TradeCount: 24, FirstTradeAt: ts(2024, 1, 1), LastTradeAt: ts(2026, 7, 1)},
			{Ticker: "GHOST", TradeCount: 3, FirstTradeAt: ts(2024, 1, 1), LastTradeAt: ts(2024, 2, 1)},
			{Ticker: "NOYAHOO", TradeCount: 1, FirstTradeAt: ts(2024, 1, 1), LastTradeAt: ts(2024, 2, 1)},
		},
		positions: []sqlcgen.ListPositionsForChartRow{
			{ID: 1, Ticker: "BTC", Name: "Bitcoin", YahooTicker: &yahoo, PriceCurrency: sqlcgen.CurrencyEnumUSD},
			{ID: 2, Ticker: "NOYAHOO", Name: "No Yahoo", YahooTicker: nil, PriceCurrency: sqlcgen.CurrencyEnumPLN},
		},
	}
	svc := NewService(fq, &fakeHistory{})
	res, err := svc.ListTickers(context.Background())
	if err != nil {
		t.Fatalf("ListTickers: %v", err)
	}
	if len(res.Data) != 1 || res.Data[0].Ticker != "BTC" || res.Data[0].TradeCount != 24 {
		t.Fatalf("unexpected data: %+v", res.Data)
	}
	reasons := map[string]string{}
	for _, x := range res.Excluded {
		reasons[x.Ticker] = x.Reason
	}
	if reasons["GHOST"] != "NO_POSITION_MATCH" || reasons["NOYAHOO"] != "MISSING_YAHOO_TICKER" {
		t.Fatalf("unexpected excluded: %+v", res.Excluded)
	}
}

func TestGetChart_Errors(t *testing.T) {
	yahoo := "BTC-USD"
	fq := &fakeQueries{
		positions: []sqlcgen.ListPositionsForChartRow{
			{ID: 1, Ticker: "BTC", Name: "Bitcoin", YahooTicker: &yahoo, PriceCurrency: sqlcgen.CurrencyEnumPLN},
			{ID: 2, Ticker: "NOYAHOO", Name: "No Yahoo", YahooTicker: nil, PriceCurrency: sqlcgen.CurrencyEnumPLN},
		},
		trades: map[string][]sqlcgen.ListTradesByTickerRow{},
	}
	svc := NewService(fq, &fakeHistory{})
	ctx := context.Background()

	if _, err := svc.GetChart(ctx, "BTC", "bogus", nil, nil); !isStatus(err, 400) {
		t.Fatalf("expected 400 for bad interval, got %v", err)
	}
	if _, err := svc.GetChart(ctx, "UNKNOWN", "wk", nil, nil); !isStatus(err, 404) {
		t.Fatalf("expected 404 for unknown ticker, got %v", err)
	}
	if _, err := svc.GetChart(ctx, "NOYAHOO", "wk", nil, nil); !isStatus(err, 404) {
		t.Fatalf("expected 404 for ticker with no trades, got %v", err)
	}
}

func TestGetChart_PLNMarkersNeedNoFX(t *testing.T) {
	yahoo := "BTC-USD"
	fq := &fakeQueries{
		positions: []sqlcgen.ListPositionsForChartRow{
			{ID: 1, Ticker: "BTC", Name: "Bitcoin", YahooTicker: &yahoo, PriceCurrency: sqlcgen.CurrencyEnumPLN},
		},
		trades: map[string][]sqlcgen.ListTradesByTickerRow{
			"BTC": {{
				ID: 101, Portfolio: sqlcgen.PortfolioEnumMAIN, Ticker: "BTC",
				AccountType: sqlcgen.AccountTypeEnumREGULAR,
				UnitsDelta:  pgnum.FromDecimal(decimal.NewFromFloat(0.01)),
				PricePln:    pgnum.FromDecimal(decimal.NewFromInt(250000)),
				TotalPln:    pgnum.FromDecimal(decimal.NewFromInt(2500)),
				PurchasedAt: ts(2024, 1, 5), TradeType: sqlcgen.TradeTypeEnumDCABUY,
			}},
		},
	}
	day := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	hist := &fakeHistory{points: []linearregression.PricedOHLCPoint{
		{Date: day, Open: 42100, Close: 42800.5, High: 43500, Low: 41800, Source: linearregression.PriceSourceYahoo},
	}}
	svc := NewService(fq, hist)
	chart, err := svc.GetChart(context.Background(), "BTC", "wk", nil, nil)
	if err != nil {
		t.Fatalf("GetChart: %v", err)
	}
	if len(chart.PriceSeries) != 1 || chart.PriceSeries[0].Close != 42800.5 {
		t.Fatalf("unexpected price series: %+v", chart.PriceSeries)
	}
	if len(chart.Trades) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(chart.Trades))
	}
	m := chart.Trades[0]
	if m.PriceNative.Cmp(m.PricePln) != 0 {
		t.Fatalf("PLN ticker: priceNative must equal pricePln, got %s vs %s", m.PriceNative, m.PricePln)
	}
	if m.SessionID != nil {
		t.Fatalf("expected nil session for NULL session_id, got %d", *m.SessionID)
	}
}

func isStatus(err error, status int) bool {
	var appErr *httpx.AppError
	return errors.As(err, &appErr) && appErr.Status == status
}
