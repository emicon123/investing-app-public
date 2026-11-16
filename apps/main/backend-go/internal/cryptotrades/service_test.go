package cryptotrades

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as prior
// phases (business logic table-tested here; real Postgres covered by
// integration_test.go). crypto_trades carries no UNIQUE constraint, so no 409 path exists
// to fake (unlike tax-returns' year).
type fakeQueries struct {
	rows   []sqlcgen.CryptoTrade
	nextID int64
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{nextID: 1}
}

func (f *fakeQueries) insert(row sqlcgen.CryptoTrade) sqlcgen.CryptoTrade {
	row.ID = f.nextID
	f.nextID++
	f.rows = append(f.rows, row)
	return row
}

func (f *fakeQueries) ListCryptoTrades(_ context.Context, arg sqlcgen.ListCryptoTradesParams) ([]sqlcgen.CryptoTrade, error) {
	var out []sqlcgen.CryptoTrade
	for _, r := range f.rows {
		if arg.Ticker != nil && r.Ticker != *arg.Ticker {
			continue
		}
		if arg.Year != nil && r.TradeDate.Time.Year() != int(*arg.Year) {
			continue
		}
		if arg.Exchange != nil && r.Exchange != *arg.Exchange {
			continue
		}
		if arg.Side != nil && r.Side != *arg.Side {
			continue
		}
		out = append(out, r)
	}
	start := int(arg.PageOffset)
	if start > len(out) {
		return []sqlcgen.CryptoTrade{}, nil
	}
	end := min(start+int(arg.PageLimit), len(out))
	return out[start:end], nil
}

func (f *fakeQueries) CountCryptoTrades(_ context.Context, arg sqlcgen.CountCryptoTradesParams) (int64, error) {
	rows, _ := f.ListCryptoTrades(context.Background(), sqlcgen.ListCryptoTradesParams{
		Ticker: arg.Ticker, Year: arg.Year, Exchange: arg.Exchange, Side: arg.Side,
		PageLimit: int32(len(f.rows) + 1),
	})
	return int64(len(rows)), nil
}

