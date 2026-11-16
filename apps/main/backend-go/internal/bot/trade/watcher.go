package trade

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/bot/calendar"
	"investing-app/backend-go/internal/bot/control"
	"investing-app/backend-go/internal/bot/signal"
	"investing-app/common-go/lifecycle"
	"investing-app/common-go/pgnum"
)

// WatcherQueries is the read surface the release watcher needs: today's plan
// scenarios with their calendar actuals in one join, plus the name+time fallback
// for scenarios carrying no calendar FK.
type WatcherQueries interface {
	WatcherSnapshots(ctx context.Context, arg sqlcgen.WatcherSnapshotsParams) ([]sqlcgen.WatcherSnapshotsRow, error)
	FindCalendarByNameAndTime(ctx context.Context, arg sqlcgen.FindCalendarByNameAndTimeParams) ([]sqlcgen.BotCalendarEvent, error)
}

// Watcher is the Go port of EventReleaseWatcherService: a single 1-second DB tick
// (no per-event threads — sequential evaluation IS the max-1-concurrent
// enforcement), outbound refresh separately gated (20s cadence, worth-refresh
// window, 429 backoff), cluster netting, and handoff to live execution reusing the
// tracking services so tracking rows stay the single source of truth.
//
// Restart/idempotency: ticks ignore anything older than now−watchAfter (a restart
// never resurrects a stale window); the in-memory resolved set is a fast path only —
// the DB guards (OPEN-exists, 60s duplicate, per-event executed flags) are the real
// exactly-once enforcement and survive restarts.
//
// The watcher NEVER runs unless BOT_SCHEDULER_ENABLED=true (shadow mode — Java is
// the only actor until the flip). See SchedulerEnabled + StartWatcher below.
type Watcher struct {
	queries      WatcherQueries
	signals      *signal.Service
	trades       *Service
	control      *control.Service
	execution    *LiveExecutionService
	orchestrator *calendar.Orchestrator
	cfg          ExecutionConfig
	now          func() time.Time

	mu            sync.Mutex
	resolved      map[string]bool
	firstActualAt map[string]time.Time
	lastRefreshAt *time.Time
}

// NewWatcher wires the watcher (explicit constructor wiring, ADR-094 §2).
func NewWatcher(queries WatcherQueries, signals *signal.Service, trades *Service, controlSvc *control.Service, execution *LiveExecutionService, orchestrator *calendar.Orchestrator, cfg ExecutionConfig) *Watcher {
	return &Watcher{
		queries: queries, signals: signals, trades: trades, control: controlSvc,
		execution: execution, orchestrator: orchestrator, cfg: cfg,
		now: time.Now, resolved: map[string]bool{}, firstActualAt: map[string]time.Time{},
	}
}

// SchedulerEnabled reports whether this process may run the release watcher and the
// calendar purge. Default false — one flag for all bot background work, so shadow
// mode is a single switch (BOT_SCHEDULER_ENABLED=true/1 arms both).
func SchedulerEnabled() bool {
	return calendar.SchedulerEnabled()
}

// StartWatcher ticks every second until ctx is cancelled — the Go equivalent of
// EventReleaseWatcherService.tick's @Scheduled(fixedDelay=1000). Tick runs synchronously in this
// goroutine, so a slow upstream call or a live execution can never pile ticks (a pending ticker
// value is dropped by the runtime, at most one is buffered). Each tick runs inside lifecycle.Guard:
// a panic costs one tick, not the watcher (task-174 F1.3). Callers start it under a
// lifecycle.Group so shutdown waits for an in-flight execution's commit phase (ADR-114).
func StartWatcher(ctx context.Context, w *Watcher) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case fire := <-ticker.C:
			if err := lifecycle.Guard("bot-watcher-tick", func() error {
				w.Tick(ctx, fire)
				return nil
			}); err != nil {
				slog.Error("release watcher: tick aborted", "error", err)
			}
		}
	}
}

// Tick evaluates one instant. Exported for tests (Java's package-private doTick).
func (w *Watcher) Tick(ctx context.Context, now time.Time) {
	windowStart := now.Add(-time.Duration(w.cfg.WatchAfterSeconds) * time.Second)
	windowEnd := now.Add(time.Duration(w.cfg.WatchBeforeSeconds) * time.Second)
	clusters, err := w.loadSnapshots(ctx, windowStart, windowEnd)
	if err != nil {
		slog.Error("release watcher: snapshot load failed", "error", err)
		return
	}
	if len(clusters) == 0 {
		return
	}
	w.maybeRefresh(ctx, now, clusters)
	for _, cluster := range clusters {
		if w.isResolved(cluster.eventTime) {
			continue
		}
		w.evaluateCluster(ctx, cluster, now)
	}
	w.prune(now, windowStart)
}

