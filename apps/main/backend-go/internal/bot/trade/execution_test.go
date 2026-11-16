package trade

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// fakeOKX is a recording stand-in for OKXClient: every method appends its name to
// calls, so the flag-OFF test proves ZERO OKX calls, and the SL-fail test drives
// the emergency path deterministically.
type fakeOKX struct {
	calls        []string
	equity       decimal.Decimal
	ctVal        decimal.Decimal
	position     decimal.Decimal
	failLeverage bool
	failEntry    bool
	failAlgo     bool
	failClose    bool
	orderID      string
	algoID       string

	// Failure-injection hooks (task 191 S1). Each runs inside the matching call, so a test can
	// cancel a context, block, or panic at exactly that point of the commit phase.
	onLeverage          func()
	onEntry             func()
	onStop              func()
	onClose             func()
	closeLeavesPosition bool // ClosePositionMarket "succeeds" but the position stays open
}

func newFakeOKX() *fakeOKX {
	return &fakeOKX{
		equity:  decimal.RequireFromString("10000"),
		ctVal:   decimal.RequireFromString("100"),
		orderID: "ord-1", algoID: "algo-1",
	}
}

func (f *fakeOKX) record(name string) {
	f.calls = append(f.calls, name)
}

func (f *fakeOKX) GetEquityUsdt() (decimal.Decimal, error) {
	f.record("equity")
	return f.equity, nil
}

func (f *fakeOKX) SetLeverage(_ string, _ int) error {
	f.record("leverage")
	if f.onLeverage != nil {
		f.onLeverage()
	}
	if f.failLeverage {
		return &OkxClientException{Code: "T", Message: "nope"}
	}
	return nil
}

func (f *fakeOKX) GetContractValue(_ string) (decimal.Decimal, error) {
	f.record("ctVal")
	return f.ctVal, nil
}

func (f *fakeOKX) PlaceMarketOrder(_, _, _ string) (string, error) {
	f.record("entry")
	if f.onEntry != nil {
		f.onEntry()
	}
	if f.failEntry {
		return "", &OkxClientException{Code: "T", Message: "rejected"}
	}
	return f.orderID, nil
}

func (f *fakeOKX) PlaceStopLossAlgo(_, _, _ string, _ decimal.Decimal) (string, error) {
	f.record("stop")
	if f.onStop != nil {
		f.onStop()
	}
	if f.failAlgo {
		return "", &OkxClientException{Code: "T", Message: "sl failed"}
	}
	return f.algoID, nil
}

func (f *fakeOKX) GetPositionContracts(_ string) (decimal.Decimal, error) {
	f.record("verify")
	return f.position, nil
}

func (f *fakeOKX) ClosePositionMarket(_, _ string) error {
	f.record("closeMarket")
	if f.onClose != nil {
		f.onClose()
	}
	if f.failClose {
		return &OkxClientException{Code: "T", Message: "close failed"}
	}
	if f.closeLeavesPosition {
		f.position = decimal.NewFromInt(1)
		return nil
	}
	f.position = decimal.Zero
	return nil
}

type fakePrices struct {
	btc decimal.Decimal
	fx  decimal.Decimal
	err bool
}

func (p *fakePrices) BTCPrice(_ context.Context) (decimal.Decimal, error) {
	if p.err {
		return decimal.Zero, errors.New("no price")
	}
	return p.btc, nil
}

func (p *fakePrices) FxRateToPln(_ context.Context, _ string) (decimal.Decimal, error) {
	if p.err {
		return decimal.Zero, errors.New("no fx")
	}
	return p.fx, nil
}

// decisionOf builds the Decision the watcher would hand to Execute.
func decisionOf(side string, lev int, confidence *decimal.Decimal, contributors []int64, audit string) Decision {
	return Decision{
		ClusterTime: time.Date(2026, 9, 10, 12, 30, 0, 0, time.UTC),
		Side:        side, Leverage: lev, Confidence: confidence, Contributors: contributors, Audit: audit,
	}
}

func execFixture(f *fakeQueries, okx *fakeOKX, live bool) *LiveExecutionService {
	svc := NewService(f, nil, openGate(), &fakePrices{
		btc: decimal.RequireFromString("60000"),
		fx:  decimal.RequireFromString("4"),
	}, DefaultExecutionConfig())
	return NewLiveExecutionService(openGate(), okx, svc, &fakePrices{
		btc: decimal.RequireFromString("60000"),
		fx:  decimal.RequireFromString("4"),
	}, DefaultExecutionConfig(), func() bool { return live })
}

