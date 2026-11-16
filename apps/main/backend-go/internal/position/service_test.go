package position

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as prior phases
// (business logic table-tested here; real Postgres covered by integration_test.go).
// Task 156: assets live in their own map; positions rows carry asset_id/is_primary.
type fakeQueries struct {
	assets    map[int64]sqlcgen.Asset
	rows      map[int64]sqlcgen.Position
	nextID    int64
	nextAID   int64
	settings  map[string]string
	macro     *sqlcgen.MacroState
	latestRec pgtype.Timestamptz
	hasCash   bool
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{assets: map[int64]sqlcgen.Asset{}, rows: map[int64]sqlcgen.Position{}, nextID: 1, nextAID: 1, settings: map[string]string{}}
}

// Task 173: the fake keeps soft-deleted rows in the maps (DeletedAt.Valid) and applies the same
// live-only / any-state split as the real queries, so filtering bugs are not hidden here.
func (f *fakeQueries) ListAllPositions(_ context.Context) ([]sqlcgen.Position, error) {
	out := make([]sqlcgen.Position, 0, len(f.rows))
	for _, r := range f.rows {
		if !r.DeletedAt.Valid {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeQueries) ListAllAssets(_ context.Context) ([]sqlcgen.Asset, error) {
	out := make([]sqlcgen.Asset, 0, len(f.assets))
	for _, a := range f.assets {
		if !a.DeletedAt.Valid {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeQueries) GetPositionByID(_ context.Context, id int64) (sqlcgen.Position, error) {
	r, ok := f.rows[id]
	if !ok || r.DeletedAt.Valid {
		return sqlcgen.Position{}, pgx.ErrNoRows
	}
	return r, nil
}

func (f *fakeQueries) GetAssetByID(_ context.Context, id int64) (sqlcgen.Asset, error) {
	a, ok := f.assets[id]
	if !ok || a.DeletedAt.Valid {
		return sqlcgen.Asset{}, pgx.ErrNoRows
	}
	return a, nil
}

func (f *fakeQueries) GetAssetByIDAnyState(_ context.Context, id int64) (sqlcgen.Asset, error) {
	a, ok := f.assets[id]
	if !ok {
		return sqlcgen.Asset{}, pgx.ErrNoRows
	}
	return a, nil
}

func (f *fakeQueries) GetAssetByKeyAnyState(_ context.Context, arg sqlcgen.GetAssetByKeyAnyStateParams) (sqlcgen.Asset, error) {
	for _, a := range f.assets {
		if a.Key == arg.Key && a.Portfolio == arg.Portfolio {
			return a, nil
		}
	}
	return sqlcgen.Asset{}, pgx.ErrNoRows
}

func (f *fakeQueries) GetPositionByNaturalKeyAnyState(_ context.Context, arg sqlcgen.GetPositionByNaturalKeyAnyStateParams) (sqlcgen.Position, error) {
	for _, r := range f.rows {
		if r.Ticker == arg.Ticker && r.Exchange == arg.Exchange && r.Portfolio == arg.Portfolio {
			return r, nil
		}
	}
	return sqlcgen.Position{}, pgx.ErrNoRows
}

func (f *fakeQueries) RestoreAsset(_ context.Context, arg sqlcgen.RestoreAssetParams) (sqlcgen.Asset, error) {
	a, ok := f.assets[arg.ID]
	if !ok || !a.DeletedAt.Valid {
		return sqlcgen.Asset{}, pgx.ErrNoRows
	}
	a.DeletedAt = pgtype.Timestamptz{}
	a.Name, a.Exposure, a.AssetClass = arg.Name, arg.Exposure, arg.AssetClass
	if arg.TargetAllocationPct.Valid {
		a.TargetAllocationPct = arg.TargetAllocationPct
	}
	if arg.TargetAllocationPctRiskOff.Valid {
		a.TargetAllocationPctRiskOff = arg.TargetAllocationPctRiskOff
	}
	f.assets[arg.ID] = a
	return a, nil
}

func (f *fakeQueries) RestorePosition(_ context.Context, arg sqlcgen.RestorePositionParams) (sqlcgen.Position, error) {
	r, ok := f.rows[arg.ID]
	if !ok || !r.DeletedAt.Valid {
		return sqlcgen.Position{}, pgx.ErrNoRows
	}
	r.DeletedAt = pgtype.Timestamptz{}
	r.AssetID, r.IsPrimary, r.Name, r.AssetClass, r.PriceCurrency = arg.AssetID, arg.IsPrimary, arg.Name, arg.AssetClass, arg.PriceCurrency
	if arg.Exposure != nil {
		r.Exposure = *arg.Exposure
	}
	if arg.YahooTicker != nil {
		r.YahooTicker = arg.YahooTicker
	}
	if arg.PurchaseCurrency != nil {
		r.PurchaseCurrency = *arg.PurchaseCurrency
	}
	if arg.CompanyBacked != nil {
		r.CompanyBacked = *arg.CompanyBacked
	}
	f.rows[arg.ID] = r
	return r, nil
}

// liveRows / liveAssets count the rows a live-only query would see.
func (f *fakeQueries) liveRows() int {
	n := 0
	for _, r := range f.rows {
		if !r.DeletedAt.Valid {
			n++
		}
	}
	return n
}

func (f *fakeQueries) liveAssets() int {
	n := 0
	for _, a := range f.assets {
		if !a.DeletedAt.Valid {
			n++
		}
	}
	return n
}

// InsertAsset mirrors assets_key_portfolio_uq's real UNIQUE constraint (key, portfolio) so
// service/handler tests exercise the same 23505 -> 409 mapping production hits.
func (f *fakeQueries) InsertAsset(_ context.Context, arg sqlcgen.InsertAssetParams) (sqlcgen.Asset, error) {
	for _, a := range f.assets {
		if a.Key == arg.Key && a.Portfolio == arg.Portfolio {
			return sqlcgen.Asset{}, &pgconn.PgError{Code: "23505", Message: `duplicate key value violates unique constraint "assets_key_portfolio_uq"`}
		}
	}
	id := f.nextAID
	f.nextAID++
	a := sqlcgen.Asset{
		ID: id, Portfolio: arg.Portfolio, Key: arg.Key, Name: arg.Name,
		Exposure: arg.Exposure, AssetClass: arg.AssetClass,
		TargetAllocationPct: arg.TargetAllocationPct, TargetAllocationPctRiskOff: arg.TargetAllocationPctRiskOff,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	f.assets[id] = a
	return a, nil
}

// InsertPosition mirrors positions_uq's real composite UNIQUE constraint
// (ticker, exchange, portfolio) — apps/main/db/migration/V1__init.sql — so
// service/handler tests can exercise the same 23505 -> 409 mapping production hits.
func (f *fakeQueries) InsertPosition(_ context.Context, arg sqlcgen.InsertPositionParams) (sqlcgen.Position, error) {
	for _, r := range f.rows {
		if r.Ticker == arg.Ticker && r.Exchange == arg.Exchange && r.Portfolio == arg.Portfolio {
			return sqlcgen.Position{}, &pgconn.PgError{Code: "23505", Message: `duplicate key value violates unique constraint "positions_uq"`}
		}
	}
	id := f.nextID
	f.nextID++
	row := sqlcgen.Position{
		ID: id, Ticker: arg.Ticker, Name: arg.Name, Portfolio: arg.Portfolio, Exchange: arg.Exchange,
		AssetClass: arg.AssetClass, Exposure: arg.Exposure, PriceCurrency: arg.PriceCurrency, PurchaseCurrency: arg.PurchaseCurrency,
		UnitsIke: arg.UnitsIke, UnitsIkze: arg.UnitsIkze, UnitsRegularEur: arg.UnitsRegularEur,
		UnitsRegularUsd: arg.UnitsRegularUsd, UnitsRegularPln: arg.UnitsRegularPln,
		AssetID: arg.AssetID, IsPrimary: arg.IsPrimary, YahooTicker: arg.YahooTicker,
		CompanyBacked: arg.CompanyBacked, CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	f.rows[id] = row
	return row, nil
}

func (f *fakeQueries) UpdatePositionFields(_ context.Context, arg sqlcgen.UpdatePositionFieldsParams) (sqlcgen.Position, error) {
	row, ok := f.rows[arg.ID]
	if !ok {
		return sqlcgen.Position{}, pgx.ErrNoRows
	}
	row.UnitsIke = arg.UnitsIke
	row.UnitsIkze = arg.UnitsIkze
	row.UnitsRegularEur = arg.UnitsRegularEur
	row.UnitsRegularUsd = arg.UnitsRegularUsd
	row.UnitsRegularPln = arg.UnitsRegularPln
	row.TotalSpentPln = arg.TotalSpentPln
	row.AvgBuyPrice = arg.AvgBuyPrice
	row.Name = arg.Name
	row.AssetClass = arg.AssetClass
	row.YahooTicker = arg.YahooTicker
	row.CompanyBacked = arg.CompanyBacked
	row.AssetID = arg.AssetID
	f.rows[arg.ID] = row
	return row, nil
}

func (f *fakeQueries) UpdatePositionDelta(_ context.Context, arg sqlcgen.UpdatePositionDeltaParams) (sqlcgen.Position, error) {
	row, ok := f.rows[arg.ID]
	if !ok {
		return sqlcgen.Position{}, pgx.ErrNoRows
	}
	row.UnitsIke, row.UnitsIkze = arg.UnitsIke, arg.UnitsIkze
	row.UnitsRegularEur, row.UnitsRegularUsd, row.UnitsRegularPln = arg.UnitsRegularEur, arg.UnitsRegularUsd, arg.UnitsRegularPln
	row.TotalSpentPln = arg.TotalSpentPln
	f.rows[arg.ID] = row
	return row, nil
}

func (f *fakeQueries) UpdateAssetFields(_ context.Context, arg sqlcgen.UpdateAssetFieldsParams) (sqlcgen.Asset, error) {
	a, ok := f.assets[arg.ID]
	if !ok {
		return sqlcgen.Asset{}, pgx.ErrNoRows
	}
	a.TargetAllocationPct = arg.TargetAllocationPct
	a.TargetAllocationPctRiskOff = arg.TargetAllocationPctRiskOff
	a.Name = arg.Name
	f.assets[arg.ID] = a
	return a, nil
}

var fakeNow = pgtype.Timestamptz{Time: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC), Valid: true}

func (f *fakeQueries) SoftDeletePosition(_ context.Context, id int64) error {
	if r, ok := f.rows[id]; ok && !r.DeletedAt.Valid {
		r.DeletedAt = fakeNow
		f.rows[id] = r
	}
	return nil
}

func (f *fakeQueries) SoftDeleteAsset(_ context.Context, id int64) error {
	a, ok := f.assets[id]
	if !ok || a.DeletedAt.Valid {
		return nil
	}
	a.DeletedAt = fakeNow
	f.assets[id] = a
	for pid, p := range f.rows {
		if p.AssetID == id && !p.DeletedAt.Valid {
			p.DeletedAt = fakeNow
			f.rows[pid] = p
		}
	}
	return nil
}

func (f *fakeQueries) ClearAssetPrimary(_ context.Context, assetID int64) error {
	for pid, p := range f.rows {
		if p.AssetID == assetID && p.IsPrimary {
			p.IsPrimary = false
			f.rows[pid] = p
		}
	}
	return nil
}

func (f *fakeQueries) SetSubassetPrimary(_ context.Context, arg sqlcgen.SetSubassetPrimaryParams) (sqlcgen.Position, error) {
	p, ok := f.rows[arg.ID]
	if !ok || p.AssetID != arg.AssetID || p.DeletedAt.Valid {
		return sqlcgen.Position{}, pgx.ErrNoRows
	}
	p.IsPrimary = true
	f.rows[arg.ID] = p
	return p, nil
}

func (f *fakeQueries) GetSettingByKey(_ context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	v, ok := f.settings[key]
	if !ok {
		return sqlcgen.GetSettingByKeyRow{}, pgx.ErrNoRows
	}
	return sqlcgen.GetSettingByKeyRow{Key: key, Value: v}, nil
}

func (f *fakeQueries) MacroStateLatestByComputedAt(_ context.Context) (sqlcgen.MacroState, error) {
	if f.macro == nil {
		return sqlcgen.MacroState{}, pgx.ErrNoRows
	}
	return *f.macro, nil
}

func (f *fakeQueries) LatestCashRecordedAt(_ context.Context) (pgtype.Timestamptz, error) {
	if !f.hasCash {
		return pgtype.Timestamptz{}, pgx.ErrNoRows
	}
	return f.latestRec, nil
}

func (f *fakeQueries) CashPositionsByRecordedAt(_ context.Context, _ pgtype.Timestamptz) ([]sqlcgen.CashPosition, error) {
	return nil, nil
}

// fakePrices is a stand-in for PriceSource — cache-only lookups, no live fetch, matching the
// service-layer test convention used across other Go phases.
type fakePrices struct {
	pricePlnByTicker map[string]decimal.Decimal
	rates            map[string]decimal.Decimal
}

func newFakePrices() *fakePrices {
	return &fakePrices{pricePlnByTicker: map[string]decimal.Decimal{}, rates: map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1)}}
}

func (f *fakePrices) PricePlnByYahooTicker(_ context.Context) (map[string]decimal.Decimal, error) {
	return f.pricePlnByTicker, nil
}
func (f *fakePrices) AllRatesToPln(_ context.Context) (map[string]decimal.Decimal, error) {
	return f.rates, nil
}
func (f *fakePrices) RateToPln(_ context.Context, currency string) (decimal.Decimal, error) {
	if v, ok := f.rates[currency]; ok {
		return v, nil
	}
	return decimal.Zero, httpx.NewServiceUnavailableError("no rate for " + currency)
}

// putAsset seeds one asset row directly (bypassing the service). targetPct is the
// asset-level target ("" = NULL/no target).
func (f *fakeQueries) putAsset(portfolio, key, targetPct string) int64 {
	id := f.nextAID
	f.nextAID++
	var target, targetRiskOff pgtype.Numeric
	if targetPct != "" {
		target = pgnum.FromDecimal(decimal.RequireFromString(targetPct))
	}
	f.assets[id] = sqlcgen.Asset{
		ID: id, Portfolio: sqlcgen.PortfolioEnum(portfolio), Key: key, Name: key,
		Exposure: sqlcgen.ExposureEnumCRYPTO, AssetClass: sqlcgen.AssetClassEnumCRYPTO,
		TargetAllocationPct: target, TargetAllocationPctRiskOff: targetRiskOff,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	return id
}

// putSub seeds one subasset row directly under an asset created by putAsset.
// unitsPln seeds UnitsRegularPln (10 in most tests, matching the old convention).
func (f *fakeQueries) putSub(assetID int64, ticker, portfolio, unitsPln, yahooTicker string, primary bool) int64 {
	id := f.nextID
	f.nextID++
	var yahoo *string
	if yahooTicker != "" {
		y := yahooTicker
		yahoo = &y
	}
	f.rows[id] = sqlcgen.Position{
		ID: id, Ticker: ticker, Name: ticker, Portfolio: sqlcgen.PortfolioEnum(portfolio),
		Exchange: sqlcgen.ExchangeEnumXTB, AssetClass: sqlcgen.AssetClassEnumCRYPTO, Exposure: sqlcgen.ExposureEnumCRYPTO,
		PriceCurrency: sqlcgen.CurrencyEnumPLN, PurchaseCurrency: sqlcgen.CurrencyEnumPLN,
		UnitsIke: pgnum.FromDecimal(decimal.Zero), UnitsIkze: pgnum.FromDecimal(decimal.Zero),
		UnitsRegularEur: pgnum.FromDecimal(decimal.Zero), UnitsRegularUsd: pgnum.FromDecimal(decimal.Zero),
		UnitsRegularPln: pgnum.FromDecimal(decimal.RequireFromString(unitsPln)),
		AssetID:         assetID, IsPrimary: primary, YahooTicker: yahoo, CompanyBacked: true,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	return id
}

// setCost sets a subasset row's cost basis (total_spent_pln).
func (f *fakeQueries) setCost(id int64, spent string) {
	r := f.rows[id]
	r.TotalSpentPln = pgnum.FromDecimal(decimal.RequireFromString(spent))
	f.rows[id] = r
}

func testAsset(t *testing.T, fq *fakeQueries, assetID int64) assetInfo {
	t.Helper()
	a, err := decodeAsset(fq.assets[assetID])
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func testSubs(t *testing.T, fq *fakeQueries, assetID int64) []decoded {
	t.Helper()
	assets, err := decodeAssets([]sqlcgen.Asset{fq.assets[assetID]})
	if err != nil {
		t.Fatal(err)
	}
	var rows []sqlcgen.Position
	for _, r := range fq.rows {
		if r.AssetID == assetID {
			rows = append(rows, r)
		}
	}
	subs, err := decodeRows(rows, assets)
	if err != nil {
		t.Fatal(err)
	}
	return subs
}

func TestComputeFields_DegradesOnMissingPrice(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "X", "50")
	fq.putSub(aid, "X", "MAIN", "10", "MISSING.YAHOO", true)
	a := testAsset(t, fq, aid)
	subs := testSubs(t, fq, aid)
	svc := &Service{prices: newFakePrices()}
	computed, err := svc.computeFields(context.Background(), a, subs, map[string]decimal.Decimal{}, nil, map[string]decimal.Decimal{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if computed.PricePln != nil || computed.ValuePln != nil || computed.CurrentPct != nil {
		t.Fatalf("expected degraded (nil) fields except unitsTotal, got %+v", computed)
	}
	if computed.UnitsTotal == nil || !computed.UnitsTotal.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("expected unitsTotal=10 to survive degradation, got %v", computed.UnitsTotal)
	}
}

func TestComputeFields_ZeroTargetWithNonzeroValue_UsesSentinelDeviation(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "X", "0")
	fq.putSub(aid, "X", "MAIN", "10", "ZT.YAHOO", true)
	a := testAsset(t, fq, aid)
	subs := testSubs(t, fq, aid)
	prices := newFakePrices()
	prices.pricePlnByTicker["ZT.YAHOO"] = decimal.NewFromInt(5) // 10 units * 5 = 50 value, target=0
	prices.rates["PLN"] = decimal.NewFromInt(1)
	svc := &Service{prices: prices}
	totals := map[string]decimal.Decimal{"MAIN": decimal.NewFromInt(1000)}
	computed, err := svc.computeFields(context.Background(), a, subs, totals, nil, prices.pricePlnByTicker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if computed.DeviationPct == nil || !computed.DeviationPct.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("expected ADR-053 sentinel deviationPct=1, got %v", computed.DeviationPct)
	}
}

func TestComputeFields_FoldsSubassetValueIntoAsset(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "10")
	fq.putSub(aid, "PARENT", "MAIN", "10", "PARENT.YAHOO", true)
	fq.putSub(aid, "SUB", "MAIN", "4", "SUB.YAHOO", false)
	a := testAsset(t, fq, aid)
	subs := testSubs(t, fq, aid)
	prices := newFakePrices()
	prices.pricePlnByTicker["PARENT.YAHOO"] = decimal.NewFromInt(5) // 10*5 = 50
	prices.pricePlnByTicker["SUB.YAHOO"] = decimal.NewFromInt(2)    // 4*2 = 8
	prices.rates["PLN"] = decimal.NewFromInt(1)
	svc := &Service{prices: prices}
	totals := map[string]decimal.Decimal{"MAIN": decimal.NewFromInt(580)} // 58/580 = 10%
	computed, err := svc.computeFields(context.Background(), a, subs, totals, nil, prices.pricePlnByTicker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if computed.ValuePln == nil || !computed.ValuePln.Equal(decimal.NewFromInt(58)) {
		t.Fatalf("expected folded valuePln=58, got %v", computed.ValuePln)
	}
	if computed.DeviationPct == nil || !computed.DeviationPct.IsZero() {
		t.Fatalf("expected deviationPct=0 at exactly target, got %v", computed.DeviationPct)
	}
}

func TestDelete_PrimaryWithSiblingsIs409(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "100")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	fq.putSub(aid, "SUB", "MAIN", "4", "", false)
	svc := NewService(fq, nil, newFakePrices())

	err := svc.Delete(context.Background(), 1)
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 409 {
		t.Fatalf("expected 409 (set a new primary first), got %v", err)
	}
	if fq.liveRows() != 2 {
		t.Fatalf("expected no rows deleted, got %d left", fq.liveRows())
	}
}

func TestDelete_NonPrimaryDeletesSingleRow(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "100")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	fq.putSub(aid, "SUB", "MAIN", "4", "", false)
	svc := NewService(fq, nil, newFakePrices())

	if err := svc.Delete(context.Background(), 2); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fq.rows[1].DeletedAt.Valid {
		t.Fatal("expected primary row to survive deleting a non-primary subasset")
	}
	if !fq.rows[2].DeletedAt.Valid {
		t.Fatal("expected non-primary row itself to be soft-deleted")
	}
	if fq.assets[aid].DeletedAt.Valid {
		t.Fatal("asset must stay live")
	}
}

func TestDelete_LastSubassetRemovesAsset(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "SOLO", "100")
	fq.putSub(aid, "SOLO", "MAIN", "10", "", true)
	svc := NewService(fq, nil, newFakePrices())

	if err := svc.Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fq.liveRows() != 0 || len(fq.rows) != 1 {
		t.Fatalf("expected subasset soft-deleted (row kept), got %d live of %d", fq.liveRows(), len(fq.rows))
	}
	if !fq.assets[aid].DeletedAt.Valid {
		t.Fatal("expected asset soft-deleted with its last subasset")
	}
}

func TestSetPrimary_FlipsBooleanOnly(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "100")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	fq.putSub(aid, "SUB", "MAIN", "4", "", false)
	svc := NewService(fq, nil, newFakePrices())

	dto, err := svc.SetPrimary(context.Background(), aid, 2)
	if err != nil {
		t.Fatalf("SetPrimary: %v", err)
	}
	if fq.rows[1].IsPrimary || !fq.rows[2].IsPrimary {
		t.Fatalf("expected primary flipped to row 2, got %+v / %+v", fq.rows[1], fq.rows[2])
	}
	if dto.Subassets[0].Ticker != "SUB" || !dto.Subassets[0].IsPrimary {
		t.Fatalf("expected SUB first and primary in response, got %+v", dto.Subassets)
	}
	// no target moved: the asset still carries 100
	if dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("expected asset target untouched at 100, got %v", dto.TargetAllocationPct)
	}
}

