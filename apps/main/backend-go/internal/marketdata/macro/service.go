package macro

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/settings"
	"investing-app/common-go/pgnum"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls — same interface-for- fakes
// convention as marketdata.Service (unit tests fake the DB; the testcontainers-go integration test
// exercises the real *sqlcgen.Queries).
type Queries interface {
	UpsertObservation(ctx context.Context, arg sqlcgen.UpsertObservationParams) error
	ObsBySeriesAsc(ctx context.Context, seriesCode string) ([]sqlcgen.ObsBySeriesAscRow, error)
	ObsLatestBySeries(ctx context.Context, seriesCode string) (sqlcgen.ObsLatestBySeriesRow, error)
	ObsLatestBySeriesAndSource(ctx context.Context, arg sqlcgen.ObsLatestBySeriesAndSourceParams) (sqlcgen.ObsLatestBySeriesAndSourceRow, error)
	ObsBySeriesAndDate(ctx context.Context, arg sqlcgen.ObsBySeriesAndDateParams) (sqlcgen.ObsBySeriesAndDateRow, error)
	UpsertMacroState(ctx context.Context, arg sqlcgen.UpsertMacroStateParams) (sqlcgen.MacroState, error)
	MacroStateLatestByComputedAt(ctx context.Context) (sqlcgen.MacroState, error)
	MacroStateHistoryAsc(ctx context.Context) ([]sqlcgen.MacroState, error)
	GetSettingByKey(ctx context.Context, key string) (sqlcgen.GetSettingByKeyRow, error)
}

// TransactFunc runs fn inside a single DB transaction — Refresh's whole persist phase (raw upserts,
// tip extension, derived upserts, state upsert) commits atomically. Production passes a pgx
// Begin/Commit closure; unit tests pass a direct call-through.
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// Service is the macro-state service layer: handler -> Service -> sqlc Queries. It holds the
// Fetcher fan-out (8 small fetch funcs, composed — doc 13 §5) plus the benchmark overlay
// service; the blend math itself lives in blend.go as pure functions.
type Service struct {
	queries   Queries
	transact  TransactFunc
	fetcher   *Fetcher
	benchmark *BenchmarkService
}

// NewService wires queries + transaction runner + fetch fan-out + benchmark overlay.
func NewService(queries Queries, transact TransactFunc, fetcher *Fetcher, benchmark *BenchmarkService) *Service {
	return &Service{queries: queries, transact: transact, fetcher: fetcher, benchmark: benchmark}
}

// ── Service-layer DTOs (free of sqlc row types and openapi response types) ────────────────

// StateDTO is the service-layer shape of MacroStateDto.
type StateDTO struct {
	AsOfDate         *time.Time
	BlendFactor      *decimal.Decimal
	Zone             string
	HasZone          bool
	CompositeZ       *decimal.Decimal
	SmoothedZ        *decimal.Decimal
	RiskOff          bool
	DeriskEnabled    bool
	NetLiqLatest     *decimal.Decimal
	NetLiqYoY        *decimal.Decimal
	NetLiqYoYAccel   *decimal.Decimal
	NetLiqRiskOff    *bool
	GlobalM2Latest   *decimal.Decimal
	GlobalM2YoY      *decimal.Decimal
	GlobalM2YoYAccel *decimal.Decimal
	GlobalM2RiskOff  *bool
	GlobalM2Stale    bool
	Components       []ComponentStatus
	ComputedAt       *time.Time
}

// HistoryPoint is the service-layer shape of MacroStateHistoryPoint.
type HistoryPoint struct {
	AsOfDate    time.Time
	BlendFactor *decimal.Decimal
	Zone        string
	HasZone     bool
}

// ── Reads ────────────────────────────────────────────────────────────────────────────────

// CurrentState returns the latest persisted state enriched with live settings + component
// freshness — the direct port of MacroDataService.currentStateDto. With no computed state yet
// it returns the disabled-looking default (riskOff=false, globalM2Stale=true).
func (s *Service) CurrentState(ctx context.Context) (StateDTO, error) {
	enabled := s.deriskEnabled(ctx)
	components, err := s.buildComponents(ctx, startOfDayUTC(warsawToday()))
	if err != nil {
		return StateDTO{}, err
	}
	state, err := s.queries.MacroStateLatestByComputedAt(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StateDTO{RiskOff: false, DeriskEnabled: enabled, GlobalM2Stale: true, Components: components}, nil
		}
		return StateDTO{}, fmt.Errorf("macro: latest state: %w", err)
	}
	return s.toDTO(state, enabled, components), nil
}

