package siblingcore

import (
	"investing-app/common-go/positioncalc"
	"investing-app/common-go/rebalancecalc"
)

// SubassetAssetID, SubassetIsPrimary and SubassetTicker make Subasset a positioncalc.SubassetRow,
// so the shared ordering helpers apply: positioncalc.OrderSubassets(subs, assetID) (primary
// first, then ticker ascending) and positioncalc.PrimaryOf(subs, assetID) (nil while a
// set-primary flip is in flight).
func (s Subasset) SubassetAssetID() int64  { return s.AssetID }
func (s Subasset) SubassetIsPrimary() bool { return s.IsPrimary }
func (s Subasset) SubassetTicker() string  { return s.Ticker }

var _ positioncalc.SubassetRow = Subasset{}

// PositionInputs maps one book's live subassets (in their read order) onto the shared valuation
// kernel's inputs: each row's target is its asset's, and a non-primary row folds into its asset's
// primary ticker (a read-time derivation from asset_id, never a stored string). Rows of an asset
// missing from assets get no target and no parent.
func PositionInputs(assets []Asset, subs []Subasset) []rebalancecalc.PositionInput {
	byID := make(map[int64]Asset, len(assets))
	for _, a := range assets {
		byID[a.ID] = a
	}
	primaryTicker := make(map[int64]string, len(assets))
	for _, s := range subs {
		if _, ok := byID[s.AssetID]; ok && s.IsPrimary {
			primaryTicker[s.AssetID] = s.Ticker
		}
	}
	out := make([]rebalancecalc.PositionInput, 0, len(subs))
	for _, s := range subs {
		in := rebalancecalc.PositionInput{
			Ticker: s.Ticker, Name: s.Name, Exchange: s.Exchange, PriceCurrency: s.PriceCurrency,
			UnitsTotal: s.Holding.Units.Total(), YahooTicker: s.YahooTicker, IsCompanionRow: !s.IsPrimary,
		}
		if a, ok := byID[s.AssetID]; ok {
			in.TargetAllocationPct = a.TargetAllocationPct
		}
		if !s.IsPrimary {
			if t, ok := primaryTicker[s.AssetID]; ok {
				in.CompanionParentTicker = &t
			}
		}
		out = append(out, in)
	}
	return out
}