// ClusterSnapshot is one same-timestamp release cluster under evaluation.
type ClusterSnapshot struct {
	eventTime time.Time
	events    []EventSnapshot
}

// EventSnapshot is one event leg with its ladder and latest calendar print.
type EventSnapshot struct {
	eventName  string
	calendarID *int64
	actual     string
	forecast   string
	previous   string
	ladder     []LadderRung
}

// LadderRung is one scenario rung of an event's ladder.
type LadderRung struct {
	scenarioID int64
	label      string
	action     string
	leverage   *int
	min        *decimal.Decimal
	max        *decimal.Decimal
	confidence *decimal.Decimal
}

// ladderOrder is the fixed evaluation order (ADR-090 §1): shared contiguous
// boundaries resolve to the more extreme rung — a deliberate tie-break.
var ladderOrder = []string{
	"EXTREME_MISS", "MISS", "SLIGHT_MISS", "INLINE", "SLIGHT_BEAT", "BEAT", "EXTREME_BEAT",
}

func ladderRank(label string) int {
	for i, l := range ladderOrder {
		if l == label {
			return i
		}
	}
	return len(ladderOrder)
}

// MatchSurprise returns the first ladder rung whose [min,max] contains surprise
// (null bounds are open), evaluated in fixed ladder order. Pure function —
// table-tested.
func MatchSurprise(surprise decimal.Decimal, ladder []LadderRung) *LadderRung {
	ordered := append([]LadderRung{}, ladder...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ladderRank(ordered[i].label) < ladderRank(ordered[j].label)
	})
	for i := range ordered {
		r := &ordered[i]
		if r.min != nil && surprise.Cmp(*r.min) < 0 {
			continue
		}
		if r.max != nil && surprise.Cmp(*r.max) > 0 {
			continue
		}
		out := *r
		return &out
	}
	return nil
}

// ClusterLeg is one evaluated leg's netting contribution.
type ClusterLeg struct {
	SignedLeverage int
	Confidence     *decimal.Decimal
}

// EvaluatedEvent is one signal-recorded leg of a cluster evaluation.
type EvaluatedEvent struct {
	eventName      string
	matchedLabel   string
	decision       string
	signedLeverage int
	signalID       *int64
}

// NetDecision is one cluster's net order (or HOLD).
type NetDecision struct {
	Action     string
	Leverage   int
	Confidence *decimal.Decimal
}

// NetCluster ports EventReleaseWatcherService.netCluster: signed leverage
// (LONG→+lev, SHORT→−lev, HOLD/no-match→0) summed; |sum|<1 → HOLD; else the sign
// wins with min(10,|sum|) leverage (the V65 1–10 cap applies to net leverage
// exactly as to single-event leverage). Net confidence is the mean of contributing
// non-zero confidences. Pure function — table-tested.
func NetCluster(legs []ClusterLeg) NetDecision {
	sum := 0
	var confs []decimal.Decimal
	for _, l := range legs {
		sum += l.SignedLeverage
		if l.SignedLeverage != 0 && l.Confidence != nil && l.Confidence.Cmp(decimal.Zero) != 0 {
			confs = append(confs, *l.Confidence)
		}
	}
	var mean *decimal.Decimal
	if len(confs) > 0 {
		total := decimal.Zero
		for _, c := range confs {
			total = total.Add(c)
		}
		m := total.DivRound(decimal.NewFromInt(int64(len(confs))), 4)
		mean = &m
	}
	if sum < 1 && sum > -1 {
		return NetDecision{Action: "HOLD", Leverage: 0, Confidence: mean}
	}
	lev := sum
	if lev < 0 {
		lev = -lev
	}
	if lev > 10 {
		lev = 10
	}
	action := "LONG"
	if sum < 0 {
		action = "SHORT"
	}
	return NetDecision{Action: action, Leverage: lev, Confidence: mean}
}

