package position

import (
	"github.com/shopspring/decimal"

	"investing-app/common-go/positioncalc"
	"investing-app/common-go/siblingcore"
)

// baseDTO is an asset with its subassets and nothing computed (the create response).
func baseDTO(a siblingcore.Asset, subs []siblingcore.Subasset) AssetDTO {
	dto := AssetDTO{Asset: a}
	for _, s := range subs {
		dto.Subassets = append(dto.Subassets, SubassetDTO{
			ID: s.ID, AssetID: s.AssetID, IsPrimary: s.IsPrimary,
			Ticker: s.Ticker, Name: s.Name, Exchange: s.Exchange, PriceCurrency: s.PriceCurrency,
			Units: s.Holding.Units, EffectiveUnits: s.Holding.Units.Total(),
			GroupName: s.GroupName, YahooTicker: s.YahooTicker, SortOrder: s.SortOrder,
		})
	}
	return dto
}

// buildDTO is an asset with group-level computed fields and priced subassets (subs ordered
// primary first).
func buildDTO(a siblingcore.Asset, subs []siblingcore.Subasset, bookValuePln decimal.Decimal, prices, rates map[string]decimal.Decimal) AssetDTO {
	dto := AssetDTO{Asset: a, Computed: computeFields(a, subs, bookValuePln, prices, rates)}
	for _, s := range subs {
		dto.Subassets = append(dto.Subassets, pricedSubasset(s, prices, rates))
	}
	return dto
}

// computeFields: valuePln = Σ subassets (the primary's own value plus every other subasset's
// units × price), deviationPct on the asset's target, UnitsTotal = the primary's own units,
// ProfitPct = the group's profit over every subasset's recorded cost, and the entry-price block
// (task 161c-1) — all positioncalc.ComputeFields. An asset without a primary (a set-primary flip
// in flight) has nothing computed.
func computeFields(a siblingcore.Asset, subs []siblingcore.Subasset, bookValuePln decimal.Decimal, prices, rates map[string]decimal.Decimal) ComputedFields {
	var primary *siblingcore.Subasset
	var companions []positioncalc.Companion
	for i := range subs {
		if subs[i].IsPrimary {
			primary = &subs[i]
			continue
		}
		c := subs[i]
		companions = append(companions, positioncalc.Companion{
			Ticker: c.Ticker, YahooTicker: c.YahooTicker, UnitsTotal: c.Holding.Units.Total(), TotalSpentPln: c.Holding.TotalSpentPln,
		})
	}
	if primary == nil {
		return ComputedFields{}
	}
	if companions == nil {
		companions = []positioncalc.Companion{}
	}
	return positioncalc.ComputeFields(
		primary.Ticker, primary.YahooTicker, primary.AssetClass, primary.PriceCurrency, a.TargetAllocationPct,
		&primary.Holding, primary.Holding.TotalSpentPln, bookValuePln, companions, prices, rates,
	)
}

// pricedSubasset prices one subasset from the caches.
func pricedSubasset(s siblingcore.Subasset, prices, rates map[string]decimal.Decimal) SubassetDTO {
	effective, pricePln, priceNative, valuePln := positioncalc.SubPositionValues(
		s.YahooTicker, s.AssetClass, s.PriceCurrency, &s.Holding, prices, rates)
	return SubassetDTO{
		ID: s.ID, AssetID: s.AssetID, IsPrimary: s.IsPrimary,
		Ticker: s.Ticker, Name: s.Name, Exchange: s.Exchange, PriceCurrency: s.PriceCurrency,
		Units: s.Holding.Units, EffectiveUnits: effective, PricePln: pricePln, PriceNative: priceNative, ValuePln: valuePln,
		YahooTicker: s.YahooTicker, GroupName: s.GroupName, SortOrder: s.SortOrder,
	}
}
