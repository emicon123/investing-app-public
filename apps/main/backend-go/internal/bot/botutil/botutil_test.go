package botutil

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

func testRow() sqlcgen.BotTrade {
	opened := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	return sqlcgen.BotTrade{
		ID: 1, Side: "LONG", Status: "OPEN",
		Quantity:           pgnum.FromDecimal(decimal.RequireFromString("0.01")),
		Leverage:           3,
		EntryPrice:         pgnum.FromDecimal(decimal.RequireFromString("60000")),
		OpenedAt:           pgtype.Timestamptz{Time: opened, Valid: true},
		CreatedAt:          pgtype.Timestamptz{Time: opened, Valid: true},
		ScalpSessionStatus: "NONE",
	}
}

func TestEnrichTrade_LongUnrealized(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 1, 40, 0, time.UTC) // held 100s
	current := decimal.RequireFromString("61000")
	out, err := EnrichTrade(testRow(), &current, now)
	if err != nil {
		t.Fatalf("EnrichTrade: %v", err)
	}
	if out.CurrentPrice == nil || *out.CurrentPrice != 61000 {
		t.Fatalf("expected currentPrice 61000, got %+v", out.CurrentPrice)
	}
	// (61000−60000)/60000×100 = 1.6667 (4dp).
	if out.UnrealizedPnlPct == nil || *out.UnrealizedPnlPct != 1.6667 {
		t.Fatalf("expected unrealized pct 1.6667, got %+v", out.UnrealizedPnlPct)
	}
	// 1000×0.01×3 = 30.
	if out.UnrealizedPnlPln == nil || *out.UnrealizedPnlPln != 30 {
		t.Fatalf("expected unrealized pnl 30, got %+v", out.UnrealizedPnlPln)
	}
	if out.HeldSeconds == nil || *out.HeldSeconds != 100 {
		t.Fatalf("expected held 100s, got %+v", out.HeldSeconds)
	}
	if out.TimeToHardCloseSeconds == nil || *out.TimeToHardCloseSeconds != 800 {
		t.Fatalf("expected 800s to hard close, got %+v", out.TimeToHardCloseSeconds)
	}
	if out.ScalpSessionStatus == nil || *out.ScalpSessionStatus != "NONE" {
		t.Fatalf("expected scalp NONE always emitted, got %+v", out.ScalpSessionStatus)
	}
}

func TestEnrichTrade_ShortAndMissingPrice(t *testing.T) {
	row := testRow()
	row.Side = "SHORT"
	current := decimal.RequireFromString("59000")
	out, err := EnrichTrade(row, &current, time.Now().UTC())
	if err != nil {
		t.Fatalf("EnrichTrade: %v", err)
	}
	// SHORT: (60000−59000)×0.01×3 = 30.
	if out.UnrealizedPnlPln == nil || *out.UnrealizedPnlPln != 30 {
		t.Fatalf("expected short pnl 30, got %+v", out.UnrealizedPnlPln)
	}
	// Nil price omits price-derived fields but heldSeconds survives.
	out, err = EnrichTrade(testRow(), nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("EnrichTrade: %v", err)
	}
	if out.CurrentPrice != nil || out.UnrealizedPnlPln != nil || out.UnrealizedPnlPct != nil {
		t.Fatalf("expected price fields omitted without a price, got %+v", out)
	}
	if out.HeldSeconds == nil {
		t.Fatal("expected heldSeconds computed regardless of price")
	}
}

func TestPageSize_Validation(t *testing.T) {
	p, s, err := PageSize(nil, nil, 0, 20)
	if err != nil || p != 0 || s != 20 {
		t.Fatalf("expected defaults, got %d/%d/%v", p, s, err)
	}
	neg := -1
	if _, _, err := PageSize(&neg, nil, 0, 20); err == nil {
		t.Fatal("expected 422 for negative page, got nil")
	}
	zero := 0
	if _, s, err := PageSize(nil, &zero, 0, 20); err != nil || s != 20 {
		t.Fatalf("expected size 0 to fall back to default, got %d/%v", s, err)
	}
}
