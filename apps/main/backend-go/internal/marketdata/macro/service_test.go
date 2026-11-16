package macro

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/settings"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as Phase 1's
// marketdata fake (business logic table-tested here; real Postgres covered by
// integration_test.go). Keyed exactly like the UNIQUE constraints: observations by
// (series, date), states by as_of_date.
type fakeQueries struct {
	obs         map[string]fakeObs // series|date -> obs
	states      map[string]sqlcgen.MacroState
	settings    map[string]string
	settingsErr error // when set, every GetSettingByKey fails with it
}

type fakeObs struct {
	source string
	date   time.Time
	value  decimal.Decimal
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{obs: map[string]fakeObs{}, states: map[string]sqlcgen.MacroState{}, settings: map[string]string{}}
}

func obsKey(series string, date time.Time) string {
	return series + "|" + startOfDayUTC(date).Format("2006-01-02")
}

func (f *fakeQueries) UpsertObservation(_ context.Context, arg sqlcgen.UpsertObservationParams) error {
	v, err := pgnum.ToDecimal(arg.Value)
	if err != nil {
		return err
	}
	f.obs[obsKey(arg.SeriesCode, arg.ObservationDate.Time)] = fakeObs{source: arg.Source, date: startOfDayUTC(arg.ObservationDate.Time), value: v}
	return nil
}

func (f *fakeQueries) obsPoints(series string) []MacroPoint {
	out := []MacroPoint{}
	for key, o := range f.obs {
		if len(key) > len(series) && key[:len(series)] == series && key[len(series)] == '|' {
			out = append(out, MacroPoint{Date: o.date, Value: o.value})
		}
	}
	return sortAsc(out)
}

func (f *fakeQueries) ObsBySeriesAsc(_ context.Context, seriesCode string) ([]sqlcgen.ObsBySeriesAscRow, error) {
	out := []sqlcgen.ObsBySeriesAscRow{}
	for _, p := range f.obsPoints(seriesCode) {
		o := f.obs[obsKey(seriesCode, p.Date)]
		out = append(out, sqlcgen.ObsBySeriesAscRow{
			SeriesCode: seriesCode, Source: o.source,
			ObservationDate: pgnum.Date(p.Date), Value: pgnum.FromDecimal(p.Value),
		})
	}
	return out, nil
}

func (f *fakeQueries) latest(series, source string) (fakeObs, bool) {
	var best *fakeObs
	for key, o := range f.obs {
		if len(key) <= len(series) || key[:len(series)] != series || key[len(series)] != '|' {
			continue
		}
		if source != "" && o.source != source {
			continue
		}
		o := o
		if best == nil || o.date.After(best.date) {
			best = &o
		}
	}
	if best == nil {
		return fakeObs{}, false
	}
	return *best, true
}

func (f *fakeQueries) ObsLatestBySeries(_ context.Context, seriesCode string) (sqlcgen.ObsLatestBySeriesRow, error) {
	o, ok := f.latest(seriesCode, "")
	if !ok {
		return sqlcgen.ObsLatestBySeriesRow{}, pgx.ErrNoRows
	}
	return sqlcgen.ObsLatestBySeriesRow{SeriesCode: seriesCode, Source: o.source, ObservationDate: pgnum.Date(o.date), Value: pgnum.FromDecimal(o.value)}, nil
}

func (f *fakeQueries) ObsLatestBySeriesAndSource(_ context.Context, arg sqlcgen.ObsLatestBySeriesAndSourceParams) (sqlcgen.ObsLatestBySeriesAndSourceRow, error) {
	o, ok := f.latest(arg.SeriesCode, arg.Source)
	if !ok {
		return sqlcgen.ObsLatestBySeriesAndSourceRow{}, pgx.ErrNoRows
	}
	return sqlcgen.ObsLatestBySeriesAndSourceRow{SeriesCode: arg.SeriesCode, Source: o.source, ObservationDate: pgnum.Date(o.date), Value: pgnum.FromDecimal(o.value)}, nil
}

