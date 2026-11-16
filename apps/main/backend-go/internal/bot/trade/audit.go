package trade

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

// AuditQueries is the narrow write surface the durable audit trail needs.
type AuditQueries interface {
	AppendTradeNote(ctx context.Context, arg sqlcgen.AppendTradeNoteParams) error
	InsertTradeAction(ctx context.Context, arg sqlcgen.InsertTradeActionParams) (sqlcgen.BotTradeAction, error)
}

// AuditTrail is the Phase 5 audit-trail helper (doc 13 §5 smell #4): a plain struct
// with two methods — AppendNote (trade notes column) and RecordAction (durable
// bot_trade_action row) — shared by the trade service, live execution, and the
// scalp claim/finish path. Deliberately NOT a formal Command interface type: a
// func(ctx) error plus this logged, durable record is the entire requirement
// (Execute()+Audit(), never Execute()+Undo()).
type AuditTrail struct {
	queries AuditQueries
}

// NewAuditTrail wires the trail around queries.
func NewAuditTrail(queries AuditQueries) *AuditTrail { return &AuditTrail{queries: queries} }

// AppendNote appends one machine-readable line to the trade's notes column.
func (a *AuditTrail) AppendNote(ctx context.Context, tradeID int64, note string) error {
	if err := a.queries.AppendTradeNote(ctx, sqlcgen.AppendTradeNoteParams{ID: tradeID, Notes: &note}); err != nil {
		return fmt.Errorf("trade: audit note: %w", err)
	}
	return nil
}

// RecordAction persists one durable trade-action row (the SYSTEM/SCALP_* rows and
// every management decision live here — this table IS the audit trail).
func (a *AuditTrail) RecordAction(ctx context.Context, tradeID int64, actionType string, price, quantity *decimal.Decimal, reasoning, source string) error {
	if _, err := a.queries.InsertTradeAction(ctx, sqlcgen.InsertTradeActionParams{
		TradeID:    tradeID,
		ActionType: actionType,
		Price:      pgnum.FromDecimalPtr(price),
		Quantity:   pgnum.FromDecimalPtr(quantity),
		Reasoning:  toNullString(reasoning),
		Source:     source,
	}); err != nil {
		return fmt.Errorf("trade: audit action: %w", err)
	}
	return nil
}

func toNullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