var decimalPattern = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)$`)

// ParseEventValue ports NumericEventValueParser.parse: unit-preserving (K/M/B/% are
// STRIPPED, never multiplied — "213K"→213.0, "0.3%"→0.3, so surprise lands in the
// same display units prompt.md's ladders are authored in), fail-closed to empty.
// Empty on either side → surprise undefined → HOLD signal, never an exception and
// never a new HTTP 422. Pure function — table-tested.
func ParseEventValue(raw string) (decimal.Decimal, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "-" || strings.EqualFold(s, "n/a") {
		return decimal.Zero, false
	}
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSpace(s)
	if s != "" {
		if last := s[len(s)-1]; last == 'K' || last == 'k' || last == 'M' || last == 'm' ||
			last == 'B' || last == 'b' || last == '%' {
			s = strings.TrimSpace(s[:len(s)-1])
		}
	}
	if s == "" || !decimalPattern.MatchString(s) {
		return decimal.Zero, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, false
	}
	return d, true
}

// maybeRefresh re-fetches calendar actuals, throttled to actualsRefreshSeconds and
// additionally gated to clusters inside
// [eventTime+refreshStartOffset, eventTime+staleAfter+refreshStopBuffer] — no
// refresh before data could exist, none once a fresh actual would just be
// STALE_SKIP'd (the 2026-09-12 429 fix). Honors the orchestrator's 429 backoff.
// DB-state evaluation is unaffected — only this outbound call is narrowed.
func (w *Watcher) maybeRefresh(ctx context.Context, now time.Time, clusters []ClusterSnapshot) {
	worthIt := false
	for _, c := range clusters {
		start := c.eventTime.Add(time.Duration(w.cfg.RefreshStartOffsetSeconds) * time.Second)
		stop := c.eventTime.Add(time.Duration(w.cfg.StaleAfterSeconds+w.cfg.RefreshStopBufferSeconds) * time.Second)
		if !now.Before(start) && now.Before(stop) {
			worthIt = true
			break
		}
	}
	if !worthIt {
		return
	}
	if w.lastRefreshAt != nil &&
		now.Before(w.lastRefreshAt.Add(time.Duration(w.cfg.ActualsRefreshSeconds)*time.Second)) {
		return
	}
	if w.orchestrator != nil && now.Before(w.orchestrator.BackoffUntil()) {
		return
	}
	if w.orchestrator != nil {
		w.orchestrator.SyncAll(ctx)
	}
	t := now
	w.lastRefreshAt = &t
}

func (w *Watcher) evaluateCluster(ctx context.Context, cluster ClusterSnapshot, now time.Time) {
	key := cluster.eventTime.UTC().Format(time.RFC3339)
	posted := []EventSnapshot{}
	for _, e := range cluster.events {
		if strings.TrimSpace(e.actual) != "" {
			posted = append(posted, e)
		}
	}
	if len(posted) == 0 {
		return
	}
	w.mu.Lock()
	if _, ok := w.firstActualAt[key]; !ok {
		w.firstActualAt[key] = now
	}
	firstAt := w.firstActualAt[key]
	w.mu.Unlock()
	allPosted := len(posted) == len(cluster.events)
	waitElapsed := !now.Before(firstAt.Add(time.Duration(w.cfg.ClusterWaitSeconds) * time.Second))
	if !allPosted && !waitElapsed {
		return
	}
	// STALE_SKIP (ADR-092 §4, normative): an actual caught more than staleAfter
	// past eventTime is untradeable — no signals, no trade, hence no PENDING outbox
	// flag. Placed before any signal is created.
	if now.After(cluster.eventTime.Add(time.Duration(w.cfg.StaleAfterSeconds) * time.Second)) {
		w.markResolved(key)
		slog.Warn("release watcher STALE_SKIP",
			"eventTime", cluster.eventTime.Format(time.RFC3339),
			"ageSeconds", int64(now.Sub(cluster.eventTime).Seconds()))
		return
	}
	w.markResolved(key)
	legs := []ClusterLeg{}
	evaluatedEvents := []EvaluatedEvent{}
	for _, event := range cluster.events {
		if strings.TrimSpace(event.actual) == "" {
			slog.Warn("release watcher cluster data gap: no actual",
				"eventTime", cluster.eventTime.Format(time.RFC3339), "event", event.eventName)
			legs = append(legs, ClusterLeg{SignedLeverage: 0})
			continue
		}
		actual, actualOK := ParseEventValue(event.actual)
		forecast, forecastOK := ParseEventValue(event.forecast)
		decision := "HOLD"
		var matchedID *int64
		matchedLabel := "NOPARSE"
		signedLeverage := 0
		var confidence *decimal.Decimal
		var surpriseDesc string
		if actualOK && forecastOK {
			surprise := actual.Sub(forecast)
			if match := MatchSurprise(surprise, event.ladder); match != nil {
				matchedID = &match.scenarioID
				matchedLabel = match.label
				decision = match.action
				confidence = match.confidence
				if decision == "LONG" && match.leverage != nil {
					signedLeverage = *match.leverage
				} else if decision == "SHORT" && match.leverage != nil {
					signedLeverage = -*match.leverage
				}
				surpriseDesc = "surprise=" + surprise.String() + " matched=" + matchedLabel
			} else {
				surpriseDesc = "surprise=" + surprise.String() + " matched=none"
			}
		} else {
			surpriseDesc = "surprise=undefined unparseable actual='" + event.actual +
				"' forecast='" + event.forecast + "'"
		}
		legs = append(legs, ClusterLeg{SignedLeverage: signedLeverage, Confidence: confidence})
		sig, err := w.signals.Create(ctx, signal.CreateInput{
			CalendarEventID: event.calendarID, PlanScenarioID: matchedID,
			EventName: event.eventName, Actual: event.actual,
			Forecast: toNullStr(event.forecast), Previous: toNullStr(event.previous),
			SurpriseDesc: &surpriseDesc, Decision: decision,
		})
		if err != nil {
			slog.Error("release watcher: signal record failed",
				"event", event.eventName, "error", err)
			evaluatedEvents = append(evaluatedEvents, EvaluatedEvent{
				eventName: event.eventName, matchedLabel: matchedLabel,
				decision: decision, signedLeverage: signedLeverage,
			})
			continue
		}
		slog.Info("release watcher: event evaluated",
			"cluster", cluster.eventTime.Format(time.RFC3339),
			"event", event.eventName, "surprise", surpriseDesc, "decision", decision)
		id := sig.ID
		evaluatedEvents = append(evaluatedEvents, EvaluatedEvent{
			eventName: event.eventName, matchedLabel: matchedLabel,
			decision: decision, signedLeverage: signedLeverage, signalID: &id,
		})
	}
	net := NetCluster(legs)
	if net.Action == "HOLD" {
		return
	}
	if net.Confidence == nil {
		slog.Warn("release watcher cluster net has no confidence, holding",
			"eventTime", cluster.eventTime.Format(time.RFC3339),
			"action", net.Action, "leverage", net.Leverage)
		return
	}
	audit := auditLine(cluster.eventTime, net, evaluatedEvents)
	contributors := []int64{}
	for _, e := range evaluatedEvents {
		if e.signedLeverage != 0 && e.signalID != nil {
			contributors = append(contributors, *e.signalID)
		}
	}
	result := w.execution.Execute(ctx, Decision{
		ClusterTime: cluster.eventTime, Side: net.Action, Leverage: net.Leverage,
		Confidence: net.Confidence, Contributors: contributors, Audit: audit,
	})
	if result.Filled {
		w.markExecuted(ctx, contributors)
	}
}

// markExecuted flags the contributing signals once a position exists on the exchange. It runs on
// a context detached from shutdown: a SIGTERM that arrives while the execution was in flight must
// not leave a filled cluster's signals unexecuted.
func (w *Watcher) markExecuted(ctx context.Context, signalIDs []int64) {
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, id := range signalIDs {
		if _, err := w.signals.MarkExecuted(markCtx, id); err != nil {
			slog.Error("release watcher: could not mark signal executed", "signalId", id, "error", err)
		}
	}
}

func auditLine(eventTime time.Time, net NetDecision, evaluated []EvaluatedEvent) string {
	legs := []string{}
	signals := []string{}
	for _, e := range evaluated {
		leg := e.eventName + " " + e.matchedLabel + " " + e.decision
		if e.signedLeverage != 0 {
			abs := e.signedLeverage
			if abs < 0 {
				abs = -abs
			}
			leg += fmt.Sprintf(" x%d", abs)
		}
		legs = append(legs, leg)
		if e.signalID != nil {
			signals = append(signals, fmt.Sprintf("%d", *e.signalID))
		} else {
			signals = append(signals, "?")
		}
	}
	conf := "null"
	if net.Confidence != nil {
		conf = net.Confidence.String()
	}
	return fmt.Sprintf("cluster %s net %s x%d [%s]; confidence %s; signals [%s]",
		eventTime.UTC().Format(time.RFC3339), net.Action, net.Leverage,
		strings.Join(legs, ", "), conf, strings.Join(signals, ","))
}

func toNullStr(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func (w *Watcher) isResolved(key time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.resolved[key.UTC().Format(time.RFC3339)]
}

func (w *Watcher) markResolved(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.resolved[key] = true
	delete(w.firstActualAt, key)
}

func (w *Watcher) prune(now, windowStart time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k := range w.resolved {
		if t, err := time.Parse(time.RFC3339, k); err == nil && t.Before(windowStart) {
			delete(w.resolved, k)
		}
	}
	for k := range w.firstActualAt {
		if t, err := time.Parse(time.RFC3339, k); err == nil && t.Before(windowStart) {
			delete(w.firstActualAt, k)
		}
	}
	_ = now
}

// loadSnapshots ports loadSnapshots: today's scenarios in the window, grouped by
// exact event_time, then by event (calendar FK, else name+time fallback lookup),
// events sorted by name, clusters by time.
func (w *Watcher) loadSnapshots(ctx context.Context, from, to time.Time) ([]ClusterSnapshot, error) {
	rows, err := w.queries.WatcherSnapshots(ctx, sqlcgen.WatcherSnapshotsParams{
		EventTime:   pgtype.Timestamptz{Time: from, Valid: true},
		EventTime_2: pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	byTime := map[string][]sqlcgen.WatcherSnapshotsRow{}
	order := []string{}
	for _, r := range rows {
		k := r.EventTime.Time.UTC().Format(time.RFC3339)
		if _, ok := byTime[k]; !ok {
			order = append(order, k)
		}
		byTime[k] = append(byTime[k], r)
	}
	sort.Strings(order)
	clusters := []ClusterSnapshot{}
	for _, k := range order {
		byEvent := map[string][]sqlcgen.WatcherSnapshotsRow{}
		eventOrder := []string{}
		for _, r := range byTime[k] {
			ek := ""
			if r.CalendarEventID != nil {
				ek = fmt.Sprintf("#%d", *r.CalendarEventID)
			} else {
				ek = r.EventName
			}
			if _, ok := byEvent[ek]; !ok {
				eventOrder = append(eventOrder, ek)
			}
			byEvent[ek] = append(byEvent[ek], r)
		}
		events := []EventSnapshot{}
		for _, ek := range eventOrder {
			rungs := byEvent[ek]
			first := rungs[0]
			var calID *int64
			var actual, forecast, previous string
			if first.CalID != nil {
				calID = first.CalID
				actual = deref(first.CalActual)
				forecast = deref(first.CalForecast)
				previous = deref(first.CalPrevious)
			} else {
				found, err := w.queries.FindCalendarByNameAndTime(ctx, sqlcgen.FindCalendarByNameAndTimeParams{
					EventName: first.EventName, EventTime: first.EventTime,
				})
				if err == nil && len(found) > 0 {
					id := found[0].ID
					calID = &id
					actual = deref(found[0].Actual)
					forecast = deref(found[0].Forecast)
					previous = deref(found[0].Previous)
				}
			}
			ladder := make([]LadderRung, 0, len(rungs))
			for _, r := range rungs {
				ladder = append(ladder, rungFromRow(r))
			}
			events = append(events, EventSnapshot{
				eventName: first.EventName, calendarID: calID,
				actual: actual, forecast: forecast, previous: previous,
				ladder: ladder,
			})
		}
		sort.Slice(events, func(i, j int) bool { return events[i].eventName < events[j].eventName })
		et, _ := time.Parse(time.RFC3339, k)
		clusters = append(clusters, ClusterSnapshot{eventTime: et, events: events})
	}
	return clusters, nil
}

func rungFromRow(r sqlcgen.WatcherSnapshotsRow) LadderRung {
	out := LadderRung{
		scenarioID: r.ScenarioID, label: r.ScenarioLabel, action: r.Action,
	}
	if r.Leverage != nil {
		v := int(*r.Leverage)
		out.leverage = &v
	}
	if r.SurpriseMin.Valid {
		if v, err := pgnum.ToDecimal(r.SurpriseMin); err == nil {
			out.min = &v
		}
	}
	if r.SurpriseMax.Valid {
		if v, err := pgnum.ToDecimal(r.SurpriseMax); err == nil {
			out.max = &v
		}
	}
	if r.Confidence.Valid {
		if v, err := pgnum.ToDecimal(r.Confidence); err == nil {
			out.confidence = &v
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
