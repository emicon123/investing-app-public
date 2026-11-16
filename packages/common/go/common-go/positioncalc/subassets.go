package positioncalc

import "sort"

// SubassetRow is the accessor seam over each app's decoded positions row: the three fields the
// asset grouping reads. Like CurrentAndDeviationPct, OrderSubassets and PrimaryOf are used by all
// three apps (task 179), not only portfolioB/portfolioC.
type SubassetRow interface {
	SubassetAssetID() int64
	SubassetIsPrimary() bool
	SubassetTicker() string
}

// OrderSubassets returns the rows of assetID: primary first, the rest sorted by ticker (stable,
// so equal tickers keep their input order). nil when the asset has no rows.
func OrderSubassets[T SubassetRow](rows []T, assetID int64) []T {
	var out []T
	for _, r := range rows {
		if r.SubassetAssetID() == assetID && r.SubassetIsPrimary() {
			out = append(out, r)
		}
	}
	var rest []T
	for _, r := range rows {
		if r.SubassetAssetID() == assetID && !r.SubassetIsPrimary() {
			rest = append(rest, r)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].SubassetTicker() < rest[j].SubassetTicker() })
	return append(out, rest...)
}

// PrimaryOf returns a pointer into rows to the primary row of assetID, or nil when the asset is
// (transiently) primary-less — only possible mid-flip inside a transaction, never on committed
// data.
func PrimaryOf[T SubassetRow](rows []T, assetID int64) *T {
	for i := range rows {
		if rows[i].SubassetAssetID() == assetID && rows[i].SubassetIsPrimary() {
			return &rows[i]
		}
	}
	return nil
}
