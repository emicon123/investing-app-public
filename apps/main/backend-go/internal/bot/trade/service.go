package trade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/botutil"
	"investing-app/backend-go/openapi"
	"investing-app/common-go/auth"
	"investing-app/common-go/pgnum"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	ListTrades(ctx context.Context, arg sqlcgen.ListTradesParams) ([]sqlcgen.BotTrade, error)
	CountTrades(ctx context.Context, arg sqlcgen.CountTradesParams) (int64, error)
	GetTradeByID(ctx context.Context, id int64) (sqlcgen.BotTrade, error)
	ExistsTradeByID(ctx context.Context, id int64) (bool, error)
	ExistsOpenTrade(ctx context.Context) (bool, error)
	ExistsTradeOpenedAfter(ctx context.Context, openedAt pgtype.Timestamptz) (bool, error)
	InsertTrade(ctx context.Context, arg sqlcgen.InsertTradeParams) (sqlcgen.BotTrade, error)
	CloseTrade(ctx context.Context, arg sqlcgen.CloseTradeParams) (sqlcgen.BotTrade, error)
	AttachExecutionAudit(ctx context.Context, arg sqlcgen.AttachExecutionAuditParams) error
	AttachPlacedAudit(ctx context.Context, arg sqlcgen.AttachPlacedAuditParams) error
	AppendTradeNote(ctx context.Context, arg sqlcgen.AppendTradeNoteParams) error
	PendingScalpTrades(ctx context.Context) ([]sqlcgen.BotTrade, error)
	ClaimPendingScalp(ctx context.Context, id int64) (int64, error)
	FinishRunningScalp(ctx context.Context, id int64) (int64, error)
	InsertTradeAction(ctx context.Context, arg sqlcgen.InsertTradeActionParams) (sqlcgen.BotTradeAction, error)
	ListTradeActions(ctx context.Context, tradeID int64) ([]sqlcgen.BotTradeAction, error)
	InsertTradeTick(ctx context.Context, arg sqlcgen.InsertTradeTickParams) (sqlcgen.BotTradeTick, error)
	ListTradeTicks(ctx context.Context, tradeID int64) ([]sqlcgen.BotTradeTick, error)
	ListTradeTicksBetween(ctx context.Context, arg sqlcgen.ListTradeTicksBetweenParams) ([]sqlcgen.BotTradeTick, error)
	ExistingSignalIDs(ctx context.Context, ids []int64) ([]int64, error)
	ExistingCalendarEventIDs(ctx context.Context, ids []int64) ([]int64, error)
}

// TransactFunc runs fn inside a single DB transaction — claim/finish (status update
// + SYSTEM action row) and multi-tick inserts commit atomically, the direct port of
// BotScalpSessionService.claim/finish and BotTradeTickService.addTicks'
// @Transactional. Production passes a pgx Begin/Commit closure; unit tests pass a
// direct call-through.
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// TradingGate is the kill-switch read surface the entry choke point needs.
type TradingGate interface {
	// TradingStatus reports (enabled, pausedReason). Any error fails closed.
	TradingStatus(ctx context.Context) (bool, *string)
}

// Service is the trade service layer: handler -> Service -> sqlc Queries. It owns the
// entry choke point every execution path funnels through (watcher-internal and POST
// /bot/trades alike), the audit attach points, and the scalp outbox protocol.
type Service struct {
	queries  Queries
	transact TransactFunc
	gate     TradingGate
	prices   botutil.BTCPriceSource
	execCfg  ExecutionConfig
	now      func() time.Time
	audit    *AuditTrail
}

// NewService wires the service (explicit constructor wiring, ADR-094 §2).
func NewService(queries Queries, transact TransactFunc, gate TradingGate, prices botutil.BTCPriceSource, execCfg ExecutionConfig) *Service {
	if transact == nil {
		transact = func(ctx context.Context, fn func(Queries) error) error { return fn(queries) }
	}
	return &Service{
		queries: queries, transact: transact, gate: gate, prices: prices,
		execCfg: execCfg, now: time.Now, audit: NewAuditTrail(queries),
	}
}

