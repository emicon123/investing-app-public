package position

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// Characterisation golden for Service.Create (task 187 B3), captured before the split into
// validate -> load -> plan -> apply: for every creation path (new asset, attach, restore of a
// soft-deleted asset or subasset) and every rejection, the statement order around the one
// transaction, the stored rows and the returned DTO or error.
//
// Request-shape validation answers BEFORE the transaction opens (no BEGIN in the trace); every
// read and write of the other paths sits between BEGIN and COMMIT/ROLLBACK, in the order below;
// the response is re-read after the commit. The money-write advisory lock is taken by the
// production TransactFunc (pgtx.SerializedRunner) before the callback, so BEGIN stands for
// "lock, then callback".

type createTrace struct {
	Queries
	calls []string
}

func (c *createTrace) rec(s string) { c.calls = append(c.calls, s) }

func (c *createTrace) GetPositionByNaturalKeyAnyState(ctx context.Context, a sqlcgen.GetPositionByNaturalKeyAnyStateParams) (sqlcgen.Position, error) {
	c.rec("GetPositionByNaturalKeyAnyState " + a.Ticker)
	return c.Queries.GetPositionByNaturalKeyAnyState(ctx, a)
}
func (c *createTrace) GetAssetByIDAnyState(ctx context.Context, id int64) (sqlcgen.Asset, error) {
	c.rec(fmt.Sprintf("GetAssetByIDAnyState %d", id))
	return c.Queries.GetAssetByIDAnyState(ctx, id)
}
func (c *createTrace) GetAssetByID(ctx context.Context, id int64) (sqlcgen.Asset, error) {
	c.rec(fmt.Sprintf("GetAssetByID %d", id))
	return c.Queries.GetAssetByID(ctx, id)
}
func (c *createTrace) GetAssetByKeyAnyState(ctx context.Context, a sqlcgen.GetAssetByKeyAnyStateParams) (sqlcgen.Asset, error) {
	c.rec("GetAssetByKeyAnyState " + a.Key)
	return c.Queries.GetAssetByKeyAnyState(ctx, a)
}
func (c *createTrace) ListAllAssets(ctx context.Context) ([]sqlcgen.Asset, error) {
	c.rec("ListAllAssets")
	return c.Queries.ListAllAssets(ctx)
}
func (c *createTrace) ListAllPositions(ctx context.Context) ([]sqlcgen.Position, error) {
	c.rec("ListAllPositions")
	return c.Queries.ListAllPositions(ctx)
}
func (c *createTrace) InsertAsset(ctx context.Context, a sqlcgen.InsertAssetParams) (sqlcgen.Asset, error) {
	c.rec("InsertAsset " + a.Key)
	return c.Queries.InsertAsset(ctx, a)
}
func (c *createTrace) RestoreAsset(ctx context.Context, a sqlcgen.RestoreAssetParams) (sqlcgen.Asset, error) {
	c.rec(fmt.Sprintf("RestoreAsset %d", a.ID))
	return c.Queries.RestoreAsset(ctx, a)
}
func (c *createTrace) ClearAssetPrimary(ctx context.Context, id int64) error {
	c.rec(fmt.Sprintf("ClearAssetPrimary %d", id))
	return c.Queries.ClearAssetPrimary(ctx, id)
}
func (c *createTrace) RestorePosition(ctx context.Context, a sqlcgen.RestorePositionParams) (sqlcgen.Position, error) {
	c.rec(fmt.Sprintf("RestorePosition %d primary=%v", a.ID, a.IsPrimary))
	return c.Queries.RestorePosition(ctx, a)
}
func (c *createTrace) InsertPosition(ctx context.Context, a sqlcgen.InsertPositionParams) (sqlcgen.Position, error) {
	c.rec(fmt.Sprintf("InsertPosition %s asset=%d primary=%v", a.Ticker, a.AssetID, a.IsPrimary))
	return c.Queries.InsertPosition(ctx, a)
}

