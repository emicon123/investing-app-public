package historia

import (
	"testing"

	"investing-app/common-go/historiaquery"
)

// The inflation math (cumulative, annualized, inflation-adjusted deposits, real return)
// lives in common-go/historiaquery and is tested there; only the root-specific
// per-portfolio baselines are pinned here.
func TestBaselineFor(t *testing.T) {
	if BaselineFor("MAIN") != (historiaquery.YearMonth{Year: 2022, Month: 8}) ||
		BaselineFor("RETIREMENT") != (historiaquery.YearMonth{Year: 2024, Month: 2}) {
		t.Fatal("portfolio baselines must match InflationIndexService")
	}
}
