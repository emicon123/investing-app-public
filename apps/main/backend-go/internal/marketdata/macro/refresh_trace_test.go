package macro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

// Characterisation golden for refreshInTx (task 185 B4), captured before the extract-step
// refactor: the statement sequence (run-length summary + a digest of every statement with its
// arguments), the macro_state upsert, the log lines and the returned DTO, for a confirmable
// refresh (fresh China/Japan, a China tip) and a held one (stale China, previous risk_off).

type refreshTraceQueries struct {
	*fakeQueries
	calls []string
}

func (q *refreshTraceQueries) rec(format string, args ...any) {
	q.calls = append(q.calls, fmt.Sprintf(format, args...))
}

func (q *refreshTraceQueries) UpsertObservation(ctx context.Context, a sqlcgen.UpsertObservationParams) error {
	q.rec("UpsertObservation %s %s %s %s", a.SeriesCode, a.Source, a.ObservationDate.Time.Format("2006-01-02"), numText(a.Value))
	return q.fakeQueries.UpsertObservation(ctx, a)
}
func (q *refreshTraceQueries) ObsBySeriesAsc(ctx context.Context, series string) ([]sqlcgen.ObsBySeriesAscRow, error) {
	q.rec("ObsBySeriesAsc %s", series)
	return q.fakeQueries.ObsBySeriesAsc(ctx, series)
}
func (q *refreshTraceQueries) ObsLatestBySeries(ctx context.Context, series string) (sqlcgen.ObsLatestBySeriesRow, error) {
	q.rec("ObsLatestBySeries %s", series)
	return q.fakeQueries.ObsLatestBySeries(ctx, series)
}
func (q *refreshTraceQueries) ObsLatestBySeriesAndSource(ctx context.Context, a sqlcgen.ObsLatestBySeriesAndSourceParams) (sqlcgen.ObsLatestBySeriesAndSourceRow, error) {
	q.rec("ObsLatestBySeriesAndSource %s %s", a.SeriesCode, a.Source)
	return q.fakeQueries.ObsLatestBySeriesAndSource(ctx, a)
}
func (q *refreshTraceQueries) ObsBySeriesAndDate(ctx context.Context, a sqlcgen.ObsBySeriesAndDateParams) (sqlcgen.ObsBySeriesAndDateRow, error) {
	q.rec("ObsBySeriesAndDate %s %s", a.SeriesCode, a.ObservationDate.Time.Format("2006-01-02"))
	return q.fakeQueries.ObsBySeriesAndDate(ctx, a)
}
func (q *refreshTraceQueries) UpsertMacroState(ctx context.Context, a sqlcgen.UpsertMacroStateParams) (sqlcgen.MacroState, error) {
	q.rec("UpsertMacroState asOf=%s riskOff=%v nl=%s/%s/%s/%v gm2=%s/%s/%s/%v stale=%v z=%s/%s blend=%s", a.AsOfDate.Time.Format("2006-01-02"),
		a.RiskOff, numText(a.NetLiqLatestUsdMln), numText(a.NetLiqYoy), numText(a.NetLiqYoyAccel), boolText(a.NetLiqRiskOff),
		numText(a.GlobalM2LatestUsdMln), numText(a.GlobalM2Yoy), numText(a.GlobalM2YoyAccel), boolText(a.GlobalM2RiskOff),
		a.GlobalM2Stale, numText(a.CompositeZ), numText(a.SmoothedZ), numText(a.BlendFactor))
	return q.fakeQueries.UpsertMacroState(ctx, a)
}
func (q *refreshTraceQueries) MacroStateLatestByComputedAt(ctx context.Context) (sqlcgen.MacroState, error) {
	q.rec("MacroStateLatestByComputedAt")
	return q.fakeQueries.MacroStateLatestByComputedAt(ctx)
}
func (q *refreshTraceQueries) GetSettingByKey(ctx context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	q.rec("GetSettingByKey %s", key)
	return q.fakeQueries.GetSettingByKey(ctx, key)
}