func TestSetPrimary_ForeignSubassetIs400(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "60")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	other := fq.putAsset("MAIN", "OTHER", "40")
	fq.putSub(other, "OTHER", "MAIN", "5", "", true)
	svc := NewService(fq, nil, newFakePrices())

	_, err := svc.SetPrimary(context.Background(), aid, 3)
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != 400 {
		t.Fatalf("expected 400 VALIDATION_ERROR, got %v", err)
	}
}

func setDec(d decimal.Decimal) httpx.Optional[decimal.Decimal] {
	return httpx.Optional[decimal.Decimal]{Set: true, Value: &d}
}

func nullDec() httpx.Optional[decimal.Decimal] { return httpx.Optional[decimal.Decimal]{Set: true} }

func appErrWithStatus(t *testing.T, err error, status int) *httpx.AppError {
	t.Helper()
	var appErr *httpx.AppError
	ok := errors.As(err, &appErr)
	if !ok || appErr.Status != status {
		t.Fatalf("expected AppError %d, got %v", status, err)
	}
	return appErr
}

// requireAppErr is appErrWithStatus for the callers that only assert the status.
func requireAppErr(t *testing.T, err error, status int) {
	t.Helper()
	_ = appErrWithStatus(t, err, status)
}

// TestUpdateAsset_EditsTargetAndName_NoSumGuard pins the owner-mandated semantics: a plain
// target edit validates the 0..100 range ONLY — a book that does not sum to 100 (60+30 here,
// and the edit itself makes it 70+30) must stay editable, because the UI's Σ indicator
// relies on partial sums being savable.
func TestUpdateAsset_EditsTargetAndName_NoSumGuard(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "60")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	other := fq.putAsset("MAIN", "OTHER", "30")
	fq.putSub(other, "OTHER", "MAIN", "5", "", true)
	svc := NewService(fq, nil, newFakePrices())

	name := "Renamed"
	dto, err := svc.UpdateAsset(context.Background(), aid, UpdateAssetInput{TargetAllocationPct: setDec(decimal.NewFromInt(70)), Name: &name})
	if err != nil {
		t.Fatalf("UpdateAsset (sum 100 not required): %v", err)
	}
	if dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(decimal.NewFromInt(70)) || dto.Name != "Renamed" {
		t.Fatalf("expected target 70 + new name, got %+v", dto)
	}
	if _, err := svc.UpdateAsset(context.Background(), aid, UpdateAssetInput{Name: new("Again")}); err != nil {
		t.Fatalf("a name-only edit must never be Σ-guarded: %v", err)
	}
}

