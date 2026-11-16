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
	"portfolio-b/backend-go/db/sqlcgen"
)

// AccurateCutoff mirrors HistoriaPositionValuesBackfillService.ACCURATE_CUTOFF:
// pre-cutoff NULL rows are intentionally skipped (their prices predate reliable
// history), never filled.
var AccurateCutoff = time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)

// BackfillResult mirrors HistoriaPositionValuesBackfillService.BackfillResult.
type BackfillResult struct {
	Updated  int
	Skipped  int
	Warnings []string
}

// Service is the portfolioB position_values backfill: fills NULL position_values rows on/after
// AccurateCutoff by rewinding today's units through purchase lots and position transfers, pricing
// at historical closes, and scaling the breakdown to the stored portfolio_value (last-row plug).
// Runs in one transaction.
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

// Backfill fills missing position_values rows. The whole run commits atomically
// (one pgx.BeginFunc transaction: committed iff backfillInTx returns nil, rolled
// back on an error or a panic — ADR-110); an OHLC fetch failure for every ticker
// still proceeds (those tickers are skipped per-row with warnings), but a DB
// failure aborts the run with an error.
//
// The Yahoo series are fetched first, with no transaction open (174-remediation §11.6: never hold
// a transaction across network calls): the missing-row check and the ticker list are read outside
// it — the backfill writes only historia.position_values, no money rows — and the transaction then
// re-reads and writes.
func (s *Service) Backfill(ctx context.Context) (BackfillResult, error) {
	q := sqlcgen.New(s.pool)
	missing, err := q.ListHistoriaMissingPositionValues(ctx, pgnum.Date(AccurateCutoff))
	if err != nil {
		return BackfillResult{}, fmt.Errorf("admin: backfill missing rows: %w", err)
	}
	var maps priceMaps
	if len(missing) > 0 {
		if maps, err = s.buildMaps(ctx, q); err != nil {
			return BackfillResult{}, err
		}
	}
	var result BackfillResult
	var runErr error
	txErr := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		result, runErr = s.backfillInTx(ctx, q.WithTx(tx), maps)
		return runErr
	})
	if runErr != nil {
		return BackfillResult{}, runErr
	}
	if txErr != nil {
		return BackfillResult{}, fmt.Errorf("admin: backfill transaction: %w", txErr)
	}
	return result, nil
}

// priceMaps is buildMaps' result: the close series per yahoo ticker and per FX pair, and the
// warnings for the series that came back empty.
type priceMaps struct {
	price, fx map[string][]priceprovider.OHLCPoint
	warnings  []string
}

// backfillInTx is Backfill's body, run inside its transaction (q is bound to it) with the series
// fetched before it opened.
func (s *Service) backfillInTx(ctx context.Context, q *sqlcgen.Queries, maps priceMaps) (BackfillResult, error) {
	warnings := []string{}
	total, err := q.CountHistoriaAll(ctx)
	if err != nil {
		return BackfillResult{}, fmt.Errorf("admin: backfill count: %w", err)
	}
	missing, err := q.ListHistoriaMissingPositionValues(ctx, pgnum.Date(AccurateCutoff))
	if err != nil {
		return BackfillResult{}, fmt.Errorf("admin: backfill missing rows: %w", err)
	}
	if len(missing) == 0 {
		return BackfillResult{Updated: 0, Skipped: int(total), Warnings: warnings}, nil
	}
	slog.Info("admin: backfill portfolioB", "missing", len(missing), "total", total, "cutoff", "2026-07-14")

	warnings = append(warnings, maps.warnings...)
	b, err := loadBackfillBook(ctx, q)
	if err != nil {
		return BackfillResult{}, err
	}

	updated, skipped := 0, 0
	for _, h := range missing {
		written, err := fillRow(ctx, q, h, b, maps, &warnings)
		if err != nil {
			return BackfillResult{}, err
		}
		if written {
			updated++
		} else {
			skipped++
		}
	}

	return BackfillResult{Updated: updated, Skipped: skipped, Warnings: warnings}, nil
}

// backfillBook is today's book as the rewind needs it: the primaries (one per asset, the
// position_values keys), each asset's other subassets, and the rewind of today's units per ticker
// through the purchase lots and position transfers.
type backfillBook struct {
	tradables        []sqlcgen.ListBackfillPositionsRow
	subassetsByAsset map[int64][]sqlcgen.ListBackfillPositionsRow
	rewind           *backfill.Rewind
}

