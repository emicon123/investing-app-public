package position

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/pgnum"
)

// assetInfo is this package's own pgtype-free view of an `assets` row — the group root.
// Targets live ONLY here (positions rows never carry them since V75).
type assetInfo struct {
	ID                         int64
	Portfolio                  string
	Key                        string
	Name                       string
	Exposure                   string
	AssetClass                 string
	TargetAllocationPct        *decimal.Decimal
	TargetAllocationPctRiskOff *decimal.Decimal
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// decoded is this package's own pgtype-free view of a `positions` (subasset) row — the
// intermediate shape service.go works with before adding computed fields. Group membership
// comes from AssetID (the FK); AssetKey/IsPrimary ride along for grouping and display.
// Named distinctly from AssetDTO because it carries no computed/subasset fields — those are
// attached later by the service.
type decoded struct {
	ID               int64
	Ticker           string
	Name             string
	Portfolio        string
	Exchange         string
	AssetClass       string
	Exposure         string
	PriceCurrency    string
	PurchaseCurrency string
	UnitsIke         decimal.Decimal
	UnitsIkze        decimal.Decimal
	UnitsRegularEur  decimal.Decimal
	UnitsRegularUsd  decimal.Decimal
	UnitsRegularPln  decimal.Decimal
	TotalSpentPln    *decimal.Decimal
	AvgBuyPrice      *decimal.Decimal
	YahooTicker      *string
	CompanyBacked    bool
	AssetID          int64
	AssetKey         string
	IsPrimary        bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// SubassetAssetID, SubassetIsPrimary and SubassetTicker make decoded a positioncalc.SubassetRow,
// so the shared OrderSubassets / PrimaryOf group it (task 179).
func (d decoded) SubassetAssetID() int64  { return d.AssetID }
func (d decoded) SubassetIsPrimary() bool { return d.IsPrimary }
func (d decoded) SubassetTicker() string  { return d.Ticker }

func (p decoded) UnitsTotal() decimal.Decimal {
	return p.UnitsIke.Add(p.UnitsIkze).Add(p.UnitsRegularEur).Add(p.UnitsRegularUsd).Add(p.UnitsRegularPln)
}

func decodeAsset(row sqlcgen.Asset) (assetInfo, error) {
	targetPct, err := pgnum.ToDecimalPtr(row.TargetAllocationPct)
	if err != nil {
		return assetInfo{}, fmt.Errorf("position: decode assets.target_allocation_pct for %s: %w", row.Key, err)
	}
	targetRiskOff, err := pgnum.ToDecimalPtr(row.TargetAllocationPctRiskOff)
	if err != nil {
		return assetInfo{}, fmt.Errorf("position: decode assets.target_allocation_pct_risk_off for %s: %w", row.Key, err)
	}
	return assetInfo{
		ID: row.ID, Portfolio: string(row.Portfolio), Key: row.Key, Name: row.Name,
		Exposure: string(row.Exposure), AssetClass: string(row.AssetClass),
		TargetAllocationPct: targetPct, TargetAllocationPctRiskOff: targetRiskOff,
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func decodeAssets(rows []sqlcgen.Asset) ([]assetInfo, error) {
	out := make([]assetInfo, 0, len(rows))
	for _, r := range rows {
		a, err := decodeAsset(r)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func decodeRow(r sqlcgen.Position, assetKeyByID map[int64]string) (decoded, error) {
	unitsIke, err := pgnum.ToDecimal(r.UnitsIke)
	if err != nil {
		return decoded{}, fmt.Errorf("position: decode units_ike for %s: %w", r.Ticker, err)
	}
	unitsIkze, err := pgnum.ToDecimal(r.UnitsIkze)
	if err != nil {
		return decoded{}, fmt.Errorf("position: decode units_ikze for %s: %w", r.Ticker, err)
	}
	unitsEur, err := pgnum.ToDecimal(r.UnitsRegularEur)
	if err != nil {
		return decoded{}, fmt.Errorf("position: decode units_regular_eur for %s: %w", r.Ticker, err)
	}
	unitsUsd, err := pgnum.ToDecimal(r.UnitsRegularUsd)
	if err != nil {
		return decoded{}, fmt.Errorf("position: decode units_regular_usd for %s: %w", r.Ticker, err)
	}
	unitsPln, err := pgnum.ToDecimal(r.UnitsRegularPln)
	if err != nil {
		return decoded{}, fmt.Errorf("position: decode units_regular_pln for %s: %w", r.Ticker, err)
	}

	var totalSpent, avgBuy *decimal.Decimal
	if r.TotalSpentPln.Valid {
		v, err := pgnum.ToDecimal(r.TotalSpentPln)
		if err != nil {
			return decoded{}, fmt.Errorf("position: decode total_spent_pln for %s: %w", r.Ticker, err)
		}
		totalSpent = &v
	}
	if r.AvgBuyPrice.Valid {
		v, err := pgnum.ToDecimal(r.AvgBuyPrice)
		if err != nil {
			return decoded{}, fmt.Errorf("position: decode avg_buy_price for %s: %w", r.Ticker, err)
		}
		avgBuy = &v
	}

	key, ok := assetKeyByID[r.AssetID]
	if !ok {
		return decoded{}, fmt.Errorf("position: subasset %s references missing asset %d", r.Ticker, r.AssetID)
	}

	return decoded{
		ID:               r.ID,
		Ticker:           r.Ticker,
		Name:             r.Name,
		Portfolio:        string(r.Portfolio),
		Exchange:         string(r.Exchange),
		AssetClass:       string(r.AssetClass),
		Exposure:         string(r.Exposure),
		PriceCurrency:    string(r.PriceCurrency),
		PurchaseCurrency: string(r.PurchaseCurrency),
		UnitsIke:         unitsIke,
		UnitsIkze:        unitsIkze,
		UnitsRegularEur:  unitsEur,
		UnitsRegularUsd:  unitsUsd,
		UnitsRegularPln:  unitsPln,
		TotalSpentPln:    totalSpent,
		AvgBuyPrice:      avgBuy,
		YahooTicker:      r.YahooTicker,
		CompanyBacked:    r.CompanyBacked,
		AssetID:          r.AssetID,
		AssetKey:         key,
		IsPrimary:        r.IsPrimary,
		CreatedAt:        r.CreatedAt.Time,
		UpdatedAt:        r.UpdatedAt.Time,
	}, nil
}

func decodeRows(rows []sqlcgen.Position, assets []assetInfo) ([]decoded, error) {
	keyByID := make(map[int64]string, len(assets))
	for _, a := range assets {
		keyByID[a.ID] = a.Key
	}
	out := make([]decoded, 0, len(rows))
	for _, r := range rows {
		d, err := decodeRow(r, keyByID)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ToDeltaParams builds the canonical UpdatePositionDelta sqlc params for a PositionUnits value —
// the single shared conversion internal/rebalancing and internal/purchasesession both call
// instead of each maintaining their own byte-identical copy.
func ToDeltaParams(id int64, u PositionUnits) sqlcgen.UpdatePositionDeltaParams {
	return sqlcgen.UpdatePositionDeltaParams{
		ID: id, UnitsIke: pgnum.FromDecimal(u.UnitsIke), UnitsIkze: pgnum.FromDecimal(u.UnitsIkze),
		UnitsRegularEur: pgnum.FromDecimal(u.UnitsRegularEur), UnitsRegularUsd: pgnum.FromDecimal(u.UnitsRegularUsd),
		UnitsRegularPln: pgnum.FromDecimal(u.UnitsRegularPln), TotalSpentPln: pgnum.FromDecimalPtr(u.TotalSpentPln),
	}
}

func toDTOBase(a assetInfo, subs []decoded) AssetDTO {
	base := make([]SubassetDTO, 0, len(subs))
	for _, d := range subs {
		base = append(base, SubassetDTO{
			ID: d.ID, AssetID: d.AssetID, IsPrimary: d.IsPrimary,
			Ticker: d.Ticker, Name: d.Name, Portfolio: d.Portfolio, Exchange: d.Exchange, AssetClass: d.AssetClass,
			PriceCurrency: d.PriceCurrency, PurchaseCurrency: d.PurchaseCurrency,
			UnitsIke: d.UnitsIke, UnitsIkze: d.UnitsIkze, UnitsRegularEur: d.UnitsRegularEur,
			UnitsRegularUsd: d.UnitsRegularUsd, UnitsRegularPln: d.UnitsRegularPln,
			EffectiveUnits: d.UnitsTotal(), YahooTicker: d.YahooTicker, CompanyBacked: d.CompanyBacked,
			TotalSpentPln: d.TotalSpentPln, AvgBuyPrice: d.AvgBuyPrice,
		})
	}
	return AssetDTO{
		ID: a.ID, Portfolio: a.Portfolio, Key: a.Key, Name: a.Name, Exposure: a.Exposure,
		AssetClass: a.AssetClass, TargetAllocationPct: a.TargetAllocationPct,
		TargetAllocationPctRiskOff: a.TargetAllocationPctRiskOff,
		CreatedAt:                  a.CreatedAt, UpdatedAt: a.UpdatedAt, Subassets: base,
	}
}
