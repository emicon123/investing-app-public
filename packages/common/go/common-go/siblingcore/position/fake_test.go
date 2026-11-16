package position

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
	"investing-app/common-go/siblingcore"
	"investing-app/common-go/siblingcore/internal/coretest"
)

// fakeStore is position.Store over coretest.Book with the adapters' semantics: live reads skip
// soft-deleted rows, *AnyState reads do not, uniqueness is (portfolio, key) / (portfolio, ticker)
// over every row, an insert writes zero units into every configured bucket.
type fakeStore struct {
	*coretest.Book
	cfg siblingcore.Config
}

var errUnique = &pgconn.PgError{Code: "23505"}

func newStore(cfg siblingcore.Config) *fakeStore { return &fakeStore{Book: &coretest.Book{}, cfg: cfg} }

func (f *fakeStore) asset(id int64) *siblingcore.Asset {
	for i := range f.Assets {
		if f.Assets[i].ID == id {
			return &f.Assets[i]
		}
	}
	return nil
}

func (f *fakeStore) GetAsset(_ context.Context, id int64) (siblingcore.Asset, error) {
	if a := f.asset(id); a != nil && !a.Deleted {
		return *a, nil
	}
	return siblingcore.Asset{}, pgx.ErrNoRows
}

func (f *fakeStore) GetAssetAnyState(_ context.Context, id int64) (siblingcore.Asset, error) {
	if a := f.asset(id); a != nil {
		return *a, nil
	}
	return siblingcore.Asset{}, pgx.ErrNoRows
}

func (f *fakeStore) GetSubasset(_ context.Context, id int64) (siblingcore.Subasset, error) {
	if s := f.Sub(id); s != nil && !s.Deleted {
		return *s, nil
	}
	return siblingcore.Subasset{}, pgx.ErrNoRows
}

func (f *fakeStore) FindAssetByKeyAnyState(_ context.Context, p siblingcore.Portfolio, key string) (siblingcore.Asset, error) {
	for _, a := range f.Assets {
		if a.Portfolio == p && a.Key == key {
			return a, nil
		}
	}
	return siblingcore.Asset{}, pgx.ErrNoRows
}

func (f *fakeStore) FindSubassetByTickerAnyState(_ context.Context, p siblingcore.Portfolio, ticker string) (siblingcore.Subasset, error) {
	for _, s := range f.Subs {
		if s.Portfolio == p && s.Ticker == ticker {
			return s, nil
		}
	}
	return siblingcore.Subasset{}, pgx.ErrNoRows
}

func (f *fakeStore) InsertAsset(ctx context.Context, a siblingcore.Asset) (siblingcore.Asset, error) {
	if _, err := f.FindAssetByKeyAnyState(ctx, a.Portfolio, a.Key); err == nil {
		return siblingcore.Asset{}, errUnique
	}
	a.ID = f.NextID()
	f.Assets = append(f.Assets, a)
	f.Log("insert asset " + a.Key)
	return a, nil
}

func (f *fakeStore) InsertSubasset(ctx context.Context, s siblingcore.Subasset) (siblingcore.Subasset, error) {
	if _, err := f.FindSubassetByTickerAnyState(ctx, s.Portfolio, s.Ticker); err == nil {
		return siblingcore.Subasset{}, errUnique
	}
	zero := map[siblingcore.Bucket]decimal.Decimal{}
	for _, b := range f.cfg.Buckets {
		zero[b.Bucket] = decimal.Zero
	}
	s.ID = f.NextID()
	s.Holding = siblingcore.Holding{Units: siblingcore.NewUnits(zero)}
	f.Subs = append(f.Subs, s)
	f.Log("insert subasset " + s.Ticker)
	return s, nil
}

func (f *fakeStore) RestoreAsset(_ context.Context, r AssetRestore) (siblingcore.Asset, error) {
	a := f.asset(r.ID)
	a.Deleted, a.Name, a.Exposure, a.AssetClass = false, r.Name, r.Exposure, r.AssetClass
	if r.TargetAllocationPct != nil {
		a.TargetAllocationPct = r.TargetAllocationPct
	}
	if r.TargetAllocationPctRiskOff != nil {
		a.TargetAllocationPctRiskOff = r.TargetAllocationPctRiskOff
	}
	f.Log("restore asset " + a.Key)
	return *a, nil
}