func numText(n pgtype.Numeric) string {
	if !n.Valid {
		return "NULL"
	}
	return pgnum.ToDecimalOrZero(n).String()
}

func boolText(b *bool) string {
	if b == nil {
		return "NULL"
	}
	return fmt.Sprint(*b)
}

// storedDigest hashes the observation store a refresh leaves behind, in key order.
func storedDigest(fq *fakeQueries) string {
	keys := make([]string, 0, len(fq.obs))
	for k := range fq.obs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		o := fq.obs[k]
		fmt.Fprintf(&b, "%s|%s|%s\n", k, o.source, o.value.String())
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
}

// runLength summarises a trace by statement name: "UpsertObservation x80", ...
func runLength(calls []string) string {
	var out []string
	prev, n := "", 0
	flush := func() {
		if n > 0 {
			out = append(out, fmt.Sprintf("%s x%d", prev, n))
		}
	}
	for _, c := range calls {
		name, _, _ := strings.Cut(c, " ")
		if name != prev {
			flush()
			prev, n = name, 0
		}
		n++
	}
	flush()
	return strings.Join(out, "\n")
}

// monthlySeries is n month-start points ending at end, value = base + step*i + curve*i^2.
func monthlySeries(end string, n int, base, step, curve string) []MacroPoint {
	e, _ := time.Parse("2006-01-02", end)
	b, s, c := decimal.RequireFromString(base), decimal.RequireFromString(step), decimal.RequireFromString(curve)
	out := make([]MacroPoint, 0, n)
	for i := range n {
		d := e.AddDate(0, i-(n-1), 0)
		x := decimal.NewFromInt(int64(i))
		out = append(out, MacroPoint{Date: d, Value: b.Add(s.Mul(x)).Add(c.Mul(x).Mul(x))})
	}
	return out
}

// weeklySeries is n weekly points ending at end, value = base + step*i + curve*i^2.
func weeklySeries(end string, n int, base, step, curve string) []MacroPoint {
	e, _ := time.Parse("2006-01-02", end)
	b, s, c := decimal.RequireFromString(base), decimal.RequireFromString(step), decimal.RequireFromString(curve)
	out := make([]MacroPoint, 0, n)
	for i := range n {
		x := decimal.NewFromInt(int64(i))
		out = append(out, MacroPoint{Date: e.AddDate(0, 0, 7*(i-(n-1))), Value: b.Add(s.Mul(x)).Add(c.Mul(x).Mul(x))})
	}
	return out
}

func refreshFetcher(cnEnd string, tips map[string]FetchFunc) *Fetcher {
	return &Fetcher{
		Raw: []SeriesFetch{
			{seriesWALCL, SourceFred, stubSeries(weeklySeries("2026-06-24", 220, "7000000", "9000", "-25")...)},
			{seriesWDTGAL, SourceFred, stubSeries(weeklySeries("2026-06-24", 220, "900000", "1000", "0")...)},
			{seriesRRPONTSYD, SourceFred, stubSeries(weeklySeries("2026-06-24", 220, "350", "1", "0")...)},
			{seriesM2SL, SourceFred, stubSeries(monthlySeries("2026-06-01", 50, "21000", "60", "-1")...)},
			{seriesM2EA, SourceECB, stubSeries(monthlySeries("2026-06-01", 50, "15000000", "20000", "0")...)},
			{seriesM2UK, SourceBOE, stubSeries(monthlySeries("2026-05-01", 49, "3000000", "5000", "0")...)},
			{SeriesM2CN, SourceChinadata, stubSeries(monthlySeries(cnEnd, 47, "280000000", "1000000", "0")...)},
			{SeriesM2JP, SourceBOJ, stubSeries(monthlySeries("2026-06-01", 50, "1250000000", "2000000", "0")...)},
		},
		FX: map[string]FetchFunc{
			seriesEXUSEU: stubSeries(monthlySeries("2026-06-01", 50, "1.08", "0.001", "0")...),
			seriesEXUSUK: stubSeries(monthlySeries("2026-06-01", 50, "1.27", "0", "0")...),
			seriesEXCHUS: stubSeries(monthlySeries("2026-06-01", 50, "7.2", "0", "0")...),
			seriesEXJPUS: stubSeries(monthlySeries("2026-06-01", 50, "150", "0", "0")...),
		},
		Tips: tips,
	}
}