// History returns every persisted macro_state row, asOfDate ascending — the direct port of
// MacroDataService.historyDtos (ADR-069 chart shape).
func (s *Service) History(ctx context.Context) ([]HistoryPoint, error) {
	rows, err := s.queries.MacroStateHistoryAsc(ctx)
	if err != nil {
		return nil, fmt.Errorf("macro: state history: %w", err)
	}
	out := make([]HistoryPoint, 0, len(rows))
	for _, r := range rows {
		blend := numericPtr(r.BlendFactor)
		zone, hasZone := zoneFromBlend(blend)
		out = append(out, HistoryPoint{AsOfDate: startOfDayUTC(r.AsOfDate.Time), BlendFactor: blend, Zone: zone, HasZone: hasZone})
	}
	return out, nil
}

// UpsertManual ports MacroDataService.upsertManual: one MANUAL observation, no recompute —
// callers follow with Refresh. (Series-code validation lives in the handler per the 422 spec.)
func (s *Service) UpsertManual(ctx context.Context, series string, date time.Time, value decimal.Decimal) error {
	return s.upsertOne(ctx, s.queries, series, SourceManual, startOfDayUTC(date), value)
}

// BenchmarkHistory delegates to the 12h-cached Yahoo weekly overlay.
func (s *Service) BenchmarkHistory(ctx context.Context, symbol string) ([]BenchmarkPoint, error) {
	return s.benchmark.Get(ctx, symbol)
}

// ── Refresh (fetch outside, persist in ONE transaction) ──────────────────────────────────

// Refresh ports MacroDataService.refresh: fetch all series fail-soft, then in a single
// transaction upsert raw + tips, derive NETLIQ/GLOBAL_M2_USD, compute YoY/accel/blend, and
// upsert macro_state. Returns the fresh state DTO (refreshStateDto).
func (s *Service) Refresh(ctx context.Context) (StateDTO, error) {
	fetched := s.fetchAll(ctx)
	var dto StateDTO
	err := s.transact(ctx, func(q Queries) error {
		var err error
		dto, err = s.refreshInTx(ctx, q, fetched, startOfDayUTC(warsawToday()))
		return err
	})
	if err != nil {
		return StateDTO{}, err
	}
	return dto, nil
}

// fetchedAll is one refresh's network input — everything below is pure DB + math.
type fetchedAll struct {
	raw  map[string][]MacroPoint
	fx   map[string][]MacroPoint
	tips map[string][]MacroPoint
}

func (s *Service) fetchAll(ctx context.Context) fetchedAll {
	f := fetchedAll{raw: map[string][]MacroPoint{}, fx: map[string][]MacroPoint{}, tips: map[string][]MacroPoint{}}
	if s.fetcher == nil {
		return f
	}
	for _, sf := range s.fetcher.Raw {
		f.raw[sf.Series] = sf.Fetch(ctx)
	}
	for series, fetch := range s.fetcher.FX {
		f.fx[series] = fetch(ctx)
	}
	for source, fetch := range s.fetcher.Tips {
		f.tips[source] = fetch(ctx)
	}
	return f
}