func (f *fakeStore) RestoreSubasset(_ context.Context, r SubassetRestore) (siblingcore.Subasset, error) {
	s := f.Sub(r.ID)
	s.Deleted, s.AssetID, s.IsPrimary, s.Name, s.AssetClass, s.PriceCurrency = false, r.AssetID, r.IsPrimary, r.Name, r.AssetClass, r.PriceCurrency
	if r.Exposure != nil {
		s.Exposure = *r.Exposure
	}
	if r.YahooTicker != nil {
		s.YahooTicker = r.YahooTicker
	}
	if r.SortOrder != nil {
		s.SortOrder = *r.SortOrder
	}
	f.Log("restore subasset " + s.Ticker)
	return *s, nil
}

func (f *fakeStore) UpdateSubassetFields(_ context.Context, s siblingcore.Subasset) error {
	cur := f.Sub(s.ID)
	cur.Holding, cur.AvgBuyPrice, cur.Name, cur.GroupName = s.Holding, s.AvgBuyPrice, s.Name, s.GroupName
	cur.AssetClass, cur.YahooTicker, cur.SortOrder, cur.AssetID = s.AssetClass, s.YahooTicker, s.SortOrder, s.AssetID
	f.Log("update subasset " + s.Ticker)
	return nil
}

func (f *fakeStore) UpdateAssetFields(_ context.Context, a siblingcore.Asset) (siblingcore.Asset, error) {
	cur := f.asset(a.ID)
	cur.TargetAllocationPct, cur.TargetAllocationPctRiskOff, cur.Name = a.TargetAllocationPct, a.TargetAllocationPctRiskOff, a.Name
	f.Log("update asset " + a.Key)
	return *cur, nil
}

func (f *fakeStore) ClearAssetPrimary(_ context.Context, assetID int64) error {
	for i := range f.Subs {
		if f.Subs[i].AssetID == assetID && !f.Subs[i].Deleted {
			f.Subs[i].IsPrimary = false
		}
	}
	f.Log("clear primary")
	return nil
}

func (f *fakeStore) SetSubassetPrimary(_ context.Context, assetID, subassetID int64) error {
	s := f.Sub(subassetID)
	if s == nil || s.Deleted || s.AssetID != assetID {
		return pgx.ErrNoRows
	}
	s.IsPrimary = true
	f.Log("set primary " + s.Ticker)
	return nil
}

func (f *fakeStore) SoftDeleteSubasset(_ context.Context, id int64) error {
	s := f.Sub(id)
	s.Deleted = true
	f.Log("delete subasset " + s.Ticker)
	return nil
}

func (f *fakeStore) SoftDeleteAsset(_ context.Context, assetID int64) error {
	f.asset(assetID).Deleted = true
	for i := range f.Subs {
		if f.Subs[i].AssetID == assetID {
			f.Subs[i].Deleted = true
		}
	}
	f.Log("delete asset")
	return nil
}

var _ Store = (*fakeStore)(nil)

// addAsset stores a live asset with one primary subasset holding units in the config's first
// bucket and returns (asset id, subasset id).
func (f *fakeStore) addAsset(p siblingcore.Portfolio, ticker, target, units string) (int64, int64) {
	a := siblingcore.Asset{ID: f.NextID(), Portfolio: p, Key: ticker, Name: ticker, Exposure: "STOCKS", AssetClass: "ETF"}
	if target != "" {
		a.TargetAllocationPct = coretest.DP(target)
	}
	f.Assets = append(f.Assets, a)
	return a.ID, f.addSub(p, a.ID, ticker, units, true)
}

func (f *fakeStore) addSub(p siblingcore.Portfolio, assetID int64, ticker, units string, primary bool) int64 {
	y := ticker + ".WA"
	s := siblingcore.Subasset{
		ID: f.NextID(), AssetID: assetID, Portfolio: p, IsPrimary: primary, Ticker: ticker, Name: ticker,
		Exchange: "XTB", AssetClass: "ETF", Exposure: "STOCKS", PriceCurrency: "PLN", YahooTicker: &y,
		Holding: siblingcore.Holding{Units: coretest.U(string(f.cfg.Buckets[0].Bucket), units)},
	}
	f.Subs = append(f.Subs, s)
	return s.ID
}

func newSvc(f *fakeStore) *Service { return NewService[Store](f.cfg, f, nil, f.Book) }

func appErr(t *testing.T, err error, status int, field, message string) {
	t.Helper()
	var e *httpx.AppError
	if !errors.As(err, &e) || e.Status != status || e.Field != field || e.Message != message {
		t.Fatalf("expected %d %s %q, got %#v", status, field, message, err)
	}
}
