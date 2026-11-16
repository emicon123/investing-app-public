package purchasesession

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

// fakeQueries is an in-memory stand-in for *sqlcgen.Queries — same convention as prior phases
// (business logic table-tested here; real Postgres covered by integration_test.go).
type fakeQueries struct {
	positions map[int64]sqlcgen.Position
	nextPosID int64
	assets    map[int64]sqlcgen.Asset
	nextAID   int64

	sessions      map[int64]sqlcgen.PurchaseSession
	nextSessionID int64
	items         map[int64]sqlcgen.PurchaseSessionItem
	nextItemID    int64

	settings   map[string]string
	adjustment map[string]decimal.Decimal
	depositSum map[string]decimal.Decimal

	assetPurchases   []sqlcgen.InsertAssetPurchaseParams
	deposits         []sqlcgen.InsertDepositParams
	binanceTransfers []sqlcgen.InsertBinanceTransferSessionParams

	macro   *sqlcgen.MacroState
	hasCash bool

	// lockHeld simulates Postgres row-level locking for GetSessionForUpdate in unit tests that
	// need to observe lock contention without a real DB (the real guarantee is verified against
	// testcontainers-go in integration_test.go's concurrency test).
	lockHeld map[int64]bool
}

func newFakeQueries() *fakeQueries {
	return &fakeQueries{
		positions: map[int64]sqlcgen.Position{}, nextPosID: 1,
		assets: map[int64]sqlcgen.Asset{}, nextAID: 1,
		sessions: map[int64]sqlcgen.PurchaseSession{}, nextSessionID: 1,
		items: map[int64]sqlcgen.PurchaseSessionItem{}, nextItemID: 1,
		settings: map[string]string{}, adjustment: map[string]decimal.Decimal{}, depositSum: map[string]decimal.Decimal{},
		lockHeld: map[int64]bool{},
	}
}