// List returns trades openedAt DESC, paged, enriched — the direct port of
// BotTradeService.list.
func (s *Service) List(ctx context.Context, status, side string, page, size int) (ListResult, error) {
	if page < 0 || size < 0 {
		return ListResult{}, botutil.Unprocessable("page", "must not be negative")
	}
	if status != "" && status != "OPEN" && status != "CLOSED" {
		return ListResult{}, botutil.Unprocessable("status", fmt.Sprintf("Unknown status: %s", status))
	}
	if side != "" && side != "LONG" && side != "SHORT" {
		return ListResult{}, botutil.Unprocessable("side", fmt.Sprintf("Unknown side: %s", side))
	}
	rows, err := s.queries.ListTrades(ctx, sqlcgen.ListTradesParams{
		Status: status, Side: side, Limit: int32(size), Offset: int32(page * size),
	})
	if err != nil {
		return ListResult{}, fmt.Errorf("trade: list: %w", err)
	}
	total, err := s.queries.CountTrades(ctx, sqlcgen.CountTradesParams{Status: status, Side: side})
	if err != nil {
		return ListResult{}, fmt.Errorf("trade: count: %w", err)
	}
	data := make([]openapi.BotTrade, 0, len(rows))
	for _, r := range rows {
		enriched, err := s.enrich(ctx, r)
		if err != nil {
			return ListResult{}, fmt.Errorf("trade: enrich %d: %w", r.ID, err)
		}
		data = append(data, enriched)
	}
	return ListResult{Data: data, Total: total, Page: page, Size: size}, nil
}

// Get returns one enriched trade — the direct port of BotTradeService.get.
// Unknown id answers 404.
func (s *Service) Get(ctx context.Context, id int64) (openapi.BotTrade, error) {
	row, err := s.queries.GetTradeByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return openapi.BotTrade{}, botutil.NotFound("Trade not found")
		}
		return openapi.BotTrade{}, fmt.Errorf("trade: get: %w", err)
	}
	return s.enrich(ctx, row)
}

// Create opens a trade — the direct port of BotTradeService.create, the single choke
// point every entry path funnels through. Guards in order: kill switch (409),
// max-1-concurrent OPEN (409 MAX_CONCURRENT_TRADE), 60s duplicate window (409
// DUPLICATE_TRADE_WINDOW, ERROR log — must never fire if netting is correct).
func (s *Service) Create(ctx context.Context, in CreateInput) (openapi.BotTrade, error) {
	if err := s.assertTradingEnabled(ctx); err != nil {
		return openapi.BotTrade{}, err
	}
	side := strings.ToUpper(strings.TrimSpace(in.Side))
	if side != "LONG" && side != "SHORT" {
		return openapi.BotTrade{}, botutil.Unprocessable("side", fmt.Sprintf("Unknown side: %s", in.Side))
	}
	if !in.QuantitySet || in.Quantity.Cmp(decimal.Zero) <= 0 {
		return openapi.BotTrade{}, botutil.Unprocessable("quantity", "must be greater than 0")
	}
	leverage := 1
	if in.Leverage != nil {
		leverage = *in.Leverage
	}
	if leverage < 1 || leverage > 10 {
		return openapi.BotTrade{}, botutil.Unprocessable("leverage", "must be between 1 and 10")
	}
	if !in.EntryPriceSet || in.EntryPrice.Cmp(decimal.Zero) <= 0 {
		return openapi.BotTrade{}, botutil.Unprocessable("entryPrice", "must be greater than 0")
	}
	if err := s.checkNoConcurrentEntry(ctx); err != nil {
		return openapi.BotTrade{}, guardAppError(err)
	}
	signalID, err := s.resolveOptionalFK(ctx, "signal", in.SignalID, s.queries.ExistingSignalIDs)
	if err != nil {
		return openapi.BotTrade{}, err
	}
	calendarEventID, err := s.resolveOptionalFK(ctx, "calendarEvent", in.CalendarEventID, s.queries.ExistingCalendarEventIDs)
	if err != nil {
		return openapi.BotTrade{}, err
	}
	row, err := s.queries.InsertTrade(ctx, sqlcgen.InsertTradeParams{
		SignalID: signalID, CalendarEventID: calendarEventID,
		Side: side, Quantity: pgnum.FromDecimal(in.Quantity),
		Leverage: int16(leverage), EntryPrice: pgnum.FromDecimal(in.EntryPrice),
		OkxOrderID: in.OkxOrderID, Notes: in.Notes,
	})
	if err != nil {
		return openapi.BotTrade{}, fmt.Errorf("trade: create: %w", err)
	}
	return s.enrich(ctx, row)
}

