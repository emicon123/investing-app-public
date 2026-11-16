package trade

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/push"
)

// Failure-injection tests for the post-fill commit phase (task 191 S1, D191 §3.3 rows F0-F5, F9,
// F10, F13-F16, F20-F22). The fake OKX has no network and the fake queries behave like pgx: a
// finished context fails every call.

// fakeNotifier records every alert and the state of the context each one was sent on.
type fakeNotifier struct {
	mu      sync.Mutex
	sent    []push.Notification
	ctxErrs []error
	panics  bool
	err     error
}

func (n *fakeNotifier) Broadcast(ctx context.Context, msg push.Notification) (push.DeliveryReport, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.panics {
		panic("notifier exploded")
	}
	n.sent = append(n.sent, msg)
	n.ctxErrs = append(n.ctxErrs, ctx.Err())
	return push.DeliveryReport{Attempted: 1, Delivered: 1}, n.err
}

type commitRig struct {
	exec  *LiveExecutionService
	q     *fakeQueries
	okx   *fakeOKX
	notif *fakeNotifier
}

func newCommitRig(t *testing.T) *commitRig {
	t.Helper()
	q := newFakeQueries()
	okx := newFakeOKX()
	n := &fakeNotifier{}
	prices := &fakePrices{btc: decimal.RequireFromString("60000"), fx: decimal.RequireFromString("4")}
	svc := NewService(q, nil, openGate(), prices, DefaultExecutionConfig())
	exec := NewLiveExecutionService(openGate(), okx, svc, prices, DefaultExecutionConfig(),
		func() bool { return true }, WithNotifier(n))
	exec.recordBackoff = []time.Duration{0, 0} // 3 attempts, no real sleeping
	return &commitRig{exec: exec, q: q, okx: okx, notif: n}
}

func (r *commitRig) run(ctx context.Context) ExecutionResult {
	return r.exec.Execute(ctx, decisionOf("LONG", 5, conf("0.6"), []int64{12}, "cluster audit"))
}

func (r *commitRig) called(name string) bool { return slices.Contains(r.okx.calls, name) }

func TestExecute_OpenTradeBlocksBeforeAnyOrder(t *testing.T) {
	r := newCommitRig(t)
	r.q.trades = append(r.q.trades, sqlcgen.BotTrade{
		ID: 7, Status: "OPEN", Side: "LONG",
		OpenedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})

	res := r.run(context.Background())

	if res.Outcome != OutcomeSkipped || res.Filled {
		t.Fatalf("expected SKIPPED, not filled; got %+v", res)
	}
	if len(r.okx.calls) != 0 {
		t.Fatalf("expected zero OKX calls, got %v", r.okx.calls)
	}
	if len(r.notif.sent) != 1 || r.notif.sent[0].Tag != "bot-open-7" ||
		!strings.Contains(r.notif.sent[0].Body, "#7") {
		t.Fatalf("expected one alert tagged bot-open-7, got %+v", r.notif.sent)
	}
}

func TestExecute_DuplicateWindowBlocksBeforeAnyOrder(t *testing.T) {
	r := newCommitRig(t)
	r.q.trades = append(r.q.trades, sqlcgen.BotTrade{
		ID: 3, Status: "CLOSED", Side: "SHORT",
		OpenedAt: pgtype.Timestamptz{Time: time.Now().Add(-30 * time.Second), Valid: true},
	})

	res := r.run(context.Background())

	if res.Outcome != OutcomeSkipped || res.Filled {
		t.Fatalf("expected SKIPPED, got %+v", res)
	}
	if len(r.okx.calls) != 0 {
		t.Fatalf("expected zero OKX calls, got %v", r.okx.calls)
	}
}

func TestExecute_GuardLookupFailureIsFailedWithoutOrder(t *testing.T) {
	r := newCommitRig(t)
	r.q.failNext["ExistsOpenTrade"] = errInjected

	res := r.run(context.Background())

	if res.Outcome != OutcomeFailed || len(r.okx.calls) != 0 {
		t.Fatalf("expected FAILED with no OKX call, got %+v %v", res, r.okx.calls)
	}
}