func TestRefreshInTx_TraceStateAndLogs_Golden(t *testing.T) {
	cases := []struct {
		name      string
		cnEnd     string
		tips      map[string]FetchFunc
		prevState bool // seed a previous macro_state row with risk_off=true
		summary   string
		digest    string
		stored    string // sha256 of every stored observation (series|date|source|value), sorted
		m2jp      int    // UpsertObservation calls for M2_JP in one refresh: the 50 fetched points, each written once
		state     string
		logs      string
		dto       string
	}{
		{
			name:  "fresh China/Japan with China tips: risk_off confirmed from both signals",
			cnEnd: "2026-06-01",
			tips: map[string]FetchFunc{
				SourceTradingEC: stubSeries(mp("2026-07-01", "318000000")),
				SourcePBC:       stubSeries(mp("2026-07-01", "318500000")),
				SourceYCharts:   stubSeries(mp("2026-07-01", "319000000")),
			},
			prevState: false,
			summary: `UpsertObservation x906
ObsLatestBySeriesAndSource x1
ObsBySeriesAndDate x1
UpsertObservation x1
ObsBySeriesAndDate x1
UpsertObservation x1
ObsBySeriesAndDate x1
ObsBySeriesAsc x3
UpsertObservation x220
ObsBySeriesAsc x5
UpsertObservation x47
ObsLatestBySeries x2
UpsertMacroState x1
GetSettingByKey x1
ObsLatestBySeries x7`,
			digest: "df32cf929f77c1381fc90dfc5cc9e4cbdc1756b5caa4c3dc2837300dcaf2921e",
			stored: "06fa67a159c512e74775f7e378886eb953faa77fa7d973984f1880cf3a208a29",
			m2jp:   50,
			state:  "UpsertMacroState asOf=2026-06-24 riskOff=true nl=6083975/-0.02214802/-0.00546826/true gm2=93844864.4445/0.02149232/-0.00075285/true stale=false z=1.38869384/1.13880779 blend=1",
			logs: `level=INFO msg="macro: China M2 tip extended" date=2026-07-01 source=TRADINGEC
level=INFO msg="macro: China M2 tip extended" date=2026-07-01 source=PBC
level=INFO msg="macro: state refreshed" asOf=2026-06-24 riskOff=true globalM2Stale=false
`,
			dto: "asOf=2026-06-24 blend=1 zone=RISK_ON/true riskOff=true derisk=true stale=false components=NETLIQ/DERIVED/2026-06-24/true,GLOBAL_M2_USD/DERIVED/2026-06-01/false,M2SL/FRED/2026-06-01/false,M2_EA/ECB/2026-06-01/false,M2_UK/BOE/2026-05-01/false,M2_CN/PBC/2026-07-01/false,M2_JP/BOJ/2026-06-01/false",
		},
		{
			name:      "stale China: risk_off held from the previous state",
			cnEnd:     "2026-03-01",
			tips:      map[string]FetchFunc{},
			prevState: true,
			summary: `UpsertObservation x906
ObsLatestBySeriesAndSource x1
ObsBySeriesAsc x3
UpsertObservation x220
ObsBySeriesAsc x5
UpsertObservation x47
ObsLatestBySeries x2
MacroStateLatestByComputedAt x1
UpsertMacroState x1
GetSettingByKey x1
ObsLatestBySeries x7`,
			digest: "c4543487c8217225198ab35d9fbebf7487fbe38d47279fb80c70a39f2427e10f",
			stored: "5da564a7f8c072a50d64aaa6f3b36597cd00db2c681a035f96bfb0fec4d5ada3",
			m2jp:   50,
			state:  "UpsertMacroState asOf=2026-06-24 riskOff=true nl=6083975/-0.02214802/-0.00546826/true gm2=97896464.4445/0.02214839/-0.00087481/true stale=true z=1.07174852/1.10417908 blend=1",
			logs: `level=INFO msg="macro: Global-M2 not freshly confirmable; holding last risk_off" manualStale=true riskOff=true
level=INFO msg="macro: state refreshed" asOf=2026-06-24 riskOff=true globalM2Stale=true
`,
			dto: "asOf=2026-06-24 blend=1 zone=RISK_ON/true riskOff=true derisk=true stale=true components=NETLIQ/DERIVED/2026-06-24/true,GLOBAL_M2_USD/DERIVED/2026-03-01/true,M2SL/FRED/2026-06-01/false,M2_EA/ECB/2026-06-01/false,M2_UK/BOE/2026-05-01/false,M2_CN/CHINADATA/2026-03-01/true,M2_JP/BOJ/2026-06-01/false",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{
				ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return a
				},
			})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			fq := newFakeQueries()
			if c.prevState {
				fq.states["2026-01-01"] = sqlcgen.MacroState{AsOfDate: pgnum.Date(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), RiskOff: true,
					ComputedAt: pgtype.Timestamptz{Time: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Valid: true}}
			}
			q := &refreshTraceQueries{fakeQueries: fq}
			svc := NewService(q, func(_ context.Context, fn func(Queries) error) error { return fn(q) }, refreshFetcher(c.cnEnd, c.tips), NewBenchmarkService())
			ctx := context.Background()
			today := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)

			dto, err := svc.refreshInTx(ctx, q, svc.fetchAll(ctx), today)
			if err != nil {
				t.Fatalf("refreshInTx: %v", err)
			}

			var state string
			for _, call := range q.calls {
				if strings.HasPrefix(call, "UpsertMacroState ") {
					state = call
				}
			}
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(q.calls, "\n"))))
			var comps []string
			for _, cs := range dto.Components {
				comps = append(comps, fmt.Sprintf("%s/%s/%s/%v", cs.SeriesCode, cs.Source, cs.LatestObservationDate.Format("2006-01-02"), cs.Stale))
			}
			gotDTO := fmt.Sprintf("asOf=%s blend=%v zone=%s/%v riskOff=%v derisk=%v stale=%v components=%s",
				dto.AsOfDate.Format("2006-01-02"), dto.BlendFactor, dto.Zone, dto.HasZone, dto.RiskOff, dto.DeriskEnabled, dto.GlobalM2Stale,
				strings.Join(comps, ","))

			if got := storedDigest(fq); got != c.stored {
				t.Errorf("stored observations digest = %s, want %s", got, c.stored)
			}
			m2jp := 0
			for _, call := range q.calls {
				if strings.HasPrefix(call, "UpsertObservation "+SeriesM2JP+" ") {
					m2jp++
				}
			}
			if m2jp != c.m2jp {
				t.Errorf("M2_JP upserts per refresh = %d, want %d (each fetched point written once, task 187 B5a)", m2jp, c.m2jp)
			}
			if got := runLength(q.calls); got != c.summary {
				t.Errorf("statement summary differs\n got:\n%s\nwant:\n%s", got, c.summary)
			}
			if digest != c.digest {
				t.Errorf("statement digest = %s, want %s", digest, c.digest)
			}
			if state != c.state {
				t.Errorf("state upsert\n got: %s\nwant: %s", state, c.state)
			}
			if got := logs.String(); got != c.logs {
				t.Errorf("logs differ\n got:\n%s\nwant:\n%s", got, c.logs)
			}
			if gotDTO != c.dto {
				t.Errorf("dto\n got: %s\nwant: %s", gotDTO, c.dto)
			}
		})
	}
}
