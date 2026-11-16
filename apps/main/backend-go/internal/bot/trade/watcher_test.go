package trade

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/control"
	"investing-app/backend-go/internal/bot/signal"
	"investing-app/common-go/pgnum"
)

// fakeWatcherQueries serves one canned cluster per test.
type fakeWatcherQueries struct {
	rows     []sqlcgen.WatcherSnapshotsRow
	fallback []sqlcgen.BotCalendarEvent
}

func (f *fakeWatcherQueries) WatcherSnapshots(_ context.Context, _ sqlcgen.WatcherSnapshotsParams) ([]sqlcgen.WatcherSnapshotsRow, error) {
	return f.rows, nil
}

func (f *fakeWatcherQueries) FindCalendarByNameAndTime(_ context.Context, _ sqlcgen.FindCalendarByNameAndTimeParams) ([]sqlcgen.BotCalendarEvent, error) {
	return f.fallback, nil
}

type fakeSignalQueries struct {
	rows   []sqlcgen.BotSignal
	nextID int64
}

func newFakeSignalQueries() *fakeSignalQueries { return &fakeSignalQueries{nextID: 1} }

func (f *fakeSignalQueries) InsertSignal(_ context.Context, arg sqlcgen.InsertSignalParams) (sqlcgen.BotSignal, error) {
	id := f.nextID
	f.nextID++
	row := sqlcgen.BotSignal{
		ID: id, CalendarEventID: arg.CalendarEventID, PlanScenarioID: arg.PlanScenarioID,
		EventName: arg.EventName, Actual: arg.Actual, Forecast: arg.Forecast,
		Previous: arg.Previous, SurpriseDesc: arg.SurpriseDesc, Decision: arg.Decision,
		Executed: false, RawPayload: arg.RawPayload,
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	f.rows = append(f.rows, row)
	return row, nil
}

func (f *fakeSignalQueries) MarkSignalExecuted(_ context.Context, id int64) (sqlcgen.BotSignal, error) {
	for i, r := range f.rows {
		if r.ID == id {
			f.rows[i].Executed = true
			return f.rows[i], nil
		}
	}
	return sqlcgen.BotSignal{}, errNoRows()
}

func (f *fakeSignalQueries) ListSignals(_ context.Context, _ sqlcgen.ListSignalsParams) ([]sqlcgen.BotSignal, error) {
	return f.rows, nil
}

func (f *fakeSignalQueries) CountSignals(_ context.Context, _ string) (int64, error) {
	return int64(len(f.rows)), nil
}

type fakeControlQueries struct{}

func (fakeControlQueries) GetBotControl(_ context.Context) (sqlcgen.BotControl, error) {
	return sqlcgen.BotControl{ID: 1, TradingEnabled: true,
		UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}, nil
}

func (fakeControlQueries) UpdateBotControl(_ context.Context, _ sqlcgen.UpdateBotControlParams) (sqlcgen.BotControl, error) {
	return sqlcgen.BotControl{}, errNoRows()
}

func rungRow(scenarioID int64, eventTime time.Time, label, action string, lev *int16, min, max, conf string) sqlcgen.WatcherSnapshotsRow {
	var calID int64 = 7
	row := sqlcgen.WatcherSnapshotsRow{
		ScenarioID: scenarioID, CalendarEventID: &calID, EventName: "CPI m/m",
		EventTime:     pgtype.Timestamptz{Time: eventTime, Valid: true},
		ScenarioLabel: label, TriggerCondition: "actual > forecast", Action: action,
		Leverage: lev, CalID: &calID,
		CalActual: strPtr("0.4"), CalForecast: strPtr("0.3"), CalPrevious: strPtr("0.2"),
	}
	if min != "" {
		row.SurpriseMin = pgnum.FromDecimal(decimal.RequireFromString(min))
	}
	if max != "" {
		row.SurpriseMax = pgnum.FromDecimal(decimal.RequireFromString(max))
	}
	if conf != "" {
		row.Confidence = pgnum.FromDecimal(decimal.RequireFromString(conf))
	}
	return row
}

func strPtr(s string) *string { return &s }

type watcherFixture struct {
	watcher *Watcher
	trades  *fakeQueries
	signals *fakeSignalQueries
	okx     *fakeOKX
}

func newWatcherFixture(rows []sqlcgen.WatcherSnapshotsRow, live bool) *watcherFixture {
	tq := newFakeQueries()
	sq := newFakeSignalQueries()
	okx := newFakeOKX()
	prices := &fakePrices{btc: decimal.RequireFromString("60000"), fx: decimal.RequireFromString("4")}
	tradeSvc := NewService(tq, nil, openGate(), prices, DefaultExecutionConfig())
	exec := NewLiveExecutionService(openGate(), okx, tradeSvc, prices, DefaultExecutionConfig(),
		func() bool { return live })
	w := NewWatcher(&fakeWatcherQueries{rows: rows}, signal.NewService(sq), tradeSvc,
		control.NewService(fakeControlQueries{}), exec, nil, DefaultExecutionConfig())
	return &watcherFixture{watcher: w, trades: tq, signals: sq, okx: okx}
}

func beatLadder(eventTime time.Time) []sqlcgen.WatcherSnapshotsRow {
	var lev int16 = 5
	return []sqlcgen.WatcherSnapshotsRow{
		rungRow(11, eventTime, "BEAT", "LONG", &lev, "0.05", "0.2", "0.6"),
		rungRow(12, eventTime, "INLINE", "HOLD", nil, "-0.05", "0.05", ""),
	}
}

// TestWatcher_FlagOffRecordsSignalsButPlacesNothing: shadow-mode core contract —
// the watcher still evaluates and records one signal per event, but the flag gate
// refuses before any OKX call, so no trade exists and the signal stays unexecuted.
func TestWatcher_FlagOffRecordsSignalsButPlacesNothing(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fx := newWatcherFixture(beatLadder(now), false)
	fx.watcher.Tick(context.Background(), now)

	if len(fx.signals.rows) != 1 {
		t.Fatalf("expected 1 signal recorded, got %d", len(fx.signals.rows))
	}
	sig := fx.signals.rows[0]
	if sig.Decision != "LONG" || sig.Executed {
		t.Fatalf("expected unexecuted LONG signal, got %+v", sig)
	}
	if len(fx.trades.trades) != 0 {
		t.Fatalf("expected no trade with flag OFF, got %+v", fx.trades.trades)
	}
	if len(fx.okx.calls) != 0 {
		t.Fatalf("expected zero OKX calls with flag OFF, got %v", fx.okx.calls)
	}
	// A second tick resolves the cluster from memory — no duplicate signal.
	fx.watcher.Tick(context.Background(), now.Add(time.Second))
	if len(fx.signals.rows) != 1 {
		t.Fatalf("expected resolved cluster to stay silent, signals=%d", len(fx.signals.rows))
	}
}

// TestWatcher_FlagOnPlacesAndMarksExecuted: the live path — net LONG x5 places one
// protected trade (PENDING outbox, cluster audit line) and marks the contributor.
func TestWatcher_FlagOnPlacesAndMarksExecuted(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fx := newWatcherFixture(beatLadder(now), true)
	fx.watcher.Tick(context.Background(), now)

	if len(fx.trades.trades) != 1 {
		t.Fatalf("expected 1 placed trade, got %+v", fx.trades.trades)
	}
	trade := fx.trades.trades[0]
	if trade.Status != "OPEN" || trade.ScalpSessionStatus != "PENDING" {
		t.Fatalf("expected OPEN/PENDING trade, got %+v", trade)
	}
	if trade.Notes == nil || !stringsContains(*trade.Notes, "net LONG x5") {
		t.Fatalf("expected cluster audit line in notes, got %v", trade.Notes)
	}
	if !fx.signals.rows[0].Executed {
		t.Fatal("expected contributor signal marked executed on PLACED")
	}
}

// TestWatcher_StaleSkipLeavesNoTrace: an actual arriving past staleAfter is
// untradeable — no signal, no trade, no outbox flag.
func TestWatcher_StaleSkipLeavesNoTrace(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	eventTime := now.Add(-60 * time.Second) // older than the 45s STALE_SKIP tripwire
	fx := newWatcherFixture(beatLadder(eventTime), true)
	fx.watcher.Tick(context.Background(), now)

	if len(fx.signals.rows) != 0 || len(fx.trades.trades) != 0 {
		t.Fatalf("expected STALE_SKIP to leave no trace, signals=%d trades=%d",
			len(fx.signals.rows), len(fx.trades.trades))
	}
}

// TestWatcher_RefreshWindowStartsAt15s: outbound actuals polling only begins at
// eventTime+15s (and ends at staleAfter+stopBuffer = +60s) — nothing earlier.
func TestWatcher_RefreshWindowStartsAt15s(t *testing.T) {
	eventTime := time.Now().UTC().Truncate(time.Second)
	clusters := []ClusterSnapshot{{eventTime: eventTime}}
	cases := []struct {
		offset time.Duration
		want   bool
	}{
		{5 * time.Second, false},
		{14 * time.Second, false},
		{15 * time.Second, true},
		{59 * time.Second, true},
		{60 * time.Second, false},
	}
	for _, c := range cases {
		fx := newWatcherFixture(nil, false)
		fx.watcher.maybeRefresh(context.Background(), eventTime.Add(c.offset), clusters)
		if got := fx.watcher.lastRefreshAt != nil; got != c.want {
			t.Errorf("offset %s: refreshed=%v, want %v", c.offset, got, c.want)
		}
	}
}

// TestWatcher_HoldRecordsSignalWithoutTrade: surprise inside the INLINE band
// records a HOLD signal and places nothing.
func TestWatcher_HoldRecordsSignalWithoutTrade(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	rows := []sqlcgen.WatcherSnapshotsRow{
		func() sqlcgen.WatcherSnapshotsRow {
			r := rungRow(12, now, "INLINE", "HOLD", nil, "-0.05", "0.05", "")
			r.CalActual = strPtr("0.31") // surprise 0.01 → INLINE
			return r
		}(),
	}
	fx := newWatcherFixture(rows, true)
	fx.watcher.Tick(context.Background(), now)

	if len(fx.signals.rows) != 1 || fx.signals.rows[0].Decision != "HOLD" {
		t.Fatalf("expected 1 HOLD signal, got %+v", fx.signals.rows)
	}
	if len(fx.trades.trades) != 0 {
		t.Fatalf("expected no trade on HOLD, got %+v", fx.trades.trades)
	}
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestWatcher_CalendarFallbackByNameAndTime: scenarios carrying no calendar FK
// resolve the print through the name+time fallback lookup instead of going silent.
func TestWatcher_CalendarFallbackByNameAndTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	row := rungRow(11, now, "BEAT", "LONG", func() *int16 { var v int16 = 5; return &v }(), "0.05", "0.2", "0.6")
	row.CalendarEventID = nil
	row.CalID = nil
	row.CalActual = nil
	row.CalForecast = nil
	row.CalPrevious = nil
	tq := newFakeQueries()
	sq := newFakeSignalQueries()
	prices := &fakePrices{btc: decimal.RequireFromString("60000"), fx: decimal.RequireFromString("4")}
	tradeSvc := NewService(tq, nil, openGate(), prices, DefaultExecutionConfig())
	exec := NewLiveExecutionService(openGate(), newFakeOKX(), tradeSvc, prices,
		DefaultExecutionConfig(), func() bool { return false })
	w := NewWatcher(&fakeWatcherQueries{
		rows: []sqlcgen.WatcherSnapshotsRow{row},
		fallback: []sqlcgen.BotCalendarEvent{{
			ID: 9, EventName: "CPI m/m",
			EventTime: pgtype.Timestamptz{Time: now, Valid: true},
			Actual:    strPtr("0.4"), Forecast: strPtr("0.3"), Previous: strPtr("0.2"),
		}},
	}, signal.NewService(sq), tradeSvc, control.NewService(fakeControlQueries{}), exec,
		nil, DefaultExecutionConfig())
	w.Tick(context.Background(), now)

	if len(sq.rows) != 1 || sq.rows[0].Decision != "LONG" {
		t.Fatalf("expected fallback print to drive a LONG signal, got %+v", sq.rows)
	}
	if sq.rows[0].CalendarEventID == nil || *sq.rows[0].CalendarEventID != 9 {
		t.Fatalf("expected fallback calendar id 9 on the signal, got %+v", sq.rows[0])
	}
}