func (f *fakeQueries) ObsBySeriesAndDate(_ context.Context, arg sqlcgen.ObsBySeriesAndDateParams) (sqlcgen.ObsBySeriesAndDateRow, error) {
	o, ok := f.obs[obsKey(arg.SeriesCode, arg.ObservationDate.Time)]
	if !ok {
		return sqlcgen.ObsBySeriesAndDateRow{}, pgx.ErrNoRows
	}
	return sqlcgen.ObsBySeriesAndDateRow{SeriesCode: arg.SeriesCode, Source: o.source, ObservationDate: pgnum.Date(o.date), Value: pgnum.FromDecimal(o.value)}, nil
}

func (f *fakeQueries) UpsertMacroState(_ context.Context, arg sqlcgen.UpsertMacroStateParams) (sqlcgen.MacroState, error) {
	st := sqlcgen.MacroState{
		AsOfDate: arg.AsOfDate, RiskOff: arg.RiskOff,
		NetLiqLatestUsdMln: arg.NetLiqLatestUsdMln, NetLiqYoy: arg.NetLiqYoy,
		NetLiqYoyAccel: arg.NetLiqYoyAccel, NetLiqRiskOff: arg.NetLiqRiskOff,
		GlobalM2LatestUsdMln: arg.GlobalM2LatestUsdMln, GlobalM2Yoy: arg.GlobalM2Yoy,
		GlobalM2YoyAccel: arg.GlobalM2YoyAccel, GlobalM2RiskOff: arg.GlobalM2RiskOff,
		GlobalM2Stale: arg.GlobalM2Stale, CompositeZ: arg.CompositeZ,
		SmoothedZ: arg.SmoothedZ, BlendFactor: arg.BlendFactor, ComputedAt: arg.ComputedAt,
	}
	f.states[startOfDayUTC(arg.AsOfDate.Time).Format("2006-01-02")] = st
	return st, nil
}

func (f *fakeQueries) MacroStateLatestByComputedAt(_ context.Context) (sqlcgen.MacroState, error) {
	var best *sqlcgen.MacroState
	for _, st := range f.states {
		if best == nil || st.ComputedAt.Time.After(best.ComputedAt.Time) {
			best = &st
		}
	}
	if best == nil {
		return sqlcgen.MacroState{}, pgx.ErrNoRows
	}
	return *best, nil
}