// TestUpdateAsset_NullClearsTarget_AbsentKeepsIt_ZeroIsExplicit pins the tri-state (NULL "no
// target" != 0 "explicit zero", ADR-053) for both regime targets.
func TestUpdateAsset_NullClearsTarget_AbsentKeepsIt_ZeroIsExplicit(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "30")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	svc := NewService(fq, nil, newFakePrices())
	ctx := context.Background()

	if dto, err := svc.UpdateAsset(ctx, aid, UpdateAssetInput{Name: new("x")}); err != nil || dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(decimal.NewFromInt(30)) {
		t.Fatalf("absent target must leave 30 alone, got %+v (%v)", dto.TargetAllocationPct, err)
	}
	dto, err := svc.UpdateAsset(ctx, aid, UpdateAssetInput{TargetAllocationPct: nullDec(), TargetAllocationPctRiskOff: setDec(decimal.NewFromInt(12))})
	if err != nil || dto.TargetAllocationPct != nil {
		t.Fatalf("explicit null must CLEAR the target, got %v (%v)", dto.TargetAllocationPct, err)
	}
	if dto.TargetAllocationPctRiskOff == nil || !dto.TargetAllocationPctRiskOff.Equal(decimal.NewFromInt(12)) {
		t.Fatalf("expected risk-off target 12, got %v", dto.TargetAllocationPctRiskOff)
	}
	dto, err = svc.UpdateAsset(ctx, aid, UpdateAssetInput{TargetAllocationPct: setDec(decimal.Zero), TargetAllocationPctRiskOff: nullDec()})
	if err != nil || dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.IsZero() {
		t.Fatalf("zero must be an explicit zero target, not NULL, got %v (%v)", dto.TargetAllocationPct, err)
	}
	if dto.TargetAllocationPctRiskOff != nil {
		t.Fatalf("explicit null must clear the risk-off target, got %v", dto.TargetAllocationPctRiskOff)
	}
}

