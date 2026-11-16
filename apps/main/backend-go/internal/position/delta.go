package position

import "github.com/shopspring/decimal"

// This file is ported from the Java PositionDeltaApplicator (zero fields, zero repository
// dependency — see .claude/tasks/150-architect-output.md §2). Go needs no interface at all here:
// two plain functions over a domain struct, simpler than even a Strategy/Bridge interface (doc 13
// §5's Bridge-via-composition anticipated one; the Java source had none). Callers
// (internal/rebalancing, internal/purchasesession's execution.go, and this package's own
// CRUD-adjacent write paths) read a position row, decode it into a PositionUnits value, mutate its
// units via ApplyDelta/ApplyDeltaWithCurrencyOverride, then persist the result via the one
// canonical UpdatePositionDelta sqlc query.
//
// Cost basis (total_spent_pln) is NOT maintained here any more: the old UpdateTotalSpentPln
// reduced only the sold row by its own average (wrong for a group whose go-live cost sits on the
// head row, task 161 §3.4). Since task 161a-2 the trade services compute the asset-level cost with
// the shared costbasis kernel through this package's assetcost.go (common-go/costmember Rows / Apply,
// PersistCosts) and hand the result to UpdatePositionDelta via PositionUnits.TotalSpentPln.

// Account-type and currency values (the former Java AccountType/Currency/Exchange enums). Plain
// string constants rather than the sqlcgen enum types — this file's whole point is having ZERO
// framework/DB dependency, so it must not import db/sqlcgen either.
const (
	AccountTypeIKE     = "IKE"
	AccountTypeIKZE    = "IKZE"
	AccountTypeREGULAR = "REGULAR"

	CurrencyPLN = "PLN"
	CurrencyEUR = "EUR"
	CurrencyUSD = "USD"
	CurrencyCHF = "CHF"
	CurrencyGBP = "GBP"

	ExchangeXTB     = "XTB"
	ExchangeBINANCE = "BINANCE"
)

// PositionUnits is the mutable numeric subset of a `positions` row that ApplyDelta and the
// asset-level cost bookkeeping (assetcost.go) read and write — the direct port of the Position
// entity's own BigDecimal-typed fields PositionDeltaApplicator touches. Deliberately decoupled from
// sqlc/pgtype so this file has no DB dependency; the position package's own service.go
// (and rebalancing/purchasesession's callers) convert to/from pgtype.Numeric at their own
// boundary before/after calling these functions.
type PositionUnits struct {
	Exchange      string // ExchangeXTB | ExchangeBINANCE
	PriceCurrency string // CurrencyPLN | CurrencyEUR | CurrencyUSD | CurrencyCHF | CurrencyGBP

	UnitsIke        decimal.Decimal
	UnitsIkze       decimal.Decimal
	UnitsRegularEur decimal.Decimal
	UnitsRegularUsd decimal.Decimal
	UnitsRegularPln decimal.Decimal
	// TotalSpentPln is a POINTER because the column is nullable and the null must survive the delta
	// round-trip: a NULL cost means "no cost recorded" (the go-live group cost lives on the head
	// row, siblings stay NULL) and must never be rewritten to 0.0000 by a units-only write (a past
	// coexistence smoke caught SMOKET1 total_spent_pln 0.0000 vs NULL). A non-nil value only
	// changes through the costbasis kernel (costmember.Apply).
	TotalSpentPln *decimal.Decimal
}

// UnitsTotal returns the effective total units held across all account types — the direct port
// of Position.computeUnitsTotal.
func (p PositionUnits) UnitsTotal() decimal.Decimal {
	return p.UnitsIke.Add(p.UnitsIkze).Add(p.UnitsRegularEur).Add(p.UnitsRegularUsd).Add(p.UnitsRegularPln)
}

// CostPln / SetCostPln let costmember write the kernel's result back (task 163).
func (p *PositionUnits) CostPln() *decimal.Decimal     { return p.TotalSpentPln }
func (p *PositionUnits) SetCostPln(c *decimal.Decimal) { p.TotalSpentPln = c }

// ApplyDelta applies delta to accountType's sub-bucket, using pos's own PriceCurrency as the
// REGULAR bucket for XTB positions and PLN for every other exchange (Binance, PKO CHF savings) —
// the direct port of PositionDeltaApplicator.apply. This is the entry point completeSession
// itself uses (no currency override — the position's own priceCurrency decides the REGULAR
// bucket).
func ApplyDelta(pos *PositionUnits, accountType string, delta decimal.Decimal) {
	switch accountType {
	case AccountTypeIKE:
		pos.UnitsIke = pos.UnitsIke.Add(delta)
	case AccountTypeIKZE:
		pos.UnitsIkze = pos.UnitsIkze.Add(delta)
	case AccountTypeREGULAR:
		bucket := CurrencyPLN
		if pos.Exchange == ExchangeXTB {
			bucket = pos.PriceCurrency
		}
		applyRegular(pos, bucket, delta)
	}
}

// ApplyDeltaWithCurrencyOverride applies delta to accountType's sub-bucket, with an explicit
// currency override for the REGULAR bucket (falling back to pos's own PriceCurrency when
// regularCurrency is empty) — the direct port of PositionDeltaApplicator.applyWithCurrencyOverride,
// used by internal/rebalancing.ExecuteTransfer, which lets the caller specify which XTB
// sub-bucket (PLN/EUR/USD) a leg draws from or adds to.
func ApplyDeltaWithCurrencyOverride(pos *PositionUnits, accountType, regularCurrency string, delta decimal.Decimal) {
	switch accountType {
	case AccountTypeIKE:
		pos.UnitsIke = pos.UnitsIke.Add(delta)
	case AccountTypeIKZE:
		pos.UnitsIkze = pos.UnitsIkze.Add(delta)
	case AccountTypeREGULAR:
		bucket := regularCurrency
		if bucket == "" {
			bucket = pos.PriceCurrency
		}
		applyRegular(pos, bucket, delta)
	}
}

func applyRegular(pos *PositionUnits, currency string, delta decimal.Decimal) {
	switch currency {
	case CurrencyEUR:
		pos.UnitsRegularEur = pos.UnitsRegularEur.Add(delta)
	case CurrencyUSD:
		pos.UnitsRegularUsd = pos.UnitsRegularUsd.Add(delta)
	default:
		pos.UnitsRegularPln = pos.UnitsRegularPln.Add(delta)
	}
}