// GuardKind says which entry guard refused (ADR-114: the guards run before the OKX order).
type GuardKind int

const (
	GuardKillSwitch GuardKind = iota + 1
	GuardOpenTrade
	GuardDuplicateWindow
)

// EntryGuardError is a guard refusal. It wraps the same 409 error Create has always returned, so
// errors.As(err, **httpx.AppError) and the wire body are unchanged; Kind lets live execution
// tell the three refusals apart without parsing messages.
type EntryGuardError struct {
	Kind GuardKind
	Err  error
}

func (e *EntryGuardError) Error() string { return e.Err.Error() }
func (e *EntryGuardError) Unwrap() error { return e.Err }

// guardAppError returns the plain 409 for a refusal (what Create returned before the guards were
// extracted), and any other error (a DB failure) unchanged.
func guardAppError(err error) error {
	if ge, ok := errors.AsType[*EntryGuardError](err); ok {
		return ge.Err
	}
	return err
}

// CheckEntryGuards is the guard half of Create, callable before any exchange order exists:
// kill switch on, no OPEN trade, no trade opened inside the duplicate window. It answers the same
// 409s as Create (wrapped in *EntryGuardError for refusals); a DB failure comes back as a plain
// error. Live execution calls it in the prepare phase, because refusing AFTER the fill would
// orphan a filled position (D191 N1).
func (s *Service) CheckEntryGuards(ctx context.Context) error {
	if err := s.assertTradingEnabled(ctx); err != nil {
		return &EntryGuardError{Kind: GuardKillSwitch, Err: err}
	}
	return s.checkNoConcurrentEntry(ctx)
}

// checkNoConcurrentEntry is the max-1-OPEN and duplicate-window half of the guards.
func (s *Service) checkNoConcurrentEntry(ctx context.Context) error {
	open, err := s.queries.ExistsOpenTrade(ctx)
	if err != nil {
		return fmt.Errorf("trade: open guard: %w", err)
	}
	if open {
		return &EntryGuardError{Kind: GuardOpenTrade,
			Err: botutil.Conflict("MAX_CONCURRENT_TRADE: an OPEN trade already exists")}
	}
	guardCutoff := s.now().Add(-time.Duration(s.execCfg.DuplicateGuardSeconds) * time.Second)
	dupe, err := s.queries.ExistsTradeOpenedAfter(ctx, pgtype.Timestamptz{Time: guardCutoff, Valid: true})
	if err != nil {
		return fmt.Errorf("trade: duplicate guard: %w", err)
	}
	if dupe {
		slog.Error("trade: DUPLICATE_TRADE_WINDOW blocked a new entry",
			"guardSeconds", s.execCfg.DuplicateGuardSeconds)
		return &EntryGuardError{Kind: GuardDuplicateWindow,
			Err: botutil.Conflict(fmt.Sprintf("DUPLICATE_TRADE_WINDOW: a trade was opened within the last %ds",
				s.execCfg.DuplicateGuardSeconds))}
	}
	return nil
}

// openTradeID returns the id of the newest OPEN trade, for the "bot blocked" alert only.
func (s *Service) openTradeID(ctx context.Context) (int64, bool) {
	rows, err := s.queries.ListTrades(ctx, sqlcgen.ListTradesParams{Status: "OPEN", Limit: 1})
	if err != nil || len(rows) == 0 {
		return 0, false
	}
	return rows[0].ID, true
}

// assertTradingEnabled ports BotTradeService.assertTradingEnabled: a paused (or
// missing — fail-closed) kill switch blocks entry with 409. Managing or closing an
// open position intentionally never calls this.
func (s *Service) assertTradingEnabled(ctx context.Context) error {
	if s.gate == nil {
		return botutil.Conflict("Bot trading is paused")
	}
	enabled, reason := s.gate.TradingStatus(ctx)
	if !enabled {
		msg := "Bot trading is paused"
		if reason != nil && strings.TrimSpace(*reason) != "" {
			msg += ": " + strings.TrimSpace(*reason)
		}
		return botutil.Conflict(msg)
	}
	return nil
}