// refreshInTx is Refresh's persist phase, in order: the fetched raw series and China tips
// (persistFetched), the derived NETLIQ/GLOBAL_M2_USD series (deriveAndPersist), their YoY /
// acceleration signals, the risk-off decision, the blend, and the macro_state upsert.
func (s *Service) refreshInTx(ctx context.Context, q Queries, fetched fetchedAll, today time.Time) (StateDTO, error) {
	if err := s.persistFetched(ctx, q, fetched, today); err != nil {
		return StateDTO{}, err
	}
	derived, err := s.deriveAndPersist(ctx, q, fetched)
	if err != nil {
		return StateDTO{}, err
	}

	// 5. Net-liquidity YoY + 3-month acceleration; 6. Global-M2 YoY + 3-month acceleration.
	netLiq, asOf, hasAsOf := netLiqSignals(derived.netliq)
	globalM2 := globalM2Signals(derived.globalM2, derived.globalM2UK)

	riskOff, canConfirm, err := s.resolveRiskOff(ctx, q, today, netLiq.riskOff, globalM2.riskOff)
	if err != nil {
		return StateDTO{}, err
	}

	// 8. ADR-044 blend series from the raw NETLIQ/GLOBAL_M2_USD series (no dependency on
	//    previously-persisted macro_state rows).
	blendSeries := computeBlendSeries(derived.netliq, derived.globalM2, derived.globalM2UK)
	var latestBlend *BlendPoint
	if len(blendSeries) > 0 {
		latestBlend = &blendSeries[len(blendSeries)-1]
	}

	// 9. Upsert macro_state.
	stateDate := today
	if hasAsOf {
		stateDate = startOfDayUTC(asOf)
	}
	saved, err := upsertState(ctx, q, stateDate, refreshedState{
		riskOff: riskOff, canConfirm: canConfirm, netLiq: netLiq, globalM2: globalM2, blend: latestBlend,
	})
	if err != nil {
		return StateDTO{}, err
	}

	enabled := s.deriskEnabledInTx(ctx, q)
	components, err := s.buildComponentsInTx(ctx, q, today)
	if err != nil {
		return StateDTO{}, err
	}
	return s.toDTO(saved, enabled, components), nil
}

// persistFetched writes one refresh's fetched observations.
func (s *Service) persistFetched(ctx context.Context, q Queries, fetched fetchedAll, today time.Time) error {
	// 1. Raw series (fail-soft: empty fetch leaves existing data untouched).
	for _, sf := range s.fetcher.Raw {
		if err := s.upsertSeries(ctx, q, sf.Series, sf.Source, fetched.raw[sf.Series]); err != nil {
			return err
		}
	}
	// 2. ADR-068 tip extension (backbone first — a caught-up chinadata.live reclaims months
	//    a scrape had been covering via the unconditional upsert above). M2_JP, like every raw
	//    series, is written once, in step 1: it has no tips.
	return s.extendChinaM2Tip(ctx, q, fetched.tips, today)
}

// derivedSeries is a refresh's derived input: NETLIQ and GLOBAL_M2_USD as just persisted, plus
// the UK leg (USD mln) of every Global-M2 month that includes it.
type derivedSeries struct {
	netliq, globalM2 []MacroPoint
	globalM2UK       map[yearMonth]decimal.Decimal
}

// deriveAndPersist (step 4) derives NETLIQ and GLOBAL_M2_USD from the just-persisted
// observations and upserts both.
func (s *Service) deriveAndPersist(ctx context.Context, q Queries, fetched fetchedAll) (derivedSeries, error) {
	walcl, err := s.load(ctx, q, seriesWALCL)
	if err != nil {
		return derivedSeries{}, err
	}
	wdtgal, err := s.load(ctx, q, seriesWDTGAL)
	if err != nil {
		return derivedSeries{}, err
	}
	rrp, err := s.load(ctx, q, seriesRRPONTSYD)
	if err != nil {
		return derivedSeries{}, err
	}
	netliq := computeNetLiq(walcl, wdtgal, rrp)
	if err := s.upsertSeries(ctx, q, SeriesNetLiq, SourceDerived, netliq); err != nil {
		return derivedSeries{}, err
	}
	m2us, err := s.load(ctx, q, seriesM2SL)
	if err != nil {
		return derivedSeries{}, err
	}
	m2ea, err := s.load(ctx, q, seriesM2EA)
	if err != nil {
		return derivedSeries{}, err
	}
	m2uk, err := s.load(ctx, q, seriesM2UK)
	if err != nil {
		return derivedSeries{}, err
	}
	m2cn, err := s.load(ctx, q, SeriesM2CN)
	if err != nil {
		return derivedSeries{}, err
	}
	m2jp, err := s.load(ctx, q, SeriesM2JP)
	if err != nil {
		return derivedSeries{}, err
	}
	globalM2, globalM2UK := computeGlobalM2(m2us, m2ea, m2uk, m2cn, m2jp,
		fetched.fx[seriesEXUSEU], fetched.fx[seriesEXUSUK], fetched.fx[seriesEXCHUS], fetched.fx[seriesEXJPUS])
	if err := s.upsertSeries(ctx, q, SeriesGlobalM2, SourceDerived, globalM2); err != nil {
		return derivedSeries{}, err
	}
	return derivedSeries{netliq: netliq, globalM2: globalM2, globalM2UK: globalM2UK}, nil
}