func traceError(err error) string {
	var appErr *httpx.AppError
	if errors.As(err, &appErr) {
		return fmt.Sprintf("ERR %d %s field=%q %q", appErr.Status, appErr.Code, appErr.Field, appErr.Message)
	}
	return "ERR " + err.Error()
}

func traceDTO(dto AssetDTO) string {
	var subs []string
	for _, s := range dto.Subassets {
		subs = append(subs, fmt.Sprintf("%s#%d/primary=%v", s.Ticker, s.ID, s.IsPrimary))
	}
	return fmt.Sprintf("asset #%d %s/%s/%s/%s/%s/%v/%v subs=[%s]", dto.ID, dto.Portfolio, dto.Key, dto.Name, dto.Exposure, dto.AssetClass,
		traceDec(dto.TargetAllocationPct), traceDec(dto.TargetAllocationPctRiskOff), strings.Join(subs, " "))
}

func traceDec(d *decimal.Decimal) string {
	if d == nil {
		return "nil"
	}
	return d.String()
}

// traceRow is the stored subasset row the create touched.
func traceRow(fq *fakeQueries, ticker string) string {
	for _, r := range fq.rows {
		if r.Ticker != ticker {
			continue
		}
		yahoo := "nil"
		if r.YahooTicker != nil {
			yahoo = *r.YahooTicker
		}
		units, _ := pgnum.ToDecimal(r.UnitsRegularPln)
		return fmt.Sprintf("row #%d %s/%s/%s/%s/%s/%s pc=%s yahoo=%s companyBacked=%v primary=%v deleted=%v units=%s",
			r.ID, r.Portfolio, r.Exchange, r.AssetClass, r.Exposure, r.PriceCurrency, r.Name, r.PurchaseCurrency, yahoo, r.CompanyBacked, r.IsPrimary, r.DeletedAt.Valid, units)
	}
	return "row " + ticker + " absent"
}

