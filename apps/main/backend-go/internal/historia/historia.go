// Package historia serves the history endpoint and writes the nightly historia snapshots (ported
// from the Java HistoriaService + HistoriaScheduler + InflationIndexService's inflation-read half,
// ADR-094 / doc 13 §7). It owns the `historia` table exclusively (the scheduler below is the only
// writer; the GET path never writes).
//
// Deliberately NOT ported from the Java history packages (doc 13 §5 smell #3): the
// Iterator/Memento/Caretaker/Visitor "behavioral quartet". A Go slice already iterates, a struct
// copied by value already is a memento, and there is no undo requirement — this package is a
// paginated query plus a snapshot struct, full stop. no_patterns_test.go pins that.
package historia

import (
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/xirr"
)

// defaultPageSize mirrors HistoriaService.DEFAULT_PAGE_SIZE (and HistoriaController's
// ?size default of 50).
const defaultPageSize = 50

// minDate / maxDate are the defaults for absent ?from / ?to (both inclusive).
var (
	minDate = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	maxDate = time.Date(2100, 12, 31, 0, 0, 0, 0, time.UTC)
)

// trackedPortfolios mirrors HistoriaScheduler.TRACKED_PORTFOLIOS — the two portfolios
// snapshotted every night, in order.
var trackedPortfolios = []string{"MAIN", "RETIREMENT"}

// ListParams is the service-layer shape of GET /history's query params, after the
// handler unwraps the oapi-codegen binding (nil = absent).
type ListParams struct {
	Portfolio *string
	From      *time.Time
	To        *time.Time
	Page      *int
	Size      *int
}

// EntryDTO is the service-layer shape of one historia row with its response-time
// computed fields — free of sqlc row types and openapi response types (doc 13's
// layering rule). Nil pointers are the inflation enrichments (and xirr) being
// unavailable, exactly as HistoriaEntryDto carries nulls.
type EntryDTO struct {
	ID             int64
	SnapshotDate   time.Time
	Portfolio      string
	TotalDeposits  decimal.Decimal
	PortfolioValue decimal.Decimal
	Xirr           *decimal.Decimal
	ReturnPct      decimal.Decimal
	ProfitPln      decimal.Decimal
	PositionValues map[string]decimal.Decimal // nil = legacy NULL (pre-V30)
	CreatedAt      time.Time
	// InflationAdjustedDeposits is the PLN break-even line (every deposit inflated from
	// its own month to the snapshot's); RealReturnPct is PortfolioValue against it.
	InflationAdjustedDeposits *decimal.Decimal
	RealReturnPct             *decimal.Decimal
	AnnualizedInflationPct    *decimal.Decimal
}

// PageDTO is one paginated listHistoria result.
type PageDTO struct {
	Items  []EntryDTO
	Total  int64
	Number int
	Size   int
}

// CashFlow is one dated PLN cash flow — deposits enter negative for the XIRR solver
// (common-go/xirr, ADR-112), positive for the inflation-adjusted deposits (inflation
// math in common-go/historiaquery), the current portfolio value enters positive as the
// XIRR terminal flow. One shape shared with the sibling apps' historia read path.
type CashFlow = xirr.Flow

// dateOnly normalizes t to a UTC-midnight calendar date.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dateKey is the map key for per-date lookups (snapshot upserts, log lines).
func dateKey(t time.Time) string {
	return dateOnly(t).Format("2006-01-02")
}
