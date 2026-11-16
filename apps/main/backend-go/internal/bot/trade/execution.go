package trade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/bot/botutil"
)

// Outcome is the live-execution result. SKIPPED_FLAG_DISABLED is Go-only (Java has
// no flag): the BOT_LIVE_EXECUTION_ENABLED gate refused before any OKX call.
type Outcome string

const (
	OutcomePlaced              Outcome = "PLACED"
	OutcomeSkippedPaused       Outcome = "SKIPPED_PAUSED"
	OutcomeSkipped             Outcome = "SKIPPED"
	OutcomeSkippedFlagDisabled Outcome = "SKIPPED_FLAG_DISABLED"
	OutcomeFailed              Outcome = "FAILED"
	OutcomeEmergencyClosed     Outcome = "EMERGENCY_CLOSED"
	// OutcomePlacedUnrecorded: filled and protected on OKX, but the DB row could not be written.
	OutcomePlacedUnrecorded Outcome = "PLACED_UNRECORDED"
	// OutcomeUnprotected: filled, no stop-loss, and the position could not be flattened.
	OutcomeUnprotected Outcome = "UNPROTECTED"
	// OutcomeUnknown: the commit phase panicked, so what exists on the exchange is not known.
	OutcomeUnknown Outcome = "UNKNOWN"
)

// ExecutionResult pairs the outcome with the persisted trade id (nil unless a trade row was
// written) and whether a position was opened on OKX. The watcher marks the contributing signals
// executed iff Filled.
type ExecutionResult struct {
	Outcome Outcome
	TradeID *int64
	Filled  bool
}

// Decision is one net cluster order (Parameter Object, D191 §6.1): what the watcher decided, not
// yet how big it is.
type Decision struct {
	ClusterTime  time.Time // cluster event time (UTC); the source of the deterministic clOrdId (S3)
	Side         string    // LONG | SHORT
	Leverage     int       // net, 1..10
	Confidence   *decimal.Decimal
	Contributors []int64
	Audit        string
}

// Budgets of the commit phase (D191 §5.2): 45 s for entry + protect + record, then a separate
// 10 s for the alert. 45 + 10 < the 60 s lifecycle WorkerTimeout < the 90 s compose grace period.
const (
	commitBudget = 45 * time.Second
	alertBudget  = 10 * time.Second
)

// recordBackoff is the pause before each retry of RecordEntry: 3 attempts in all.
var recordBackoff = []time.Duration{500 * time.Millisecond, time.Second}

// entryPlan is a Decision plus its sizing, everything the commit phase needs (value object).
type entryPlan struct {
	Decision  Decision
	InstID    string
	EntrySide string // OKX side of the entry: buy | sell
	CloseSide string // OKX side of the stop-loss / close: sell | buy
	Contracts string // OKX sz
	Quantity  decimal.Decimal
	Price     decimal.Decimal // reference price: entry price on the books and the SL trigger base
	Trigger   decimal.Decimal
	Leverage  int
	MarginPln *decimal.Decimal
}

// ExecOption configures optional collaborators of LiveExecutionService. Variadic on the
// constructor, so existing call sites keep compiling.
type ExecOption func(*LiveExecutionService)

// WithNotifier wires the alert port (nil keeps alerts log-only).
func WithNotifier(n Notifier) ExecOption {
	return func(e *LiveExecutionService) { e.notifier = n }
}

// LiveExecutionService is the Go port of LiveExecutionService: net cluster decisions
// become real OKX orders. OKX call sites exist ONLY here (via the OKXClient
// interface) — every other trade path is tracking-only.
//
// SAFETY GATE (Architect binding, first instruction of Execute): when
// BOT_LIVE_EXECUTION_ENABLED is not "true", Execute returns SKIPPED_FLAG_DISABLED
// with ZERO OKX calls (the client is never touched — it may even be nil) and a warn
// log carrying action/leverage/confidence. Mode matrix: OFF → never any order;
// ON + OKX_DEMO_MODE=true → demo orders (x-simulated-trading:1); ON + false → live
// orders (explicit go-ahead only, after shadow/paper burn-in).
type LiveExecutionService struct {
	gate        TradingGate
	okx         OKXClient
	trades      *Service
	prices      botutil.BTCPriceSource
	cfg         ExecutionConfig
	liveEnabled func() bool
	notifier    Notifier

	// Tunables fixed in production, overridable by in-package tests.
	commitBudget  time.Duration
	alertBudget   time.Duration
	recordBackoff []time.Duration
}

