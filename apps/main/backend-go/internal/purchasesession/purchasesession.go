// Package purchasesession serves the purchase-session endpoints (ported from the Java
// PurchaseSessionService, PurchasePlanningService and the SessionState classes, ADR-094 / doc 13
// §7). It owns `purchase_sessions`/`purchase_session_items` exclusively, plus the
// purchase-completion write path into `binance_transfers` (source='SESSION').
//
// Per diagram doc 150 §2, this package is split into three files matching three genuinely separate
// responsibilities (Single Responsibility, applied while the code is being touched anyway — not
// scope creep):
//
//   - lifecycle.go: session/item CRUD gated by status. createSession's OWN persistence of the
//     new purchase_sessions/purchase_session_items rows lives here too (architect-output §1's
//     refinement) — it never touches positions/deposits/asset_purchases/binance_transfers.
//   - planning.go: the PurchasePlanningService port — pure functions over plain Go values, ZERO
//     DB dependency (confirm by checking this package's import list: planning.go imports no
//     sqlcgen/pgx/pgtype).
//   - execution.go: exactly completeSession's body — the FOR UPDATE-gated, money-critical
//     transaction touching positions/deposits/asset_purchases/binance_transfers.
package purchasesession

import (
	"time"

	"github.com/shopspring/decimal"
)

// Session status values — the direct port of SessionStatus (2 values only; see task 150
// Finding 2, the phase task file's "draft/confirmed/done" description does not match the code).
const (
	StatusPLANNED = "PLANNED"
	StatusDONE    = "DONE"
)

// PortfoliosIncluded values — the direct port of PortfoliosIncluded.
const (
	PortfoliosIncludedBOTH       = "BOTH"
	PortfoliosIncludedMAIN       = "MAIN"
	PortfoliosIncludedRETIREMENT = "RETIREMENT"
)

// CanUpdate/CanComplete/CanDelete replace the Java SessionState/PlannedState/DoneState classes,
// collapsed from an interface + 2 structs to a value type (plain string) + pure predicate functions
// — the diagram doc 150 §0's explicitly adopted, simplified shape for a 2-state machine. Same
// intent (behavior keyed by state), no class ceremony. (The Java onComplete() transition has no Go
// counterpart: the complete query sets status = 'DONE' directly.)
func CanUpdate(status string) bool   { return status == StatusPLANNED }
func CanComplete(status string) bool { return status == StatusPLANNED }
func CanDelete(status string) bool   { return status == StatusPLANNED }

// SessionItem is the service-layer shape of one purchase_session_items row, plus the transient
// (unsaved, id==0) rows planning.go builds in memory. A pointer type throughout
// planning.go/execution.go so identity-based dedup/merge (removeByIdentity in upsertSubassetRow)
// works on the row itself.
type SessionItem struct {
	ID              int64
	SessionID       int64
	Portfolio       string
	Ticker          string
	AccountType     string
	PlannedUnits    *decimal.Decimal
	PlannedValuePln *decimal.Decimal
	ActualUnits     *decimal.Decimal
	ActualValuePln  *decimal.Decimal
	PricePlnAtPlan  *decimal.Decimal
	IsMigrationSell bool
	IsMigrationBuy  bool
	PriceCurrency   string // enriched post-mapping, never persisted (mirrors enrichPriceCurrencies)
	Exchange        string // enriched post-mapping, never persisted — needed for the wire shape's
	// Exchange field (see internal/purchasesession/handler.go's own doc comment on the
	// PurchaseSessionItemPlanned spec/generated-type gap: the original DTO carries priceCurrency,
	// not exchange, at the item level — the two fields are simply not the same piece of data, so
	// this is populated as its own best-effort lookup, not a renamed priceCurrency).
}

// CurrencyBreakdown is the direct port of CurrencyBreakdownDto.
type CurrencyBreakdown struct {
	XtbPln, XtbEur, XtbEurPln, XtbUsd, XtbUsdPln, BinanceEur, BinanceEurPln, UsdPlnRate, EurPlnRate decimal.Decimal
}

// AccountLimits is the direct port of AccountLimitsDto.
type AccountLimits struct {
	IkeUsedPln, IkeRemainingPln, IkzeUsedPln, IkzeRemainingPln, IkeAnnualLimitPln, IkzeAnnualLimitPln decimal.Decimal
}

// SessionDetail is the direct port of PurchaseSessionDetail.
type SessionDetail struct {
	ID                        int64
	Status                    string
	PlannedAt                 time.Time
	DoneAt                    *time.Time
	Notes                     *string
	RetirementBudgetPln       decimal.Decimal
	MainBudgetPln             decimal.Decimal
	PortfoliosIncluded        string
	Items                     []*SessionItem
	CurrencyBreakdown         CurrencyBreakdown
	RetirementLimitsRemaining AccountLimits
	MainLimitsRemaining       AccountLimits
}

// SessionSummary is the direct port of PurchaseSessionSummary.
type SessionSummary struct {
	ID                  int64
	Status              string
	PlannedAt           time.Time
	DoneAt              *time.Time
	PortfoliosIncluded  string
	RetirementBudgetPln decimal.Decimal
	MainBudgetPln       decimal.Decimal
	TotalActualPln      *decimal.Decimal
	ItemCount           int
}

// CompleteResult is the direct port of PurchaseSessionCompleteResult.
type CompleteResult struct {
	SessionID          int64
	Status             string
	PurchasesWritten   int
	UpdatedPositionIds []int64
	DepositsCreated    int
}

// CreateSessionInput is the direct port of CreatePurchaseSessionRequest.
type CreateSessionInput struct {
	PortfoliosIncluded string
	CashOverridePln    *decimal.Decimal
	Notes              *string
}

// UpdateItemEntry is the direct port of UpdateSessionItemEntry.
type UpdateItemEntry struct {
	ID             int64
	ActualUnits    *decimal.Decimal
	ActualValuePln *decimal.Decimal
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