func conf(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

// TestExecute_FlagOffMakesZeroOKXCalls is THE money-safety test: with the flag OFF
// (default), Execute records signals' worth of intent in NOTHING but a warn log —
// the fake OKX client sees zero calls, no trade row is written, and the outcome is
// SKIPPED_FLAG_DISABLED.
func TestExecute_FlagOffMakesZeroOKXCalls(t *testing.T) {
	f := newFakeQueries()
	okx := newFakeOKX()
	exec := execFixture(f, okx, false)

	res := exec.Execute(context.Background(), decisionOf("LONG", 5, conf("0.6"), []int64{12, 13},
		"cluster audit line"))
	if res.Outcome != OutcomeSkippedFlagDisabled {
		t.Fatalf("expected SKIPPED_FLAG_DISABLED, got %s", res.Outcome)
	}
	if len(okx.calls) != 0 {
		t.Fatalf("expected ZERO OKX calls with flag OFF, got %v", okx.calls)
	}
	if len(f.trades) != 0 {
		t.Fatalf("expected no trade row with flag OFF, got %+v", f.trades)
	}
}

// TestExecute_FlagOffDefaultWhenUnset proves the constructor default (nil gate func
// → LiveExecutionFlag) honors an UNSET env as blocked — a missing env var can never
// default the backend to live trading.
func TestExecute_FlagOffDefaultWhenUnset(t *testing.T) {
	t.Setenv("BOT_LIVE_EXECUTION_ENABLED", "")
	if LiveExecutionFlag() {
		t.Fatal("expected empty BOT_LIVE_EXECUTION_ENABLED to read blocked")
	}
	t.Setenv("BOT_LIVE_EXECUTION_ENABLED", "yes")
	if LiveExecutionFlag() {
		t.Fatal("expected non-true value to read blocked")
	}
	t.Setenv("BOT_LIVE_EXECUTION_ENABLED", "true")
	if !LiveExecutionFlag() {
		t.Fatal("expected 'true' to arm")
	}
}

func TestExecute_PlacedPathAudits(t *testing.T) {
	f := newFakeQueries()
	okx := newFakeOKX()
	exec := execFixture(f, okx, true)

	res := exec.Execute(context.Background(), decisionOf("LONG", 5, conf("0.6"), []int64{12},
		"cluster 2026-09-10T12:30:00Z net LONG x2; confidence 0.45; signals [12]"))
	if res.Outcome != OutcomePlaced || res.TradeID == nil {
		t.Fatalf("expected PLACED with trade id, got %+v", res)
	}
	for _, want := range []string{"equity", "leverage", "ctVal", "entry", "stop"} {
		found := false
		for _, c := range okx.calls {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected OKX call %q, got %v", want, okx.calls)
		}
	}
	trade := f.trades[0]
	if trade.OkxOrderID == nil || *trade.OkxOrderID != "ord-1" {
		t.Fatalf("expected order id persisted, got %+v", trade)
	}
	if trade.OkxAlgoID == nil || *trade.OkxAlgoID != "algo-1" {
		t.Fatalf("expected algo id audit, got %+v", trade)
	}
	if trade.ScalpSessionStatus != "PENDING" {
		t.Fatalf("expected PENDING outbox flag on PLACED, got %q", trade.ScalpSessionStatus)
	}
	if trade.Notes == nil || *trade.Notes == "" {
		t.Fatal("expected cluster audit line in notes")
	}
	if !trade.MarginAllocatedPln.Valid || !trade.SizingConfidence.Valid {
		t.Fatal("expected margin/confidence audit columns persisted")
	}
}

func TestExecute_KillSwitchPausesBeforeAnyOKXCall(t *testing.T) {
	f := newFakeQueries()
	okx := newFakeOKX()
	svc := NewService(f, nil, &fakeGate{enabled: false}, &fakePrices{
		btc: decimal.RequireFromString("60000"), fx: decimal.RequireFromString("4"),
	}, DefaultExecutionConfig())
	exec := NewLiveExecutionService(&fakeGate{enabled: false}, okx, svc,
		&fakePrices{btc: decimal.RequireFromString("60000"), fx: decimal.RequireFromString("4")},
		DefaultExecutionConfig(), func() bool { return true })

	res := exec.Execute(context.Background(), decisionOf("LONG", 5, conf("0.6"), nil, "audit"))
	if res.Outcome != OutcomeSkippedPaused {
		t.Fatalf("expected SKIPPED_PAUSED, got %s", res.Outcome)
	}
	if len(okx.calls) != 0 {
		t.Fatalf("expected zero OKX calls when paused, got %v", okx.calls)
	}
}

func TestExecute_SizingVetoSkips(t *testing.T) {
	f := newFakeQueries()
	okx := newFakeOKX()
	okx.equity = decimal.Zero // zero equity → sizing veto
	exec := execFixture(f, okx, true)

	res := exec.Execute(context.Background(), decisionOf("LONG", 5, conf("0.6"), nil, "audit"))
	if res.Outcome != OutcomeSkipped {
		t.Fatalf("expected SKIPPED on sizing veto, got %s", res.Outcome)
	}
	for _, c := range okx.calls {
		if c == "entry" || c == "stop" {
			t.Fatalf("expected no order after veto, got %v", okx.calls)
		}
	}
}