// liquiditySignals is one liquidity series' latest value, YoY, 3-month YoY acceleration and its
// risk-off vote (acceleration < 0); each is nil when the series is too short.
type liquiditySignals struct {
	latest, yoy, accel *decimal.Decimal
	riskOff            *bool
}

// netLiqSignals computes the net-liquidity signals on the 364-day YoY / 91-day acceleration
// windows, plus the series' last date (the state's as-of date).
func netLiqSignals(netliq []MacroPoint) (liquiditySignals, time.Time, bool) {
	var sig liquiditySignals
	asOf, hasAsOf := lastDate(netliq)
	if v, ok := lastValue(netliq); ok {
		sig.latest = &v
	}
	if hasAsOf {
		var yoyPrev *decimal.Decimal
		if v, ok := yoy(netliq, asOf, yoyDays); ok {
			sig.yoy = &v
		}
		if v, ok := yoy(netliq, asOf.AddDate(0, 0, -accelDays), yoyDays); ok {
			yoyPrev = &v
		}
		if sig.yoy != nil && yoyPrev != nil {
			a := sig.yoy.Sub(*yoyPrev)
			sig.accel = &a
		}
	}
	sig.riskOff = negativeVote(sig.accel)
	return sig, asOf, hasAsOf
}

// globalM2Signals computes the Global-M2 signals on the 12-month YoY / 3-month acceleration
// windows (UK basket kept consistent).
func globalM2Signals(globalM2 []MacroPoint, globalM2UK map[yearMonth]decimal.Decimal) liquiditySignals {
	var sig liquiditySignals
	if v, ok := lastValue(globalM2); ok {
		sig.latest = &v
	}
	if v, ok := yoyMonthly(globalM2, globalM2UK, m2YoYMonths); ok {
		sig.yoy = &v
	}
	if v, ok := yoyAccelMonthly(globalM2, globalM2UK, m2YoYMonths, m2AccelMonths); ok {
		sig.accel = &v
	}
	sig.riskOff = negativeVote(sig.accel)
	return sig
}

func negativeVote(accel *decimal.Decimal) *bool {
	if accel == nil {
		return nil
	}
	b := accel.Sign() < 0
	return &b
}

// resolveRiskOff is step 7: risk-off needs a fresh Global-M2 confirmation (both votes), else the
// last persisted state's risk_off is HELD (telemetry only since ADR-044 — blendFactor drives the
// gate). canConfirm=false is persisted as global_m2_stale.
func (s *Service) resolveRiskOff(ctx context.Context, q Queries, today time.Time, nlRiskOff, gm2RiskOff *bool) (riskOff, canConfirm bool, err error) {
	manualStale, err := s.globalM2CoreStale(ctx, q, today)
	if err != nil {
		return false, false, err
	}
	canConfirm = !manualStale && gm2RiskOff != nil
	if canConfirm {
		return nlRiskOff != nil && *nlRiskOff && *gm2RiskOff, true, nil
	}
	prev, err := q.MacroStateLatestByComputedAt(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, false, fmt.Errorf("macro: previous state: %w", err)
	}
	if err == nil {
		riskOff = prev.RiskOff
	}
	slog.Info("macro: Global-M2 not freshly confirmable; holding last risk_off",
		"manualStale", manualStale, "riskOff", riskOff)
	return riskOff, false, nil
}

// refreshedState is everything one refresh persists into macro_state besides its date.
type refreshedState struct {
	riskOff, canConfirm bool
	netLiq, globalM2    liquiditySignals
	blend               *BlendPoint
}

