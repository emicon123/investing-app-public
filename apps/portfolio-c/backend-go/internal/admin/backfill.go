package admin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"investing-app/common-go/historiaquery"
	"investing-app/common-go/pgnum"
	"investing-app/common-go/priceprovider"
	"investing-app/common-go/siblingcore/backfill"
	"portfolio-c/backend-go/db/sqlcgen"
	"portfolio-c/backend-go/internal/book"
)

// BackfillResult mirrors HistoriaPositionValuesBackfillService.BackfillResult.
type BackfillResult struct {
	Updated  int
	Skipped  int
	Warnings []string
}

// Service is the portfolioC position_values backfill: per portfolio, fills NULL position_values rows by
// rewinding today's units through that portfolio's purchase lots and position transfers
// (siblingcore/backfill), pricing at historical closes (PLN directly, EUR/USD via FX), and scaling the breakdown to the stored portfolio_value (last-row
// plug). Runs in one transaction.
type Service struct {
	pool *pgxpool.Pool
	ohlc priceprovider.OHLCProvider
	now  func() time.Time
}

// NewService wires the pool + OHLC provider (explicit constructor wiring, ADR-094
// §2 — no DI framework).
func NewService(pool *pgxpool.Pool, ohlc priceprovider.OHLCProvider) *Service {
	return &Service{pool: pool, ohlc: ohlc, now: time.Now}
}

// Backfill fills missing position_values rows for every portfolio, in one
// pgx.BeginFunc transaction: committed iff every portfolio succeeded, rolled back
// on an error or a panic (ADR-110).
//
// Each portfolio's Yahoo series are fetched first, with no transaction open (174-remediation
// §11.6: never hold a transaction across network calls) — the backfill writes only
// historia.position_values, no money rows; the transaction then re-reads and writes.
func (s *Service) Backfill(ctx context.Context) (BackfillResult, error) {
	q := sqlcgen.New(s.pool)
	maps := map[sqlcgen.PortfolioEnum]priceMaps{}
	for _, portfolio := range book.PortfolioNames() {
		p := sqlcgen.PortfolioEnum(portfolio)
		m, err := s.fetchMaps(ctx, q, p)
		if err != nil {
			return BackfillResult{}, err
		}
		maps[p] = m
	}
	warnings := []string{}
	updated, skipped := 0, 0
	var runErr error
	txErr := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := q.WithTx(tx)
		for _, portfolio := range book.PortfolioNames() { // one historia row per portfolio per day, never combined
			p := sqlcgen.PortfolioEnum(portfolio)
			u, sk, err := s.backfillPortfolio(ctx, q, p, maps[p], &warnings)
			if err != nil {
				runErr = err
				return err
			}
			updated += u
			skipped += sk
		}
		return nil
	})
	if runErr != nil {
		return BackfillResult{}, runErr
	}
	if txErr != nil {
		return BackfillResult{}, fmt.Errorf("admin: backfill transaction: %w", txErr)
	}
	return BackfillResult{Updated: updated, Skipped: skipped, Warnings: warnings}, nil
}

// priceMaps is buildMaps' result for one portfolio: the close series per yahoo ticker and per FX
// pair, and the warnings for the series that came back empty.
type priceMaps struct {
	price, fx map[string][]priceprovider.OHLCPoint
	warnings  []string
}

// fetchMaps reads, outside any transaction, whether portfolio has rows to fill and if so its
// tickers, and fetches their series.
func (s *Service) fetchMaps(ctx context.Context, q *sqlcgen.Queries, portfolio sqlcgen.PortfolioEnum) (priceMaps, error) {
	missing, err := q.ListHistoriaMissingPositionValues(ctx, portfolio)
	if err != nil {
		return priceMaps{}, fmt.Errorf("admin: backfill missing rows %s: %w", portfolio, err)
	}
	if len(missing) == 0 {
		return priceMaps{}, nil
	}
	positions, err := q.ListBackfillPositions(ctx, portfolio)
	if err != nil {
		return priceMaps{}, fmt.Errorf("admin: backfill positions %s: %w", portfolio, err)
	}
	priceMap, fxMap, warnings, err := s.buildMaps(ctx, q, portfolio, positions)
	if err != nil {
		return priceMaps{}, err
	}
	return priceMaps{price: priceMap, fx: fxMap, warnings: warnings}, nil
}