func TestExecute_RecordFailureAfterFillStillPlacesStopLoss(t *testing.T) {
	r := newCommitRig(t)
	r.q.failNext["InsertTrade"] = errInjected

	res := r.run(context.Background())

	if res.Outcome != OutcomePlacedUnrecorded || !res.Filled || res.TradeID != nil {
		t.Fatalf("expected PLACED_UNRECORDED filled with no trade id, got %+v", res)
	}
	entry, stop := slices.Index(r.okx.calls, "entry"), slices.Index(r.okx.calls, "stop")
	if entry < 0 || stop < entry {
		t.Fatalf("expected entry then stop, got %v", r.okx.calls)
	}
	if r.called("closeMarket") {
		t.Fatalf("a protected position must not be closed, got %v", r.okx.calls)
	}
	if len(r.q.trades) != 0 {
		t.Fatalf("expected no trade row, got %+v", r.q.trades)
	}
	if len(r.notif.sent) != 1 || !strings.Contains(r.notif.sent[0].Body, "ord-1") ||
		r.notif.sent[0].Urgency != push.UrgencyNormal {
		t.Fatalf("expected one alert carrying the order id, got %+v", r.notif.sent)
	}
}

func TestExecute_TransientRecordFailureIsRetried(t *testing.T) {
	r := newCommitRig(t)
	r.q.failTimes["InsertTrade"] = 2

	res := r.run(context.Background())

	if res.Outcome != OutcomePlaced || res.TradeID == nil || len(r.q.trades) != 1 {
		t.Fatalf("expected PLACED after two retries with one row, got %+v trades=%d", res, len(r.q.trades))
	}
	if len(r.notif.sent) != 0 {
		t.Fatalf("a recovered record needs no alert, got %+v", r.notif.sent)
	}
}

// SIGTERM that lands exactly on the fill: the commit phase must still protect and record.
func TestExecute_ContextCancelledAtFillStillProtectsAndRecords(t *testing.T) {
	r := newCommitRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.okx.onEntry = cancel

	res := r.run(ctx)

	if res.Outcome != OutcomePlaced || res.TradeID == nil || !res.Filled {
		t.Fatalf("expected PLACED despite cancellation, got %+v", res)
	}
	if !r.called("stop") {
		t.Fatalf("expected the stop-loss to be placed, got %v", r.okx.calls)
	}
	if len(r.q.trades) != 1 || r.q.trades[0].OkxAlgoID == nil || *r.q.trades[0].OkxAlgoID != "algo-1" {
		t.Fatalf("expected the trade recorded with its algo id, got %+v", r.q.trades)
	}
}

func TestExecute_ContextCancelledBeforeOrderPlacesNothing(t *testing.T) {
	r := newCommitRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.okx.onLeverage = cancel

	res := r.run(ctx)

	if res.Outcome != OutcomeSkipped || res.Filled {
		t.Fatalf("expected SKIPPED, got %+v", res)
	}
	if r.called("entry") || r.called("stop") || len(r.q.trades) != 0 {
		t.Fatalf("nothing may be placed or recorded, got %v trades=%d", r.okx.calls, len(r.q.trades))
	}
}

func TestExecute_SLFailEmergencyClosesThenRecordsClosedTrade(t *testing.T) {
	r := newCommitRig(t)
	r.okx.failAlgo = true
	tradesAtClose := -1
	r.okx.onClose = func() { tradesAtClose = len(r.q.trades) }

	res := r.run(context.Background())

	if res.Outcome != OutcomeEmergencyClosed || !res.Filled || res.TradeID == nil {
		t.Fatalf("expected EMERGENCY_CLOSED filled with a trade id, got %+v", res)
	}
	if want := []string{"equity", "leverage", "ctVal", "entry", "stop", "closeMarket", "verify"}; !slices.Equal(r.okx.calls, want) {
		t.Fatalf("OKX call order = %v, want %v", r.okx.calls, want)
	}
	if tradesAtClose != 0 {
		t.Fatalf("the row must be created after the exchange is flat, %d rows existed at close", tradesAtClose)
	}
	trade := r.q.trades[0]
	if trade.Status != "CLOSED" || trade.ScalpSessionStatus != "NONE" {
		t.Fatalf("expected CLOSED/NONE, got %q/%q", trade.Status, trade.ScalpSessionStatus)
	}
	if trade.Notes == nil || !strings.Contains(*trade.Notes, "SL_PLACE_FAILED_EMERGENCY_CLOSED orderId=ord-1") ||
		!strings.HasPrefix(*trade.Notes, "cluster audit") {
		t.Fatalf("expected audit line plus emergency marker, got %v", trade.Notes)
	}
	if len(r.notif.sent) != 1 || r.notif.sent[0].Urgency != push.UrgencyNormal ||
		!strings.Contains(r.notif.sent[0].Body, "ord-1") {
		t.Fatalf("expected one normal alert with the order id, got %+v", r.notif.sent)
	}
}