// upsertState writes the macro_state row for stateDate.
func upsertState(ctx context.Context, q Queries, stateDate time.Time, st refreshedState) (sqlcgen.MacroState, error) {
	saved, err := q.UpsertMacroState(ctx, sqlcgen.UpsertMacroStateParams{
		AsOfDate:             pgnum.Date(stateDate),
		RiskOff:              st.riskOff,
		NetLiqLatestUsdMln:   numericScale4(st.netLiq.latest),
		NetLiqYoy:            numericScale8(st.netLiq.yoy),
		NetLiqYoyAccel:       numericScale8(st.netLiq.accel),
		NetLiqRiskOff:        st.netLiq.riskOff,
		GlobalM2LatestUsdMln: numericScale4(st.globalM2.latest),
		GlobalM2Yoy:          numericScale8(st.globalM2.yoy),
		GlobalM2YoyAccel:     numericScale8(st.globalM2.accel),
		GlobalM2RiskOff:      st.globalM2.riskOff,
		GlobalM2Stale:        !st.canConfirm,
		CompositeZ:           blendNumericScale8(st.blend, func(b *BlendPoint) *decimal.Decimal { return b.CompositeZ }),
		SmoothedZ:            blendNumericScale8(st.blend, func(b *BlendPoint) *decimal.Decimal { return b.SmoothedZ }),
		BlendFactor:          blendNumericScale6(st.blend),
		ComputedAt:           pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	})
	if err != nil {
		return sqlcgen.MacroState{}, fmt.Errorf("macro: upsert state: %w", err)
	}
	slog.Info("macro: state refreshed", "asOf", stateDate.Format("2006-01-02"),
		"riskOff", st.riskOff, "globalM2Stale", saved.GlobalM2Stale)
	return saved, nil
}

// ── ADR-068 tip extension ────────────────────────────────────────────────────────────────

// extendChinaM2Tip ports MacroDataService.extendChinaM2Tip: scraped tips only ever write
// months STRICTLY NEWER than the newest CHINADATA backbone row. Tip iteration order
// (TRADINGEC, PBC, YCHARTS) mirrors ChinaM2Chain.fetchTips' LinkedHashMap order.
func (s *Service) extendChinaM2Tip(ctx context.Context, q Queries, tips map[string][]MacroPoint, today time.Time) error {
	backbone, err := q.ObsLatestBySeriesAndSource(ctx, sqlcgen.ObsLatestBySeriesAndSourceParams{
		SeriesCode: SeriesM2CN, Source: SourceChinadata,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // no backbone: nothing to sanity-check a scrape against
		}
		return fmt.Errorf("macro: china backbone: %w", err)
	}
	backboneDate := startOfDayUTC(backbone.ObservationDate.Time)
	backboneValue, err := pgnum.ToDecimal(backbone.Value)
	if err != nil {
		return fmt.Errorf("macro: china backbone value: %w", err)
	}
	for _, source := range []string{SourceTradingEC, SourcePBC, SourceYCharts} {
		for _, p := range tips[source] {
			if !isPlausibleChinaM2Tip(source, p, backboneDate, backboneValue, today) {
				continue
			}
			existing, err := q.ObsBySeriesAndDate(ctx, sqlcgen.ObsBySeriesAndDateParams{
				SeriesCode: SeriesM2CN, ObservationDate: pgnum.Date(p.Date),
			})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("macro: china tip existing: %w", err)
			}
			if err == nil && !outranks(source, existing.Source) {
				continue
			}
			if err := s.upsertOne(ctx, q, SeriesM2CN, source, p.Date, p.Value); err != nil {
				return err
			}
			slog.Info("macro: China M2 tip extended", "date", p.Date.Format("2006-01-02"), "source", source)
		}
	}
	return nil
}

// outranks ports MacroDataService.outranks: candidate may overwrite incumbent only when its
// precedence is at least as good (lower number wins; unknown sources never win).
func outranks(candidate, incumbent string) bool {
	c, ok := m2CNSourcePrecedence[candidate]
	if !ok {
		c = int(^uint(0) >> 1) // MaxInt
	}
	i, ok := m2CNSourcePrecedence[incumbent]
	if !ok {
		i = int(^uint(0) >> 1)
	}
	return c <= i
}

