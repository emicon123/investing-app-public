package historia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/common-go/historiaquery"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/xirr"
)

// Queries is the narrow slice of *sqlcgen.Queries this service calls, declared as an
// interface so service-layer unit tests can fake the DB without a real Postgres
// connection (integration_test.go still exercises the real *sqlcgen.Queries against
// testcontainers-go, per the "never mock the database" gate).
type Queries interface {
	ListHistoriaByPortfolio(ctx context.Context, arg sqlcgen.ListHistoriaByPortfolioParams) ([]sqlcgen.ListHistoriaByPortfolioRow, error)
	ListHistoriaAllRange(ctx context.Context, arg sqlcgen.ListHistoriaAllRangeParams) ([]sqlcgen.ListHistoriaAllRangeRow, error)
	ListHistoriaAll(ctx context.Context) ([]sqlcgen.ListHistoriaAllRow, error)
	UpsertHistoria(ctx context.Context, arg sqlcgen.UpsertHistoriaParams) error
	ListHistoriaPositions(ctx context.Context, portfolio sqlcgen.PortfolioEnum) ([]sqlcgen.ListHistoriaPositionsRow, error)
	ListHistoriaExternalDeposits(ctx context.Context, arg sqlcgen.ListHistoriaExternalDepositsParams) ([]sqlcgen.ListHistoriaExternalDepositsRow, error)
	ListHistoriaInflationAsc(ctx context.Context) ([]sqlcgen.ListHistoriaInflationAscRow, error)
	GetPriceCachePlnByTicker(ctx context.Context, ticker string) (pgtype.Numeric, error)
}

// TransactFunc runs fn inside a single DB transaction — SnapshotPortfolio's upsert
// commits atomically per portfolio, the direct port of HistoriaScheduler's explicit
// TransactionTemplate-per-portfolio write step. Production passes a pgx Begin/Commit
// closure; unit tests pass a direct call-through.
type TransactFunc func(ctx context.Context, fn func(Queries) error) error

// FxProvider is the one varying step behind every PLN conversion here — deposits,
// XIRR cash flows, and the value-computation FX snapshot. Production passes an
// adapter over the marketdatacore.Service (its GetRateToPln cache-read +
// fetch-on-stale path IS the "Phase-1 providers" route, not a new write path from
// this package — historia itself never upserts price_cache/fx_rate_cache); tests
// pass a stub map.
type FxProvider interface {
	GetRateToPln(ctx context.Context, currency string) (decimal.Decimal, error)
	AllRates(ctx context.Context) (map[string]decimal.Decimal, error)
}

// Service is the historia service layer: handler/scheduler -> Service -> sqlc Queries.
type Service struct {
	queries  Queries
	transact TransactFunc
	fx       FxProvider
	now      func() time.Time // wall clock; tests override to pin "today"
}

// NewService wires queries + transaction runner + FX provider (explicit constructor
// wiring, ADR-094 §2 — no DI framework).
func NewService(queries Queries, transact TransactFunc, fx FxProvider) *Service {
	return &Service{queries: queries, transact: transact, fx: fx, now: time.Now}
}

// errPriceUnavailable aborts a value computation: never value-at-zero (a zero value would make the
// consumer see the asset as maximally underweight). The scheduler maps it to value=ZERO with an
// empty breakdown.
func errPriceUnavailable(ticker string) error {
	return fmt.Errorf("historia: price unavailable for %s", ticker)
}

// List returns one page of historia entries. portfolio nil/"ALL" (case-insensitive) merges both
// portfolios date-ascending; a concrete value is matched case-insensitively and an unknown one
// answers 400. Pagination is in-memory (fetch, sort, sublist — out-of-range pages answer empty data
// with the total preserved), and size<=0 falls back to 50. A negative page answers 400. The GET
// path never writes.
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

	var rows []historiaRow
	if p.Portfolio == nil || strings.EqualFold(*p.Portfolio, "ALL") {
		var err error
		if p.From != nil || p.To != nil {
			rows, err = s.listAllRange(ctx, from, to)
		} else {
			rows, err = s.listAll(ctx)
		}
		if err != nil {
			return PageDTO{}, err
		}
	} else {
		upper := strings.ToUpper(*p.Portfolio)
		if upper != "MAIN" && upper != "RETIREMENT" {
			// Legacy wire text (Java's Portfolio.valueOf message), kept verbatim — 400
			// VALIDATION_ERROR.
			return PageDTO{}, httpx.NewValidationError("",
				"No enum constant pl.investing.entity.enums.Portfolio."+upper)
		}
		var err error
		rows, err = s.listByPortfolio(ctx, upper, from, to)
		if err != nil {
			return PageDTO{}, err
		}
	}

	// Re-sort the merged fetch by snapshot date even though each query already orders ASC — the
	// merge of two portfolios is only ordered because of this.
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

	// Inflation enrichment is best-effort: any failure leaves the raw DTOs with null inflation
	// fields rather than failing the whole read.
	s.enrichInflation(ctx, dtos)

	return PageDTO{Items: dtos, Total: int64(total), Number: page, Size: size}, nil
}