// backfillPortfolio fills portfolio's missing rows inside Backfill's transaction (q is bound to
// it) with the series fetched before it opened; a portfolio with nothing missing counts all its
// rows as skipped.
func (s *Service) backfillPortfolio(ctx context.Context, q *sqlcgen.Queries, portfolio sqlcgen.PortfolioEnum, maps priceMaps, warnings *[]string) (updated, skipped int, err error) {
	total, err := q.CountHistoriaByPortfolio(ctx, portfolio)
	if err != nil {
		return 0, 0, fmt.Errorf("admin: backfill count %s: %w", portfolio, err)
	}
	missing, err := q.ListHistoriaMissingPositionValues(ctx, portfolio)
	if err != nil {
		return 0, 0, fmt.Errorf("admin: backfill missing rows %s: %w", portfolio, err)
	}
	if len(missing) == 0 {
		return 0, int(total), nil
	}
	slog.Info("admin: backfill", "portfolio", portfolio, "missing", len(missing), "total", total)

	b, err := loadBackfillBook(ctx, q, portfolio)
	if err != nil {
		return 0, 0, err
	}
	*warnings = append(*warnings, maps.warnings...)

	for _, h := range missing {
		written, err := fillRow(ctx, q, portfolio, h, b, maps, warnings)
		if err != nil {
			return updated, skipped, err
		}
		if written {
			updated++
		} else {
			skipped++
		}
	}
	return updated, skipped, nil
}

// backfillBook is one portfolio's book as the rewind needs it: its positions and the rewind of
// today's units per ticker through the purchase lots and position transfers.
type backfillBook struct {
	positions []sqlcgen.ListBackfillPositionsRow
	rewind    *backfill.Rewind
}

// loadBackfillBook reads portfolio's positions, lots and transfers and indexes them.
func loadBackfillBook(ctx context.Context, q *sqlcgen.Queries, portfolio sqlcgen.PortfolioEnum) (*backfillBook, error) {
	positions, err := q.ListBackfillPositions(ctx, portfolio)
	if err != nil {
		return nil, fmt.Errorf("admin: backfill positions %s: %w", portfolio, err)
	}
	lots, err := q.ListBackfillLots(ctx, portfolio)
	if err != nil {
		return nil, fmt.Errorf("admin: backfill lots %s: %w", portfolio, err)
	}
	transfers, err := q.ListBackfillTransfers(ctx, portfolio)
	if err != nil {
		return nil, fmt.Errorf("admin: backfill transfers %s: %w", portfolio, err)
	}
	currentUnits := map[string]decimal.Decimal{}
	for _, p := range positions {
		currentUnits[p.Ticker] = pgnum.ToDecimalOrZero(p.Units)
	}
	changes := make([]backfill.UnitChange, 0, len(lots)+len(transfers))
	for _, l := range lots {
		if l.PurchasedAt.Valid {
			changes = append(changes, backfill.LotChange(l.Ticker, l.PurchasedAt.Time, pgnum.ToDecimalOrZero(l.Units)))
		}
	}
	for _, t := range transfers {
		if t.TransferredAt.Valid {
			changes = append(changes, backfill.TransferChange(t.Ticker, t.TransferredAt.Time, pgnum.ToDecimalOrZero(t.UnitsDelta)))
		}
	}
	return &backfillBook{positions: positions, rewind: backfill.NewRewind(currentUnits, changes)}, nil
}

