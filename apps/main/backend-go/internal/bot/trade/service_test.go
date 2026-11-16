package trade

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as
// prior phases (business logic table-tested here; real Postgres covered by
// integration_test.go).
type fakeQueries struct {
	trades   []sqlcgen.BotTrade
	actions  []sqlcgen.BotTradeAction
	ticks    []sqlcgen.BotTradeTick
	signals  map[int64]bool
	cals     map[int64]bool
	nextID   int64
	failNext map[string]error // method name -> error returned on every call
	// failTimes: method name -> how many more calls fail with errInjected before succeeding.
	failTimes map[string]int
}

var errInjected = errors.New("injected failure")

// gate mimics pgx: a finished context fails every call, and a test can inject a persistent
// (failNext) or counted (failTimes) failure per method.
func (f *fakeQueries) gate(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err, ok := f.failNext[op]; ok {
		return err
	}
	if n := f.failTimes[op]; n > 0 {
		f.failTimes[op] = n - 1
		return errInjected
	}
	return nil
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{
		signals: map[int64]bool{}, cals: map[int64]bool{},
		nextID: 1, failNext: map[string]error{}, failTimes: map[string]int{},
	}
}

type fakeGate struct {
	enabled bool
	reason  *string
}

func (g *fakeGate) TradingStatus(_ context.Context) (bool, *string) {
	return g.enabled, g.reason
}

func openGate() *fakeGate { return &fakeGate{enabled: true} }

func errNoRows() error { return pgx.ErrNoRows }

func testService(f *fakeQueries) *Service {
	return NewService(f, nil, openGate(), nil, DefaultExecutionConfig())
}

func (f *fakeQueries) ListTrades(ctx context.Context, arg sqlcgen.ListTradesParams) ([]sqlcgen.BotTrade, error) {
	if err := f.gate(ctx, "ListTrades"); err != nil {
		return nil, err
	}
	var out []sqlcgen.BotTrade
	for _, r := range f.trades {
		if arg.Status != "" && r.Status != arg.Status {
			continue
		}
		if arg.Side != "" && r.Side != arg.Side {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedAt.Time.After(out[j].OpenedAt.Time) })
	start := int(arg.Offset)
	if start > len(out) {
		start = len(out)
	}
	end := start + int(arg.Limit)
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], nil
}

func (f *fakeQueries) CountTrades(_ context.Context, arg sqlcgen.CountTradesParams) (int64, error) {
	rows, _ := f.ListTrades(context.Background(), sqlcgen.ListTradesParams{
		Status: arg.Status, Side: arg.Side, Limit: 1 << 30,
	})
	return int64(len(rows)), nil
}

func (f *fakeQueries) GetTradeByID(_ context.Context, id int64) (sqlcgen.BotTrade, error) {
	for _, r := range f.trades {
		if r.ID == id {
			return r, nil
		}
	}
	return sqlcgen.BotTrade{}, errNoRows()
}

