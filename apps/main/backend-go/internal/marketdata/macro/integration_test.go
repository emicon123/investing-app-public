package macro

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// TestMacroState_RealPostgresRoundTrip exercises macro_observations/macro_state reads and
// writes against a real postgres:17.10-alpine container, migrated by the same Flyway CLI image
// used in prod — the "never mock the database" gate, asserting against literally the same
// schema Java's MacroDataIT does. Covers: observation upsert round-trip incl. NUMERIC(20,4),
// the unconditional backbone-over-MANUAL overwrite, manual freshest-wins reads, the
// computed_at-vs-asOf latest-state trap, history ordering, and a full stub-fetch refresh
// through the REAL single-transaction runner.
func TestMacroState_RealPostgresRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers-go integration test in -short mode")
	}

	pg := testsupport.StartMigratedPostgres(t, "../../../../db/migration")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("connect to migrated postgres: %v", err)
	}
	defer pool.Close()

	queries := sqlcgen.New(pool)
	transact := pgtx.Runner(pool, func(tx pgx.Tx) Queries { return queries.WithTx(tx) })
	svc := NewService(queries, transact, emptyFetcher(), NewBenchmarkService())

	t.Run("observation upsert round-trips NUMERIC(20,4) exactly", func(t *testing.T) {
		date := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		if err := queries.UpsertObservation(ctx, sqlcgen.UpsertObservationParams{
			SeriesCode: SeriesM2CN, Source: SourceChinadata,
			ObservationDate: pgnum.Date(date), Value: pgnum.FromDecimal(decimal.RequireFromString("353670000.1234")),
		}); err != nil {
			t.Fatalf("UpsertObservation: %v", err)
		}
		rows, err := queries.ObsBySeriesAsc(ctx, SeriesM2CN)
		if err != nil || len(rows) != 1 {
			t.Fatalf("expected 1 row, got %v,%v", rows, err)
		}
		if v, _ := pgnum.ToDecimal(rows[0].Value); v.Cmp(decimal.RequireFromString("353670000.1234")) != 0 {
			t.Fatalf("expected 353670000.1234, got %s", v.String())
		}
	})

	t.Run("backbone overwrites MANUAL unconditionally (no precedence at write time)", func(t *testing.T) {
		date := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		manual := sqlcgen.UpsertObservationParams{SeriesCode: SeriesM2CN, Source: SourceManual,
			ObservationDate: pgnum.Date(date), Value: pgnum.FromDecimal(decimal.NewFromInt(356000000))}
		if err := queries.UpsertObservation(ctx, manual); err != nil {
			t.Fatal(err)
		}
		backbone := manual
		backbone.Source = SourceChinadata
		backbone.Value = pgnum.FromDecimal(decimal.NewFromInt(356709760))
		if err := queries.UpsertObservation(ctx, backbone); err != nil {
			t.Fatal(err)
		}
		row, err := queries.ObsBySeriesAndDate(ctx, sqlcgen.ObsBySeriesAndDateParams{SeriesCode: SeriesM2CN, ObservationDate: pgnum.Date(date)})
		if err != nil {
			t.Fatal(err)
		}
		if row.Source != SourceChinadata {
			t.Fatalf("expected CHINADATA to win unconditionally, got %s", row.Source)
		}
		if v, _ := pgnum.ToDecimal(row.Value); v.Cmp(decimal.NewFromInt(356709760)) != 0 {
			t.Fatalf("expected 356709760, got %s", v.String())
		}
	})

	t.Run("freshest observation wins at read time regardless of source", func(t *testing.T) {
		latest, err := queries.ObsLatestBySeries(ctx, SeriesM2CN)
		if err != nil {
			t.Fatal(err)
		}
		if got := latest.ObservationDate.Time.Format("2006-01-02"); got != "2026-06-01" {
			t.Fatalf("expected 2026-06-01, got %s", got)
		}
	})

	t.Run("latest state is by computed_at, history by as_of_date", func(t *testing.T) {
		early := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
		asOfToday := pgnum.Date(time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
		asOfReal := pgnum.Date(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC))
		if _, err := queries.UpsertMacroState(ctx, sqlcgen.UpsertMacroStateParams{
			AsOfDate: asOfToday, RiskOff: true,
			ComputedAt: pgtype.Timestamptz{Time: early, Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := queries.UpsertMacroState(ctx, sqlcgen.UpsertMacroStateParams{
			AsOfDate: asOfReal, RiskOff: false,
			BlendFactor: pgnum.FromDecimal(decimal.RequireFromString("0.31")),
			ComputedAt:  pgtype.Timestamptz{Time: early.Add(time.Hour), Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
		dto, err := svc.CurrentState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if dto.AsOfDate.Format("2006-01-02") != "2026-07-24" || dto.RiskOff {
			t.Fatalf("computed_at trap: expected 2026-07-24/riskOff=false, got %s/%v",
				dto.AsOfDate.Format("2006-01-02"), dto.RiskOff)
		}
		hist, err := svc.History(ctx)
		if err != nil || len(hist) != 2 {
			t.Fatalf("expected 2 history rows, got %v,%v", hist, err)
		}
		if hist[0].AsOfDate.Format("2006-01-02") != "2026-07-24" {
			t.Fatalf("history must be asOf-ascending, first=%s", hist[0].AsOfDate.Format("2006-01-02"))
		}
	})

	t.Run("settings default true, explicit false disables", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM settings WHERE key='rebalancing.macro_derisk_enabled'`); err != nil {
			t.Fatal(err)
		}
		if !svc.deriskEnabled(ctx) {
			t.Fatalf("missing setting must default true")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO settings(key,value) VALUES('rebalancing.macro_derisk_enabled','false')`); err != nil {
			t.Fatal(err)
		}
		if svc.deriskEnabled(ctx) {
			t.Fatalf("explicit false must disable")
		}
	})

	t.Run("full refresh through the real single transaction persists derived + state", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `TRUNCATE macro_state, macro_observations RESTART IDENTITY`); err != nil {
			t.Fatal(err)
		}
		stubSvc := NewService(queries, transact, &Fetcher{
			Raw: []SeriesFetch{
				{seriesWALCL, SourceFred, stubSeries(mp("2026-06-24", "7000000"))},
				{seriesWDTGAL, SourceFred, stubSeries(mp("2026-06-24", "900000"))},
				{seriesRRPONTSYD, SourceFred, stubSeries(mp("2026-06-23", "3.5"))},
			},
			FX:   map[string]FetchFunc{},
			Tips: map[string]FetchFunc{},
		}, NewBenchmarkService())
		dto, err := stubSvc.Refresh(ctx)
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if dto.AsOfDate.Format("2006-01-02") != "2026-06-24" {
			t.Fatalf("expected asOf 2026-06-24, got %s", dto.AsOfDate.Format("2006-01-02"))
		}
		nl, err := queries.ObsBySeriesAsc(ctx, SeriesNetLiq)
		if err != nil || len(nl) != 1 {
			t.Fatalf("expected 1 NETLIQ row, got %v,%v", nl, err)
		}
		if v, _ := pgnum.ToDecimal(nl[0].Value); v.Cmp(decimal.RequireFromString("6096500")) != 0 {
			t.Fatalf("expected NETLIQ 6096500, got %s", v.String())
		}
		state, err := queries.MacroStateLatestByComputedAt(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := pgnum.ToDecimal(state.NetLiqLatestUsdMln); v.Cmp(decimal.RequireFromString("6096500")) != 0 {
			t.Fatalf("expected state net_liq 6096500, got %s", v.String())
		}
	})
}