func (f *fakeQueries) FindCryptoTradesByYear(_ context.Context, year int32) ([]sqlcgen.CryptoTrade, error) {
	var out []sqlcgen.CryptoTrade
	for _, r := range f.rows {
		if r.TradeDate.Time.Year() == int(year) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeQueries) InsertCryptoTrade(_ context.Context, arg sqlcgen.InsertCryptoTradeParams) (sqlcgen.CryptoTrade, error) {
	now := pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	return f.insert(sqlcgen.CryptoTrade{
		TradeDate: arg.TradeDate, Ticker: arg.Ticker, Exchange: arg.Exchange,
		AccountType: arg.AccountType, Side: arg.Side, Quantity: arg.Quantity,
		UnitPrice: arg.UnitPrice, PriceCurrency: arg.PriceCurrency,
		TotalAmount: arg.TotalAmount, AmountPln: arg.AmountPln,
		Source: "MANUAL", Notes: arg.Notes, CreatedAt: now,
	}), nil
}

func fakeRow(ticker string, year int, side sqlcgen.TradeSideEnum, amount string) sqlcgen.CryptoTrade {
	return sqlcgen.CryptoTrade{
		TradeDate: pgtype.Date{Time: time.Date(year, 6, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		Ticker:    ticker, Exchange: sqlcgen.ExchangeEnumBINANCE,
		AccountType: sqlcgen.AccountTypeEnumREGULAR, Side: side,
		Quantity:      pgnum.FromDecimal(decimal.NewFromFloat(0.01)),
		UnitPrice:     pgnum.FromDecimal(decimal.NewFromInt(65000)),
		PriceCurrency: sqlcgen.CurrencyEnumUSD,
		TotalAmount:   pgnum.FromDecimal(decimal.NewFromInt(650)),
		AmountPln:     pgnum.FromDecimal(decimal.RequireFromString(amount)),
		Source:        "MANUAL",
		CreatedAt:     pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}
}

func validInput() CreateInput {
	return CreateInput{
		TradeDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Ticker: "BTC-USD",
		Exchange: "BINANCE", AccountType: "REGULAR", Side: "BUY",
		Quantity: decimal.NewFromFloat(0.01), UnitPrice: decimal.NewFromInt(65000),
		PriceCurrency: "USD", TotalAmount: decimal.NewFromInt(650),
		AmountPln: decimal.NewFromInt(2530),
	}
}

func TestList_DefaultsAndFilters(t *testing.T) {
	fq := newFakeQueries()
	fq.insert(fakeRow("BTC-USD", 2025, sqlcgen.TradeSideEnumBUY, "2530"))
	fq.insert(fakeRow("ETH-USD", 2025, sqlcgen.TradeSideEnumSELL, "1200"))
	fq.insert(fakeRow("BTC-USD", 2024, sqlcgen.TradeSideEnumBUY, "2000"))
	svc := NewService(fq)
	ctx := context.Background()

	page, err := svc.List(ctx, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 3 || page.Number != 0 || page.Size != 50 {
		t.Fatalf("expected default page 0/50 total 3, got %+v", page)
	}

	ticker := "BTC-USD"
	page, _ = svc.List(ctx, &ticker, nil, nil, nil, nil, nil)
	if page.Total != 2 {
		t.Fatalf("ticker filter: expected 2, got %d", page.Total)
	}
	year := 2025
	page, _ = svc.List(ctx, nil, &year, nil, nil, nil, nil)
	if page.Total != 2 {
		t.Fatalf("year filter: expected 2, got %d", page.Total)
	}
	exchange := "BINANCE"
	side := "SELL"
	page, _ = svc.List(ctx, nil, nil, &exchange, &side, nil, nil)
	if page.Total != 1 || page.Items[0].Ticker != "ETH-USD" {
		t.Fatalf("exchange+side filter: got %+v", page)
	}
	// Blank ticker = no filter (Java isBlank parity).
	blank := "  "
	page, _ = svc.List(ctx, &blank, nil, nil, nil, nil, nil)
	if page.Total != 3 {
		t.Fatalf("blank ticker must not filter, got %d", page.Total)
	}
	// Pagination slices.
	p, size := 1, 2
	page, _ = svc.List(ctx, nil, nil, nil, nil, &p, &size)
	if page.Total != 3 || len(page.Items) != 1 {
		t.Fatalf("page 1 size 2: got %+v", page)
	}
	// No server-side maximum (spec fix 2026-09-15): size 5000 passes validation.
	huge := 5000
	if _, err := svc.List(ctx, nil, nil, nil, nil, nil, &huge); err != nil {
		t.Fatalf("large size must pass, got %v", err)
	}
}

func TestList_InvalidParamsAre400(t *testing.T) {
	svc := NewService(newFakeQueries())
	ctx := context.Background()

	neg, zero := -1, 0
	if _, err := svc.List(ctx, nil, nil, nil, nil, &neg, nil); !isStatus(err, 400) {
		t.Fatalf("negative page must be 400, got %v", err)
	}
	if _, err := svc.List(ctx, nil, nil, nil, nil, nil, &zero); !isStatus(err, 400) {
		t.Fatalf("zero size must be 400, got %v", err)
	}
	badEx := "KRAKEN"
	if _, err := svc.List(ctx, nil, nil, &badEx, nil, nil, nil); !isStatus(err, 400) {
		t.Fatalf("bad exchange must be 400, got %v", err)
	}
	badSide := "HOLD"
	if _, err := svc.List(ctx, nil, nil, nil, &badSide, nil, nil); !isStatus(err, 400) {
		t.Fatalf("bad side must be 400, got %v", err)
	}
}

func TestCreate_ForcesManualAndValidates(t *testing.T) {
	svc := NewService(newFakeQueries())
	ctx := context.Background()

	dto, err := svc.Create(ctx, validInput())
	if err != nil {
		t.Fatal(err)
	}
	if dto.Source != "MANUAL" {
		t.Fatalf("server must force source=MANUAL, got %q", dto.Source)
	}
	if dto.Ticker != "BTC-USD" || !dto.AmountPln.Equal(decimal.NewFromInt(2530)) {
		t.Fatalf("unexpected dto: %+v", dto)
	}

	cases := []struct {
		name   string
		mutate func(*CreateInput)
		field  string
	}{
		{"blank ticker", func(in *CreateInput) { in.Ticker = " " }, "ticker"},
		{"long ticker", func(in *CreateInput) { in.Ticker = strings.Repeat("X", 21) }, "ticker"},
		{"bad exchange", func(in *CreateInput) { in.Exchange = "KRAKEN" }, "exchange"},
		{"bad account", func(in *CreateInput) { in.AccountType = "ROTH" }, "accountType"},
		{"bad side", func(in *CreateInput) { in.Side = "HOLD" }, "side"},
		{"bad currency", func(in *CreateInput) { in.PriceCurrency = "JPY" }, "priceCurrency"},
		{"zero quantity", func(in *CreateInput) { in.Quantity = decimal.Zero }, "quantity"},
		{"zero unitPrice", func(in *CreateInput) { in.UnitPrice = decimal.Zero }, "unitPrice"},
		{"zero total", func(in *CreateInput) { in.TotalAmount = decimal.Zero }, "totalAmount"},
		{"zero pln", func(in *CreateInput) { in.AmountPln = decimal.Zero }, "amountPln"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.mutate(&in)
			_, err := svc.Create(ctx, in)
			var appErr *httpx.AppError
			ok := errors.As(err, &appErr)
			if !ok || appErr.Status != 422 || appErr.Field != tc.field {
				t.Fatalf("expected 422 on %s, got %v", tc.field, err)
			}
		})
	}
}

func TestTaxSummary_ServiceFetchesYear(t *testing.T) {
	fq := newFakeQueries()
	fq.insert(fakeRow("BTC-USD", 2025, sqlcgen.TradeSideEnumBUY, "10000"))
	fq.insert(fakeRow("BTC-USD", 2025, sqlcgen.TradeSideEnumSELL, "16000"))
	fq.insert(fakeRow("ETH-USD", 2024, sqlcgen.TradeSideEnumBUY, "9999"))
	svc := NewService(fq)

	s, err := svc.TaxSummary(context.Background(), 2025)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ByTicker) != 1 || s.ByTicker[0].Ticker != "BTC-USD" {
		t.Fatalf("expected only 2025 BTC rows, got %+v", s.ByTicker)
	}
	if !s.NetGainPln.Equal(decimal.NewFromInt(6000)) {
		t.Fatalf("net: got %s", s.NetGainPln)
	}
}

func isStatus(err error, status int) bool {
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	return ok && appErr.Status == status
}