func TestCreate_TraceStatementsRowsAndResult_Golden(t *testing.T) {
	pct := func(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }
	yes, no := true, false
	str := func(s string) *string { return &s }
	base := CreateInput{Ticker: "T", Name: "Thing", Portfolio: "MAIN", Exchange: "XTB", AssetClass: "ETF", PriceCurrency: "PLN"}
	withNew := func(in CreateInput, n NewAssetInput) CreateInput { in.NewAsset = &n; return in }
	withAsset := func(in CreateInput, id int64) CreateInput { in.AssetID = &id; return in }

	cases := []struct {
		name  string
		setup func(fq *fakeQueries) // returns nothing; asset ids are deterministic (1)
		in    func() CreateInput
		row   string // ticker whose stored row is reported
		want  string
	}{
		{
			name: "new asset carries the targets, first subasset is primary",
			in: func() CreateInput {
				return withNew(base, NewAssetInput{Name: "New", Exposure: "EQUITY", AssetClass: "ETF", TargetAllocationPct: pct("2.5"), TargetAllocationPctRiskOff: pct("1")})
			},
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByKeyAnyState T
InsertAsset T
InsertPosition T asset=1 primary=true
COMMIT
ListAllAssets
ListAllPositions
=> asset #1 MAIN/T/New/EQUITY/ETF/2.5/1 subs=[T#1/primary=true]
row #1 MAIN/XTB/ETF/EQUITY/PLN/Thing pc=PLN yahoo=nil companyBacked=false primary=true deleted=false units=0`,
		},
		{
			name: "new asset, explicit subasset fields (stock defaults overridden)",
			in: func() CreateInput {
				in := withNew(base, NewAssetInput{Name: "Apple", Exposure: "EQUITY", AssetClass: "STOCK"})
				in.AssetClass, in.Exposure, in.PurchaseCurrency, in.YahooTicker, in.CompanyBacked = "STOCK", str("GOLD"), str("USD"), str("AAPL"), &no
				return in
			},
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByKeyAnyState T
InsertAsset T
InsertPosition T asset=1 primary=true
COMMIT
ListAllAssets
ListAllPositions
=> asset #1 MAIN/T/Apple/EQUITY/STOCK/nil/nil subs=[T#1/primary=true]
row #1 MAIN/XTB/STOCK/GOLD/PLN/Thing pc=USD yahoo=AAPL companyBacked=false primary=true deleted=false units=0`,
		},
		{
			name: "new crypto asset: companyBacked derived from the asset class",
			in: func() CreateInput {
				in := withNew(base, NewAssetInput{Name: "Coin", Exposure: "CRYPTO", AssetClass: "CRYPTO"})
				in.AssetClass = "CRYPTO"
				return in
			},
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByKeyAnyState T
InsertAsset T
InsertPosition T asset=1 primary=true
COMMIT
ListAllAssets
ListAllPositions
=> asset #1 MAIN/T/Coin/CRYPTO/CRYPTO/nil/nil subs=[T#1/primary=true]
row #1 MAIN/XTB/CRYPTO/CRYPTO/PLN/Thing pc=PLN yahoo=nil companyBacked=true primary=true deleted=false units=0`,
		},
		{
			name:  "attach: non-primary by default, exposure inherited from the asset",
			setup: func(fq *fakeQueries) { fq.putSub(fq.putAsset("MAIN", "BTC", "20"), "BTC-USD", "MAIN", "1", "", true) },
			in:    func() CreateInput { return withAsset(base, 1) },
			row:   "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByID 1
InsertPosition T asset=1 primary=false
COMMIT
ListAllAssets
ListAllPositions
=> asset #1 MAIN/BTC/BTC/CRYPTO/CRYPTO/20/nil subs=[BTC-USD#1/primary=true T#2/primary=false]
row #2 MAIN/XTB/ETF/CRYPTO/PLN/Thing pc=PLN yahoo=nil companyBacked=false primary=false deleted=false units=0`,
		},
		{
			name:  "attach as primary under an asset without a live primary",
			setup: func(fq *fakeQueries) { fq.putSub(fq.putAsset("MAIN", "BTC", "20"), "BTC-USD", "MAIN", "1", "", false) },
			in:    func() CreateInput { in := withAsset(base, 1); in.IsPrimary = &yes; return in },
			row:   "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByID 1
ListAllAssets
ListAllPositions
InsertPosition T asset=1 primary=true
COMMIT
ListAllAssets
ListAllPositions
=> asset #1 MAIN/BTC/BTC/CRYPTO/CRYPTO/20/nil subs=[T#2/primary=true BTC-USD#1/primary=false]
row #2 MAIN/XTB/ETF/CRYPTO/PLN/Thing pc=PLN yahoo=nil companyBacked=false primary=true deleted=false units=0`,
		},
		{
			name:  "attach as primary while the asset has one: 409, rolled back",
			setup: func(fq *fakeQueries) { fq.putSub(fq.putAsset("MAIN", "BTC", "20"), "BTC-USD", "MAIN", "1", "", true) },
			in:    func() CreateInput { in := withAsset(base, 1); in.IsPrimary = &yes; return in },
			row:   "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByID 1
ListAllAssets
ListAllPositions
ROLLBACK
=> ERR 409 CONFLICT field="" "asset already has a primary subasset"
row T absent`,
		},
		{
			name: "attach to an unknown asset: 404",
			in:   func() CreateInput { return withAsset(base, 999) },
			row:  "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByID 999
ROLLBACK
=> ERR 404 NOT_FOUND field="" "Asset with id 999 not found"
row T absent`,
		},
		{
			name:  "attach across portfolios: 400",
			setup: func(fq *fakeQueries) { fq.putSub(fq.putAsset("MAIN", "BTC", "20"), "BTC-USD", "MAIN", "1", "", true) },
			in:    func() CreateInput { in := withAsset(base, 1); in.Portfolio = "RETIREMENT"; return in },
			row:   "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByID 1
ROLLBACK
=> ERR 400 VALIDATION_ERROR field="portfolio" "must match the asset's portfolio"
row T absent`,
		},
		{
			name: "attach to a soft-deleted asset: 404",
			setup: func(fq *fakeQueries) {
				a := fq.putAsset("MAIN", "A", "1")
				fq.putSub(a, "A", "MAIN", "0", "", true)
				if err := NewService(fq, nil, newFakePrices()).DeleteAsset(context.Background(), a); err != nil {
					panic(err)
				}
			},
			in:  func() CreateInput { return withAsset(base, 1) },
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByID 1
ROLLBACK
=> ERR 404 NOT_FOUND field="" "Asset with id 1 not found"
row T absent`,
		},
		{
			name: "re-adding a soft-deleted asset restores asset and primary subasset, keeps units",
			setup: func(fq *fakeQueries) {
				a := fq.putAsset("MAIN", "T", "7")
				fq.putSub(a, "T", "MAIN", "3", "T.DE", true)
				fq.putSub(a, "T.SIB", "MAIN", "1", "", false)
				if err := NewService(fq, nil, newFakePrices()).DeleteAsset(context.Background(), a); err != nil {
					panic(err)
				}
			},
			in: func() CreateInput {
				in := withNew(base, NewAssetInput{Name: "T renamed", Exposure: "EQUITY", AssetClass: "ETF"})
				in.PriceCurrency, in.Name = "EUR", "Thing renamed"
				return in
			},
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByIDAnyState 1
GetAssetByKeyAnyState T
RestoreAsset 1
ClearAssetPrimary 1
RestorePosition 1 primary=true
COMMIT
ListAllAssets
ListAllPositions
=> asset #1 MAIN/T/T renamed/EQUITY/ETF/7/nil subs=[T#1/primary=true]
row #1 MAIN/XTB/ETF/CRYPTO/EUR/Thing renamed pc=PLN yahoo=T.DE companyBacked=true primary=true deleted=false units=3`,
		},
		{
			name: "re-adding a soft-deleted subasset under its live asset restores it as non-primary",
			setup: func(fq *fakeQueries) {
				a := fq.putAsset("MAIN", "A", "7")
				fq.putSub(a, "A", "MAIN", "3", "", true)
				fq.putSub(a, "T", "MAIN", "2", "", false)
				if err := NewService(fq, nil, newFakePrices()).Delete(context.Background(), 2); err != nil {
					panic(err)
				}
			},
			in:  func() CreateInput { return withAsset(base, 1) },
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByIDAnyState 1
GetAssetByID 1
RestorePosition 2 primary=false
COMMIT
ListAllAssets
ListAllPositions
=> asset #1 MAIN/A/A/CRYPTO/CRYPTO/7/nil subs=[A#1/primary=true T#2/primary=false]
row #2 MAIN/XTB/ETF/CRYPTO/PLN/Thing pc=PLN yahoo=nil companyBacked=true primary=false deleted=false units=2`,
		},
		{
			name: "a new ticker whose key matches a deleted asset restores that asset",
			setup: func(fq *fakeQueries) {
				a := fq.putAsset("MAIN", "T", "30")
				fq.putSub(a, "T.OLD", "MAIN", "0", "", true)
				if err := NewService(fq, nil, newFakePrices()).DeleteAsset(context.Background(), a); err != nil {
					panic(err)
				}
			},
			in: func() CreateInput {
				return withNew(base, NewAssetInput{Name: "Gold", Exposure: "GOLD", AssetClass: "ETF"})
			},
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByKeyAnyState T
RestoreAsset 1
ClearAssetPrimary 1
InsertPosition T asset=1 primary=true
COMMIT
ListAllAssets
ListAllPositions
=> asset #1 MAIN/T/Gold/GOLD/ETF/30/nil subs=[T#2/primary=true]
row #2 MAIN/XTB/ETF/GOLD/PLN/Thing pc=PLN yahoo=nil companyBacked=false primary=true deleted=false units=0`,
		},
		{
			name: "live duplicate natural key: 409",
			setup: func(fq *fakeQueries) {
				fq.putSub(fq.putAsset("MAIN", "T", "1"), "T", "MAIN", "1", "", true)
			},
			in: func() CreateInput {
				return withNew(base, NewAssetInput{Name: "x", Exposure: "EQUITY", AssetClass: "ETF"})
			},
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByIDAnyState 1
GetAssetByKeyAnyState T
ROLLBACK
=> ERR 409 DUPLICATE_ENTRY field="" "Resource already exists"
row #1 MAIN/XTB/CRYPTO/CRYPTO/PLN/T pc=PLN yahoo=nil companyBacked=true primary=true deleted=false units=1`,
		},
		{
			name:  "live asset already holds the new key: 409",
			setup: func(fq *fakeQueries) { fq.putAsset("MAIN", "T", "1") },
			in: func() CreateInput {
				return withNew(base, NewAssetInput{Name: "x", Exposure: "CRYPTO", AssetClass: "ETF"})
			},
			row: "T",
			want: `BEGIN
GetPositionByNaturalKeyAnyState T
GetAssetByKeyAnyState T
ROLLBACK
=> ERR 409 DUPLICATE_ENTRY field="" "Resource already exists"
row T absent`,
		},
		{
			name: "neither assetId nor newAsset: 400 before the transaction",
			in:   func() CreateInput { return base },
			row:  "T",
			want: `=> ERR 400 VALIDATION_ERROR field="assetId" "either assetId or newAsset is required"
row T absent`,
		},
		{
			name: "both assetId and newAsset: 400 before the transaction",
			in: func() CreateInput {
				return withNew(withAsset(base, 1), NewAssetInput{Name: "n", Exposure: "CRYPTO", AssetClass: "ETF"})
			},
			row: "T",
			want: `=> ERR 400 VALIDATION_ERROR field="newAsset" "newAsset must be omitted when assetId is set"
row T absent`,
		},
		{
			name: "target out of range: 400 before the transaction",
			in: func() CreateInput {
				return withNew(base, NewAssetInput{Name: "n", Exposure: "CRYPTO", AssetClass: "ETF", TargetAllocationPct: pct("100.5")})
			},
			row: "T",
			want: `=> ERR 400 VALIDATION_ERROR field="targetAllocationPct" "must be between 0 and 100"
row T absent`,
		},
		{
			name: "risk-off target out of range: 400 before the transaction",
			in: func() CreateInput {
				return withNew(base, NewAssetInput{Name: "n", Exposure: "CRYPTO", AssetClass: "ETF", TargetAllocationPctRiskOff: pct("-1")})
			},
			row: "T",
			want: `=> ERR 400 VALIDATION_ERROR field="targetAllocationPctRiskOff" "must be between 0 and 100"
row T absent`,
		},
		{
			name: "newAsset without exposure: 400 before the transaction",
			in:   func() CreateInput { return withNew(base, NewAssetInput{Name: "n", AssetClass: "ETF"}) },
			row:  "T",
			want: `=> ERR 400 VALIDATION_ERROR field="newAsset" "name, exposure and assetClass are required"
row T absent`,
		},
		{
			name: "a new asset's first subasset cannot be non-primary: 400 before the transaction",
			in: func() CreateInput {
				in := withNew(base, NewAssetInput{Name: "n", Exposure: "CRYPTO", AssetClass: "ETF"})
				in.IsPrimary = &no
				return in
			},
			row: "T",
			want: `=> ERR 400 VALIDATION_ERROR field="isPrimary" "a new asset's first subasset is always primary"
row T absent`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fq := newFakeQueries()
			if c.setup != nil {
				c.setup(fq)
			}
			tr := &createTrace{Queries: fq}
			transact := func(_ context.Context, fn func(Queries) error) error {
				tr.rec("BEGIN")
				if err := fn(tr); err != nil {
					tr.rec("ROLLBACK")
					return err
				}
				tr.rec("COMMIT")
				return nil
			}
			svc := NewService(tr, transact, newFakePrices())

			dto, err := svc.Create(context.Background(), c.in())

			result := traceDTO(dto)
			if err != nil {
				result = traceError(err)
			}
			got := strings.Join(append(tr.calls, "=> "+result, traceRow(fq, c.in().Ticker)), "\n")
			if got != c.want {
				t.Errorf("trace differs\n got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}