// SnapshotPortfolio computes and upserts one portfolio's daily snapshot for today
// (the Warsaw calendar date) — the live path StartScheduler's 23:00 job calls
// every night. Unchanged behavior: a 1-line wrapper over snapshotPortfolioAsOf
// (ADR-097 Decision §2 split), identical to this method's body before the split.
func (s *Service) SnapshotPortfolio(ctx context.Context, portfolio string) error {
	return s.snapshotPortfolioAsOf(ctx, portfolio, historiaquery.WarsawDate(s.now()))
}

// BackfillSnapshot is the one-off entry point (ADR-097 Decision §2) for writing a
// snapshot row for an explicit past date — e.g. recovering a missed nightly run,
// like the 2026-09-18 gap this ADR documents. It reuses snapshotPortfolioAsOf
// completely unchanged: the same computeValues/totalDepositsPln/xirrOrNull logic
// the live 23:00 job runs, just upserted under an explicit snapshot_date instead
// of "today." Known, accepted limitation (ADR-097 Decision §2): this prices off
// the *current* price_cache, not a historical close for date — consistent with
// the live job's own behavior, which has never done point-in-time historical
// pricing either. Callers must run ADR-097 Decision §3's trade-window sanity
// check before calling this — it is not enforced inside this method.
func (s *Service) BackfillSnapshot(ctx context.Context, portfolio string, date time.Time) error {
	return s.snapshotPortfolioAsOf(ctx, portfolio, dateOnly(date))
}

// snapshotPortfolioAsOf computes and upserts one portfolio's snapshot for asOf (ported from the
// Java HistoriaScheduler.snapshotPortfolio), steps (1)-(4) in order: per-asset values (any failure
// → ZERO with an empty breakdown), EXTERNAL deposits at FX (per-deposit failure → PLN fallback or
// skip), XIRR (any failure → null), then one transaction upserting the row for asOf. A
// deposits-read or upsert failure returns an error so the caller (scheduler loop, or the ADR-097
// backfill endpoint) can log it fail-soft; value/XIRR failures are absorbed into the row. Split out
// of the original SnapshotPortfolio per ADR-097 Decision §2 so BackfillSnapshot can target an
// explicit past date without duplicating this logic — body is otherwise byte-identical to the
// pre-split SnapshotPortfolio (today renamed to asOf throughout).
func (s *Service) snapshotPortfolioAsOf(ctx context.Context, portfolio string, asOf time.Time) error {
	if portfolio != "MAIN" && portfolio != "RETIREMENT" {
		return fmt.Errorf("historia: unknown portfolio %q", portfolio)
	}

	values, sum := s.computeValuesOrZero(ctx, portfolio)

	deposits, err := s.totalDepositsPln(ctx, portfolio)
	if err != nil {
		return fmt.Errorf("historia: snapshot %s deposits: %w", portfolio, err)
	}

	xirr := s.xirrOrNull(ctx, portfolio, asOf)

	positionJSON, err := marshalPositionValues(values)
	if err != nil {
		return fmt.Errorf("historia: snapshot %s position values: %w", portfolio, err)
	}
	var xirrNum pgtype.Numeric
	if xirr != nil {
		xirrNum = pgnum.FromDecimal(*xirr)
	}
	upsert := sqlcgen.UpsertHistoriaParams{
		SnapshotDate:   pgnum.Date(asOf),
		Portfolio:      sqlcgen.PortfolioEnum(portfolio),
		TotalDeposits:  pgnum.FromDecimal(deposits),
		PortfolioValue: pgnum.FromDecimal(sum),
		Xirr:           xirrNum,
		PositionValues: positionJSON,
	}
	if err := s.transact(ctx, func(q Queries) error {
		return q.UpsertHistoria(ctx, upsert)
	}); err != nil {
		return fmt.Errorf("historia: snapshot %s upsert: %w", portfolio, err)
	}

	slog.Info("historia: snapshot stored",
		"portfolio", portfolio, "date", dateKey(asOf),
		"value", sum.String(), "deposits", deposits.String())
	return nil
}