func (f *fakeQueries) MacroStateHistoryAsc(_ context.Context) ([]sqlcgen.MacroState, error) {
	out := []sqlcgen.MacroState{}
	for _, st := range f.states {
		out = append(out, st)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].AsOfDate.Time.Before(out[j-1].AsOfDate.Time); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func (f *fakeQueries) GetSettingByKey(_ context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	if f.settingsErr != nil {
		return sqlcgen.GetSettingByKeyRow{}, f.settingsErr
	}
	v, ok := f.settings[key]
	if !ok {
		return sqlcgen.GetSettingByKeyRow{}, pgx.ErrNoRows
	}
	return sqlcgen.GetSettingByKeyRow{Key: key, Value: v}, nil
}

func passthroughTransact(ctx context.Context, fn func(Queries) error) error {
	return fn(nil) // replaced per-test via closure below
}

func testService(fq *fakeQueries, fetcher *Fetcher) *Service {
	transact := func(_ context.Context, fn func(Queries) error) error { return fn(fq) }
	return NewService(fq, transact, fetcher, NewBenchmarkService())
}

func emptyFetcher() *Fetcher {
	return &Fetcher{Raw: []SeriesFetch{}, FX: map[string]FetchFunc{}, Tips: map[string]FetchFunc{}}
}

var _ = passthroughTransact

// ── Refresh pipeline ─────────────────────────────────────────────────────────────────────

func TestRefresh_PersistsRawDerivedAndState_Idempotent(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	today := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	fetcher := &Fetcher{
		Raw: []SeriesFetch{
			{seriesWALCL, SourceFred, stubSeries(mp("2026-06-24", "7000000"))},
			{seriesWDTGAL, SourceFred, stubSeries(mp("2026-06-24", "900000"))},
			{seriesRRPONTSYD, SourceFred, stubSeries(mp("2026-06-23", "3.5"))},
		},
		FX:   map[string]FetchFunc{},
		Tips: map[string]FetchFunc{},
	}
	svc := testService(fq, fetcher)

	dto, err := svc.refreshInTx(ctx, fq, svc.fetchAll(ctx), today)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// NETLIQ derived + persisted.
	nl, err := fq.ObsBySeriesAsc(ctx, SeriesNetLiq)
	if err != nil || len(nl) != 1 {
		t.Fatalf("expected 1 NETLIQ obs, got %v,%v", nl, err)
	}
	if v, _ := pgnum.ToDecimal(nl[0].Value); v.Cmp(decimal.RequireFromString("6096500")) != 0 {
		t.Fatalf("expected NETLIQ 6096500, got %s", v.String())
	}
	if nl[0].Source != SourceDerived {
		t.Fatalf("expected DERIVED source, got %s", nl[0].Source)
	}
	// State as-of the last netliq date (NOT today — releases lag).
	if dto.AsOfDate == nil || dto.AsOfDate.Format("2006-01-02") != "2026-06-24" {
		t.Fatalf("expected asOf 2026-06-24, got %v", dto.AsOfDate)
	}
	if dto.ComputedAt == nil {
		t.Fatalf("expected computedAt")
	}
	// No fresh CN/JP → stale flag + held riskOff=false (no previous state).
	if !dto.GlobalM2Stale {
		t.Fatalf("expected globalM2Stale=true with no CN/JP data")
	}
	if dto.RiskOff {
		t.Fatalf("expected held riskOff=false with no previous state")
	}
	// Settings default: derisk enabled when the row is missing.
	if !dto.DeriskEnabled {
		t.Fatalf("expected deriskEnabled default true")
	}

	// Second identical refresh → still exactly one state row (upsert by as_of_date).
	if _, err := svc.refreshInTx(ctx, fq, svc.fetchAll(ctx), today); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	hist, err := svc.History(ctx)
	if err != nil || len(hist) != 1 {
		t.Fatalf("expected 1 state row after two refreshes, got %v,%v", hist, err)
	}
}

func stubSeries(pts ...MacroPoint) FetchFunc {
	return func(context.Context) []MacroPoint { return pts }
}

// ── Manual upsert: freshest-wins at READ time, backbone reclaims at WRITE ────────────────

func TestManualUpsert_FreshestWinsReadTime_BackboneReclaimsOnRefresh(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	svc := testService(fq, emptyFetcher())
	autoDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	manualDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// Automated backbone row…
	if err := fq.UpsertObservation(ctx, sqlcgen.UpsertObservationParams{
		SeriesCode: SeriesM2CN, Source: SourceChinadata,
		ObservationDate: pgnum.Date(autoDate), Value: pgnum.FromDecimal(decimal.NewFromInt(353670000)),
	}); err != nil {
		t.Fatal(err)
	}
	// …then a newer MANUAL paste for the same series.
	if err := svc.UpsertManual(ctx, SeriesM2CN, manualDate, decimal.NewFromInt(356000000)); err != nil {
		t.Fatal(err)
	}
	// Read-time: the freshest observation (MANUAL June) wins regardless of source.
	latest, err := fq.ObsLatestBySeries(ctx, SeriesM2CN)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Source != SourceManual || latest.ObservationDate.Time.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("expected freshest MANUAL row to win, got %s @ %s", latest.Source, latest.ObservationDate.Time.Format("2006-01-02"))
	}
	// Value persisted at scale 4 (upsertOne parity with Java setScale).
	if v, _ := pgnum.ToDecimal(latest.Value); v.Cmp(decimal.RequireFromString("356000000.0000")) != 0 {
		t.Fatalf("expected scale-4 value, got %s", v.String())
	}

	// Next refresh whose backbone caught up to June RECLAIMS the month unconditionally
	// (backbone-over-MANUAL parity — last writer wins, no precedence at write time).
	fetcher := &Fetcher{
		Raw: []SeriesFetch{
			{SeriesM2CN, SourceChinadata, stubSeries(MacroPoint{Date: manualDate, Value: decimal.NewFromInt(356709760)})}},
		FX:   map[string]FetchFunc{},
		Tips: map[string]FetchFunc{},
	}
	svc2 := testService(fq, fetcher)
	if _, err := svc2.refreshInTx(ctx, fq, svc2.fetchAll(ctx), time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	row, err := fq.ObsBySeriesAndDate(ctx, sqlcgen.ObsBySeriesAndDateParams{SeriesCode: SeriesM2CN, ObservationDate: pgnum.Date(manualDate)})
	if err != nil {
		t.Fatal(err)
	}
	if row.Source != SourceChinadata {
		t.Fatalf("expected backbone to reclaim June, got source %s", row.Source)
	}
}

// ── ADR-068 tip precedence + plausibility ────────────────────────────────────────────────

func tipService(fq *fakeQueries, backboneDate time.Time, backboneValue decimal.Decimal, tips map[string]FetchFunc) *Service {
	_ = fq.UpsertObservation(context.Background(), sqlcgen.UpsertObservationParams{
		SeriesCode: SeriesM2CN, Source: SourceChinadata,
		ObservationDate: pgnum.Date(backboneDate), Value: pgnum.FromDecimal(backboneValue),
	})
	return testService(fq, &Fetcher{Raw: []SeriesFetch{}, FX: map[string]FetchFunc{}, Tips: tips})
}

func TestTipPrecedence_TradingEcBeatsYChartsForSameMonth(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	tipDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	svc := tipService(fq,
		time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(353670000),
		map[string]FetchFunc{
			SourceTradingEC: stubSeries(MacroPoint{Date: tipDate, Value: decimal.NewFromInt(356709760)}),
			SourceYCharts:   stubSeries(MacroPoint{Date: tipDate, Value: decimal.NewFromInt(356710843)}),
		})
	if err := svc.extendChinaM2Tip(ctx, fq, svc.fetchAll(ctx).tips, today); err != nil {
		t.Fatal(err)
	}
	row, err := fq.ObsBySeriesAndDate(ctx, sqlcgen.ObsBySeriesAndDateParams{SeriesCode: SeriesM2CN, ObservationDate: pgnum.Date(tipDate)})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := pgnum.ToDecimal(row.Value); v.Cmp(decimal.NewFromInt(356709760)) != 0 || row.Source != SourceTradingEC {
		t.Fatalf("expected TRADINGEC 356709760 to hold June, got %s %s", row.Source, v.String())
	}
}

func TestTipGuards_RejectImplausibleTips(t *testing.T) {
	ctx := context.Background()
	backboneDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	backboneValue := decimal.NewFromInt(353670000)
	cases := []struct {
		name  string
		date  time.Time
		value decimal.Decimal
	}{
		{"not strictly newer than backbone", backboneDate, decimal.NewFromInt(356709760)},
		{"future month", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(356709760)},
		{"beyond 18m gap", time.Date(2027, 12, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(356709760)},
		{"outside ±30% band (unit slip)", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(353670000000)},
		{"non-positive value", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), decimal.Zero},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fq := newFakeQueries()
			today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
			svc := tipService(fq, backboneDate, backboneValue, map[string]FetchFunc{
				SourceTradingEC: stubSeries(MacroPoint{Date: c.date, Value: c.value}),
			})
			if err := svc.extendChinaM2Tip(ctx, fq, svc.fetchAll(ctx).tips, today); err != nil {
				t.Fatal(err)
			}
			if _, err := fq.ObsBySeriesAndDate(ctx, sqlcgen.ObsBySeriesAndDateParams{SeriesCode: SeriesM2CN, ObservationDate: pgnum.Date(c.date)}); err == nil && !c.date.Equal(backboneDate) {
				t.Fatalf("tip should have been rejected but was persisted")
			}
		})
	}
}