// resolveOptionalFK ports Java's findById-or-null tolerance: unknown ids degrade to
// null instead of tripping the FK constraint.
func (s *Service) resolveOptionalFK(ctx context.Context, _ string, id *int64, exists func(context.Context, []int64) ([]int64, error)) (*int64, error) {
	if id == nil {
		return nil, nil
	}
	found, err := exists(ctx, []int64{*id})
	if err != nil {
		return nil, fmt.Errorf("trade: resolve reference: %w", err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return id, nil
}

// AttachPlacedAudit persists the PLACED-path audit AND flips the post-trade outbox
// flag to PENDING in the same statement — the direct port of
// BotTradeService.attachPlacedAudit. This is the ONLY path that ever sets PENDING
// (create leaves NONE), so a crash between fill-persist and this update fail-closes
// to no-session.
func (s *Service) AttachPlacedAudit(ctx context.Context, tradeID int64, okxAlgoID string, marginPln, confidence *decimal.Decimal) error {
	return s.queries.AttachPlacedAudit(ctx, sqlcgen.AttachPlacedAuditParams{
		ID: tradeID, OkxAlgoID: toNullString(okxAlgoID),
		MarginAllocatedPln: pgnum.FromDecimalPtr(marginPln),
		SizingConfidence:   pgnum.FromDecimalPtr(confidence),
	})
}

// AttachExecutionAudit persists the execution audit without touching the outbox flag
// — the direct port of BotTradeService.attachExecutionAudit (emergency-close path
// keeps NONE).
func (s *Service) AttachExecutionAudit(ctx context.Context, tradeID int64, okxAlgoID string, marginPln, confidence *decimal.Decimal) error {
	return s.queries.AttachExecutionAudit(ctx, sqlcgen.AttachExecutionAuditParams{
		ID: tradeID, OkxAlgoID: toNullString(okxAlgoID),
		MarginAllocatedPln: pgnum.FromDecimalPtr(marginPln),
		SizingConfidence:   pgnum.FromDecimalPtr(confidence),
	})
}

// AppendAuditNote appends one machine-readable line to the trade notes.
func (s *Service) AppendAuditNote(ctx context.Context, tradeID int64, note string) error {
	return s.audit.AppendNote(ctx, tradeID, note)
}

// Close closes a trade — the direct port of BotTradeService.close. Never
// kill-switch-gated (exiting risk is always allowed). Unknown id → 404, already
// closed → 409. Omitted pnl_pln computes as ±(exit−entry)×quantity×leverage
// pre-persist (pre-existing quote-currency labeling debt, ADR-090 §5, ported as-is).
func (s *Service) Close(ctx context.Context, id int64, in CloseInput) (openapi.BotTrade, error) {
	if !in.ExitPriceSet || in.ExitPrice.Cmp(decimal.Zero) <= 0 {
		return openapi.BotTrade{}, botutil.Unprocessable("exitPrice", "must be greater than 0")
	}
	existing, err := s.queries.GetTradeByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return openapi.BotTrade{}, botutil.NotFound("Trade not found")
		}
		return openapi.BotTrade{}, fmt.Errorf("trade: close lookup: %w", err)
	}
	if existing.Status == "CLOSED" {
		return openapi.BotTrade{}, botutil.Conflict("Already closed")
	}
	pnlPln := in.PnlPln
	if pnlPln == nil {
		pnlPln = computeClosePnl(existing, in.ExitPrice)
	}
	row, err := s.queries.CloseTrade(ctx, sqlcgen.CloseTradeParams{
		ID: id, ExitPrice: pgnum.FromDecimal(in.ExitPrice),
		PnlPln: pgnum.FromDecimalPtr(pnlPln), PnlPct: pgnum.FromDecimalPtr(in.PnlPct),
	})
	if err != nil {
		if isNoRows(err) {
			// Lost a concurrent close race: the row flipped since the read.
			return openapi.BotTrade{}, botutil.Conflict("Already closed")
		}
		return openapi.BotTrade{}, fmt.Errorf("trade: close: %w", err)
	}
	return s.enrich(ctx, row)
}