// isPlausibleChinaM2Tip ports MacroDataService.isPlausibleChinaM2Tip: strictly newer than the
// backbone, never a future month, ≤18 months past the backbone, within ±30% of it.
func isPlausibleChinaM2Tip(source string, p MacroPoint, backboneDate time.Time, backboneValue decimal.Decimal, today time.Time) bool {
	if p.Value.Sign() <= 0 {
		return false
	}
	if !startOfDayUTC(p.Date).After(backboneDate) {
		return false
	}
	y, m, _ := today.Date()
	currentMonth := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	if startOfDayUTC(p.Date).After(currentMonth) {
		return false
	}
	by, bm, _ := backboneDate.Date()
	py, pm, _ := startOfDayUTC(p.Date).Date()
	if (py-by)*12+(int(pm)-int(bm)) > m2CNTipMaxGapMonths {
		return false
	}
	drift := divHalfUp(p.Value.Sub(backboneValue).Abs(), backboneValue, 8)
	return drift.Cmp(m2CNTipTolerance) <= 0
}

// ── Persistence helpers ────────────────────────────────────────────────────────────────

func (s *Service) upsertSeries(ctx context.Context, q Queries, series, source string, points []MacroPoint) error {
	if len(points) == 0 {
		return nil
	}
	for _, p := range points {
		if err := s.upsertOne(ctx, q, series, source, p.Date, p.Value); err != nil {
			return err
		}
	}
	return nil
}

// upsertOne ports MacroDataService.upsertOne: unconditional (series, date) overwrite with the
// value at scale 4 HALF_UP — backbone-over-MANUAL parity is deliberate, not a bug.
func (s *Service) upsertOne(ctx context.Context, q Queries, series, source string, date time.Time, value decimal.Decimal) error {
	if err := q.UpsertObservation(ctx, sqlcgen.UpsertObservationParams{
		SeriesCode:      series,
		Source:          source,
		ObservationDate: pgnum.Date(startOfDayUTC(date)),
		Value:           pgnum.FromDecimal(roundHalfUp(value, 4)),
	}); err != nil {
		return fmt.Errorf("macro: upsert %s @ %s: %w", series, date.Format("2006-01-02"), err)
	}
	return nil
}

func (s *Service) load(ctx context.Context, q Queries, series string) ([]MacroPoint, error) {
	rows, err := q.ObsBySeriesAsc(ctx, series)
	if err != nil {
		return nil, fmt.Errorf("macro: load %s: %w", series, err)
	}
	out := make([]MacroPoint, 0, len(rows))
	for _, r := range rows {
		v, err := pgnum.ToDecimal(r.Value)
		if err != nil {
			return nil, fmt.Errorf("macro: decode %s @ %s: %w", series, r.ObservationDate.Time.Format("2006-01-02"), err)
		}
		out = append(out, MacroPoint{Date: startOfDayUTC(r.ObservationDate.Time), Value: v})
	}
	return out, nil
}

// globalM2CoreStale ports MacroDataService.isGlobalM2CoreStale: the China/Japan components
// (automated OR manual paste — freshest observation of any source wins at read time) must be
// within the 90-day monthly cadence.
func (s *Service) globalM2CoreStale(ctx context.Context, q Queries, today time.Time) (bool, error) {
	cnDate, err := s.latestDate(ctx, q, SeriesM2CN)
	if err != nil {
		return false, err
	}
	jpDate, err := s.latestDate(ctx, q, SeriesM2JP)
	if err != nil {
		return false, err
	}
	return isStale(cnDate, today, m2StaleDays) || isStale(jpDate, today, m2StaleDays), nil
}

func (s *Service) latestDate(ctx context.Context, q Queries, series string) (*time.Time, error) {
	row, err := q.ObsLatestBySeries(ctx, series)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("macro: latest %s: %w", series, err)
	}
	t := startOfDayUTC(row.ObservationDate.Time)
	return &t, nil
}

// ── DTO assembly ─────────────────────────────────────────────────────────────────────────

// deriskEnabled is the read path's single settings read: no transaction (task 182 / F4.4 — a
// failing transaction used to answer false, the opposite of the fail-safe below).
func (s *Service) deriskEnabled(ctx context.Context) bool {
	return s.deriskEnabledInTx(ctx, s.queries)
}

