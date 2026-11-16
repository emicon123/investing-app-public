package trade

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

// Protection says what the exchange side looked like when the entry is recorded (D191 §6.1). It
// selects which audit statements RecordEntry runs in its transaction.
type Protection int

const (
	// ProtectedBySL: trade OPEN, okx_algo_id set, outbox flag PENDING (the only path that arms
	// the host scalp launcher, ADR-092).
	ProtectedBySL Protection = iota + 1
	// EmergencyClosed: the stop-loss was refused and the position was flattened. Trade CLOSED at
	// the entry price (P&L 0), marker note, outbox flag NONE.
	EmergencyClosed
	// Unprotected: filled, no stop-loss, and the position could not be flattened. Trade OPEN,
	// marker note, outbox flag NONE, so no scalp session is started for it.
	Unprotected
)

// FilledEntry is one position that already exists on the exchange, ready to be recorded.
type FilledEntry struct {
	Side        string
	QuantityBtc decimal.Decimal // sizing quantity (semantics unchanged until S8)
	EntryPrice  decimal.Decimal // reference price used for the SL trigger
	Leverage    int
	OkxOrderID  string
	OkxAlgoID   string // empty unless ProtectedBySL
	Protection  Protection
	MarginPln   *decimal.Decimal
	Confidence  *decimal.Decimal
	Notes       string // cluster audit line, plus the marker line for the two unhappy paths
}

// RecordEntry writes the entry in ONE transaction: the trade row and the audit statements for its
// Protection commit together or not at all (D191 S1: a crash between the two can no longer leave a
// row whose outbox flag or algo id is missing). It runs NO guards: the position already exists on
// the exchange, and refusing to record it would only hide it. Returns the new trade id.
func (s *Service) RecordEntry(ctx context.Context, e FilledEntry) (int64, error) {
	var id int64
	err := s.transact(ctx, func(q Queries) error {
		orderID := e.OkxOrderID
		row, err := q.InsertTrade(ctx, sqlcgen.InsertTradeParams{
			Side: e.Side, Quantity: pgnum.FromDecimal(e.QuantityBtc),
			Leverage: int16(e.Leverage), EntryPrice: pgnum.FromDecimal(e.EntryPrice),
			OkxOrderID: &orderID, Notes: toNullString(e.Notes),
		})
		if err != nil {
			return fmt.Errorf("insert trade: %w", err)
		}
		id = row.ID
		audit := sqlcgen.AttachExecutionAuditParams{
			ID: id, OkxAlgoID: toNullString(e.OkxAlgoID),
			MarginAllocatedPln: pgnum.FromDecimalPtr(e.MarginPln),
			SizingConfidence:   pgnum.FromDecimalPtr(e.Confidence),
		}
		switch e.Protection {
		case ProtectedBySL:
			err = q.AttachPlacedAudit(ctx, sqlcgen.AttachPlacedAuditParams(audit))
		case EmergencyClosed:
			zero := decimal.Zero
			if _, err = q.CloseTrade(ctx, sqlcgen.CloseTradeParams{
				ID: id, ExitPrice: pgnum.FromDecimal(e.EntryPrice), PnlPln: pgnum.FromDecimal(zero),
			}); err == nil {
				err = q.AttachExecutionAudit(ctx, audit)
			}
		case Unprotected:
			err = q.AttachExecutionAudit(ctx, audit)
		default:
			err = fmt.Errorf("unknown protection %d", e.Protection)
		}
		if err != nil {
			return fmt.Errorf("attach audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("trade: record entry: %w", err)
	}
	return id, nil
}