func TestExecute_EmergencyCloseRecordFailureStillReportsClosed(t *testing.T) {
	r := newCommitRig(t)
	r.okx.failAlgo = true
	r.q.failNext["InsertTrade"] = errInjected

	res := r.run(context.Background())

	if res.Outcome != OutcomeEmergencyClosed || res.TradeID != nil || !res.Filled {
		t.Fatalf("expected EMERGENCY_CLOSED with no trade id, got %+v", res)
	}
	if len(r.notif.sent) != 1 {
		t.Fatalf("expected one alert, got %+v", r.notif.sent)
	}
}

func TestExecute_SLFailAndCloseFailRecordsOpenUnprotected(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fakeOKX)
	}{
		{"close errors", func(o *fakeOKX) { o.failClose = true }},
		{"close accepted but position remains", func(o *fakeOKX) { o.closeLeavesPosition = true }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newCommitRig(t)
			r.okx.failAlgo = true
			c.setup(r.okx)

			res := r.run(context.Background())

			if res.Outcome != OutcomeUnprotected || !res.Filled || res.TradeID == nil {
				t.Fatalf("expected UNPROTECTED filled with a trade id, got %+v", res)
			}
			trade := r.q.trades[0]
			if trade.Status != "OPEN" || trade.ScalpSessionStatus != "NONE" {
				t.Fatalf("expected OPEN/NONE, got %q/%q", trade.Status, trade.ScalpSessionStatus)
			}
			if trade.Notes == nil || !strings.Contains(*trade.Notes, "UNPROTECTED_CLOSE_FAILED orderId=ord-1") {
				t.Fatalf("expected the unprotected marker, got %v", trade.Notes)
			}
			if len(r.notif.sent) != 1 || r.notif.sent[0].Urgency != push.UrgencyHigh ||
				!strings.Contains(r.notif.sent[0].Body, "ord-1") {
				t.Fatalf("expected one URGENT alert with the order id, got %+v", r.notif.sent)
			}
		})
	}
}

func TestExecute_PanicAfterFillIsRecoveredAndAlerted(t *testing.T) {
	r := newCommitRig(t)
	r.okx.onStop = func() { panic("boom in the stop-loss call") }

	res := r.run(context.Background())

	if res.Outcome != OutcomeUnknown || res.Filled {
		t.Fatalf("expected UNKNOWN, got %+v", res)
	}
	if want := []string{"equity", "leverage", "ctVal", "entry", "stop"}; !slices.Equal(r.okx.calls, want) {
		t.Fatalf("no OKX call may follow the panic: got %v, want %v", r.okx.calls, want)
	}
	if len(r.notif.sent) != 1 || r.notif.sent[0].Urgency != push.UrgencyHigh ||
		!strings.Contains(r.notif.sent[0].Body, "ord-1") {
		t.Fatalf("expected one URGENT alert naming the order, got %+v", r.notif.sent)
	}
}

func TestExecute_PanickingNotifierDoesNotEscape(t *testing.T) {
	r := newCommitRig(t)
	r.notif.panics = true
	r.q.failNext["InsertTrade"] = errInjected

	res := r.run(context.Background())

	if res.Outcome != OutcomePlacedUnrecorded {
		t.Fatalf("expected PLACED_UNRECORDED, got %+v", res)
	}
}

func TestExecute_NilNotifierIsLogOnly(t *testing.T) {
	r := newCommitRig(t)
	r.exec.notifier = nil
	r.q.failNext["InsertTrade"] = errInjected

	if res := r.run(context.Background()); res.Outcome != OutcomePlacedUnrecorded {
		t.Fatalf("expected PLACED_UNRECORDED, got %+v", res)
	}
}

func TestExecute_NotConfiguredPushDoesNotFailExecution(t *testing.T) {
	r := newCommitRig(t)
	r.notif.err = push.ErrNotConfigured
	r.q.failNext["InsertTrade"] = errInjected

	if res := r.run(context.Background()); res.Outcome != OutcomePlacedUnrecorded {
		t.Fatalf("expected PLACED_UNRECORDED, got %+v", res)
	}
}

// F21: the commit budget runs out after the fill. The record fails fast, and the alert still goes
// out on its own fresh context instead of the exhausted one.
func TestExecute_BudgetExceededStillAlertsOnFreshContext(t *testing.T) {
	r := newCommitRig(t)
	r.exec.commitBudget = 20 * time.Millisecond
	r.okx.onStop = func() { time.Sleep(60 * time.Millisecond) }

	res := r.run(context.Background())

	if res.Outcome != OutcomePlacedUnrecorded || !res.Filled {
		t.Fatalf("expected PLACED_UNRECORDED, got %+v", res)
	}
	if len(r.notif.sent) != 1 {
		t.Fatalf("expected one alert, got %+v", r.notif.sent)
	}
	if err := r.notif.ctxErrs[0]; err != nil {
		t.Fatalf("the alert must use a fresh context, got %v", err)
	}
}