// computeValuesOrZero runs computeValues, absorbing every failure into ZERO plus an
// empty breakdown — HistoriaScheduler step (1)'s catch.
func (s *Service) computeValuesOrZero(ctx context.Context, portfolio string) (map[string]decimal.Decimal, decimal.Decimal) {
	values, err := s.computeValues(ctx, portfolio)
	if err != nil {
		slog.Warn("historia: could not compute portfolio value (empty price_cache?)",
			"portfolio", portfolio, "error", err)
		return map[string]decimal.Decimal{}, decimal.Zero
	}
	sum := decimal.Zero
	for _, v := range values {
		sum = sum.Add(v)
	}
	return values, sum
}

// computeValues ports ValueComputationService.computeValues 1:1 over the single
// shared read-only positions SELECT: subasset values fold into their asset's primary
// ticker (task 156 — same addends as the old parent/companion fold, regrouped by the
// asset_id FK instead of the companion_parent_ticker string), PLN price from the price
// cache, CHF rows priced by the CHF FX rate with no fallback, and PriceUnavailable
// failing the whole computation loudly instead of valuing at zero.
func (s *Service) computeValues(ctx context.Context, portfolio string) (map[string]decimal.Decimal, error) {
	rows, err := s.queries.ListHistoriaPositions(ctx, sqlcgen.PortfolioEnum(portfolio))
	if err != nil {
		return nil, fmt.Errorf("historia: read positions: %w", err)
	}
	fxRates, err := s.fx.AllRates(ctx)
	if err != nil {
		return nil, fmt.Errorf("historia: read fx rates: %w", err)
	}

	primaryTickerByAsset := map[int64]string{}
	for _, row := range rows {
		if row.IsPrimary {
			primaryTickerByAsset[row.AssetID] = row.Ticker
		}
	}

	subassetValues := map[int64]decimal.Decimal{}
	for _, row := range rows {
		if row.IsPrimary {
			continue
		}
		if _, ok := primaryTickerByAsset[row.AssetID]; !ok {
			continue
		}
		units, err := unitsTotal(row)
		if err != nil {
			return nil, err
		}
		price, err := s.nativePricePln(ctx, row, fxRates)
		if err != nil {
			return nil, err
		}
		subassetValues[row.AssetID] = subassetValues[row.AssetID].Add(units.Mul(price))
	}

	values := map[string]decimal.Decimal{}
	for _, row := range rows {
		if !row.IsPrimary {
			continue
		}
		units, err := unitsTotal(row)
		if err != nil {
			return nil, err
		}
		price, err := s.nativePricePln(ctx, row, fxRates)
		if err != nil {
			return nil, err
		}
		values[row.Ticker] = units.Mul(price).Add(subassetValues[row.AssetID])
	}
	return values, nil
}

// nativePricePln ports ValueComputationService.getNativePricePln: blank yahoo ticker
// → unavailable; CHF → the CHF FX rate (no 1.0 fallback — a missing rate fails
// loudly); otherwise the price_cache PLN lookup (a cache miss fails loudly, never a
// live fetch — the scheduler must not fan out to Yahoo per position).
func (s *Service) nativePricePln(ctx context.Context, row sqlcgen.ListHistoriaPositionsRow, fxRates map[string]decimal.Decimal) (decimal.Decimal, error) {
	ticker := row.Ticker
	if row.YahooTicker != nil {
		ticker = *row.YahooTicker
	}
	if row.YahooTicker == nil || strings.TrimSpace(*row.YahooTicker) == "" {
		return decimal.Zero, errPriceUnavailable(row.Ticker)
	}
	if string(row.PriceCurrency) == "CHF" {
		rate, ok := fxRates["CHF"]
		if !ok {
			return decimal.Zero, errPriceUnavailable(ticker)
		}
		return rate, nil
	}
	priceNum, err := s.queries.GetPriceCachePlnByTicker(ctx, *row.YahooTicker)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, errPriceUnavailable(ticker)
		}
		return decimal.Zero, fmt.Errorf("historia: read price cache for %s: %w", ticker, err)
	}
	price, err := pgnum.ToDecimal(priceNum)
	if err != nil {
		return decimal.Zero, fmt.Errorf("historia: decode price for %s: %w", ticker, err)
	}
	return price, nil
}