func TestUpdateAsset_RangeAndUnknownAsset(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "30")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	svc := NewService(fq, nil, newFakePrices())
	ctx := context.Background()

	for _, v := range []string{"100.01", "-0.5"} {
		_, err := svc.UpdateAsset(ctx, aid, UpdateAssetInput{TargetAllocationPct: setDec(decimal.RequireFromString(v))})
		requireAppErr(t, err, 400)
	}
	_, err := svc.UpdateAsset(ctx, aid, UpdateAssetInput{TargetAllocationPctRiskOff: setDec(decimal.NewFromInt(101))})
	requireAppErr(t, err, 400)
	if !fq.assets[aid].TargetAllocationPct.Valid {
		t.Fatal("a rejected edit must write nothing")
	}
	_, err = svc.UpdateAsset(ctx, 999, UpdateAssetInput{Name: new("x")})
	requireAppErr(t, err, 404)
}

// TestSetPrimary_SumTripwire: Σ == 100 is enforced ONLY here. A book left off 100 by an
// in-progress target edit rejects the flip (400) and flips nothing; a complete book passes.
func TestSetPrimary_SumTripwire(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "60")
	fq.putSub(aid, "PARENT", "MAIN", "10", "", true)
	fq.putSub(aid, "SUB", "MAIN", "4", "", false)
	other := fq.putAsset("MAIN", "OTHER", "30")
	fq.putSub(other, "OTHER", "MAIN", "5", "", true)
	svc := NewService(fq, nil, newFakePrices())

	_, err := svc.SetPrimary(context.Background(), aid, 2) // 60+30 = 90
	requireAppErr(t, err, 400)
	if !fq.rows[1].IsPrimary || fq.rows[2].IsPrimary {
		t.Fatalf("a tripwire rejection must flip nothing, got %+v / %+v", fq.rows[1], fq.rows[2])
	}
	// complete the book (sum 100) -> the same flip now passes
	if _, err := svc.UpdateAsset(context.Background(), other, UpdateAssetInput{TargetAllocationPct: setDec(decimal.NewFromInt(40))}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetPrimary(context.Background(), aid, 2); err != nil {
		t.Fatalf("SetPrimary on a complete book: %v", err)
	}
	if fq.rows[1].IsPrimary || !fq.rows[2].IsPrimary {
		t.Fatal("expected the flip to commit")
	}
}

