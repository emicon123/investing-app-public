package historiaquery

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/xirr"
)

// defaultPageSize mirrors AbstractHistoriaService.DEFAULT_PAGE_SIZE (and both
// siblings' HistoriaController ?size default of 50).
const defaultPageSize = 50

// minDate / maxDate mirror AbstractHistoriaService.MIN_DATE / MAX_DATE — the
// defaults for absent ?from / ?to (both inclusive).
var (
	minDate = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	maxDate = time.Date(2100, 12, 31, 0, 0, 0, 0, time.UTC)
)

// ListParams is the service-layer shape of GET /history's query params, after the
// handler unwraps the oapi-codegen binding (nil = absent). Portfolio is nil for
// portfolioB (no such param) and required for portfolioC (the handler rejects nil before
// the service ever sees it — see Config.RequirePortfolio).
type ListParams struct {
	Portfolio *string
	From      *time.Time
	To        *time.Time
	Page      *int
	Size      *int
}

// CashFlow is one dated PLN cash flow — the one shape of common-go/xirr (ADR-112). Deposits enter
// as stored (positive PLN amounts; the XIRR callers negate them, the inflation-adjusted deposits
// use them as-is), built from the deposits table.
type CashFlow = xirr.Flow

// Row is one decoded historia table row in plain Go types — free of sqlc row
// types, so both siblings' adapters satisfy Queries through this one shape.
// Portfolio is "" for portfolioB (no such column) and VARIABLE/FIXED for portfolioC.
type Row struct {
	ID             int64
	SnapshotDate   time.Time
	Portfolio      string
	TotalDeposits  decimal.Decimal
	PortfolioValue decimal.Decimal
	Xirr           *decimal.Decimal
	PositionValues map[string]decimal.Decimal // nil = legacy NULL (pre position_values migration)
	CreatedAt      time.Time
}

// EntryDTO is the service-layer shape of one historia row with its response-time
// computed fields. Nil pointers are the inflation enrichments (and xirr) being
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
	PositionValues map[string]decimal.Decimal
	CreatedAt      time.Time
	// InflationAdjustedDeposits is the PLN break-even line (see
	// InflationAdjustedDeposits); RealReturnPct is PortfolioValue against it.
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

// dateOnly normalizes t to a UTC-midnight calendar date.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DecodePositionValues parses the position_values JSONB breakdown (NULL legacy
// rows → nil map). Shared so both siblings' adapters decode identically.
func DecodePositionValues(raw []byte) (map[string]decimal.Decimal, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var floats map[string]float64
	if err := json.Unmarshal(raw, &floats); err != nil {
		return nil, err
	}
	out := make(map[string]decimal.Decimal, len(floats))
	for k, v := range floats {
		out[k] = decimal.NewFromFloat(v)
	}
	return out, nil
}

// MarshalPositionValues serializes the snapshot breakdown for the JSONB column. An empty map
// persists as {}, not NULL.
func MarshalPositionValues(values map[string]decimal.Decimal) ([]byte, error) {
	floats := make(map[string]float64, len(values))
	for k, v := range values {
		floats[k] = v.InexactFloat64()
	}
	return json.Marshal(floats)
}