// unitsTotal ports Position.computeUnitsTotal: all five unit columns summed.
func unitsTotal(row sqlcgen.ListHistoriaPositionsRow) (decimal.Decimal, error) {
	total := decimal.Zero
	for _, col := range []pgtype.Numeric{
		row.UnitsIke, row.UnitsIkze, row.UnitsRegularEur, row.UnitsRegularUsd, row.UnitsRegularPln,
	} {
		units, err := pgnum.ToDecimal(col)
		if err != nil {
			return decimal.Zero, fmt.Errorf("historia: decode units for %s: %w", row.Ticker, err)
		}
		total = total.Add(units)
	}
	return total, nil
}

// totalDepositsPln is snapshot step (2): EXTERNAL deposits only (ADR-059) converted at FX, with a
// per-deposit fallback — PLN falls back to its raw amount, anything else without a rate is skipped.
// A deposits-READ failure is a real error.
func (s *Service) totalDepositsPln(ctx context.Context, portfolio string) (decimal.Decimal, error) {
	rows, err := s.queries.ListHistoriaExternalDeposits(ctx, sqlcgen.ListHistoriaExternalDepositsParams{
		Portfolio:     sqlcgen.PortfolioEnum(portfolio),
		CapitalSource: sqlcgen.CapitalSourceEnumEXTERNAL,
	})
	if err != nil {
		return decimal.Zero, err
	}
	total := decimal.Zero
	for _, r := range rows {
		amount, err := pgnum.ToDecimal(r.Amount)
		if err != nil {
			continue
		}
		rate, err := s.fx.GetRateToPln(ctx, string(r.Currency))
		if err != nil {
			slog.Warn("historia: could not convert deposit to PLN",
				"amount", amount.String(), "currency", string(r.Currency), "error", err)
			if string(r.Currency) == "PLN" {
				total = total.Add(amount)
			}
			continue
		}
		total = total.Add(amount.Mul(rate))
	}
	return total, nil
}