// computeClosePnl ports close()'s omitted-pnl math. (Pre-existing Java debt, ADR-090
// §5 — quote-currency figure labeled pnl_pln with no FX leg — ported as-is, never
// spread into sizing.)
func computeClosePnl(row sqlcgen.BotTrade, exitPrice decimal.Decimal) *decimal.Decimal {
	entry, err := pgnum.ToDecimal(row.EntryPrice)
	if err != nil {
		return nil
	}
	qty, err := pgnum.ToDecimal(row.Quantity)
	if err != nil {
		return nil
	}
	var diff decimal.Decimal
	if row.Side == "LONG" {
		diff = exitPrice.Sub(entry)
	} else {
		diff = entry.Sub(exitPrice)
	}
	pnl := diff.Mul(qty)
	if row.Leverage != 0 {
		pnl = pnl.Mul(decimal.NewFromInt(int64(row.Leverage)))
	}
	return &pnl
}

// AddAction appends a management action — the direct port of
// BotTradeActionService.create. Unknown trade → 404, unknown actionType/source →
// 422, omitted source defaults to BOT. Never kill-switch-gated.
func (s *Service) AddAction(ctx context.Context, tradeID int64, in ActionInput) (sqlcgen.BotTradeAction, error) {
	exists, err := s.queries.ExistsTradeByID(ctx, tradeID)
	if err != nil {
		return sqlcgen.BotTradeAction{}, fmt.Errorf("trade: action trade lookup: %w", err)
	}
	if !exists {
		return sqlcgen.BotTradeAction{}, botutil.NotFound("Trade not found")
	}
	actionType := strings.ToUpper(strings.TrimSpace(in.ActionType))
	if !ValidActionTypes[actionType] {
		return sqlcgen.BotTradeAction{}, botutil.Unprocessable("actionType",
			fmt.Sprintf("Unknown actionType: %s", in.ActionType))
	}
	source := "BOT"
	if in.Source != nil && strings.TrimSpace(*in.Source) != "" {
		source = strings.ToUpper(strings.TrimSpace(*in.Source))
		if !ValidActionSources[source] {
			return sqlcgen.BotTradeAction{}, botutil.Unprocessable("source",
				fmt.Sprintf("Unknown source: %s", *in.Source))
		}
	}
	var reasoning *string
	if in.Reasoning != nil && strings.TrimSpace(*in.Reasoning) != "" {
		reasoning = in.Reasoning
	}
	row, err := s.queries.InsertTradeAction(ctx, sqlcgen.InsertTradeActionParams{
		TradeID: tradeID, ActionType: actionType,
		Price: pgnum.FromDecimalPtr(in.Price), Quantity: pgnum.FromDecimalPtr(in.Quantity),
		Reasoning: reasoning, Source: source,
	})
	if err != nil {
		return sqlcgen.BotTradeAction{}, fmt.Errorf("trade: add action: %w", err)
	}
	return row, nil
}

// ListActions returns a trade's action timeline, oldest first — the direct port of
// BotTradeActionService.list. Unknown trade → 404.
func (s *Service) ListActions(ctx context.Context, tradeID int64) ([]sqlcgen.BotTradeAction, error) {
	exists, err := s.queries.ExistsTradeByID(ctx, tradeID)
	if err != nil {
		return nil, fmt.Errorf("trade: actions trade lookup: %w", err)
	}
	if !exists {
		return nil, botutil.NotFound("Trade not found")
	}
	rows, err := s.queries.ListTradeActions(ctx, tradeID)
	if err != nil {
		return nil, fmt.Errorf("trade: list actions: %w", err)
	}
	if rows == nil {
		rows = []sqlcgen.BotTradeAction{}
	}
	return rows, nil
}

