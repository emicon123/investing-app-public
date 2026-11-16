package historiaquery

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"investing-app/common-go/httpx"
)

// Queries is the narrow slice of per-app sqlc queries the historia read path
// needs, expressed in plain Go types so both siblings' generated sqlcgen packages
// satisfy it through a thin adapter (no per-app service code).
type Queries interface {
	// ListRange returns rows for portfolio in [from, to] (inclusive, date-ascending).
	// Portfolio "" means "no portfolio dimension" (portfolioB — the adapter ignores it).
	ListRange(ctx context.Context, portfolio string, from, to time.Time) ([]Row, error)
	// ListAll returns every row for portfolio, date-ascending (the no-?from/?to path —
	// AbstractPortfolioHistoriaService switches query on from/to presence, and so do we).
	ListAll(ctx context.Context, portfolio string) ([]Row, error)
	// MomAsc returns the whole inflation index as a (year, month) → CPI map.
	MomAsc(ctx context.Context) (map[YearMonth]decimal.Decimal, error)
	// DepositFlows returns (date, amountPln) pairs for portfolio, date-ascending —
	// the inflation-adjusted deposits cash flows (all deposits are PLN in both apps, no FX).
	DepositFlows(ctx context.Context, portfolio string) ([]CashFlow, error)
}

// Config is the per-app parameterization — the ONLY things that differ between
// portfolioB's and portfolioC's historia read path:
//
//   - RequirePortfolio: false for portfolioB (no ?portfolio param at all), true for
//     portfolioC (?portfolio=VARIABLE|FIXED required — absent or unknown answers 400).
//   - ValidPortfolios: the accepted values when RequirePortfolio is true.
//   - InflationBase: the fixed month inflation is cumulated from — 2017-01 for
//     portfolioB, 2026-07 for portfolioC. A per-app constant, never unified: each app's
//     thin package pins its own value with a test.
type Config struct {
	RequirePortfolio bool
	ValidPortfolios  []string
	InflationBase    YearMonth
	// EnumQualifier prefixes the 400 message for an unknown portfolio, keeping the legacy wire text
	// ("No enum constant <qualifier>.<NAME>", originally Java's Portfolio.valueOf).
	EnumQualifier string
}

// Service is the historia read service shared by portfolioB and portfolioC: thin wiring hands it an adapter
// over its own *sqlcgen.Queries plus its Config, and gets the shared behavior back (paginated list
// + best-effort inflation enrichment).
type Service struct {
	queries Queries
	cfg     Config
}

// NewService wires queries + per-app config (explicit constructor wiring, ADR-094
// §2 — no DI framework).
func NewService(queries Queries, cfg Config) *Service {
	return &Service{queries: queries, cfg: cfg}
}

// List returns one page of historia entries (ported from the Java
// AbstractHistoriaService.listHistoria, ADR-094): absent ?from/?to default to
// 2000-01-01/2100-12-31, size<=0 falls back to 50 (and the effective size is echoed in PageInfo,
// exactly as the controllers do), pagination is in-memory (fetch, sort, sublist — out-of-range
// pages answer empty data with the total preserved), and a negative page answers 400. The GET path
// never writes.
func (s *Service) List(ctx context.Context, p ListParams) (PageDTO, error) {
	page := 0
	if p.Page != nil {
		page = *p.Page
	}
	if page < 0 {
		return PageDTO{}, httpx.NewValidationError("page", "Page index must not be less than zero")
	}
	size := defaultPageSize
	if p.Size != nil && *p.Size > 0 {
		size = *p.Size
	}
	from, to := minDate, maxDate
	if p.From != nil {
		from = dateOnly(*p.From)
	}
	if p.To != nil {
		to = dateOnly(*p.To)
	}

	portfolio := ""
	if s.cfg.RequirePortfolio {
		if p.Portfolio == nil || strings.TrimSpace(*p.Portfolio) == "" {
			return PageDTO{}, httpx.NewValidationError("portfolio", "Required request parameter 'portfolio' is missing")
		}
		upper := strings.ToUpper(strings.TrimSpace(*p.Portfolio))
		ok := slices.Contains(s.cfg.ValidPortfolios, upper)
		if !ok {
			// Legacy wire text (Java's Portfolio.valueOf message), kept verbatim — 400
			// VALIDATION_ERROR.
			return PageDTO{}, httpx.NewValidationError("",
				"No enum constant "+s.cfg.EnumQualifier+"."+upper)
		}
		portfolio = upper
	}

	var rows []Row
	var err error
	if p.From != nil || p.To != nil {
		rows, err = s.queries.ListRange(ctx, portfolio, from, to)
	} else {
		rows, err = s.queries.ListAll(ctx, portfolio)
	}
	if err != nil {
		return PageDTO{}, fmt.Errorf("historiaquery: list: %w", err)
	}

	// Re-sort the fetch by snapshot date even though each query already orders ASC (kept from the
	// original, cheap and harmless).
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].SnapshotDate.Before(rows[j].SnapshotDate)
	})

	total := len(rows)
	start := min(page*size, total)
	end := min(start+size, total)
	dtos := make([]EntryDTO, 0, end-start)
	for _, r := range rows[start:end] {
		dtos = append(dtos, r.toDTO())
	}

	// Inflation enrichment is best-effort: any failure (or a missing index) leaves the raw DTOs
	// with null inflation fields rather than failing the read.
	s.enrich(ctx, portfolio, dtos)

	return PageDTO{Items: dtos, Total: int64(total), Number: page, Size: size}, nil
}

// enrich adds annualized inflation, the inflation-adjusted deposits break-even line and the real
// return derived from it, best-effort — any failure (or no index data at all) leaves the DTOs'
// three fields null.
func (s *Service) enrich(ctx context.Context, portfolio string, dtos []EntryDTO) {
	if len(dtos) == 0 {
		return
	}
	mom, err := s.queries.MomAsc(ctx)
	if err != nil || len(mom) == 0 {
		return
	}
	flows, err := s.queries.DepositFlows(ctx, portfolio)
	if err != nil {
		return
	}
	for i, d := range dtos {
		cum := CumulativeSince(s.cfg.InflationBase, d.SnapshotDate, mom)
		ann := Annualized(cum, s.cfg.InflationBase, YearMonthOf(d.SnapshotDate))
		adjusted := InflationAdjustedDeposits(flows, d.SnapshotDate, mom)
		dtos[i].AnnualizedInflationPct = &ann
		dtos[i].InflationAdjustedDeposits = adjusted
		dtos[i].RealReturnPct = RealReturn(d.PortfolioValue, adjusted)
	}
}

// toDTO applies HistoriaMapper's two computed fields: returnPct (scale 8 HALF_UP,
// ZERO when deposits are zero) and profitPln. Inflation fields stay null until
// enrich fills them — mirroring the mapper's null expressions.
func (r Row) toDTO() EntryDTO {
	var returnPct decimal.Decimal
	if !r.TotalDeposits.IsZero() {
		returnPct = r.PortfolioValue.DivRound(r.TotalDeposits, 8).Sub(decimal.NewFromInt(1))
	}
	return EntryDTO{
		ID: r.ID, SnapshotDate: r.SnapshotDate, Portfolio: r.Portfolio,
		TotalDeposits: r.TotalDeposits, PortfolioValue: r.PortfolioValue, Xirr: r.Xirr,
		ReturnPct: returnPct, ProfitPln: r.PortfolioValue.Sub(r.TotalDeposits),
		PositionValues: r.PositionValues, CreatedAt: r.CreatedAt,
	}
}