// fillRow computes and writes one missing historia row's breakdown. It reports false (skipped)
// for a row with no date, a non-positive portfolio_value, or nothing priceable that day.
func fillRow(ctx context.Context, q *sqlcgen.Queries, portfolio sqlcgen.PortfolioEnum, h sqlcgen.ListHistoriaMissingPositionValuesRow, b *backfillBook, maps priceMaps, warnings *[]string) (bool, error) {
	if !h.SnapshotDate.Valid {
		return false, nil
	}
	snapDate := h.SnapshotDate.Time
	portfolioValue, err := pgnum.ToDecimal(h.PortfolioValue)
	if err != nil || portfolioValue.Compare(decimal.Zero) <= 0 {
		return false, nil
	}
	raw := map[string]decimal.Decimal{}
	rawSum := decimal.Zero
	for _, pos := range b.positions {
		if val, ok := positionValueAt(portfolio, pos, b, snapDate, maps, warnings); ok {
			raw[pos.Ticker] = val
			rawSum = rawSum.Add(val)
		}
	}
	if len(raw) == 0 || rawSum.Compare(decimal.Zero) == 0 {
		*warnings = append(*warnings, string(portfolio)+" "+snapDate.Format("2006-01-02")+" empty raw")
		return false, nil
	}
	scaled := backfill.ScaleToPortfolioValue(raw, portfolioValue)
	check := decimal.Zero
	for _, v := range scaled {
		check = check.Add(v)
	}
	if check.Sub(portfolioValue).Abs().Compare(decimal.RequireFromString("0.02")) > 0 {
		*warnings = append(*warnings, string(portfolio)+" "+snapDate.Format("2006-01-02")+" sum mismatch "+check.String()+" vs "+portfolioValue.String())
	}
	encoded, err := historiaquery.MarshalPositionValues(scaled)
	if err != nil {
		return false, fmt.Errorf("admin: backfill encode: %w", err)
	}
	if err := q.UpdateHistoriaPositionValues(ctx, sqlcgen.UpdateHistoriaPositionValuesParams{
		ID: h.ID, PositionValues: encoded,
	}); err != nil {
		return false, fmt.Errorf("admin: backfill update row %d: %w", h.ID, err)
	}
	return true, nil
}

// positionValueAt values one position on snapDate (units rewound to the date x its PLN price,
// rounded to 4dp). ok is false for a position with no yahoo ticker, no units that day, no price,
// or a value under 0.005.
func positionValueAt(portfolio sqlcgen.PortfolioEnum, pos sqlcgen.ListBackfillPositionsRow, b *backfillBook, snapDate time.Time, maps priceMaps, warnings *[]string) (decimal.Decimal, bool) {
	if pos.YahooTicker == nil || *pos.YahooTicker == "" {
		return decimal.Zero, false
	}
	units := b.rewind.UnitsAt(pos.Ticker, snapDate)
	if units.Compare(decimal.Zero) <= 0 {
		return decimal.Zero, false
	}
	pricePln, ok := pricePlnAt(portfolio, pos, snapDate, maps, warnings)
	if !ok {
		return decimal.Zero, false
	}
	val := units.Mul(pricePln).Round(4)
	if val.Compare(decimal.RequireFromString("0.005")) < 0 {
		val = decimal.Zero
	}
	return val, val.Compare(decimal.Zero) > 0
}

// pricePlnAt is the position's close on snapDate in PLN: PLN directly, EUR/USD via that day's FX
// close; anything else is unpriced. Every miss appends its warning.
func pricePlnAt(portfolio sqlcgen.PortfolioEnum, pos sqlcgen.ListBackfillPositionsRow, snapDate time.Time, maps priceMaps, warnings *[]string) (decimal.Decimal, bool) {
	closeNative, ok := priceprovider.NearestClose(maps.price[*pos.YahooTicker], snapDate)
	if !ok {
		*warnings = append(*warnings, string(portfolio)+" "+snapDate.Format("2006-01-02")+" no price for "+*pos.YahooTicker)
		return decimal.Zero, false
	}
	pricePln := decimal.NewFromFloat(closeNative)
	if string(pos.PriceCurrency) == "PLN" {
		return pricePln, true
	}
	var fxTicker string
	switch string(pos.PriceCurrency) {
	case "EUR":
		fxTicker = "EURPLN=X"
	case "USD":
		fxTicker = "USDPLN=X"
	default:
		*warnings = append(*warnings, "no fx for "+string(pos.PriceCurrency))
		return decimal.Zero, false
	}
	fx, ok := priceprovider.NearestClose(maps.fx[fxTicker], snapDate)
	if !ok {
		*warnings = append(*warnings, string(portfolio)+" "+snapDate.Format("2006-01-02")+" no fx "+fxTicker)
		return decimal.Zero, false
	}
	return pricePln.Mul(decimal.NewFromFloat(fx)), true
}