// The alert for a failure after the fill is delivered even when shutdown cancelled the root ctx.
func TestExecute_AlertSurvivesShutdownContext(t *testing.T) {
	r := newCommitRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.okx.onEntry = cancel
	r.q.failNext["InsertTrade"] = errInjected

	if res := r.run(ctx); res.Outcome != OutcomePlacedUnrecorded {
		t.Fatalf("expected PLACED_UNRECORDED, got %+v", res)
	}
	if len(r.notif.sent) != 1 || r.notif.ctxErrs[0] != nil {
		t.Fatalf("expected one alert on a live context, got %+v %v", r.notif.sent, r.notif.ctxErrs)
	}
}

// recordingQueries logs the statements that run inside the transaction.
type recordingQueries struct {
	Queries
	log *[]string
}

func (r recordingQueries) InsertTrade(ctx context.Context, a sqlcgen.InsertTradeParams) (sqlcgen.BotTrade, error) {
	*r.log = append(*r.log, "InsertTrade")
	return r.Queries.InsertTrade(ctx, a)
}

func (r recordingQueries) AttachPlacedAudit(ctx context.Context, a sqlcgen.AttachPlacedAuditParams) error {
	*r.log = append(*r.log, "AttachPlacedAudit")
	return r.Queries.AttachPlacedAudit(ctx, a)
}

func TestExecute_PlacedRecordsInOneTransaction(t *testing.T) {
	q := newFakeQueries()
	okx := newFakeOKX()
	prices := &fakePrices{btc: decimal.RequireFromString("60000"), fx: decimal.RequireFromString("4")}
	var txCalls int
	var inTx []string
	transact := func(_ context.Context, fn func(Queries) error) error {
		txCalls++
		return fn(recordingQueries{Queries: q, log: &inTx})
	}
	svc := NewService(q, transact, openGate(), prices, DefaultExecutionConfig())
	exec := NewLiveExecutionService(openGate(), okx, svc, prices, DefaultExecutionConfig(),
		func() bool { return true })

	res := exec.Execute(context.Background(), decisionOf("LONG", 5, conf("0.6"), nil, "audit"))

	if res.Outcome != OutcomePlaced {
		t.Fatalf("expected PLACED, got %+v", res)
	}
	if txCalls != 1 {
		t.Fatalf("expected the entry to be recorded in exactly one transaction, got %d", txCalls)
	}
	if want := []string{"InsertTrade", "AttachPlacedAudit"}; !slices.Equal(inTx, want) {
		t.Fatalf("statements inside the transaction = %v, want %v", inTx, want)
	}
	if q.trades[0].ScalpSessionStatus != "PENDING" || q.trades[0].OkxAlgoID == nil {
		t.Fatalf("expected PENDING with an algo id, got %+v", q.trades[0])
	}
}

func TestRecordEntry_FailureInsideTransactionPropagates(t *testing.T) {
	q := newFakeQueries()
	q.failNext["AttachPlacedAudit"] = errInjected
	svc := testService(q)

	_, err := svc.RecordEntry(context.Background(), FilledEntry{
		Side: "LONG", QuantityBtc: decimal.RequireFromString("0.002"),
		EntryPrice: decimal.RequireFromString("60000"), Leverage: 5,
		OkxOrderID: "o", OkxAlgoID: "a", Protection: ProtectedBySL,
	})

	if !errors.Is(err, errInjected) {
		t.Fatalf("expected the injected error, got %v", err)
	}
}

func TestRecordEntry_RunsNoGuards(t *testing.T) {
	q := newFakeQueries()
	q.trades = append(q.trades, sqlcgen.BotTrade{
		ID: 1, Status: "OPEN", OpenedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	svc := NewService(q, nil, &fakeGate{enabled: false}, nil, DefaultExecutionConfig())

	id, err := svc.RecordEntry(context.Background(), FilledEntry{
		Side: "SHORT", QuantityBtc: decimal.RequireFromString("0.002"),
		EntryPrice: decimal.RequireFromString("60000"), Leverage: 5,
		OkxOrderID: "o", OkxAlgoID: "a", Protection: ProtectedBySL,
	})

	if err != nil || id == 0 {
		t.Fatalf("a position that exists must be recordable despite OPEN row and paused switch: %v", err)
	}
}