// NewLiveExecutionService wires the service. liveEnabled is injected (production:
// LiveExecutionFlag reading BOT_LIVE_EXECUTION_ENABLED; tests: a fixed func) so the
// flag-off test needs no env manipulation.
func NewLiveExecutionService(gate TradingGate, okx OKXClient, trades *Service, prices botutil.BTCPriceSource, cfg ExecutionConfig, liveEnabled func() bool, opts ...ExecOption) *LiveExecutionService {
	if liveEnabled == nil {
		liveEnabled = LiveExecutionFlag
	}
	e := &LiveExecutionService{
		gate: gate, okx: okx, trades: trades, prices: prices,
		cfg: cfg, liveEnabled: liveEnabled,
		commitBudget: commitBudget, alertBudget: alertBudget, recordBackoff: recordBackoff,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute turns one net cluster decision into a protected OKX position in two phases (ADR-114).
//
// PREPARE (cancellable, so no order is placed if it fails or shutdown starts): flag, kill switch,
// entry guards (OPEN trade, duplicate window), price, equity, sizing, leverage, contract size.
// POINT OF NO RETURN: the entry order is sent. COMMIT (runs to completion on a context detached
// from shutdown, with its own 45 s budget): entry, stop-loss, one-transaction record, alert. A
// position that exists is protected BEFORE anything touches the DB, and an unrecorded or
// unprotected position is always alerted.
func (e *LiveExecutionService) Execute(ctx context.Context, d Decision) ExecutionResult {
	if !e.liveEnabled() {
		var conf string
		if d.Confidence != nil {
			conf = d.Confidence.String()
		}
		slog.Warn("live execution blocked: BOT_LIVE_EXECUTION_ENABLED != true (shadow mode); zero OKX calls made",
			"side", d.Side, "leverage", d.Leverage, "confidence", conf)
		return ExecutionResult{Outcome: OutcomeSkippedFlagDisabled}
	}
	plan, res, ok := e.prepare(ctx, d)
	if !ok {
		return res
	}
	return e.commit(ctx, plan)
}

// prepare runs everything before the point of no return. ok=false carries the final result.
func (e *LiveExecutionService) prepare(ctx context.Context, d Decision) (entryPlan, ExecutionResult, bool) {
	if d.Side != "LONG" && d.Side != "SHORT" {
		return entryPlan{}, ExecutionResult{Outcome: OutcomeSkipped}, false
	}
	if e.gate != nil {
		if enabled, _ := e.gate.TradingStatus(ctx); !enabled {
			slog.Warn("live execution skipped: kill switch paused")
			return entryPlan{}, ExecutionResult{Outcome: OutcomeSkippedPaused}, false
		}
	}
	if res, blocked := e.checkGuards(ctx); blocked {
		return entryPlan{}, res, false
	}
	entryPrice, err := e.prices.BTCPrice(ctx)
	if err != nil {
		slog.Error("live execution failed: no BTC price available", "error", err)
		return entryPlan{}, e.prepareFailed(ctx), false
	}
	equity, err := e.okx.GetEquityUsdt()
	if err != nil {
		if okxErr, ok := errors.AsType[*OkxClientException](err); ok {
			slog.Error("live execution failed: equity lookup", "code", okxErr.Code, "error", okxErr.Message)
		} else {
			slog.Error("live execution failed: equity lookup", "error", err)
		}
		return entryPlan{}, e.prepareFailed(ctx), false
	}
	var confVal decimal.Decimal
	confSet := d.Confidence != nil
	if confSet {
		confVal = *d.Confidence
	}
	sizing, sized := Size(e.cfg, equity, confVal, confSet, d.Leverage, entryPrice)
	if !sized {
		slog.Warn("live execution skipped: sizing vetoed",
			"equity", equity.String(), "confidence", confVal.String(), "leverage", d.Leverage)
		return entryPlan{}, ExecutionResult{Outcome: OutcomeSkipped}, false
	}
	marginPln := e.toPln(ctx, sizing.MarginUsdt)
	if err := e.okx.SetLeverage(BTCUSDollarSwap, sizing.AppliedLeverage); err != nil {
		slog.Error("live execution failed: set-leverage", "error", err)
		return entryPlan{}, e.prepareFailed(ctx), false
	}
	ctVal, err := e.okx.GetContractValue(BTCUSDollarSwap)
	if err != nil {
		slog.Error("live execution failed: ctVal lookup", "error", err)
		return entryPlan{}, e.prepareFailed(ctx), false
	}
	sz := contractsFor(sizing.QuantityBtc, ctVal, entryPrice)
	if sz == "" {
		slog.Warn("live execution skipped: quantity converts to zero contracts",
			"quantityBtc", sizing.QuantityBtc.String())
		return entryPlan{}, ExecutionResult{Outcome: OutcomeSkipped}, false
	}
	if ctx.Err() != nil {
		slog.Warn("live execution skipped: shutdown began before the entry order; nothing was placed")
		return entryPlan{}, ExecutionResult{Outcome: OutcomeSkipped}, false
	}
	one := decimal.NewFromInt(1)
	p := entryPlan{
		Decision: d, InstID: BTCUSDollarSwap, EntrySide: "buy", CloseSide: "sell", Contracts: sz,
		Quantity: sizing.QuantityBtc, Price: entryPrice, Leverage: sizing.AppliedLeverage,
		MarginPln: marginPln,
		Trigger:   entryPrice.Mul(one.Sub(e.cfg.StopDistancePct)),
	}
	if d.Side == "SHORT" {
		p.EntrySide, p.CloseSide = "sell", "buy"
		p.Trigger = entryPrice.Mul(one.Add(e.cfg.StopDistancePct))
	}
	p.Trigger = p.Trigger.Round(2)
	return p, ExecutionResult{}, true
}

// prepareFailed is FAILED, or SKIPPED when the failure is only shutdown cancelling the context.
func (e *LiveExecutionService) prepareFailed(ctx context.Context) ExecutionResult {
	if ctx.Err() != nil {
		return ExecutionResult{Outcome: OutcomeSkipped}
	}
	return ExecutionResult{Outcome: OutcomeFailed}
}

// checkGuards runs the entry guards BEFORE any order (D191 N1). blocked=true carries the result.
func (e *LiveExecutionService) checkGuards(ctx context.Context) (ExecutionResult, bool) {
	err := e.trades.CheckEntryGuards(ctx)
	if err == nil {
		return ExecutionResult{}, false
	}
	guard, refused := errors.AsType[*EntryGuardError](err)
	if !refused {
		slog.Error("live execution failed: entry guard lookup", "error", err)
		return e.prepareFailed(ctx), true
	}
	switch guard.Kind {
	case GuardKillSwitch:
		slog.Warn("live execution skipped: kill switch paused", "reason", guard.Error())
		return ExecutionResult{Outcome: OutcomeSkippedPaused}, true
	case GuardOpenTrade:
		slog.Warn("live execution skipped: an OPEN bot_trade row blocks entry; close it by hand once the position is gone")
		if id, ok := e.trades.openTradeID(ctx); ok {
			e.alert(ctx, blockedOpenTradeAlert(id))
		}
	default:
		slog.Error("live execution skipped: duplicate trade window", "reason", guard.Error())
	}
	return ExecutionResult{Outcome: OutcomeSkipped}, true
}

// commit is the point of no return: it runs on a context detached from shutdown, bounded by
// commitBudget. A panic anywhere in it is recovered into UNKNOWN + an urgent alert, and no
// further OKX call is made.
func (e *LiveExecutionService) commit(parent context.Context, p entryPlan) (res ExecutionResult) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), e.commitBudget)
	defer cancel()
	orderID := ""
	defer func() {
		if r := recover(); r != nil {
			ref := orderRef(orderID, p)
			slog.Error("live execution PANIC in commit phase; position state unknown, no further OKX calls",
				"orderId", orderID, "cluster", p.Decision.ClusterTime.Format(time.RFC3339),
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			e.alert(parent, unknownAlert(ref))
			res = ExecutionResult{Outcome: OutcomeUnknown}
		}
	}()

	orderID, err := e.okx.PlaceMarketOrder(p.InstID, p.EntrySide, p.Contracts)
	if err != nil {
		slog.Error("live execution failed: entry", "error", err)
		return ExecutionResult{Outcome: OutcomeFailed}
	}
	algoID, slErr := e.okx.PlaceStopLossAlgo(p.InstID, p.CloseSide, p.Contracts, p.Trigger)
	fe := FilledEntry{
		Side: p.Decision.Side, QuantityBtc: p.Quantity, EntryPrice: p.Price, Leverage: p.Leverage,
		OkxOrderID: orderID, MarginPln: p.MarginPln, Confidence: p.Decision.Confidence,
		Notes: p.Decision.Audit,
	}
	if slErr != nil {
		slog.Error("live execution SL failed after fill, emergency closing",
			"orderId", orderID, "error", slErr)
		return e.afterFailedStop(ctx, parent, p, fe)
	}
	fe.Protection, fe.OkxAlgoID = ProtectedBySL, algoID
	tradeID, recErr := e.record(ctx, fe)
	if recErr != nil {
		slog.Error("live execution PLACED_UNRECORDED: position is open and protected on OKX but has no bot_trade row",
			"orderId", orderID, "algoId", algoID, "error", recErr)
		e.alert(parent, placedUnrecordedAlert(orderID, algoID, p))
		return ExecutionResult{Outcome: OutcomePlacedUnrecorded, Filled: true}
	}
	slog.Info("live execution placed",
		"tradeId", tradeID, "side", p.Decision.Side, "leverage", p.Leverage,
		"orderId", orderID, "algoId", algoID)
	return ExecutionResult{Outcome: OutcomePlaced, TradeID: &tradeID, Filled: true}
}

