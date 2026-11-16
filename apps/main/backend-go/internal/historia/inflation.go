package historia

import "investing-app/common-go/historiaquery"

// The inflation math itself (CumulativeFactor/CumulativeSince, Annualized,
// InflationAdjustedDeposits, RealReturn) is the single copy in
// common-go/historiaquery, shared with portfolioB and portfolioC. What stays root-specific
// is InflationIndexService's per-portfolio baselines — the fixed month inflation is
// cumulated from (annualized inflation is the only thing the baseline still feeds).

// BaselineMain / BaselineRetirement mirror InflationIndexService.BASE_BY_PORTFOLIO
// (MAIN 2022-08, RETIREMENT 2024-02).
var (
	BaselineMain       = historiaquery.YearMonth{Year: 2022, Month: 8}
	BaselineRetirement = historiaquery.YearMonth{Year: 2024, Month: 2}
)

// BaselineFor mirrors InflationIndexService.baselineFor — the fixed per-portfolio
// baseline month inflation is cumulated from.
func BaselineFor(portfolio string) historiaquery.YearMonth {
	if portfolio == "RETIREMENT" {
		return BaselineRetirement
	}
	return BaselineMain
}