// TestSetPrimary_MainChecksRiskOffTable: MAIN's second regime table (COALESCE(riskOff, riskOn))
// must also sum to 100; RETIREMENT never reads it.
func TestSetPrimary_MainChecksRiskOffTable(t *testing.T) {
	setup := func(portfolio string) (*fakeQueries, int64) {
		fq := newFakeQueries()
		aid := fq.putAsset(portfolio, "A", "50")
		fq.putSub(aid, "A", portfolio, "10", "", true)
		fq.putSub(aid, "A-SUB", portfolio, "1", "", false)
		bid := fq.putAsset(portfolio, "B", "50")
		fq.putSub(bid, "B", portfolio, "5", "", true)
		// A's risk-off target is 10 -> risk-off table sums to 10 + 50 = 60 (B falls back to 50)
		a := fq.assets[aid]
		a.TargetAllocationPctRiskOff = pgnum.FromDecimal(decimal.NewFromInt(10))
		fq.assets[aid] = a
		return fq, aid
	}
	fq, aid := setup("MAIN")
	_, err := NewService(fq, nil, newFakePrices()).SetPrimary(context.Background(), aid, 2)
	requireAppErr(t, err, 400)

	fq, aid = setup("RETIREMENT")
	if _, err := NewService(fq, nil, newFakePrices()).SetPrimary(context.Background(), aid, 2); err != nil {
		t.Fatalf("RETIREMENT ignores the risk-off table: %v", err)
	}
}

func TestCreate_NewAssetFirstSubassetIsPrimaryAndCarriesTargets(t *testing.T) {
	fq := newFakeQueries()
	svc := NewService(fq, nil, newFakePrices())
	target := decimal.RequireFromString("2.5")
	dto, err := svc.Create(context.Background(), CreateInput{
		Ticker: "NEW", Name: "New thing", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN",
		NewAsset: &NewAssetInput{Name: "New", Exposure: "EQUITY", AssetClass: "ETF", TargetAllocationPct: &target},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dto.Key != "NEW" || dto.Exposure != "EQUITY" || len(dto.Subassets) != 1 || !dto.Subassets[0].IsPrimary {
		t.Fatalf("expected a new asset (key = ticker) with one primary subasset, got %+v", dto)
	}
	if dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(target) {
		t.Fatalf("target must land on the asset, got %v", dto.TargetAllocationPct)
	}
	if fq.rows[1].Exposure != sqlcgen.ExposureEnumEQUITY {
		t.Fatalf("subasset row exposure must default to the new asset's, got %s", fq.rows[1].Exposure)
	}
}

func TestCreate_AttachToAsset(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "BTC", "20")
	fq.putSub(aid, "BTC-USD", "MAIN", "1", "", true)
	svc := NewService(fq, nil, newFakePrices())
	base := CreateInput{Ticker: "PL.ETN", Name: "ETN", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN"}

	// attach: non-primary by default, exposure inherited from the asset (CRYPTO in the fixture)
	in := base
	in.AssetID = &aid
	dto, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if len(dto.Subassets) != 2 || dto.Subassets[1].Ticker != "PL.ETN" || dto.Subassets[1].IsPrimary {
		t.Fatalf("expected a non-primary second subasset, got %+v", dto.Subassets)
	}
	if fq.rows[2].Exposure != sqlcgen.ExposureEnumCRYPTO {
		t.Fatalf("attached subasset must inherit the asset's exposure, got %s", fq.rows[2].Exposure)
	}

	// unknown asset -> 404
	missing := int64(999)
	in = base
	in.Ticker, in.AssetID = "X1", &missing
	_, err = svc.Create(context.Background(), in)
	requireAppErr(t, err, 404)

	// cross-portfolio attach -> 400
	in = base
	in.Ticker, in.AssetID, in.Portfolio = "X2", &aid, "RETIREMENT"
	_, err = svc.Create(context.Background(), in)
	requireAppErr(t, err, 400)

	// a second primary on an asset that already has one -> 409
	yes := true
	in = base
	in.Ticker, in.AssetID, in.IsPrimary = "X3", &aid, &yes
	_, err = svc.Create(context.Background(), in)
	requireAppErr(t, err, 409)

	// both / neither of assetId and newAsset -> 400
	_, err = svc.Create(context.Background(), base)
	requireAppErr(t, err, 400)
	in = base
	in.Ticker, in.AssetID, in.NewAsset = "X4", &aid, &NewAssetInput{Name: "n", Exposure: "CRYPTO", AssetClass: "ETF"}
	_, err = svc.Create(context.Background(), in)
	requireAppErr(t, err, 400)
}

func TestCreate_DuplicateNewAssetKeyIs409(t *testing.T) {
	fq := newFakeQueries()
	fq.putAsset("MAIN", "DUP", "1")
	svc := NewService(fq, nil, newFakePrices())
	_, err := svc.Create(context.Background(), CreateInput{
		Ticker: "DUP", Name: "d", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN",
		NewAsset: &NewAssetInput{Name: "d", Exposure: "CRYPTO", AssetClass: "ETF"},
	})
	appErr := appErrWithStatus(t, err, 409)
	if appErr.Code != httpx.CodeDuplicateEntry {
		t.Fatalf("expected DUPLICATE_ENTRY, got %s", appErr.Code)
	}
}

func TestDelete_UnknownIs404(t *testing.T) {
	svc := NewService(newFakeQueries(), nil, newFakePrices())
	requireAppErr(t, svc.Delete(context.Background(), 1), 404)
	requireAppErr(t, svc.DeleteAsset(context.Background(), 1), 404)
}

func TestDeleteAsset_CascadesToSubassets(t *testing.T) {
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "BTC", "20")
	fq.putSub(aid, "BTC-USD", "MAIN", "1", "", true)
	fq.putSub(aid, "PL.ETN", "MAIN", "1", "", false)
	keep := fq.putAsset("MAIN", "ETH", "10")
	fq.putSub(keep, "ETH-USD", "MAIN", "1", "", true)
	svc := NewService(fq, nil, newFakePrices())
	if err := svc.DeleteAsset(context.Background(), aid); err != nil {
		t.Fatal(err)
	}
	if fq.liveAssets() != 1 || fq.liveRows() != 1 {
		t.Fatalf("expected only the other asset + its subasset to stay live, got %d assets %d rows", fq.liveAssets(), fq.liveRows())
	}
	if len(fq.assets) != 2 || len(fq.rows) != 3 {
		t.Fatal("soft delete must never remove rows")
	}
	requireAppErr(t, svc.DeleteAsset(context.Background(), aid), 404)
}

func TestList_GroupsSubassetsUnderAsset_PrimaryFirst(t *testing.T) {
	// Exactly one top-level entry per asset — subassets ride nested inside their
	// asset, primary first.
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "PARENT", "100")
	fq.putSub(aid, "PARENT", "MAIN", "10", "PARENT.YAHOO", true)
	fq.putSub(aid, "SUB", "MAIN", "4", "", false)
	svc := NewService(fq, nil, newFakePrices())

	items, total, err := svc.List(context.Background(), "", 0, 20)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected exactly 1 top-level asset (total=1), got total=%d len=%d", total, len(items))
	}
	if items[0].Key != "PARENT" {
		t.Fatalf("expected PARENT asset, got %s", items[0].Key)
	}
	subs := items[0].Subassets
	if len(subs) != 2 || subs[0].Ticker != "PARENT" || subs[1].Ticker != "SUB" {
		t.Fatalf("expected subassets [PARENT SUB], got %+v", subs)
	}
}

