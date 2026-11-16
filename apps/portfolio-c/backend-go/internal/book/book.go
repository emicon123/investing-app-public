// Package book is portfolioC's one siblingcore parameterisation (ADR-113): two independent books
// (portfolio_enum VARIABLE, FIXED) with a single units column each.
package book

import "investing-app/common-go/siblingcore"

// The portfolio_enum values.
const (
	Variable siblingcore.Portfolio = "VARIABLE"
	Fixed    siblingcore.Portfolio = "FIXED"
)

// Config: VARIABLE and FIXED, one bucket whose request field is "units"; every bought unit records
// a purchase lot.
var Config = siblingcore.Config{
	Portfolios:    []siblingcore.Portfolio{Variable, Fixed},
	PortfolioEnum: "pl.shared.entity.enums.Portfolio",
	Buckets:       []siblingcore.BucketSpec{{Bucket: siblingcore.SingleBucket, Field: "units", TracksLots: true}},
}

// PortfolioNames is Config.Portfolios as strings, in config order — the one list the historia
// read path, the admin backfill and account cash iterate or validate against.
func PortfolioNames() []string {
	out := make([]string, 0, len(Config.Portfolios))
	for _, p := range Config.Portfolios {
		out = append(out, string(p))
	}
	return out
}