// ── computed_at-vs-asOf trap + history ordering + settings ──────────────────────────────

func TestLatestState_OrderedByComputedAtNotAsOfDate(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	// A fallback row persisted "today" (asOf=today, computed first)…
	early := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	asOfToday := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	if _, err := fq.UpsertMacroState(ctx, sqlcgen.UpsertMacroStateParams{
		AsOfDate: pgnum.Date(asOfToday), RiskOff: true,
		ComputedAt: pgtype.Timestamptz{Time: early, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	// …then a REAL row keyed on an earlier legitimate as-of date, computed later.
	asOfReal := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	if _, err := fq.UpsertMacroState(ctx, sqlcgen.UpsertMacroStateParams{
		AsOfDate: pgnum.Date(asOfReal), RiskOff: false, BlendFactor: pgnum.FromDecimal(decimal.RequireFromString("0.31")),
		ComputedAt: pgtype.Timestamptz{Time: early.Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	svc := testService(fq, emptyFetcher())
	dto, err := svc.CurrentState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dto.AsOfDate.Format("2006-01-02") != "2026-07-24" || dto.RiskOff {
		t.Fatalf("expected the later-COMPUTED row (2026-07-24, riskOff=false), got %s riskOff=%v",
			dto.AsOfDate.Format("2006-01-02"), dto.RiskOff)
	}
	// History is ordered by as_of_date ASC (chart order), independent of computed_at.
	hist, err := svc.History(ctx)
	if err != nil || len(hist) != 2 {
		t.Fatalf("expected 2 history rows, got %v,%v", hist, err)
	}
	if hist[0].AsOfDate.Format("2006-01-02") != "2026-07-24" || hist[1].AsOfDate.Format("2006-01-02") != "2026-07-29" {
		t.Fatalf("history not asOf-ordered: %v %v", hist[0].AsOfDate, hist[1].AsOfDate)
	}
	if hist[0].BlendFactor == nil || hist[0].BlendFactor.Cmp(decimal.RequireFromString("0.31")) != 0 {
		t.Fatalf("expected blend 0.31 on first row, got %v", hist[0].BlendFactor)
	}
	if !hist[0].HasZone || hist[0].Zone != "MID_OFF" {
		t.Fatalf("expected MID_OFF zone for 0.31, got %q", hist[0].Zone)
	}
}

func TestDeriskEnabled_MissingMalformedAndFalse(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	svc := testService(fq, emptyFetcher())
	if !svc.deriskEnabled(ctx) {
		t.Fatalf("missing setting must default true")
	}
	fq.settings[settings.DefMacroDeriskEnabled.Key] = "false"
	if svc.deriskEnabled(ctx) {
		t.Fatalf("explicit false must disable")
	}
	fq.settings[settings.DefMacroDeriskEnabled.Key] = " TRUE " // parseBoolean semantics: trim + case-insensitive
	if !svc.deriskEnabled(ctx) {
		t.Fatalf("whitespace/case must still parse true")
	}
	fq.settings[settings.DefMacroDeriskEnabled.Key] = "yes" // malformed → false (Java code, not its javadoc)
	if svc.deriskEnabled(ctx) {
		t.Fatalf("malformed value must be false per Boolean.parseBoolean")
	}
}

// TestDeriskEnabled_ReadsWithoutTransactionAndFailsSafe pins task 182 B3 (F4.4): the single
// settings read needs no transaction, and every failure answers true (derisk on) — before, a
// failing transaction left the result at false while a failing read inside it answered true.
func TestDeriskEnabled_ReadsWithoutTransactionAndFailsSafe(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	failingTx := func(context.Context, func(Queries) error) error { return errors.New("begin: pool closed") }
	svc := NewService(fq, failingTx, emptyFetcher(), NewBenchmarkService())

	if !svc.deriskEnabled(ctx) {
		t.Fatalf("missing setting must default true, even when a transaction cannot start")
	}
	fq.settings[settings.DefMacroDeriskEnabled.Key] = "false"
	if svc.deriskEnabled(ctx) {
		t.Fatalf("explicit false must disable")
	}
	fq.settingsErr = errors.New("conn reset by peer")
	if !svc.deriskEnabled(ctx) {
		t.Fatalf("a failing settings read must fail safe to true")
	}
}

func TestCurrentState_NoRows_ReturnsDisabledDefault(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	fq.settings[settings.DefMacroDeriskEnabled.Key] = "true"
	svc := testService(fq, emptyFetcher())
	dto, err := svc.CurrentState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dto.AsOfDate != nil || dto.BlendFactor != nil || dto.ComputedAt != nil {
		t.Fatalf("expected nil state fields, got %+v", dto)
	}
	if dto.RiskOff || !dto.DeriskEnabled || !dto.GlobalM2Stale {
		t.Fatalf("expected riskOff=false/derisk=true/stale=true, got %+v", dto)
	}
	if dto.Components == nil {
		t.Fatalf("expected non-nil (possibly empty) components for wire []")
	}
}