// AddTicks appends scalp ticks — the direct port of BotTradeTickService.addTicks.
// Empty batch answers with an empty list (still 201 at the handler). Omitted price
// falls back to the trade's entryPrice. Unknown trade → 404.
func (s *Service) AddTicks(ctx context.Context, tradeID int64, ticks []TickInput) ([]sqlcgen.BotTradeTick, error) {
	trade, err := s.queries.GetTradeByID(ctx, tradeID)
	if err != nil {
		if isNoRows(err) {
			return nil, botutil.NotFound("Trade not found")
		}
		return nil, fmt.Errorf("trade: ticks trade lookup: %w", err)
	}
	if len(ticks) == 0 {
		return []sqlcgen.BotTradeTick{}, nil
	}
	entryPrice, err := pgnum.ToDecimal(trade.EntryPrice)
	if err != nil {
		return nil, fmt.Errorf("trade: decode entry price: %w", err)
	}
	out := make([]sqlcgen.BotTradeTick, 0, len(ticks))
	err = s.transact(ctx, func(q Queries) error {
		for _, t := range ticks {
			price := entryPrice
			if t.Price != nil {
				price = *t.Price
			}
			row, err := q.InsertTradeTick(ctx, sqlcgen.InsertTradeTickParams{
				TradeID: tradeID, Price: pgnum.FromDecimal(price),
				Volume: pgnum.FromDecimalPtr(t.Volume), Cvd: pgnum.FromDecimalPtr(t.Cvd),
				Atr1m: pgnum.FromDecimalPtr(t.Atr1m), ObImbalance: pgnum.FromDecimalPtr(t.ObImbalance),
				FundingRate:    pgnum.FromDecimalPtr(t.FundingRate),
				LongShortRatio: pgnum.FromDecimalPtr(t.LongShortRatio),
				OpenInterest:   pgnum.FromDecimalPtr(t.OpenInterest),
			})
			if err != nil {
				return err
			}
			out = append(out, row)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("trade: add ticks: %w", err)
	}
	return out, nil
}

// ListTicks returns a trade's ticks, oldest first — the direct port of
// BotTradeTickService.list. The range applies only when BOTH from and to are
// present. Unknown trade → 404.
func (s *Service) ListTicks(ctx context.Context, tradeID int64, from, to *time.Time) ([]sqlcgen.BotTradeTick, error) {
	exists, err := s.queries.ExistsTradeByID(ctx, tradeID)
	if err != nil {
		return nil, fmt.Errorf("trade: ticks trade lookup: %w", err)
	}
	if !exists {
		return nil, botutil.NotFound("Trade not found")
	}
	var rows []sqlcgen.BotTradeTick
	if from != nil && to != nil {
		rows, err = s.queries.ListTradeTicksBetween(ctx, sqlcgen.ListTradeTicksBetweenParams{
			TradeID:     tradeID,
			CreatedAt:   pgtype.Timestamptz{Time: *from, Valid: true},
			CreatedAt_2: pgtype.Timestamptz{Time: *to, Valid: true},
		})
	} else {
		rows, err = s.queries.ListTradeTicks(ctx, tradeID)
	}
	if err != nil {
		return nil, fmt.Errorf("trade: list ticks: %w", err)
	}
	if rows == nil {
		rows = []sqlcgen.BotTradeTick{}
	}
	return rows, nil
}

// PendingScalp returns PENDING outbox rows, oldest first — the direct port of
// BotScalpSessionService.listPending. Intentionally ignores trading_enabled.
func (s *Service) PendingScalp(ctx context.Context) ([]openapi.BotTrade, error) {
	rows, err := s.queries.PendingScalpTrades(ctx)
	if err != nil {
		return nil, fmt.Errorf("trade: pending scalp: %w", err)
	}
	out := make([]openapi.BotTrade, 0, len(rows))
	for _, r := range rows {
		enriched, err := s.enrich(ctx, r)
		if err != nil {
			return nil, fmt.Errorf("trade: enrich pending %d: %w", r.ID, err)
		}
		out = append(out, enriched)
	}
	return out, nil
}

// Claim atomically claims a PENDING scalp row (PENDING→RUNNING + SYSTEM/
// SCALP_SESSION_STARTED action row in one transaction) — the direct port of
// BotScalpSessionService.claim. 409 when the row is not PENDING (someone else won —
// not an error to the launcher); 404 when the trade does not exist. RUNNING-stuck
// rows are never auto-reclaimed (fail-closed). Never kill-switch-gated.
func (s *Service) Claim(ctx context.Context, tradeID int64) (openapi.BotTrade, error) {
	var row sqlcgen.BotTrade
	err := s.transact(ctx, func(q Queries) error {
		moved, err := q.ClaimPendingScalp(ctx, tradeID)
		if err != nil {
			return err
		}
		if moved == 0 {
			return errNotPending{}
		}
		r, err := q.GetTradeByID(ctx, tradeID)
		if err != nil {
			return err
		}
		row = r
		trail := NewAuditTrail(q)
		return trail.RecordAction(ctx, tradeID, "SCALP_SESSION_STARTED", nil, nil,
			"SCALP_SESSION_STARTED launcher="+launcherIdentity(ctx), "SYSTEM")
	})
	if err != nil {
		if _, ok := err.(errNotPending); ok {
			return s.claimConflict(ctx, tradeID)
		}
		return openapi.BotTrade{}, fmt.Errorf("trade: claim: %w", err)
	}
	return s.enrich(ctx, row)
}

// Finish moves a RUNNING scalp row to DONE (+ SYSTEM/SCALP_SESSION_FINISHED row
// carrying the exit code) — the direct port of BotScalpSessionService.finish.
// Anything else → 409; missing exitCode records "unknown", the transition still
// happens. Never kill-switch-gated.
func (s *Service) Finish(ctx context.Context, tradeID int64, exitCode *int) (openapi.BotTrade, error) {
	code := "unknown"
	if exitCode != nil {
		code = fmt.Sprintf("%d", *exitCode)
	}
	var row sqlcgen.BotTrade
	err := s.transact(ctx, func(q Queries) error {
		moved, err := q.FinishRunningScalp(ctx, tradeID)
		if err != nil {
			return err
		}
		if moved == 0 {
			return errNotRunning{}
		}
		r, err := q.GetTradeByID(ctx, tradeID)
		if err != nil {
			return err
		}
		row = r
		trail := NewAuditTrail(q)
		return trail.RecordAction(ctx, tradeID, "SCALP_SESSION_FINISHED", nil, nil,
			"SCALP_SESSION_FINISHED exitCode="+code+" launcher="+launcherIdentity(ctx), "SYSTEM")
	})
	if err != nil {
		if _, ok := err.(errNotRunning); ok {
			return s.finishConflict(ctx, tradeID)
		}
		return openapi.BotTrade{}, fmt.Errorf("trade: finish: %w", err)
	}
	return s.enrich(ctx, row)
}

// errNotPending / errNotRunning are transaction-internal sentinels: the rowcount said
// "no transition", and the 404-vs-409 split needs a follow-up read OUTSIDE the
// rolled-back transaction.
type errNotPending struct{}
type errNotRunning struct{}

func (errNotPending) Error() string { return "not pending" }
func (errNotRunning) Error() string { return "not running" }

func (s *Service) claimConflict(ctx context.Context, tradeID int64) (openapi.BotTrade, error) {
	_, err := s.queries.GetTradeByID(ctx, tradeID)
	if err != nil {
		if isNoRows(err) {
			return openapi.BotTrade{}, botutil.NotFound("Trade not found")
		}
		return openapi.BotTrade{}, fmt.Errorf("trade: claim lookup: %w", err)
	}
	return openapi.BotTrade{}, botutil.Conflict(
		fmt.Sprintf("SCALP_ALREADY_CLAIMED: trade %d is not PENDING", tradeID))
}

func (s *Service) finishConflict(ctx context.Context, tradeID int64) (openapi.BotTrade, error) {
	_, err := s.queries.GetTradeByID(ctx, tradeID)
	if err != nil {
		if isNoRows(err) {
			return openapi.BotTrade{}, botutil.NotFound("Trade not found")
		}
		return openapi.BotTrade{}, fmt.Errorf("trade: finish lookup: %w", err)
	}
	return openapi.BotTrade{}, botutil.Conflict(
		fmt.Sprintf("SCALP_NOT_RUNNING: trade %d is not RUNNING", tradeID))
}

// launcherIdentity ports BotScalpSessionService.launcherIdentity: the machine
// principal that claimed/finished the session (the service-token identity when the
// auth middleware recorded one), defaulting to "scalp-launcher".
func launcherIdentity(ctx context.Context) string {
	if principal, ok := auth.PrincipalFromContext(ctx); ok && principal != "" {
		return principal
	}
	return "scalp-launcher"
}

func (s *Service) enrich(ctx context.Context, row sqlcgen.BotTrade) (openapi.BotTrade, error) {
	return botutil.EnrichTrade(row, s.currentBTC(ctx), s.now())
}

// currentBTC reads the live BTC-USD price for enrichment; nil when unavailable
// (Java's currentBtcPrice fail-soft — enrichment fields are simply omitted).
func (s *Service) currentBTC(ctx context.Context) *decimal.Decimal {
	if s.prices == nil {
		return nil
	}
	price, err := s.prices.BTCPrice(ctx)
	if err != nil {
		return nil
	}
	return &price
}

func isNoRows(err error) bool {
	return err != nil && (err == pgx.ErrNoRows || strings.Contains(err.Error(), "no rows"))
}
