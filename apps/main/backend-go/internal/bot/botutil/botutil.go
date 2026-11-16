// Package botutil holds the small shared helpers used by Phase 5's six bot
// sub-packages (calendar, control, plan, signal, stats, trade) — envelope metadata,
// validation-error constructors, pagination defaults, and the trade-row enrichment
// (BotMapper.toDto port). It imports only sqlcgen/openapi/common-go, so no import
// cycle is possible; domain logic stays in the sub-packages.
package botutil

import (
	"context"
	"time"

	"github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// NewMeta mirrors httpx.NewMeta (Meta.create()) for the raw openapi response shapes
// that carry their own meta field instead of an httpx envelope.
func NewMeta() openapi.Meta {
	m := httpx.NewMeta()
	id := types.UUID(m.RequestID)
	ts := m.Timestamp
	return openapi.Meta{RequestId: &id, Timestamp: &ts}
}

// Unprocessable builds the 422 validation error docs/openapi.yaml documents for bot
// write-path failures (NOT Java's 400 — the spec is the binding contract for Go, same
// precedent as prior phases).
func Unprocessable(field, message string) *httpx.AppError {
	return &httpx.AppError{Status: 422, Code: httpx.CodeValidationError, Field: field, Message: message}
}

// Conflict builds a 409 — the direct equivalent of BotTradeService's
// ResponseStatusException(CONFLICT, ...) guards (kill switch, max-concurrent,
// duplicate window, scalp claim/finish races), which all funnel through
// GlobalExceptionHandler.handleResponseStatus as AUTH_ERROR-coded bodies. The code
// string below preserves Java's machine-readable reason prefixes
// (MAX_CONCURRENT_TRADE, DUPLICATE_TRADE_WINDOW, SCALP_*).
func Conflict(message string) *httpx.AppError {
	return &httpx.AppError{Status: 409, Code: httpx.CodeAuthError, Message: message}
}

// NotFound builds a 404 — the direct equivalent of the NoSuchElementException /
// ResponseStatusException(NOT_FOUND) paths ("Trade not found", "Signal not found",
// "No plan for ...", "No plans yet").
func NotFound(message string) *httpx.AppError {
	return httpx.NewNotFoundError(message)
}

// PageSize resolves page/size query params to concrete values. Nil takes the spec
// default; a negative value answers 422 (Java's PageRequest.of would throw on these;
// Go reports them as validation failures per the spec-is-binding precedent).
func PageSize(page, size *int, defPage, defSize int) (int, int, *httpx.AppError) {
	p, s := defPage, defSize
	if page != nil {
		p = *page
	}
	if size != nil {
		s = *size
	}
	if p < 0 {
		return 0, 0, Unprocessable("page", "must not be negative")
	}
	if s < 0 {
		return 0, 0, Unprocessable("size", "must not be negative")
	}
	if s == 0 {
		s = defSize
	}
	return p, s, nil
}

// BTCPriceSource is the narrow read-only price dependency every bot read path needs:
// the current BTC-USD price for live enrichment, and USD→PLN for the sizing audit
// column. Wired in main.go to Phase 1's marketdata.Service (the same read-only
// direction historia already uses) — zero writes outside bot_*/asset_news.
type BTCPriceSource interface {
	BTCPrice(ctx context.Context) (decimal.Decimal, error)
	FxRateToPln(ctx context.Context, currency string) (decimal.Decimal, error)
}

// EnrichTrade ports BotMapper.toDto: computed-at-read fields (currentPrice,
// unrealized PnL, heldSeconds, timeToHardCloseSeconds) over a stored row. A nil
// currentPrice omits the price-derived fields but heldSeconds is still computed —
// exactly Java's two-block shape. now is injected (tests pin it; handlers pass
// time.Now()).
func EnrichTrade(row sqlcgen.BotTrade, currentPrice *decimal.Decimal, now time.Time) (openapi.BotTrade, error) {
	quantity, err := pgnum.ToDecimal(row.Quantity)
	if err != nil {
		return openapi.BotTrade{}, err
	}
	entryPrice, err := pgnum.ToDecimal(row.EntryPrice)
	if err != nil {
		return openapi.BotTrade{}, err
	}
	out := openapi.BotTrade{
		Id:         row.ID,
		Side:       openapi.BotTradeSide(row.Side),
		Status:     openapi.BotTradeStatus(row.Status),
		Quantity:   quantity.InexactFloat64(),
		Leverage:   int(row.Leverage),
		EntryPrice: entryPrice.InexactFloat64(),
		OpenedAt:   row.OpenedAt.Time.UTC(),
		CreatedAt:  row.CreatedAt.Time.UTC(),
	}
	out.SignalId = row.SignalID
	out.CalendarEventId = row.CalendarEventID
	if row.ExitPrice.Valid {
		if v, err := pgnum.ToDecimal(row.ExitPrice); err == nil {
			f := v.InexactFloat64()
			out.ExitPrice = &f
		} else {
			return openapi.BotTrade{}, err
		}
	}
	if row.PnlPln.Valid {
		if v, err := pgnum.ToDecimal(row.PnlPln); err == nil {
			f := v.InexactFloat64()
			out.PnlPln = &f
		} else {
			return openapi.BotTrade{}, err
		}
	}
	if row.PnlPct.Valid {
		if v, err := pgnum.ToDecimal(row.PnlPct); err == nil {
			f := v.InexactFloat64()
			out.PnlPct = &f
		} else {
			return openapi.BotTrade{}, err
		}
	}
	out.OkxOrderId = row.OkxOrderID
	out.OkxAlgoId = row.OkxAlgoID
	if row.MarginAllocatedPln.Valid {
		if v, err := pgnum.ToDecimal(row.MarginAllocatedPln); err == nil {
			f := v.InexactFloat64()
			out.MarginAllocatedPln = &f
		} else {
			return openapi.BotTrade{}, err
		}
	}
	if row.SizingConfidence.Valid {
		if v, err := pgnum.ToDecimal(row.SizingConfidence); err == nil {
			f := v.InexactFloat64()
			out.SizingConfidence = &f
		} else {
			return openapi.BotTrade{}, err
		}
	}
	out.Notes = row.Notes
	if row.ClosedAt.Valid {
		t := row.ClosedAt.Time.UTC()
		out.ClosedAt = &t
	}
	if currentPrice != nil && row.Status == "OPEN" && entryPrice.Cmp(decimal.Zero) != 0 {
		cp := currentPrice.InexactFloat64()
		out.CurrentPrice = &cp
		var diff decimal.Decimal
		if row.Side == "LONG" {
			diff = currentPrice.Sub(entryPrice)
		} else {
			diff = entryPrice.Sub(*currentPrice)
		}
		// Java: diff.divide(entry, 4, HALF_UP).multiply(100). shopspring Round is
		// half-away, identical to HALF_UP except on exact .xxxx5 ties (documented
		// precedent in historia/tradechart/cryptotrades).
		pct := diff.Div(entryPrice).Mul(decimal.NewFromInt(100)).Round(4).InexactFloat64()
		out.UnrealizedPnlPct = &pct
		pnl := diff.Mul(quantity)
		if row.Leverage != 0 {
			pnl = pnl.Mul(decimal.NewFromInt(int64(row.Leverage)))
		}
		f := pnl.InexactFloat64()
		out.UnrealizedPnlPln = &f
	} else if currentPrice != nil {
		cp := currentPrice.InexactFloat64()
		out.CurrentPrice = &cp
	}
	if row.OpenedAt.Valid {
		held := int64(now.Sub(row.OpenedAt.Time).Seconds())
		if held < 0 {
			held = 0
		}
		out.HeldSeconds = &held
		remaining := int64(900 - held)
		if remaining < 0 {
			remaining = 0
		}
		out.TimeToHardCloseSeconds = &remaining
	}
	// DB is NOT NULL DEFAULT 'NONE' — Java always emits the string, so Go always
	// sets the pointer (never omitted), matching byte-for-byte.
	status := openapi.BotTradeScalpSessionStatus(row.ScalpSessionStatus)
	out.ScalpSessionStatus = &status
	if row.ScalpSessionClaimedAt.Valid {
		t := row.ScalpSessionClaimedAt.Time.UTC()
		out.ScalpSessionClaimedAt = &t
	}
	return out, nil
}
