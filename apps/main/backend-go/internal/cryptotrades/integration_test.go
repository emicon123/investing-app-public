package cryptotrades

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/testsupport"
)

// TestCryptoTrades_RealPostgresRoundTrip exercises the crypto-trades read/write paths
// against a real postgres:17.10-alpine container, migrated by the same Flyway CLI image
// used in prod — the "never mock the database" gate. Covers: create+list round-trip with
// NUMERIC(18,8)/(18,4) precision surviving exactly, server-side source=MANUAL forcing
// (the INSERT literal, asserted on the raw row), year/ticker/exchange/side filters +
// pagination + count, and the tax-summary fixture over real rows.
func TestCryptoTrades_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `TRUNCATE crypto_trades`); err != nil {
		t.Fatal(err)
	}

	svc := NewService(sqlcgen.New(pool))
	queries := sqlcgen.New(pool)

	mustCreate := func(in CreateInput) TradeDTO {
		t.Helper()
		dto, err := svc.Create(ctx, in)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		return dto
	}

	created := mustCreate(CreateInput{
		TradeDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), Ticker: "BTC-USD",
		Exchange: "BINANCE", AccountType: "REGULAR", Side: "BUY",
		Quantity:      decimal.RequireFromString("0.015"),
		UnitPrice:     decimal.RequireFromString("65000.12345678"),
		PriceCurrency: "USD",
		TotalAmount:   decimal.RequireFromString("975.0019"),
		AmountPln:     decimal.RequireFromString("3795.0075"),
	})
	mustCreate(CreateInput{
		TradeDate: time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC), Ticker: "BTC-USD",
		Exchange: "BINANCE", AccountType: "REGULAR", Side: "SELL",
		Quantity:      decimal.RequireFromString("0.01"),
		UnitPrice:     decimal.RequireFromString("70000"),
		PriceCurrency: "USD",
		TotalAmount:   decimal.RequireFromString("700"),
		AmountPln:     decimal.RequireFromString("2800"),
	})
	mustCreate(CreateInput{
		TradeDate: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), Ticker: "ETH-USD",
		Exchange: "XTB", AccountType: "IKE", Side: "BUY",
		Quantity:      decimal.RequireFromString("0.5"),
		UnitPrice:     decimal.RequireFromString("3000"),
		PriceCurrency: "USD",
		TotalAmount:   decimal.RequireFromString("1500"),
		AmountPln:     decimal.RequireFromString("6000"),
	})

	t.Run("server forces source=MANUAL on the stored row", func(t *testing.T) {
		rows, err := queries.ListCryptoTrades(ctx, sqlcgen.ListCryptoTradesParams{
			PageLimit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Source != "MANUAL" {
				t.Fatalf("row %d: expected source MANUAL, got %q", r.ID, r.Source)
			}
		}
		if created.Source != "MANUAL" || created.ID == 0 || created.CreatedAt.IsZero() {
			t.Fatalf("unexpected created dto: %+v", created)
		}
	})

	t.Run("NUMERIC precision survives the round-trip exactly", func(t *testing.T) {
		if got := created.Quantity.String(); got != "0.015" {
			t.Fatalf("quantity: got %s", got)
		}
		if got := created.UnitPrice.String(); got != "65000.12345678" {
			t.Fatalf("unitPrice: got %s", got)
		}
		if got := created.AmountPln.String(); got != "3795.0075" {
			t.Fatalf("amountPln: got %s", got)
		}
	})

	t.Run("filters and pagination", func(t *testing.T) {
		year := 2025
		page, err := svc.List(ctx, nil, &year, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || len(page.Items) != 2 {
			t.Fatalf("year filter: got %+v", page)
		}
		ticker := "ETH-USD"
		page, err = svc.List(ctx, &ticker, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || page.Items[0].AccountType != "IKE" {
			t.Fatalf("ticker filter: got %+v", page)
		}
		exchange, side, p, size := "XTB", "BUY", 0, 1
		page, err = svc.List(ctx, nil, nil, &exchange, &side, &p, &size)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Ticker != "ETH-USD" {
			t.Fatalf("exchange+side+page: got %+v", page)
		}
	})

	t.Run("tax summary fixture over real rows", func(t *testing.T) {
		s, err := svc.TaxSummary(ctx, 2025)
		if err != nil {
			t.Fatal(err)
		}
		// BTC: buys 3795.0075, sells 2800 -> net -995.0075, belka 0.
		if len(s.ByTicker) != 1 || s.ByTicker[0].Ticker != "BTC-USD" {
			t.Fatalf("expected only BTC-USD, got %+v", s.ByTicker)
		}
		if got := s.TotalBuysPln.String(); got != "3795.0075" {
			t.Fatalf("buys: got %s", got)
		}
		if got := s.NetGainPln.String(); got != "-995.0075" {
			t.Fatalf("net: got %s", got)
		}
		if !s.EstimatedBelkaTaxPln.IsZero() {
			t.Fatalf("loss must yield zero belka, got %s", s.EstimatedBelkaTaxPln)
		}
	})
}