func TestToOpenAPIAsset_MapsUnitsRegularUsdCostBasisAndSubassets(t *testing.T) {
	// Wire-parity guard: the response must carry unitsRegularUsd, the cost basis of the
	// subasset row that incurred it, and the service-computed subassets (frontend expands them).
	usd := decimal.NewFromInt(7)
	eff := decimal.NewFromInt(17)
	spent := decimal.NewFromInt(1000)
	dto := AssetDTO{
		ID: 1, Key: "bitcoin", Portfolio: "MAIN", Name: "Bitcoin", Exposure: "CRYPTO", AssetClass: "CRYPTO",
		Subassets: []SubassetDTO{
			{ID: 5, AssetID: 1, IsPrimary: true, Ticker: "BTC-USD", Name: "X", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "CRYPTO",
				PriceCurrency: "PLN", PurchaseCurrency: "PLN", UnitsRegularUsd: usd, EffectiveUnits: eff, TotalSpentPln: &spent},
		},
	}
	out := toOpenAPIAsset(dto)
	if out.Key != "bitcoin" || out.Subassets == nil || len(*out.Subassets) != 1 {
		t.Fatalf("expected asset key + 1 subasset, got %+v", out)
	}
	sub := (*out.Subassets)[0]
	if sub.UnitsRegularUsd == nil || *sub.UnitsRegularUsd != 7 || sub.TotalSpentPln == nil || *sub.TotalSpentPln != 1000 {
		t.Fatalf("expected unitsRegularUsd=7 and totalSpentPln=1000 on the subasset, got %+v", sub)
	}
	if sub.Portfolio != "MAIN" || sub.IsPrimary == nil || !*sub.IsPrimary || sub.AssetId == nil || *sub.AssetId != 1 {
		t.Fatalf("expected portfolio/isPrimary/assetId on the subasset, got %+v", sub)
	}
}