// afterFailedStop handles "filled, stop-loss refused": flatten the position, verify it is flat,
// record the outcome and alert. An unprotected leveraged position must never survive silently.
func (e *LiveExecutionService) afterFailedStop(ctx, parent context.Context, p entryPlan, fe FilledEntry) ExecutionResult {
	if e.flatten(p, fe.OkxOrderID) {
		fe.Protection = EmergencyClosed
		fe.Notes = withMarker(fe.Notes, "SL_PLACE_FAILED_EMERGENCY_CLOSED orderId="+fe.OkxOrderID)
		res := ExecutionResult{Outcome: OutcomeEmergencyClosed, Filled: true}
		if id, err := e.record(ctx, fe); err != nil {
			slog.Error("live execution emergency close done but NOT recorded in the DB",
				"orderId", fe.OkxOrderID, "error", err)
		} else {
			res.TradeID = &id
		}
		e.alert(parent, emergencyClosedAlert(fe.OkxOrderID))
		return res
	}
	slog.Error("live execution emergency close not flat, human path: kill switch + OKX UI",
		"orderId", fe.OkxOrderID)
	fe.Protection = Unprotected
	fe.Notes = withMarker(fe.Notes, "UNPROTECTED_CLOSE_FAILED orderId="+fe.OkxOrderID)
	res := ExecutionResult{Outcome: OutcomeUnprotected, Filled: true}
	if id, err := e.record(ctx, fe); err != nil {
		slog.Error("live execution UNPROTECTED position NOT recorded in the DB",
			"orderId", fe.OkxOrderID, "error", err)
	} else {
		res.TradeID = &id
	}
	e.alert(parent, unprotectedAlert(fe.OkxOrderID, p))
	return res
}

