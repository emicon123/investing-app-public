// Package rebalancecalc holds the proportional-DCA math shared by portfolioB and portfolioC: allocation,
// value computation and purchase history (ADR-094, doc 13 §7; ported from the Java
// AbstractProportionalStrategy / AbstractValueComputationService /
// AbstractPortfolioValueComputationService / PurchaseHistoryEngine cluster). Unlike the main app's
// internal/rebalancingcalc (investing-app-specific business logic: the main and retirement
// strategies and the macro-derisk gate), the two siblings' math was the SAME Java class, not two
// independently-converged implementations — see .claude/tasks/151-architect-output.md §2/§4 and
// docs/architecture/diagrams/151-phase8-portfolio-b-portfolio-c-core-cluster.md §5 for the direct-source
// verification behind that claim. This package is therefore shared by BOTH apps' backend-go
// modules, not app-local.
//
// Per doc 13 §5 smell #2, the six-step Template Method of the Java base class
// (buildRows→computeTargetValues→allocate→computeUnits→makeItem→allocateToAccounts) collapses to
// ONE pure Go function, AllocateProportional, taking the one step that genuinely varies per app
// (allocateToAccounts: portfolioB's real IKE_TATA→IKE_MAMA→IKZE_TATA→IKZE_MAMA waterfall vs. portfolioC's
// identity passthrough) as a passed-in func value — no interface, no base struct, no per-app
// subclass. This package has NO database dependency of its own (a pure transform): each app's own
// internal/rebalancing package fetches and decodes its own positions (via its own sqlc Queries —
// the one place the portfolioB/portfolioC entity shapes genuinely differ, 5 IKE/IKZE unit buckets vs. one
// flat `units` column) into this package's plain PositionInput/PositionRow shapes before calling
// in.
//
// The Java portfolioB strategy bypassed the base class's orchestrating calculate() method (a
// singleton-bean thread-safety workaround — see architect-output §2) and called the five shared
// steps directly, feeding real per-request account balances into allocateToAccounts. That
// workaround has no reason to exist in Go: a pure function with no instance state has nothing to
// race on, so AllocateProportional calls allocateToAccounts unconditionally and identically for
// both apps — portfolioB's closure does the real waterfall, portfolioC's closure is `func(buyList, _)
// []BuyListItem { return buyList }` (an identity passthrough).
package rebalancecalc

import "github.com/shopspring/decimal"

// hundred is used throughout for percentage math (target/current allocation display).
var hundred = decimal.NewFromInt(100)

// PositionInput is the pre-value-computation, per-app-decoded view of one `positions` row —
// the one place the portfolioB/portfolioC entity shapes genuinely differ (portfolioB: units_ike_tata +
// units_ike_mama + units_ikze_tata + units_ikze_mama + units_inne summed via
// Position.computeUnitsTotal; portfolioC: the single `units` column) is resolved by each app's own
// decode step BEFORE constructing this value, via UnitsTotal — the Bridge doc 13 §5 already
// names, applied at decode time rather than as a runtime interface field.
type PositionInput struct {
	Ticker                string
	Name                  string
	Exchange              string
	PriceCurrency         string // "PLN" | "EUR" | "USD" | "CHF" | "GBP"
	TargetAllocationPct   *decimal.Decimal
	UnitsTotal            decimal.Decimal
	YahooTicker           *string
	IsCompanionRow        bool
	CompanionParentTicker *string
}

// PositionRow is ComputeValues' per-tradable-row output and AllocateProportional's own input —
// already resolved (ValuePln folds in any companion rows' value; PriceNativePln is the cache-only
// PLN-denominated native price used for units-to-buy math, zero when unavailable, matching
// AbstractValueComputationService.getNativePricePln's null->zero defensive fallback at the
// call site, see valuecomputation.go).
type PositionRow struct {
	Ticker              string
	Name                string
	Exchange            string
	TargetAllocationPct *decimal.Decimal
	ValuePln            decimal.Decimal // N_i
	PriceNativePln      decimal.Decimal // for computeUnits' DR_i; zero => DR_i = 0
}

// AccountAlloc is the direct port of AccountAllocDto — one buy item's slice of a single named
// account (portfolioB only; portfolioC's identity allocateToAccounts closure never produces these).
type AccountAlloc struct {
	AccountType string
	AllocPln    decimal.Decimal
	UnitsToBuy  decimal.Decimal
}

// BuyListItem holds the fields AllocateProportional's makeItem step populates; availableTickers is
// populated by each app's own rebalancing service AFTER calling AllocateProportional, never inside
// this package.
type BuyListItem struct {
	Ticker             string
	Name               string
	Exchange           string
	PurchaseCurrency   string
	AllocatedPln       decimal.Decimal
	AllocatedNative    decimal.Decimal
	UnitsToBuy         decimal.Decimal
	CurrentPct         decimal.Decimal
	TargetPct          decimal.Decimal
	DeviationPct       decimal.Decimal
	AccountAllocations []AccountAlloc
	AvailableTickers   []string
}