func (f *fakeQueries) ExistsTradeByID(_ context.Context, id int64) (bool, error) {
	for _, r := range f.trades {
		if r.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeQueries) ExistsOpenTrade(ctx context.Context) (bool, error) {
	if err := f.gate(ctx, "ExistsOpenTrade"); err != nil {
		return false, err
	}
	for _, r := range f.trades {
		if r.Status == "OPEN" {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeQueries) ExistsTradeOpenedAfter(ctx context.Context, openedAt pgtype.Timestamptz) (bool, error) {
	if err := f.gate(ctx, "ExistsTradeOpenedAfter"); err != nil {
		return false, err
	}
	for _, r := range f.trades {
		if r.OpenedAt.Time.After(openedAt.Time) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeQueries) InsertTrade(ctx context.Context, arg sqlcgen.InsertTradeParams) (sqlcgen.BotTrade, error) {
	if err := f.gate(ctx, "InsertTrade"); err != nil {
		return sqlcgen.BotTrade{}, err
	}
	id := f.nextID
	f.nextID++
	now := time.Now().UTC()
	row := sqlcgen.BotTrade{
		ID: id, SignalID: arg.SignalID, CalendarEventID: arg.CalendarEventID,
		Side: arg.Side, Status: "OPEN", Quantity: arg.Quantity, Leverage: arg.Leverage,
		EntryPrice: arg.EntryPrice, OkxOrderID: arg.OkxOrderID, Notes: arg.Notes,
		OpenedAt:           pgtype.Timestamptz{Time: now, Valid: true},
		CreatedAt:          pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:          pgtype.Timestamptz{Time: now, Valid: true},
		ScalpSessionStatus: "NONE",
	}
	f.trades = append(f.trades, row)
	return row, nil
}

func (f *fakeQueries) CloseTrade(ctx context.Context, arg sqlcgen.CloseTradeParams) (sqlcgen.BotTrade, error) {
	if err := f.gate(ctx, "CloseTrade"); err != nil {
		return sqlcgen.BotTrade{}, err
	}
	for i, r := range f.trades {
		if r.ID == arg.ID && r.Status == "OPEN" {
			now := time.Now().UTC()
			f.trades[i].ExitPrice = arg.ExitPrice
			f.trades[i].PnlPln = arg.PnlPln
			f.trades[i].PnlPct = arg.PnlPct
			f.trades[i].Status = "CLOSED"
			f.trades[i].ClosedAt = pgtype.Timestamptz{Time: now, Valid: true}
			return f.trades[i], nil
		}
	}
	return sqlcgen.BotTrade{}, errNoRows()
}

func (f *fakeQueries) AttachExecutionAudit(ctx context.Context, arg sqlcgen.AttachExecutionAuditParams) error {
	if err := f.gate(ctx, "AttachExecutionAudit"); err != nil {
		return err
	}
	for i, r := range f.trades {
		if r.ID == arg.ID {
			f.trades[i].OkxAlgoID = arg.OkxAlgoID
			f.trades[i].MarginAllocatedPln = arg.MarginAllocatedPln
			f.trades[i].SizingConfidence = arg.SizingConfidence
			return nil
		}
	}
	return errNoRows()
}

func (f *fakeQueries) AttachPlacedAudit(ctx context.Context, arg sqlcgen.AttachPlacedAuditParams) error {
	if err := f.gate(ctx, "AttachPlacedAudit"); err != nil {
		return err
	}
	for i, r := range f.trades {
		if r.ID == arg.ID {
			f.trades[i].OkxAlgoID = arg.OkxAlgoID
			f.trades[i].MarginAllocatedPln = arg.MarginAllocatedPln
			f.trades[i].SizingConfidence = arg.SizingConfidence
			f.trades[i].ScalpSessionStatus = "PENDING"
			return nil
		}
	}
	return errNoRows()
}

func (f *fakeQueries) AppendTradeNote(ctx context.Context, arg sqlcgen.AppendTradeNoteParams) error {
	if err := f.gate(ctx, "AppendTradeNote"); err != nil {
		return err
	}
	for i, r := range f.trades {
		if r.ID == arg.ID {
			if r.Notes == nil || *r.Notes == "" {
				f.trades[i].Notes = arg.Notes
			} else {
				combined := *r.Notes + "\n" + *arg.Notes
				f.trades[i].Notes = &combined
			}
			return nil
		}
	}
	return errNoRows()
}

func (f *fakeQueries) PendingScalpTrades(_ context.Context) ([]sqlcgen.BotTrade, error) {
	var out []sqlcgen.BotTrade
	for _, r := range f.trades {
		if r.ScalpSessionStatus == "PENDING" {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedAt.Time.Before(out[j].OpenedAt.Time) })
	return out, nil
}

func (f *fakeQueries) ClaimPendingScalp(_ context.Context, id int64) (int64, error) {
	for i, r := range f.trades {
		if r.ID == id && r.ScalpSessionStatus == "PENDING" {
			now := time.Now().UTC()
			f.trades[i].ScalpSessionStatus = "RUNNING"
			f.trades[i].ScalpSessionClaimedAt = pgtype.Timestamptz{Time: now, Valid: true}
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeQueries) FinishRunningScalp(_ context.Context, id int64) (int64, error) {
	for i, r := range f.trades {
		if r.ID == id && r.ScalpSessionStatus == "RUNNING" {
			f.trades[i].ScalpSessionStatus = "DONE"
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeQueries) InsertTradeAction(_ context.Context, arg sqlcgen.InsertTradeActionParams) (sqlcgen.BotTradeAction, error) {
	id := f.nextID
	f.nextID++
	row := sqlcgen.BotTradeAction{
		ID: id, TradeID: arg.TradeID, ActionType: arg.ActionType,
		Price: arg.Price, Quantity: arg.Quantity, Reasoning: arg.Reasoning,
		Source:    arg.Source,
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	f.actions = append(f.actions, row)
	return row, nil
}

func (f *fakeQueries) ListTradeActions(_ context.Context, tradeID int64) ([]sqlcgen.BotTradeAction, error) {
	var out []sqlcgen.BotTradeAction
	for _, a := range f.actions {
		if a.TradeID == tradeID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeQueries) InsertTradeTick(_ context.Context, arg sqlcgen.InsertTradeTickParams) (sqlcgen.BotTradeTick, error) {
	id := f.nextID
	f.nextID++
	row := sqlcgen.BotTradeTick{
		ID: id, TradeID: arg.TradeID, Price: arg.Price, Volume: arg.Volume,
		Cvd: arg.Cvd, Atr1m: arg.Atr1m, ObImbalance: arg.ObImbalance,
		FundingRate: arg.FundingRate, LongShortRatio: arg.LongShortRatio,
		OpenInterest: arg.OpenInterest,
		CreatedAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	f.ticks = append(f.ticks, row)
	return row, nil
}

func (f *fakeQueries) ListTradeTicks(_ context.Context, tradeID int64) ([]sqlcgen.BotTradeTick, error) {
	var out []sqlcgen.BotTradeTick
	for _, t := range f.ticks {
		if t.TradeID == tradeID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeQueries) ListTradeTicksBetween(_ context.Context, arg sqlcgen.ListTradeTicksBetweenParams) ([]sqlcgen.BotTradeTick, error) {
	var out []sqlcgen.BotTradeTick
	for _, t := range f.ticks {
		if t.TradeID == arg.TradeID &&
			!t.CreatedAt.Time.Before(arg.CreatedAt.Time) &&
			!t.CreatedAt.Time.After(arg.CreatedAt_2.Time) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeQueries) ExistingSignalIDs(_ context.Context, ids []int64) ([]int64, error) {
	var out []int64
	for _, id := range ids {
		if f.signals[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeQueries) ExistingCalendarEventIDs(_ context.Context, ids []int64) ([]int64, error) {
	var out []int64
	for _, id := range ids {
		if f.cals[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func createInput(side string, qty, price string, lev *int) CreateInput {
	return CreateInput{
		Side:     side,
		Quantity: decimal.RequireFromString(qty), QuantitySet: true,
		Leverage:   lev,
		EntryPrice: decimal.RequireFromString(price), EntryPriceSet: true,
	}
}

func TestCreate_KillSwitch409(t *testing.T) {
	f := newFakeQueries()
	reason := "storm"
	svc := NewService(f, nil, &fakeGate{enabled: false, reason: &reason}, nil, DefaultExecutionConfig())
	_, err := svc.Create(context.Background(), createInput("LONG", "0.01", "60000", nil))
	if err == nil {
		t.Fatal("expected 409 when paused, got nil")
	}
	if got := err.Error(); got != "Bot trading is paused: storm" {
		t.Fatalf("expected reason in message, got %q", got)
	}
}

func TestCreate_MaxConcurrentAndDuplicate409(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	if _, err := svc.Create(ctx, createInput("LONG", "0.01", "60000", nil)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Second entry while one is OPEN → MAX_CONCURRENT_TRADE.
	if _, err := svc.Create(ctx, createInput("SHORT", "0.01", "60000", nil)); err == nil {
		t.Fatal("expected 409 MAX_CONCURRENT_TRADE, got nil")
	} else if got := err.Error(); got != "MAX_CONCURRENT_TRADE: an OPEN trade already exists" {
		t.Fatalf("unexpected message: %q", got)
	}
	// Close it, then immediate re-entry trips the 60s duplicate guard.
	if _, err := svc.Close(ctx, 1, CloseInput{
		ExitPrice: decimal.RequireFromString("61000"), ExitPriceSet: true,
	}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := svc.Create(ctx, createInput("LONG", "0.01", "60000", nil)); err == nil {
		t.Fatal("expected 409 DUPLICATE_TRADE_WINDOW, got nil")
	}
}

func TestClose_ComputesPnlAnd409OnDouble(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	lev := 3
	if _, err := svc.Create(ctx, createInput("LONG", "0.01", "60000", &lev)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	closed, err := svc.Close(ctx, 1, CloseInput{
		ExitPrice: decimal.RequireFromString("61000"), ExitPriceSet: true,
	})
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	// (61000−60000)×0.01×3 = 30.
	if closed.PnlPln == nil || *closed.PnlPln != 30 {
		t.Fatalf("expected computed pnl 30, got %+v", closed.PnlPln)
	}
	if _, err := svc.Close(ctx, 1, CloseInput{
		ExitPrice: decimal.RequireFromString("61000"), ExitPriceSet: true,
	}); err == nil {
		t.Fatal("expected 409 Already closed, got nil")
	}
	if _, err := svc.Close(ctx, 999, CloseInput{
		ExitPrice: decimal.RequireFromString("61000"), ExitPriceSet: true,
	}); err == nil {
		t.Fatal("expected 404 for unknown trade, got nil")
	}
}

func TestClaimFinish_Atomic200Then409(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	if _, err := svc.Create(ctx, createInput("LONG", "0.01", "60000", nil)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Fresh trade is NONE — claim must 409 (only PENDING transitions).
	if _, err := svc.Claim(ctx, 1); err == nil {
		t.Fatal("expected 409 claiming NONE row, got nil")
	}
	// Arm PENDING like the PLACED path does, then claim twice.
	if err := svc.AttachPlacedAudit(ctx, 1, "algo-1", nil, nil); err != nil {
		t.Fatalf("AttachPlacedAudit: %v", err)
	}
	claimed, err := svc.Claim(ctx, 1)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.ScalpSessionStatus == nil || *claimed.ScalpSessionStatus != "RUNNING" {
		t.Fatalf("expected RUNNING, got %+v", claimed.ScalpSessionStatus)
	}
	if _, err := svc.Claim(ctx, 1); err == nil {
		t.Fatal("expected 409 on double claim, got nil")
	}
	// Claim wrote exactly one SYSTEM/SCALP_SESSION_STARTED action row.
	started := 0
	for _, a := range f.actions {
		if a.ActionType == "SCALP_SESSION_STARTED" && a.Source == "SYSTEM" {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("expected 1 STARTED audit row, got %d", len(f.actions))
	}
	// Finish moves RUNNING→DONE with the exit code in the audit row.
	code := 0
	finished, err := svc.Finish(ctx, 1, &code)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if finished.ScalpSessionStatus == nil || *finished.ScalpSessionStatus != "DONE" {
		t.Fatalf("expected DONE, got %+v", finished.ScalpSessionStatus)
	}
	if _, err := svc.Finish(ctx, 1, &code); err == nil {
		t.Fatal("expected 409 finishing DONE row, got nil")
	}
	if _, err := svc.Claim(ctx, 999); err == nil {
		t.Fatal("expected 404 claiming unknown trade, got nil")
	}
}

func TestActionsAndTicks(t *testing.T) {
	f := newFakeQueries()
	svc := testService(f)
	ctx := context.Background()
	if _, err := svc.Create(ctx, createInput("LONG", "0.01", "60000", nil)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	price := decimal.RequireFromString("60500")
	action, err := svc.AddAction(ctx, 1, ActionInput{ActionType: "SET_SL", Price: &price})
	if err != nil {
		t.Fatalf("AddAction: %v", err)
	}
	if action.Source != "BOT" {
		t.Fatalf("expected default source BOT, got %q", action.Source)
	}
	if _, err := svc.AddAction(ctx, 1, ActionInput{ActionType: "BOGUS"}); err == nil {
		t.Fatal("expected 422 for unknown actionType, got nil")
	}
	if _, err := svc.AddAction(ctx, 999, ActionInput{ActionType: "SET_SL"}); err == nil {
		t.Fatal("expected 404 for unknown trade, got nil")
	}
	list, err := svc.ListActions(ctx, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 action, got %+v/%v", list, err)
	}
	// Empty tick batch → empty list, no error.
	empty, err := svc.AddTicks(ctx, 1, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty ticks, got %+v/%v", empty, err)
	}
	// Omitted price falls back to entryPrice; optionals persist.
	vol := decimal.RequireFromString("1.5")
	rows, err := svc.AddTicks(ctx, 1, []TickInput{{Volume: &vol}, {Price: &price}})
	if err != nil || len(rows) != 2 {
		t.Fatalf("expected 2 ticks, got %+v/%v", rows, err)
	}
	got, err := pgnum.ToDecimal(rows[0].Price)
	if err != nil || !got.Equal(decimal.RequireFromString("60000")) {
		t.Fatalf("expected entryPrice fallback 60000, got %v", rows[0].Price)
	}
	if _, err := svc.AddTicks(ctx, 999, []TickInput{{}}); err == nil {
		t.Fatal("expected 404 for unknown trade, got nil")
	}
}