// xirrOrNull ports HistoriaScheduler step (3) with XirrService.computeXirr's cash-flow
// shape: EXTERNAL deposits negative plus the current value as today's terminal flow.
// Any failure — including an uncomputable value computation — answers nil, never an
// error, so the job stores a null XIRR instead of aborting.
func (s *Service) xirrOrNull(ctx context.Context, portfolio string, today time.Time) *decimal.Decimal {
	rows, err := s.queries.ListHistoriaExternalDeposits(ctx, sqlcgen.ListHistoriaExternalDepositsParams{
		Portfolio:     sqlcgen.PortfolioEnum(portfolio),
		CapitalSource: sqlcgen.CapitalSourceEnumEXTERNAL,
	})
	if err != nil {
		slog.Warn("historia: XIRR deposit read failed (will store null)", "portfolio", portfolio, "error", err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	values, err := s.computeValues(ctx, portfolio)
	if err != nil {
		slog.Warn("historia: XIRR computation failed (will store null)", "portfolio", portfolio, "error", err)
		return nil
	}
	current := decimal.Zero
	for _, v := range values {
		current = current.Add(v)
	}
	flows := make([]CashFlow, 0, len(rows)+1)
	for _, r := range rows {
		if !r.DepositDate.Valid {
			continue
		}
		amount, err := pgnum.ToDecimal(r.Amount)
		if err != nil {
			continue
		}
		rate, err := s.fx.GetRateToPln(ctx, string(r.Currency))
		if err != nil {
			slog.Warn("historia: could not convert deposit to PLN for XIRR",
				"currency", string(r.Currency), "error", err)
			if string(r.Currency) == "PLN" {
				rate = decimal.NewFromInt(1)
			} else {
				continue
			}
		}
		pln := amount.Mul(rate)
		flows = append(flows, CashFlow{Date: r.DepositDate.Time, Amount: pln.Neg()})
	}
	flows = append(flows, CashFlow{Date: today, Amount: current})
	return xirr.Solve(flows)
}

// enrichInflation adds annualized inflation, the inflation-adjusted deposits break-even line and
// the real return derived from it, best-effort — any failure (or no index data at all) leaves the
// DTOs' three fields null.
func (s *Service) enrichInflation(ctx context.Context, dtos []EntryDTO) {
	if len(dtos) == 0 {
		return
	}
	mom, err := s.loadMomMap(ctx)
	if err != nil || len(mom) == 0 {
		return
	}
	flowsByPortfolio := map[string][]CashFlow{}
	for _, pf := range trackedPortfolios {
		flows, err := s.inflationCashFlows(ctx, pf)
		if err != nil {
			return
		}
		flowsByPortfolio[pf] = flows
	}
	for i, d := range dtos {
		base := BaselineFor(d.Portfolio)
		cum := historiaquery.CumulativeSince(base, d.SnapshotDate, mom)
		ann := historiaquery.Annualized(cum, base, historiaquery.YearMonthOf(d.SnapshotDate))
		adjusted := historiaquery.InflationAdjustedDeposits(flowsByPortfolio[d.Portfolio], d.SnapshotDate, mom)
		dtos[i].AnnualizedInflationPct = &ann
		dtos[i].InflationAdjustedDeposits = adjusted
		dtos[i].RealReturnPct = historiaquery.RealReturn(d.PortfolioValue, adjusted)
	}
}

// inflationCashFlows ports HistoriaService.cashFlows: EXTERNAL deposits as
// (date, amountPln) pairs for the inflation-adjusted deposits, with the same per-deposit FX
// fallback (PLN raw on failure, skip otherwise).
func (s *Service) inflationCashFlows(ctx context.Context, portfolio string) ([]CashFlow, error) {
	rows, err := s.queries.ListHistoriaExternalDeposits(ctx, sqlcgen.ListHistoriaExternalDepositsParams{
		Portfolio:     sqlcgen.PortfolioEnum(portfolio),
		CapitalSource: sqlcgen.CapitalSourceEnumEXTERNAL,
	})
	if err != nil {
		return nil, err
	}
	out := []CashFlow{}
	for _, r := range rows {
		if !r.DepositDate.Valid {
			continue
		}
		amount, err := pgnum.ToDecimal(r.Amount)
		if err != nil {
			continue
		}
		rate, err := s.fx.GetRateToPln(ctx, string(r.Currency))
		if err != nil {
			if string(r.Currency) == "PLN" {
				out = append(out, CashFlow{Date: r.DepositDate.Time, Amount: amount})
			}
			continue
		}
		out = append(out, CashFlow{Date: r.DepositDate.Time, Amount: amount.Mul(rate)})
	}
	return out, nil
}

// loadMomMap ports InflationIndexService.loadMomMap: the whole inflation table as a
// (year, month) → CPI map, ascending.
func (s *Service) loadMomMap(ctx context.Context) (map[historiaquery.YearMonth]decimal.Decimal, error) {
	rows, err := s.queries.ListHistoriaInflationAsc(ctx)
	if err != nil {
		return nil, fmt.Errorf("historia: read inflation index: %w", err)
	}
	mom := make(map[historiaquery.YearMonth]decimal.Decimal, len(rows))
	for _, r := range rows {
		cpi, err := pgnum.ToDecimal(r.CpiValue)
		if err != nil {
			return nil, fmt.Errorf("historia: decode cpi %d-%02d: %w", r.Year, r.Month, err)
		}
		mom[historiaquery.YearMonth{Year: int(r.Year), Month: int(r.Month)}] = cpi
	}
	return mom, nil
}

// ── Fetch helpers ──────────────────────────────────────────────────────────────

func (s *Service) listByPortfolio(ctx context.Context, portfolio string, from, to time.Time) ([]historiaRow, error) {
	rows, err := s.queries.ListHistoriaByPortfolio(ctx, sqlcgen.ListHistoriaByPortfolioParams{
		Portfolio:      sqlcgen.PortfolioEnum(portfolio),
		SnapshotDate:   pgnum.Date(from),
		SnapshotDate_2: pgnum.Date(to),
	})
	if err != nil {
		return nil, fmt.Errorf("historia: list by portfolio: %w", err)
	}
	out := make([]historiaRow, 0, len(rows))
	for _, r := range rows {
		decoded, err := decodeRow(r.ID, r.SnapshotDate, r.Portfolio, r.TotalDeposits, r.PortfolioValue, r.Xirr, r.PositionValues, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}

func (s *Service) listAllRange(ctx context.Context, from, to time.Time) ([]historiaRow, error) {
	rows, err := s.queries.ListHistoriaAllRange(ctx, sqlcgen.ListHistoriaAllRangeParams{
		SnapshotDate:   pgnum.Date(from),
		SnapshotDate_2: pgnum.Date(to),
	})
	if err != nil {
		return nil, fmt.Errorf("historia: list range: %w", err)
	}
	out := make([]historiaRow, 0, len(rows))
	for _, r := range rows {
		decoded, err := decodeRow(r.ID, r.SnapshotDate, r.Portfolio, r.TotalDeposits, r.PortfolioValue, r.Xirr, r.PositionValues, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}

func (s *Service) listAll(ctx context.Context) ([]historiaRow, error) {
	rows, err := s.queries.ListHistoriaAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("historia: list all: %w", err)
	}
	out := make([]historiaRow, 0, len(rows))
	for _, r := range rows {
		decoded, err := decodeRow(r.ID, r.SnapshotDate, r.Portfolio, r.TotalDeposits, r.PortfolioValue, r.Xirr, r.PositionValues, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}

// historiaRow is the decoded, sqlc-free shape of one historia table row.
type historiaRow struct {
	ID             int64
	SnapshotDate   time.Time
	Portfolio      string
	TotalDeposits  decimal.Decimal
	PortfolioValue decimal.Decimal
	Xirr           *decimal.Decimal
	PositionValues map[string]decimal.Decimal
	CreatedAt      time.Time
}

func decodeRow(id int64, snap pgtype.Date, portfolio sqlcgen.PortfolioEnum, deposits, value, xirr pgtype.Numeric, positionValues []byte, created pgtype.Timestamptz) (historiaRow, error) {
	if !snap.Valid {
		return historiaRow{}, fmt.Errorf("historia: row %d has NULL snapshot_date", id)
	}
	dep, err := pgnum.ToDecimal(deposits)
	if err != nil {
		return historiaRow{}, fmt.Errorf("historia: decode total_deposits for row %d: %w", id, err)
	}
	val, err := pgnum.ToDecimal(value)
	if err != nil {
		return historiaRow{}, fmt.Errorf("historia: decode portfolio_value for row %d: %w", id, err)
	}
	var xirrPtr *decimal.Decimal
	if xirr.Valid {
		x, err := pgnum.ToDecimal(xirr)
		if err != nil {
			return historiaRow{}, fmt.Errorf("historia: decode xirr for row %d: %w", id, err)
		}
		xirrPtr = &x
	}
	positions, err := decodePositionValues(positionValues)
	if err != nil {
		return historiaRow{}, fmt.Errorf("historia: decode position_values for row %d: %w", id, err)
	}
	return historiaRow{
		ID: id, SnapshotDate: dateOnly(snap.Time), Portfolio: string(portfolio),
		TotalDeposits: dep, PortfolioValue: val, Xirr: xirrPtr,
		PositionValues: positions, CreatedAt: created.Time,
	}, nil
}

// toDTO applies HistoriaMapper's two computed fields: returnPct (scale 8 HALF_UP,
// ZERO when deposits are zero) and profitPln. Inflation fields stay null until
// enrichInflation fills them — mirroring the mapper's null expressions.
func (r historiaRow) toDTO() EntryDTO {
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

// decodePositionValues parses the V30 JSONB breakdown (NULL legacy rows → nil map).
func decodePositionValues(raw []byte) (map[string]decimal.Decimal, error) {
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

// marshalPositionValues serializes the snapshot breakdown for the JSONB column. An empty
// (failure-path) map persists as {}, not NULL.
func marshalPositionValues(values map[string]decimal.Decimal) ([]byte, error) {
	floats := make(map[string]float64, len(values))
	for k, v := range values {
		floats[k] = v.InexactFloat64()
	}
	return json.Marshal(floats)
}