// Task 173: live-only vs including-deleted split, same as the real queries.
func (f *fakeQueries) ListAllPositions(_ context.Context) ([]sqlcgen.Position, error) {
	out := make([]sqlcgen.Position, 0, len(f.positions))
	for _, p := range f.positions {
		if !p.DeletedAt.Valid {
			out = append(out, p)
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

func (f *fakeQueries) ListAllPositionsIncludingDeleted(_ context.Context) ([]sqlcgen.Position, error) {
	out := make([]sqlcgen.Position, 0, len(f.positions))
	for _, p := range f.positions {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeQueries) ListAllAssetsIncludingDeleted(_ context.Context) ([]sqlcgen.Asset, error) {
	out := make([]sqlcgen.Asset, 0, len(f.assets))
	for _, a := range f.assets {
		out = append(out, a)
	}
	return out, nil
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
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}, nil
}
func (f *fakeQueries) CashPositionsByRecordedAt(_ context.Context, _ pgtype.Timestamptz) ([]sqlcgen.CashPosition, error) {
	return nil, nil
}
func (f *fakeQueries) GetSettingByKey(_ context.Context, key string) (sqlcgen.GetSettingByKeyRow, error) {
	v, ok := f.settings[key]
	if !ok {
		return sqlcgen.GetSettingByKeyRow{}, pgx.ErrNoRows
	}
	return sqlcgen.GetSettingByKeyRow{Key: key, Value: v}, nil
}
func (f *fakeQueries) SumDepositAmountByAccountTypeAndYear(_ context.Context, arg sqlcgen.SumDepositAmountByAccountTypeAndYearParams) (pgtype.Numeric, error) {
	return pgnum.FromDecimal(f.depositSum[string(arg.AccountType)]), nil
}
func (f *fakeQueries) GetAccountCapAdjustment(_ context.Context, arg sqlcgen.GetAccountCapAdjustmentParams) (sqlcgen.AccountCapAdjustment, error) {
	v, ok := f.adjustment[string(arg.AccountType)]
	if !ok {
		return sqlcgen.AccountCapAdjustment{}, pgx.ErrNoRows
	}
	return sqlcgen.AccountCapAdjustment{AdjustmentPln: pgnum.FromDecimal(v)}, nil
}

func (f *fakeQueries) ExistsPlannedSession(_ context.Context) (bool, error) {
	for _, s := range f.sessions {
		if s.Status == sqlcgen.SessionStatusEnumPLANNED {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeQueries) GetPlannedSession(_ context.Context) (sqlcgen.PurchaseSession, error) {
	for _, s := range f.sessions {
		if s.Status == sqlcgen.SessionStatusEnumPLANNED {
			return s, nil
		}
	}
	return sqlcgen.PurchaseSession{}, pgx.ErrNoRows
}
func (f *fakeQueries) GetSessionByID(_ context.Context, id int64) (sqlcgen.PurchaseSession, error) {
	s, ok := f.sessions[id]
	if !ok {
		return sqlcgen.PurchaseSession{}, pgx.ErrNoRows
	}
	return s, nil
}
func (f *fakeQueries) GetSessionForUpdate(_ context.Context, id int64) (sqlcgen.PurchaseSession, error) {
	s, ok := f.sessions[id]
	if !ok {
		return sqlcgen.PurchaseSession{}, pgx.ErrNoRows
	}
	return s, nil
}
func (f *fakeQueries) InsertPurchaseSession(_ context.Context, arg sqlcgen.InsertPurchaseSessionParams) (sqlcgen.PurchaseSession, error) {
	id := f.nextSessionID
	f.nextSessionID++
	row := sqlcgen.PurchaseSession{
		ID: id, Status: arg.Status, PlannedAt: arg.PlannedAt, Notes: arg.Notes,
		RetirementBudgetPln: arg.RetirementBudgetPln, MainBudgetPln: arg.MainBudgetPln, PortfoliosIncluded: arg.PortfoliosIncluded,
	}
	f.sessions[id] = row
	return row, nil
}
func (f *fakeQueries) UpdateSessionStatusDone(_ context.Context, arg sqlcgen.UpdateSessionStatusDoneParams) (sqlcgen.PurchaseSession, error) {
	s := f.sessions[arg.ID]
	s.Status = sqlcgen.SessionStatusEnumDONE
	s.DoneAt = arg.DoneAt
	f.sessions[arg.ID] = s
	return s, nil
}
func (f *fakeQueries) DeleteSessionByID(_ context.Context, id int64) error {
	delete(f.sessions, id)
	return nil
}
func (f *fakeQueries) ListSessionsByStatusPaged(_ context.Context, arg sqlcgen.ListSessionsByStatusPagedParams) ([]sqlcgen.PurchaseSession, error) {
	var out []sqlcgen.PurchaseSession
	for _, s := range f.sessions {
		if s.Status == arg.Status {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeQueries) CountSessionsByStatus(_ context.Context, status sqlcgen.SessionStatusEnum) (int64, error) {
	var n int64
	for _, s := range f.sessions {
		if s.Status == status {
			n++
		}
	}
	return n, nil
}

func (f *fakeQueries) InsertPurchaseSessionItem(_ context.Context, arg sqlcgen.InsertPurchaseSessionItemParams) (sqlcgen.PurchaseSessionItem, error) {
	id := f.nextItemID
	f.nextItemID++
	row := sqlcgen.PurchaseSessionItem{
		ID: id, SessionID: arg.SessionID, Portfolio: arg.Portfolio, Ticker: arg.Ticker, AccountType: arg.AccountType,
		PlannedUnits: arg.PlannedUnits, PlannedValuePln: arg.PlannedValuePln, ActualUnits: arg.ActualUnits,
		ActualValuePln: arg.ActualValuePln, PricePlnAtPlan: arg.PricePlnAtPlan, IsMigrationSell: arg.IsMigrationSell, IsMigrationBuy: arg.IsMigrationBuy,
	}
	f.items[id] = row
	return row, nil
}
func (f *fakeQueries) ListSessionItemsBySessionID(_ context.Context, sessionID int64) ([]sqlcgen.PurchaseSessionItem, error) {
	var out []sqlcgen.PurchaseSessionItem
	for _, it := range f.items {
		if it.SessionID == sessionID {
			out = append(out, it)
		}
	}
	return out, nil
}
func (f *fakeQueries) ListSessionItemTotals(_ context.Context, sessionIDs []int64) ([]sqlcgen.ListSessionItemTotalsRow, error) {
	var out []sqlcgen.ListSessionItemTotalsRow
	for _, id := range sessionIDs {
		row := sqlcgen.ListSessionItemTotalsRow{SessionID: id}
		total := decimal.Zero
		for _, it := range f.items {
			if it.SessionID != id {
				continue
			}
			row.ItemCount++
			if it.ActualValuePln.Valid {
				total = total.Add(pgnum.ToDecimalOrZero(it.ActualValuePln))
				row.HasActual = true
			}
		}
		if row.ItemCount > 0 {
			row.TotalActualPln = pgnum.FromDecimal(total)
			out = append(out, row)
		}
	}
	return out, nil
}
func (f *fakeQueries) ListSessionItemsOrderedForCompletion(_ context.Context, sessionID int64) ([]sqlcgen.PurchaseSessionItem, error) {
	return f.ListSessionItemsBySessionID(context.Background(), sessionID)
}
func (f *fakeQueries) UpdateSessionItemActuals(_ context.Context, arg sqlcgen.UpdateSessionItemActualsParams) (sqlcgen.PurchaseSessionItem, error) {
	it := f.items[arg.ID]
	it.ActualUnits = arg.ActualUnits
	it.ActualValuePln = arg.ActualValuePln
	f.items[arg.ID] = it
	return it, nil
}
func (f *fakeQueries) DeleteSessionItemsBySessionID(_ context.Context, sessionID int64) error {
	for id, it := range f.items {
		if it.SessionID == sessionID {
			delete(f.items, id)
		}
	}
	return nil
}

func (f *fakeQueries) UpdatePositionDelta(_ context.Context, arg sqlcgen.UpdatePositionDeltaParams) (sqlcgen.Position, error) {
	pos := f.positions[arg.ID]
	pos.UnitsIke, pos.UnitsIkze = arg.UnitsIke, arg.UnitsIkze
	pos.UnitsRegularEur, pos.UnitsRegularUsd, pos.UnitsRegularPln = arg.UnitsRegularEur, arg.UnitsRegularUsd, arg.UnitsRegularPln
	pos.TotalSpentPln = arg.TotalSpentPln
	f.positions[arg.ID] = pos
	return pos, nil
}
func (f *fakeQueries) UpdatePositionCost(_ context.Context, arg sqlcgen.UpdatePositionCostParams) error {
	pos := f.positions[arg.ID]
	pos.TotalSpentPln = arg.TotalSpentPln
	f.positions[arg.ID] = pos
	return nil
}
func (f *fakeQueries) GetAllPriceCache(_ context.Context) ([]sqlcgen.GetAllPriceCacheRow, error) {
	out := make([]sqlcgen.GetAllPriceCacheRow, 0, len(sharedFakePrices))
	for ticker, price := range sharedFakePrices {
		out = append(out, sqlcgen.GetAllPriceCacheRow{Ticker: ticker, PricePln: pgnum.FromDecimal(price)})
	}
	return out, nil
}
func (f *fakeQueries) InsertAssetPurchase(_ context.Context, arg sqlcgen.InsertAssetPurchaseParams) (sqlcgen.AssetPurchase, error) {
	f.assetPurchases = append(f.assetPurchases, arg)
	return sqlcgen.AssetPurchase{ID: int64(len(f.assetPurchases))}, nil
}
func (f *fakeQueries) InsertDeposit(_ context.Context, arg sqlcgen.InsertDepositParams) (sqlcgen.Deposit, error) {
	f.deposits = append(f.deposits, arg)
	return sqlcgen.Deposit{ID: int64(len(f.deposits))}, nil
}
func (f *fakeQueries) InsertBinanceTransferSession(_ context.Context, arg sqlcgen.InsertBinanceTransferSessionParams) (sqlcgen.BinanceTransfer, error) {
	f.binanceTransfers = append(f.binanceTransfers, arg)
	return sqlcgen.BinanceTransfer{ID: int64(len(f.binanceTransfers))}, nil
}

// addPosition adds a 1:1 asset + primary subasset priced via its own yahoo ticker
// (ticker+".YAHOO"), automatically registered in the shared fakePrices price map at
// 100 PLN — computeValues fails loudly on any tradable position without a price, so
// every fixture needs one. target (nil = no target) lives on the asset (task 156).
func (f *fakeQueries) addPosition(ticker, portfolio, exchange, priceCurrency string, target *decimal.Decimal, regularPln decimal.Decimal) int64 {
	aid := f.nextAID
	f.nextAID++
	f.assets[aid] = sqlcgen.Asset{
		ID: aid, Portfolio: sqlcgen.PortfolioEnum(portfolio), Key: ticker, Name: ticker,
		Exposure: sqlcgen.ExposureEnumCRYPTO, AssetClass: sqlcgen.AssetClassEnumETF,
		TargetAllocationPct: pgnum.FromDecimalPtr(target),
		CreatedAt:           pgtype.Timestamptz{Time: time.Now(), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	id := f.nextPosID
	f.nextPosID++
	yahoo := ticker + ".YAHOO"
	f.positions[id] = sqlcgen.Position{
		ID: id, Ticker: ticker, Name: ticker, Portfolio: sqlcgen.PortfolioEnum(portfolio), Exchange: sqlcgen.ExchangeEnum(exchange),
		AssetClass: sqlcgen.AssetClassEnumETF, Exposure: sqlcgen.ExposureEnumCASH, PriceCurrency: sqlcgen.CurrencyEnum(priceCurrency),
		PurchaseCurrency: sqlcgen.CurrencyEnum(priceCurrency),
		UnitsIke:         pgnum.FromDecimal(decimal.Zero), UnitsIkze: pgnum.FromDecimal(decimal.Zero),
		UnitsRegularEur: pgnum.FromDecimal(decimal.Zero), UnitsRegularUsd: pgnum.FromDecimal(decimal.Zero),
		UnitsRegularPln: pgnum.FromDecimal(regularPln), CompanyBacked: true, YahooTicker: &yahoo,
		AssetID: aid, IsPrimary: true,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	sharedFakePrices[yahoo] = decimal.NewFromInt(100)
	return id
}

// sharedFakePrices backs fakePrices.PricePlnByYahooTicker — package-level so addPosition (a
// method on fakeQueries, which has no reference to the Service's own PriceSource) can register
// a price for every fixture position without extra plumbing. Reset per test via t.Cleanup if a
// test needs isolation from another test's tickers (none currently do, since tickers are unique
// per test).
var sharedFakePrices = map[string]decimal.Decimal{}

type fakePrices struct{}

func (fakePrices) PricePlnByYahooTicker(_ context.Context) (map[string]decimal.Decimal, error) {
	return sharedFakePrices, nil
}
func (fakePrices) AllRatesToPln(_ context.Context) (map[string]decimal.Decimal, error) {
	return map[string]decimal.Decimal{"PLN": decimal.NewFromInt(1), "EUR": decimal.RequireFromString("4.3"), "USD": decimal.RequireFromString("4.0")}, nil
}

func newTestService(fq *fakeQueries) *Service {
	fq.settings["ike_annual_limit_pln"] = valueOrDefault(fq.settings["ike_annual_limit_pln"], "0")
	fq.settings["ikze_annual_limit_pln"] = valueOrDefault(fq.settings["ikze_annual_limit_pln"], "0")
	fq.settings["retirement.monthly_contribution_pln"] = valueOrDefault(fq.settings["retirement.monthly_contribution_pln"], "0")
	return NewService(fq, nil, nil, fakePrices{})
}

func valueOrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