// deriskEnabledInTx reads the macro_derisk_enabled setting via the shared typed settings registry
// (internal/settings) instead of duplicating its missing→true/malformed→false parsing here — this
// package's Queries satisfies settings.SettingReader structurally, no adapter needed. Any
// non-ErrNoRows read failure still fails safe to true.
func (s *Service) deriskEnabledInTx(ctx context.Context, q Queries) bool {
	v, err := settings.Get(ctx, q, settings.DefMacroDeriskEnabled, settings.MacroDeriskEnabledFallback, false)
	if err != nil {
		return true
	}
	return v
}

func (s *Service) buildComponents(ctx context.Context, today time.Time) ([]ComponentStatus, error) {
	var out []ComponentStatus
	err := s.transact(ctx, func(q Queries) error {
		var err error
		out, err = s.buildComponentsInTx(ctx, q, today)
		return err
	})
	return out, err
}

func (s *Service) buildComponentsInTx(ctx context.Context, q Queries, today time.Time) ([]ComponentStatus, error) {
	out := []ComponentStatus{}
	for _, c := range componentCadences {
		row, err := q.ObsLatestBySeries(ctx, c.series)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, fmt.Errorf("macro: component %s: %w", c.series, err)
		}
		v, err := pgnum.ToDecimal(row.Value)
		if err != nil {
			return nil, fmt.Errorf("macro: component %s value: %w", c.series, err)
		}
		latest := startOfDayUTC(row.ObservationDate.Time)
		out = append(out, ComponentStatus{
			SeriesCode:            row.SeriesCode,
			Source:                row.Source,
			LatestObservationDate: latest,
			LatestValue:           v,
			Stale:                 isStale(&latest, today, c.maxDays),
		})
	}
	return out, nil
}

func (s *Service) toDTO(state sqlcgen.MacroState, enabled bool, components []ComponentStatus) StateDTO {
	blend := numericPtr(state.BlendFactor)
	zone, hasZone := zoneFromBlend(blend)
	asOf := startOfDayUTC(state.AsOfDate.Time)
	computed := state.ComputedAt.Time
	return StateDTO{
		AsOfDate:         &asOf,
		BlendFactor:      blend,
		Zone:             zone,
		HasZone:          hasZone,
		CompositeZ:       numericPtr(state.CompositeZ),
		SmoothedZ:        numericPtr(state.SmoothedZ),
		RiskOff:          state.RiskOff,
		DeriskEnabled:    enabled,
		NetLiqLatest:     numericPtr(state.NetLiqLatestUsdMln),
		NetLiqYoY:        numericPtr(state.NetLiqYoy),
		NetLiqYoYAccel:   numericPtr(state.NetLiqYoyAccel),
		NetLiqRiskOff:    state.NetLiqRiskOff,
		GlobalM2Latest:   numericPtr(state.GlobalM2LatestUsdMln),
		GlobalM2YoY:      numericPtr(state.GlobalM2Yoy),
		GlobalM2YoYAccel: numericPtr(state.GlobalM2YoyAccel),
		GlobalM2RiskOff:  state.GlobalM2RiskOff,
		GlobalM2Stale:    state.GlobalM2Stale,
		Components:       components,
		ComputedAt:       &computed,
	}
}

// ── pgtype/scale helpers ─────────────────────────────────────────────────────────────────

func numericPtr(n pgtype.Numeric) *decimal.Decimal {
	d, err := pgnum.ToDecimal(n)
	if err != nil {
		return nil
	}
	return &d
}

func numericScale4(v *decimal.Decimal) pgtype.Numeric {
	if v == nil {
		return pgtype.Numeric{}
	}
	return pgnum.FromDecimal(roundHalfUp(*v, 4))
}

func numericScale8(v *decimal.Decimal) pgtype.Numeric {
	if v == nil {
		return pgtype.Numeric{}
	}
	return pgnum.FromDecimal(roundHalfUp(*v, 8))
}

func blendNumericScale8(b *BlendPoint, pick func(*BlendPoint) *decimal.Decimal) pgtype.Numeric {
	if b == nil || pick(b) == nil {
		return pgtype.Numeric{}
	}
	return pgnum.FromDecimal(roundHalfUp(*pick(b), 8))
}

func blendNumericScale6(b *BlendPoint) pgtype.Numeric {
	if b == nil || b.Blend == nil {
		return pgtype.Numeric{}
	}
	return pgnum.FromDecimal(roundHalfUp(*b.Blend, 6))
}