// fetchFxSeries fetches the EUR and USD close series against PLN that the portfolio's positions
// need; a failed fetch is a warning and a nil series, not an error.
func (s *Service) fetchFxSeries(ctx context.Context, positions []sqlcgen.ListBackfillPositionsRow, p1, p2 int64) (map[string][]priceprovider.OHLCPoint, []string) {
	var warnings []string
	fxMap := map[string][]priceprovider.OHLCPoint{}
	needEUR, needUSD := false, false
	for _, p := range positions {
		if string(p.PriceCurrency) == "EUR" {
			needEUR = true
		}
		if string(p.PriceCurrency) == "USD" {
			needUSD = true
		}
	}
	// A slice, not a map: the fetch and warning order is EUR then USD on every run.
	for _, c := range []struct {
		ticker string
		need   bool
	}{{"EURPLN=X", needEUR}, {"USDPLN=X", needUSD}} {
		if !c.need {
			continue
		}
		pts, err := s.ohlc.FetchOHLC(ctx, c.ticker, "1d", p1, p2)
		if err != nil {
			warnings = append(warnings, "no yahoo "+c.ticker+": "+err.Error())
			fxMap[c.ticker] = nil
			continue
		}
		fxMap[c.ticker] = pts
	}
	return fxMap, warnings
}

// buildMaps fetches the Yahoo close series per distinct yahoo ticker plus the needed FX series (EUR
// when any position is EUR-denominated, USD likewise) over the portfolio's historia date range
// (widened -5/+2 days).
func (s *Service) buildMaps(ctx context.Context, q *sqlcgen.Queries, portfolio sqlcgen.PortfolioEnum, positions []sqlcgen.ListBackfillPositionsRow) (map[string][]priceprovider.OHLCPoint, map[string][]priceprovider.OHLCPoint, []string, error) {
	warnings := []string{}
	bounds, err := q.ListHistoriaDateBounds(ctx, portfolio)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("admin: backfill date bounds %s: %w", portfolio, err)
	}
	from := s.now().AddDate(0, 0, -30)
	to := s.now()
	if d, ok := asDate(bounds.MinDate); ok {
		from = d
	}
	if d, ok := asDate(bounds.MaxDate); ok {
		to = d
	}
	p1 := from.AddDate(0, 0, -5).Unix()
	p2 := to.AddDate(0, 0, 2).Unix()

	priceMap := map[string][]priceprovider.OHLCPoint{}
	seen := map[string]bool{}
	for _, p := range positions {
		if p.YahooTicker == nil || *p.YahooTicker == "" || seen[*p.YahooTicker] {
			continue
		}
		seen[*p.YahooTicker] = true
		pts, err := s.ohlc.FetchOHLC(ctx, *p.YahooTicker, "1d", p1, p2)
		if err != nil {
			warnings = append(warnings, "no yahoo data for "+*p.YahooTicker+": "+err.Error())
			priceMap[*p.YahooTicker] = nil
			continue
		}
		if len(pts) == 0 {
			warnings = append(warnings, "no yahoo data for "+*p.YahooTicker)
		}
		priceMap[*p.YahooTicker] = pts
		slog.Info("admin: price map", "portfolio", portfolio, "ticker", *p.YahooTicker, "points", len(pts))
	}
	fxMap, fxWarnings := s.fetchFxSeries(ctx, positions, p1, p2)
	warnings = append(warnings, fxWarnings...)
	return priceMap, fxMap, warnings, nil
}

// asDate decodes sqlc's interface{} MIN/MAX(date) aggregate (pgx scans DATE as
// time.Time) into a calendar date.
func asDate(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case pgtype.Date:
		return t.Time, t.Valid
	default:
		return time.Time{}, false
	}
}