// loadBackfillBook reads the positions, lots and transfers and indexes them.
func loadBackfillBook(ctx context.Context, q *sqlcgen.Queries) (*backfillBook, error) {
	positions, err := q.ListBackfillPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin: backfill positions: %w", err)
	}
	lots, err := q.ListBackfillLots(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin: backfill lots: %w", err)
	}
	transfers, err := q.ListBackfillTransfers(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin: backfill transfers: %w", err)
	}

	b := &backfillBook{subassetsByAsset: map[int64][]sqlcgen.ListBackfillPositionsRow{}}
	currentUnits := map[string]decimal.Decimal{}
	for _, p := range positions {
		if p.IsPrimary {
			b.tradables = append(b.tradables, p)
		} else {
			b.subassetsByAsset[p.AssetID] = append(b.subassetsByAsset[p.AssetID], p)
		}
		currentUnits[p.Ticker] = unitsTotal(p)
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
	b.rewind = backfill.NewRewind(currentUnits, changes)
	return b, nil
}

// unitsAt rewinds ticker's units to snapDate.
func (b *backfillBook) unitsAt(ticker string, snapDate time.Time) decimal.Decimal {
	return b.rewind.UnitsAt(ticker, snapDate)
}

// fillRow computes and writes one missing historia row's breakdown. It reports false (skipped)
// for a row with no date, a non-positive portfolio_value, or nothing priceable that day.
func fillRow(ctx context.Context, q *sqlcgen.Queries, h sqlcgen.ListHistoriaMissingPositionValuesRow, b *backfillBook, maps priceMaps, warnings *[]string) (bool, error) {
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
	for _, parent := range b.tradables {
		if v, ok := assetValueAt(b, parent, snapDate, maps, warnings); ok {
			raw[parent.Ticker] = v
			rawSum = rawSum.Add(v)
		}
	}
	if len(raw) == 0 || rawSum.Compare(decimal.Zero) == 0 {
		*warnings = append(*warnings, snapDate.Format("2006-01-02")+" empty raw")
		return false, nil
	}
	scaled := backfill.ScaleToPortfolioValue(raw, portfolioValue)
	check := decimal.Zero
	for _, v := range scaled {
		check = check.Add(v)
	}
	if check.Sub(portfolioValue).Abs().Compare(decimal.RequireFromString("0.02")) > 0 {
		*warnings = append(*warnings, snapDate.Format("2006-01-02")+" sum mismatch "+check.String()+" vs "+portfolioValue.String())
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

// assetValueAt values one asset on snapDate: its other subassets (priced ones only) plus the
// primary, rounded to 4dp. ok is false when the asset has nothing to value that day — no units
// anywhere, an unpriced primary with no priced subasset value, or a value under 0.005.
func assetValueAt(b *backfillBook, parent sqlcgen.ListBackfillPositionsRow, snapDate time.Time, maps priceMaps, warnings *[]string) (decimal.Decimal, bool) {
	parentUnits := b.unitsAt(parent.Ticker, snapDate)
	compUnitsSum := decimal.Zero
	compValueSum := decimal.Zero
	for _, comp := range b.subassetsByAsset[parent.AssetID] {
		cu := b.unitsAt(comp.Ticker, snapDate)
		if cu.Compare(decimal.Zero) <= 0 {
			continue
		}
		cp, ok := pricePlnFor(comp.PriceCurrency, comp.YahooTicker, snapDate, maps.price, maps.fx, warnings)
		if !ok {
			continue
		}
		compValueSum = compValueSum.Add(cu.Mul(cp))
		compUnitsSum = compUnitsSum.Add(cu)
	}
	totalUnits := parentUnits.Add(compUnitsSum)
	if totalUnits.Compare(decimal.Zero) <= 0 && compValueSum.Compare(decimal.Zero) == 0 && parentUnits.Compare(decimal.Zero) == 0 {
		return decimal.Zero, false
	}
	price, ok := pricePlnFor(parent.PriceCurrency, parent.YahooTicker, snapDate, maps.price, maps.fx, warnings)
	parentValue := decimal.Zero
	if parentUnits.Compare(decimal.Zero) > 0 && ok {
		parentValue = parentUnits.Mul(price)
	} else if !ok && parentUnits.Compare(decimal.Zero) > 0 && compValueSum.Compare(decimal.Zero) == 0 {
		return decimal.Zero, false
	}
	totalValue := parentValue.Add(compValueSum)
	if totalValue.Compare(decimal.RequireFromString("0.005")) < 0 {
		return decimal.Zero, false
	}
	return totalValue.Round(4), true
}

// buildMaps fetches the Yahoo close series per distinct yahoo ticker plus the four FX series over
// the historia date range (widened -5/+2 days). An empty series appends a warning and stays in the
// map as empty (those tickers are skipped per-row, not fatal to the run).
func (s *Service) buildMaps(ctx context.Context, q *sqlcgen.Queries) (priceMaps, error) {
	warnings := []string{}
	bounds, err := q.ListHistoriaDateBounds(ctx)
	if err != nil {
		return priceMaps{}, fmt.Errorf("admin: backfill date bounds: %w", err)
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

	positions, err := q.ListBackfillPositions(ctx)
	if err != nil {
		return priceMaps{}, fmt.Errorf("admin: backfill positions for price map: %w", err)
	}
	priceMap := map[string][]priceprovider.OHLCPoint{}
	seen := map[string]bool{}
	for _, p := range positions {
		if p.YahooTicker == nil || *p.YahooTicker == "" || seen[*p.YahooTicker] {
			continue
		}
		seen[*p.YahooTicker] = true
		pts, err := s.ohlc.FetchOHLC(ctx, *p.YahooTicker, "1d", p1, p2)
		if err != nil {
			warnings = append(warnings, "no yahoo "+*p.YahooTicker+": "+err.Error())
			priceMap[*p.YahooTicker] = nil
			continue
		}
		if len(pts) == 0 {
			warnings = append(warnings, "no yahoo "+*p.YahooTicker)
		}
		priceMap[*p.YahooTicker] = pts
		slog.Info("admin: price map", "ticker", *p.YahooTicker, "points", len(pts))
	}
	fxMap := map[string][]priceprovider.OHLCPoint{}
	for _, fx := range []string{"EURPLN=X", "USDPLN=X", "CHFPLN=X", "GBPPLN=X"} {
		pts, err := s.ohlc.FetchOHLC(ctx, fx, "1d", p1, p2)
		if err != nil {
			warnings = append(warnings, "no yahoo "+fx+": "+err.Error())
			fxMap[fx] = nil
			continue
		}
		fxMap[fx] = pts
		slog.Info("admin: fx map", "ticker", fx, "points", len(pts))
	}
	return priceMaps{price: priceMap, fx: fxMap, warnings: warnings}, nil
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

// pricePlnFor ports HistoriaPositionValuesBackfillService.pricePlnFor: CHF rows
// with no yahoo ticker price off the CHF FX rate; otherwise native close × the
// currency's FX rate (PLN rows need none). Reports false when no price exists.
func pricePlnFor(currency sqlcgen.CurrencyEnum, yahooTicker *string, snapshotDate time.Time, priceMap, fxMap map[string][]priceprovider.OHLCPoint, warnings *[]string) (decimal.Decimal, bool) {
	if string(currency) == "CHF" && (yahooTicker == nil || *yahooTicker == "") {
		fx, ok := priceprovider.NearestClose(fxMap["CHFPLN=X"], snapshotDate)
		if !ok {
			return decimal.Zero, false
		}
		return decimal.NewFromFloat(fx), true
	}
	if yahooTicker == nil || *yahooTicker == "" {
		return decimal.Zero, false
	}
	close, ok := priceprovider.NearestClose(priceMap[*yahooTicker], snapshotDate)
	if !ok {
		return decimal.Zero, false
	}
	native := decimal.NewFromFloat(close)
	if string(currency) == "PLN" {
		return native, true
	}
	var fxTicker string
	switch string(currency) {
	case "EUR":
		fxTicker = "EURPLN=X"
	case "USD":
		fxTicker = "USDPLN=X"
	case "GBP":
		fxTicker = "GBPPLN=X"
	case "CHF":
		fxTicker = "CHFPLN=X"
	default:
		return native, true
	}
	fx, ok := priceprovider.NearestClose(fxMap[fxTicker], snapshotDate)
	if !ok {
		*warnings = append(*warnings, snapshotDate.Format("2006-01-02")+" no fx "+fxTicker+" for "+*yahooTicker)
		return decimal.Zero, false
	}
	return native.Mul(decimal.NewFromFloat(fx)), true
}

// unitsTotal sums the five per-account unit columns.
func unitsTotal(p sqlcgen.ListBackfillPositionsRow) decimal.Decimal {
	return pgnum.ToDecimalOrZero(p.UnitsIkeTata).
		Add(pgnum.ToDecimalOrZero(p.UnitsIkeMama)).
		Add(pgnum.ToDecimalOrZero(p.UnitsIkzeTata)).
		Add(pgnum.ToDecimalOrZero(p.UnitsIkzeMama)).
		Add(pgnum.ToDecimalOrZero(p.UnitsInne))
}