func ptrDec(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

// ── Group profit (task 156 / 161a-1): profit = Σvalue / Σcost − 1 over ALL subassets ─────────

// profitFixture builds an asset whose subassets are (ticker, units, price, cost) tuples; the
// first is the primary. Returns computed fields for it.
type profitSub struct {
	ticker, units, price, cost string // price "" = unpriced; cost "" = unknown (NULL)
	primary                    bool
}

func computeGroup(t *testing.T, portfolio string, subs []profitSub) ComputedFields {
	t.Helper()
	fq := newFakeQueries()
	aid := fq.putAsset(portfolio, "GRP", "10")
	prices := newFakePrices()
	for _, s := range subs {
		yahoo := ""
		if s.price != "" {
			yahoo = s.ticker + ".Y"
			prices.pricePlnByTicker[yahoo] = decimal.RequireFromString(s.price)
		}
		id := fq.putSub(aid, s.ticker, portfolio, s.units, yahoo, s.primary)
		if s.cost != "" {
			fq.setCost(id, s.cost)
		}
	}
	svc := &Service{prices: prices}
	totals := map[string]decimal.Decimal{portfolio: decimal.NewFromInt(100000)}
	computed, err := svc.computeFields(context.Background(), testAsset(t, fq, aid), testSubs(t, fq, aid), totals, nil, prices.pricePlnByTicker)
	if err != nil {
		t.Fatal(err)
	}
	return computed
}

func wantDec(t *testing.T, name string, got *decimal.Decimal, want string) {
	t.Helper()
	if got == nil || !got.Equal(decimal.RequireFromString(want)) {
		t.Fatalf("%s = %v, want %s", name, got, want)
	}
}

// TestComputeFields_SingleSubassetProfitUnchanged pins the pre-161 single-row formula:
// profit = value/cost - 1, avgBuyPricePln = pricePln / (1 + profit).
func TestComputeFields_SingleSubassetProfitUnchanged(t *testing.T) {
	c := computeGroup(t, "MAIN", []profitSub{{"A", "10", "11", "100", true}}) // value 110, cost 100
	wantDec(t, "profitPct", c.ProfitPct, "0.1")
	wantDec(t, "avgBuyPricePln", c.AvgBuyPricePln, "10") // 11 / 1.1
	wantDec(t, "valuePln", c.ValuePln, "110")
}

// TestComputeFields_GroupProfit_XrpShaped: the live defect — the primary carries a small cost
// (55 for 100 of value = +81.8% on its own) while the ETN subasset holds the rest of the
// exposure with ITS cost. The asset's profit is the GROUP's: (100+100)/(55+145) - 1 = 0, not
// the primary-only 200/55 - 1.
func TestComputeFields_GroupProfit_XrpShaped(t *testing.T) {
	c := computeGroup(t, "MAIN", []profitSub{
		{"XRP-USD", "10", "10", "55", true},
		{"PL.ETNVIRXRP", "10", "10", "145", false},
	})
	wantDec(t, "valuePln", c.ValuePln, "200")
	wantDec(t, "profitPct", c.ProfitPct, "0")
	// a second shape with a real gain: both rows +10%
	c = computeGroup(t, "MAIN", []profitSub{
		{"XRP-USD", "10", "11", "100", true},
		{"PL.ETNVIRXRP", "10", "11", "100", false},
	})
	wantDec(t, "profitPct", c.ProfitPct, "0.1")
	wantDec(t, "avgBuyPricePln", c.AvgBuyPricePln, "10") // primary price 11 / 1.1
}

// TestComputeFields_GroupProfit_PrimaryFlipDoesNotChangeProfit: set-primary only relabels the
// same rows, so the group profit is identical whichever row is primary.
func TestComputeFields_GroupProfit_PrimaryFlipDoesNotChangeProfit(t *testing.T) {
	rows := func(primaryFirst bool) []profitSub {
		return []profitSub{
			{"A", "10", "10", "80", primaryFirst},
			{"B", "5", "20", "120", !primaryFirst},
		}
	}
	before := computeGroup(t, "MAIN", rows(true))
	after := computeGroup(t, "MAIN", rows(false))
	wantDec(t, "profitPct (A primary)", before.ProfitPct, "0") // (100+100)/(80+120) - 1
	wantDec(t, "profitPct (B primary)", after.ProfitPct, "0")
	if !before.ProfitPct.Equal(*after.ProfitPct) || !before.ValuePln.Equal(*after.ValuePln) {
		t.Fatalf("a primary flip changed the group profit/value: %v vs %v", before.ProfitPct, after.ProfitPct)
	}
}

// TestComputeFields_GroupProfit_NullCostRowsCountValueUnpricedRowsAreExcluded (rule corrected
// 2026-09-25): a subasset with NULL/0 cost still adds its VALUE to the group profit — the go-live
// import put the whole group's cost on the head row, so the sibling's baseline is assumed to be
// inside it — but adds no cost. A costed subasset that cannot be priced contributes neither its
// cost nor a value. valuePln still sums every priced subasset.
func TestComputeFields_GroupProfit_NullCostRowsCountValueUnpricedRowsAreExcluded(t *testing.T) {
	c := computeGroup(t, "MAIN", []profitSub{
		{"A", "10", "11", "100", true},      // value 110, cost 100
		{"NOCOST", "50", "10", "", false},   // value 500, cost NULL -> value counts, no cost
		{"UNPRICED", "5", "", "500", false}, // cost 500, no price -> neither cost nor value
	})
	wantDec(t, "profitPct", c.ProfitPct, "5.1") // (110+500)/100 - 1
	wantDec(t, "valuePln (priced rows only)", c.ValuePln, "610")
	// a zero cost is "no recorded cost" too: value counts, cost does not
	c = computeGroup(t, "MAIN", []profitSub{
		{"A", "10", "11", "100", true},
		{"ZERO", "50", "10", "0", false},
	})
	wantDec(t, "profitPct (zero-cost row)", c.ProfitPct, "5.1") // (110+500)/100 - 1
}

// TestComputeFields_GroupProfit_BitcoinShapedGroupCostOnHead: the go-live shape (V21 backfill).
// The head BTC row holds the WHOLE group's cost (100000 PLN for physical BTC + the ETN together),
// the ETN row's cost is NULL; group profit = (60000+20000)/100000 - 1 = -20 % (the old
// exclusion rule showed -34 %). Promoting the ETN to primary must not move it.
func TestComputeFields_GroupProfit_BitcoinShapedGroupCostOnHead(t *testing.T) {
	rows := func(btcPrimary bool) []profitSub {
		return []profitSub{
			{"BTC-USD", "600", "100", "100000", btcPrimary},  // value 60000, group cost
			{"PL.ETNVIRBTCP", "200", "100", "", !btcPrimary}, // value 20000, cost NULL
		}
	}
	before := computeGroup(t, "MAIN", rows(true))
	after := computeGroup(t, "MAIN", rows(false))
	wantDec(t, "profitPct (BTC primary)", before.ProfitPct, "-0.2")
	wantDec(t, "profitPct (ETN primary)", after.ProfitPct, "-0.2")
	wantDec(t, "valuePln (BTC primary)", before.ValuePln, "80000")
	wantDec(t, "valuePln (ETN primary)", after.ValuePln, "80000")
}

func TestComputeFields_GroupProfit_AllCostsNullMeansNoProfit(t *testing.T) {
	c := computeGroup(t, "MAIN", []profitSub{
		{"A", "10", "11", "", true},
		{"B", "5", "10", "", false},
	})
	if c.ProfitPct != nil || c.AvgBuyPricePln != nil {
		t.Fatalf("no known cost -> profit/avgBuyPricePln stay nil, got %v / %v", c.ProfitPct, c.AvgBuyPricePln)
	}
	wantDec(t, "valuePln", c.ValuePln, "160")
}

// RETIREMENT is computed exactly like MAIN (ADR-104 lifted ADR-056): value 220, cost 200 -> +10 %,
// entry 11 / 1.1 = 10.
func TestComputeFields_RetirementHasProfit(t *testing.T) {
	c := computeGroup(t, "RETIREMENT", []profitSub{
		{"A", "10", "11", "100", true},
		{"B", "10", "11", "100", false},
	})
	wantDec(t, "profitPct", c.ProfitPct, "0.1")
	wantDec(t, "avgBuyPricePln", c.AvgBuyPricePln, "10")
}

// ── §13 re-home: assetId on PUT /positions/{id} ─────────────────────────────────────────────

// rehomeFixture: asset A (primary A1 + non-primary A2), asset B (primary B1), asset R in the
// RETIREMENT portfolio. Returns the service and the fake.
func rehomeFixture() (*Service, *fakeQueries, int64, int64, int64) {
	fq := newFakeQueries()
	a := fq.putAsset("MAIN", "A", "50")
	fq.putSub(a, "A1", "MAIN", "10", "", true) // row 1
	fq.putSub(a, "A2", "MAIN", "4", "", false) // row 2
	b := fq.putAsset("MAIN", "B", "50")
	fq.putSub(b, "B1", "MAIN", "7", "", true) // row 3
	r := fq.putAsset("RETIREMENT", "R", "100")
	fq.putSub(r, "R1", "RETIREMENT", "1", "", true) // row 4
	fq.setCost(2, "123.45")
	return NewService(fq, nil, newFakePrices()), fq, a, b, r
}

func TestUpdate_ReHome_MovesOnlyAssetID(t *testing.T) {
	svc, fq, _, b, _ := rehomeFixture()
	before := fq.rows[2]

	dto, err := svc.Update(context.Background(), 2, UpdateInput{AssetID: &b})
	if err != nil {
		t.Fatalf("re-home: %v", err)
	}
	after := fq.rows[2]
	if after.AssetID != b || after.IsPrimary {
		t.Fatalf("expected A2 under asset B and still non-primary, got %+v", after)
	}
	// nothing but asset_id moved: units, cost basis, ticker, name, class, exposure untouched
	// (numerics compared by value — pgtype.Numeric holds pointers).
	keys := map[int64]string{before.AssetID: "A", after.AssetID: "B"}
	bd, err := decodeRow(before, keys)
	if err != nil {
		t.Fatal(err)
	}
	ad, err := decodeRow(after, keys)
	if err != nil {
		t.Fatal(err)
	}
	if bd.Ticker != ad.Ticker || bd.Name != ad.Name || bd.AssetClass != ad.AssetClass || bd.Exposure != ad.Exposure ||
		bd.Exchange != ad.Exchange || bd.PriceCurrency != ad.PriceCurrency || bd.IsPrimary != ad.IsPrimary || bd.CompanyBacked != ad.CompanyBacked ||
		!bd.UnitsTotal().Equal(ad.UnitsTotal()) || bd.UnitsRegularPln.Cmp(ad.UnitsRegularPln) != 0 ||
		bd.TotalSpentPln == nil || ad.TotalSpentPln == nil || !bd.TotalSpentPln.Equal(*ad.TotalSpentPln) {
		t.Fatalf("a re-home must not change any other column:\nbefore %+v\nafter  %+v", bd, ad)
	}
	// the response is the NEW parent with the moved subasset nested
	if dto.ID != b || len(dto.Subassets) != 2 {
		t.Fatalf("expected the new parent asset B with 2 subassets, got id=%d subs=%+v", dto.ID, dto.Subassets)
	}
	found := false
	for _, s := range dto.Subassets {
		if s.Ticker == "A2" && !s.IsPrimary {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected A2 (non-primary) under B, got %+v", dto.Subassets)
	}
	// the source asset keeps its primary; targets never moved
	srcTarget, _ := pgnum.ToDecimalPtr(fq.assets[1].TargetAllocationPct)
	dstTarget, _ := pgnum.ToDecimalPtr(fq.assets[b].TargetAllocationPct)
	if !fq.rows[1].IsPrimary || srcTarget == nil || !srcTarget.Equal(decimal.NewFromInt(50)) || dstTarget == nil || !dstTarget.Equal(decimal.NewFromInt(50)) {
		t.Fatal("source asset must keep its primary and no target may move")
	}
}

func TestUpdate_ReHome_Guards(t *testing.T) {
	svc, fq, a, b, r := rehomeFixture()
	ctx := context.Background()
	snapshot := func() map[int64]sqlcgen.Position {
		out := map[int64]sqlcgen.Position{}
		maps.Copy(out, fq.rows)
		return out
	}
	assertUntouched := func(before map[int64]sqlcgen.Position) {
		t.Helper()
		for id, row := range before {
			if fq.rows[id] != row {
				t.Fatalf("a rejected re-home must write nothing, row %d changed", id)
			}
		}
	}

	// primary subasset -> 409
	before := snapshot()
	_, err := svc.Update(ctx, 1, UpdateInput{AssetID: &b})
	requireAppErr(t, err, 409)
	assertUntouched(before)

	// unknown target asset -> 404, message names the asset like Create()
	missing := int64(999)
	_, err = svc.Update(ctx, 2, UpdateInput{AssetID: &missing})
	if appErr := appErrWithStatus(t, err, 404); appErr.Message != "Asset with id 999 not found" {
		t.Fatalf("expected the Create()-identical not-found message, got %q", appErr.Message)
	}
	assertUntouched(before)

	// cross-portfolio -> 400, field assetId
	_, err = svc.Update(ctx, 2, UpdateInput{AssetID: &r})
	if appErr := appErrWithStatus(t, err, 400); appErr.Field != "assetId" {
		t.Fatalf("expected the 400 on field assetId, got %q", appErr.Field)
	}
	assertUntouched(before)

	// order of checks: the primary guard beats existence/portfolio (409 even for a bad target)
	_, err = svc.Update(ctx, 1, UpdateInput{AssetID: &missing})
	requireAppErr(t, err, 409)

	// same-as-current: ignored for a primary AND a non-primary (idempotent — a client may always send it)
	if _, err := svc.Update(ctx, 1, UpdateInput{AssetID: &a, Name: new("renamed")}); err != nil {
		t.Fatalf("same-as-current assetId on a primary must be ignored: %v", err)
	}
	if fq.rows[1].Name != "renamed" || fq.rows[1].AssetID != a {
		t.Fatalf("expected the other fields applied and the asset unchanged, got %+v", fq.rows[1])
	}
	if _, err := svc.Update(ctx, 2, UpdateInput{AssetID: &a}); err != nil || fq.rows[2].AssetID != a {
		t.Fatalf("same-as-current on a non-primary must be a no-op, got %v / %+v", err, fq.rows[2])
	}
}

// TestUpdate_OmittedAssetIDKeepsAsset is the regression for the full-row-overwrite gotcha:
// UpdatePositionFields writes asset_id, so an edit that omits assetId must pass the CURRENT one
// (writing 0 would violate the FK in production).
func TestUpdate_OmittedAssetIDKeepsAsset(t *testing.T) {
	svc, fq, a, _, _ := rehomeFixture()
	units := decimal.NewFromInt(99)
	if _, err := svc.Update(context.Background(), 2, UpdateInput{UnitsRegularPln: &units}); err != nil {
		t.Fatal(err)
	}
	if fq.rows[2].AssetID != a || a == 0 {
		t.Fatalf("an edit without assetId must keep the current asset (%d), got %d", a, fq.rows[2].AssetID)
	}
	if !fq.rows[2].TotalSpentPln.Valid {
		t.Fatal("cost basis must survive an unrelated edit")
	}
}

// Task 173 (ADR-109): re-adding a deleted ticker restores the same rows — required fields take
// the new values, omitted optional settings (targets, yahoo ticker) keep their old values,
// units/cost are kept, and other deleted siblings stay deleted.
func TestCreate_RestoresSoftDeletedAsset(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "VVMX", "7")
	pid := fq.putSub(aid, "VVMX", "MAIN", "3", "VVMX.DE", true)
	sib := fq.putSub(aid, "VVMX.SIB", "MAIN", "1", "", false)
	fq.setCost(pid, "120")
	svc := NewService(fq, nil, newFakePrices())
	if err := svc.DeleteAsset(ctx, aid); err != nil {
		t.Fatal(err)
	}
	if fq.liveAssets() != 0 || fq.liveRows() != 0 {
		t.Fatal("expected the asset and both subassets soft-deleted")
	}

	dto, err := svc.Create(ctx, CreateInput{
		Ticker: "VVMX", Name: "VanEck renamed", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "EUR",
		NewAsset: &NewAssetInput{Name: "VVMX new", Exposure: "EQUITY", AssetClass: "ETF"},
	})
	if err != nil {
		t.Fatalf("Create (restore): %v", err)
	}
	if dto.ID != aid || len(dto.Subassets) != 1 || dto.Subassets[0].ID != pid || !dto.Subassets[0].IsPrimary {
		t.Fatalf("expected the same asset/subasset ids restored, got %+v", dto)
	}
	if dto.Name != "VVMX new" || dto.Exposure != "EQUITY" {
		t.Fatalf("required asset fields must take the new values, got %+v", dto)
	}
	if dto.TargetAllocationPct == nil || !dto.TargetAllocationPct.Equal(decimal.NewFromInt(7)) {
		t.Fatalf("omitted target must keep the old value, got %v", dto.TargetAllocationPct)
	}
	r := fq.rows[pid]
	if r.DeletedAt.Valid || r.Name != "VanEck renamed" || r.PriceCurrency != sqlcgen.CurrencyEnumEUR {
		t.Fatalf("subasset must be live with the new required fields, got %+v", r)
	}
	if r.YahooTicker == nil || *r.YahooTicker != "VVMX.DE" || r.Exposure != sqlcgen.ExposureEnumCRYPTO {
		t.Fatalf("omitted optional subasset fields must keep old values, got %+v", r)
	}
	if units, _ := pgnum.ToDecimal(r.UnitsRegularPln); !units.Equal(decimal.NewFromInt(3)) {
		t.Fatalf("units must be kept, got %v", units)
	}
	if cost, _ := pgnum.ToDecimalPtr(r.TotalSpentPln); cost == nil || !cost.Equal(decimal.NewFromInt(120)) {
		t.Fatalf("cost must be kept, got %v", cost)
	}
	if !fq.rows[sib].DeletedAt.Valid {
		t.Fatal("other deleted siblings must stay deleted")
	}
	// the sibling comes back only when re-added under the asset
	if _, err := svc.Create(ctx, CreateInput{
		Ticker: "VVMX.SIB", Name: "sib", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN", AssetID: &aid,
	}); err != nil {
		t.Fatalf("restore sibling: %v", err)
	}
	if fq.rows[sib].DeletedAt.Valid || fq.rows[sib].IsPrimary || len(fq.rows) != 2 {
		t.Fatalf("expected sibling restored as non-primary without new rows, got %+v", fq.rows[sib])
	}
	// a live duplicate is still a 409
	_, err = svc.Create(ctx, CreateInput{
		Ticker: "VVMX", Name: "x", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "EUR",
		NewAsset: &NewAssetInput{Name: "x", Exposure: "EQUITY", AssetClass: "ETF"},
	})
	if appErr := appErrWithStatus(t, err, 409); appErr.Code != httpx.CodeDuplicateEntry {
		t.Fatalf("expected DUPLICATE_ENTRY, got %s", appErr.Code)
	}
}

func TestCreate_NewTickerUnderDeletedAssetKeyRestoresAsset(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "GOLD", "30")
	old := fq.putSub(aid, "GOLD.OLD", "MAIN", "0", "", true)
	svc := NewService(fq, nil, newFakePrices())
	if err := svc.DeleteAsset(ctx, aid); err != nil {
		t.Fatal(err)
	}
	// a new ticker whose key (= ticker) matches the deleted asset's key
	fq.assets[aid] = func() sqlcgen.Asset { a := fq.assets[aid]; a.Key = "GOLD.NEW"; return a }()
	dto, err := svc.Create(ctx, CreateInput{
		Ticker: "GOLD.NEW", Name: "g", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN",
		NewAsset: &NewAssetInput{Name: "Gold", Exposure: "GOLD", AssetClass: "ETF"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dto.ID != aid || len(dto.Subassets) != 1 || dto.Subassets[0].Ticker != "GOLD.NEW" || !dto.Subassets[0].IsPrimary {
		t.Fatalf("expected the deleted asset restored with the new ticker as its only live primary, got %+v", dto)
	}
	if !fq.rows[old].DeletedAt.Valid || fq.rows[old].IsPrimary {
		t.Fatalf("old deleted primary must stay deleted and lose its primary flag (I3), got %+v", fq.rows[old])
	}
}

func TestCreate_AttachToDeletedAssetIs404(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQueries()
	aid := fq.putAsset("MAIN", "A", "1")
	fq.putSub(aid, "A", "MAIN", "0", "", true)
	svc := NewService(fq, nil, newFakePrices())
	if err := svc.DeleteAsset(ctx, aid); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, CreateInput{
		Ticker: "B", Name: "b", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN", AssetID: &aid,
	})
	requireAppErr(t, err, 404)
	requireAppErr(t, svc.Delete(ctx, 1), 404)
	if _, err := svc.Get(ctx, 1); err == nil {
		t.Fatal("a deleted subasset must answer 404")
	}
}