// flatten market-closes the position and verifies the exchange is flat.
func (e *LiveExecutionService) flatten(p entryPlan, orderID string) bool {
	if err := e.okx.ClosePositionMarket(p.InstID, p.Decision.Side); err != nil {
		slog.Error("live execution emergency close failed", "orderId", orderID, "error", err)
		return false
	}
	pos, err := e.okx.GetPositionContracts(p.InstID)
	if err != nil {
		slog.Error("live execution emergency close verify failed", "orderId", orderID, "error", err)
		return false
	}
	return pos.Cmp(decimal.Zero) == 0
}

// record writes the entry with bounded retries (3 attempts): a transient DB blip must not lose
// the record of a position that exists. It stops early once the commit budget is spent.
func (e *LiveExecutionService) record(ctx context.Context, fe FilledEntry) (int64, error) {
	for attempt := 0; ; attempt++ {
		id, err := e.trades.RecordEntry(ctx, fe)
		if err == nil {
			return id, nil
		}
		slog.Error("live execution record attempt failed", "attempt", attempt+1, "orderId", fe.OkxOrderID, "error", err)
		if attempt >= len(e.recordBackoff) || !sleepCtx(ctx, e.recordBackoff[attempt]) {
			return 0, err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func withMarker(notes, marker string) string {
	if notes == "" {
		return marker
	}
	return notes + "\n" + marker
}

// orderRef names the order in alerts: the OKX order id once known, else the cluster time.
func orderRef(orderID string, p entryPlan) string {
	if orderID != "" {
		return orderID
	}
	return "klastra " + p.Decision.ClusterTime.UTC().Format(time.RFC3339)
}

// contractsFor ports LiveExecutionService.contractsFor (coin-margined sizing,
// ADR-090 amendment): 1 contract = ctVal USD notional, sz in 0.1 lotSz steps, never
// rounded up — sub-lot net notionals skip ("", never "0.1").
func contractsFor(quantityBtc, ctVal, entryPrice decimal.Decimal) string {
	if ctVal.Cmp(decimal.Zero) <= 0 || entryPrice.Cmp(decimal.Zero) <= 0 {
		return ""
	}
	contracts := quantityBtc.Mul(entryPrice).Div(ctVal).Truncate(1)
	if contracts.Cmp(decimal.RequireFromString("0.1")) < 0 {
		return ""
	}
	return contracts.String()
}

func (e *LiveExecutionService) toPln(ctx context.Context, marginUsdt decimal.Decimal) *decimal.Decimal {
	rate, err := e.prices.FxRateToPln(ctx, "USD")
	if err != nil {
		slog.Warn("live execution FX conversion unavailable, audit column left null", "error", err)
		return nil
	}
	v := marginUsdt.Mul(rate)
	return &v
}
