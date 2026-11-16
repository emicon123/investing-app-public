package rebalancing

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/position"
	"investing-app/common-go/costbasis"
	"investing-app/common-go/costmember"
	"investing-app/common-go/historiaquery"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

// Service is the rebalancing-transfer service layer: handler -> Service -> sqlc Queries.
type Service struct {
	queries  Queries
	transact TransactFunc
}

// NewService wires queries + transaction runner (explicit constructor wiring, ADR-094 §2).
func NewService(queries Queries, transact TransactFunc) *Service {
	if transact == nil {
		transact = func(ctx context.Context, fn func(Queries) error) error { return fn(queries) }
	}
	return &Service{queries: queries, transact: transact}
}

type positionKey struct {
	ticker    string
	portfolio string
}

type row struct {
	id          int64
	assetID     int64 // same asset_id = same asset: a same-asset swap CARRIES cost (task 161)
	yahooTicker *string
	ticker      string
	portfolio   string
	exchange    string
	units       position.PositionUnits
}

// ExecuteTransfer is the direct port of RebalancingTransferService.executeTransfer — the
// sanctioned manual "Sprzedaj" sell path. sold.Ticker/Units/AccountType come ENTIRELY from req
// (the caller's request DTO); nothing in this function derives what to sell from a deviationPct
// or any rebalancing target — see this package's own doc comment and
// .claude/tasks/150-architect-output.md §3 item 2 for the exhaustive manual-sell-only trace.
//
// Steps (task 185): validateTransfer checks the request shape before any transaction opens;
// inside the one transaction loadTransferState reads and checks the rows, planTransfer plans the
// cost bookkeeping and the sold row's new units, applyTransfer writes everything.
func (s *Service) ExecuteTransfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	if err := validateTransfer(req); err != nil {
		return TransferResult{}, err
	}
	err := s.transact(ctx, func(q Queries) error {
		state, err := loadTransferState(ctx, q, req)
		if err != nil {
			return err
		}
		changedCosts, err := planTransfer(state, req)
		if err != nil {
			return err
		}
		return applyTransfer(ctx, q, state, req, changedCosts)
	})
	if err != nil {
		return TransferResult{}, err
	}
	return transferResult(req), nil
}

// validateTransfer rejects a malformed request (no buy legs, a non-positive units or price).
func validateTransfer(req TransferRequest) error {
	if len(req.Buys) == 0 {
		return httpx.NewValidationError("buys", "at least one buy leg is required")
	}
	if req.Sold.Units.Sign() <= 0 {
		return httpx.NewValidationError("sold.units", "units must be positive")
	}
	if req.Sold.PricePln.Sign() <= 0 {
		return httpx.NewValidationError("sold.pricePln", "pricePln must be positive")
	}
	for _, buy := range req.Buys {
		if buy.Units.Sign() <= 0 {
			return httpx.NewValidationError("buys.units", "units must be positive")
		}
		if buy.PricePln.Sign() <= 0 {
			return httpx.NewValidationError("buys.pricePln", "pricePln must be positive")
		}
	}
	return nil
}

// transferState is what a transfer reads before it writes: every position row by (ticker,
// portfolio), the sold leg's row, each buy leg's row (index-aligned with req.Buys) and the
// price_cache PLN prices the costbasis kernel values assets with.
type transferState struct {
	byKey  map[positionKey]*row
	sold   *row
	buys   []*row
	prices map[string]decimal.Decimal
}

// loadTransferState reads the positions, resolves and checks the legs against them, then reads
// price_cache (DB only, no network) - in that order, so a rejected transfer reads nothing more.
func loadTransferState(ctx context.Context, q Queries, req TransferRequest) (*transferState, error) {
	rows, err := q.ListAllPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebalancing: list positions: %w", err)
	}
	byKey, err := decodeToMap(rows)
	if err != nil {
		return nil, err
	}
	sold, buys, err := resolveLegs(byKey, req)
	if err != nil {
		return nil, err
	}
	priceRows, err := q.GetAllPriceCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebalancing: read price cache: %w", err)
	}
	prices, err := position.PricePlnMap(priceRows)
	if err != nil {
		return nil, err
	}
	return &transferState{byKey: byKey, sold: sold, buys: buys, prices: prices}, nil
}

// resolveLegs finds the sold row (404) and checks it holds enough units in the sold bucket (400),
// finds every buy row (404), then applies the wrapper lock (400) - this precedence is part of the
// API's behaviour.
func resolveLegs(byKey map[positionKey]*row, req TransferRequest) (*row, []*row, error) {
	sold, ok := byKey[positionKey{ticker: req.Sold.Ticker, portfolio: req.Portfolio}]
	if !ok {
		return nil, nil, httpx.NewNotFoundError("Position not found for ticker: " + req.Sold.Ticker)
	}
	currentUnits := currentUnitsFor(sold.units, req.Sold.AccountType, ptrOrEmpty(req.Sold.RegularCurrency))
	if currentUnits.Cmp(req.Sold.Units) < 0 {
		return nil, nil, httpx.NewValidationError("", fmt.Sprintf(
			"Insufficient units for ticker %s (%s): have %s, selling %s",
			req.Sold.Ticker, req.Sold.AccountType, currentUnits, req.Sold.Units))
	}
	buys := make([]*row, 0, len(req.Buys))
	for _, buy := range req.Buys {
		buyPos, ok := byKey[positionKey{ticker: buy.Ticker, portfolio: req.Portfolio}]
		if !ok {
			return nil, nil, httpx.NewNotFoundError("Position not found for ticker: " + buy.Ticker)
		}
		buys = append(buys, buyPos)
	}
	if err := checkWrapperLock(req); err != nil {
		return nil, nil, err
	}
	return sold, buys, nil
}

// checkWrapperLock is the ADR-054 wrapper lock: money already inside IKE/IKZE may only be
// reinvested within that same wrapper.
func checkWrapperLock(req TransferRequest) error {
	if req.Sold.AccountType == position.AccountTypeREGULAR {
		return nil
	}
	for _, buy := range req.Buys {
		if buy.AccountType != req.Sold.AccountType {
			return httpx.NewValidationError("", fmt.Sprintf(
				"Cannot transfer out of %s into %s: %s holdings can only be reinvested within the same wrapper (sold %s from %s, attempted buy of %s into %s)",
				req.Sold.AccountType, buy.AccountType, req.Sold.AccountType,
				req.Sold.Ticker, req.Sold.AccountType, buy.Ticker, buy.AccountType))
		}
	}
	return nil
}

// planTransfer plans the asset-level average cost (task 161a-2, design 161 §2/§5) with the
// costbasis kernel from the state BEFORE any unit delta is applied: a same-asset buy leg (same
// asset_id as the sold row: another subasset, or another account bucket of the same instrument)
// CARRIES the sold slice's cost; a buy on another asset enters at market. It then takes the sold
// units off the in-memory sold row. Returns the members whose cost changed.
func planTransfer(state *transferState, req TransferRequest) ([]*position.CostMember, error) {
	members := costMembersByAsset(state.byKey)
	changedCosts := planTransferCosts(members, state.prices, state.sold, req, state.byKey)

	soldRegularCurrency := ptrOrEmpty(req.Sold.RegularCurrency)
	position.ApplyDeltaWithCurrencyOverride(&state.sold.units, req.Sold.AccountType, soldRegularCurrency, req.Sold.Units.Neg())
	if currentUnitsFor(state.sold.units, req.Sold.AccountType, soldRegularCurrency).Sign() < 0 {
		return nil, httpx.NewValidationError("", fmt.Sprintf(
			"Sold units would result in negative position for ticker %s (%s)", req.Sold.Ticker, req.Sold.AccountType))
	}
	return changedCosts, nil
}

// applyTransfer writes the transfer: the sold row and its audit row, then per buy leg its unit
// delta, audit row and (REGULAR into IKE/IKZE) auto-deposit, then the cost of every sibling the
// unit writes did not already carry - all inside the caller's transaction.
func applyTransfer(ctx context.Context, q Queries, state *transferState, req TransferRequest, changedCosts []*position.CostMember) error {
	if _, err := q.UpdatePositionDelta(ctx, toDeltaParams(state.sold)); err != nil {
		return fmt.Errorf("rebalancing: apply sold delta for %s: %w", req.Sold.Ticker, err)
	}
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	if _, err := q.InsertAssetPurchase(ctx, sqlcgen.InsertAssetPurchaseParams{
		SessionID: nil, Portfolio: sqlcgen.PortfolioEnum(state.sold.portfolio), Ticker: req.Sold.Ticker,
		AccountType: sqlcgen.AccountTypeEnum(req.Sold.AccountType), UnitsDelta: pgnum.FromDecimal(req.Sold.Units.Neg()),
		PricePln: pgnum.FromDecimal(req.Sold.PricePln), TotalPln: pgnum.FromDecimal(req.Sold.Units.Mul(req.Sold.PricePln).Neg()),
		PurchasedAt: now, TradeType: sqlcgen.TradeTypeEnumREBALANCINGTRIMSELL,
	}); err != nil {
		return fmt.Errorf("rebalancing: audit sold leg for %s: %w", req.Sold.Ticker, err)
	}

	today := pgtype.Date{Time: dateOnly(historiaquery.NowInWarsaw()), Valid: true}
	writtenByDelta := []int64{state.sold.id}
	for i, buy := range req.Buys {
		if err := applyBuyLeg(ctx, q, req.Sold, buy, state.buys[i], now, today); err != nil {
			return err
		}
		writtenByDelta = append(writtenByDelta, state.buys[i].id)
	}

	// Siblings whose recorded cost was re-scaled or received a carried cost (rows the unit
	// deltas above did not already write) are persisted inside the same transaction.
	if err := position.PersistCosts(ctx, q, changedCosts, writtenByDelta...); err != nil {
		return fmt.Errorf("rebalancing: %w", err)
	}
	return nil
}

// applyBuyLeg adds one buy leg's units to its row (in memory, then written), audits it and, for a
// REGULAR-sold leg funding an IKE/IKZE buy, writes the ADR-054/059 auto-deposit: a genuine new
// statutory contribution (INTERNAL_TRANSFER - counts toward the annual cap, excluded from
// XIRR/totalInvestedPln).
func applyBuyLeg(ctx context.Context, q Queries, sold, buy Leg, buyPos *row, now pgtype.Timestamptz, today pgtype.Date) error {
	position.ApplyDeltaWithCurrencyOverride(&buyPos.units, buy.AccountType, ptrOrEmpty(buy.RegularCurrency), buy.Units)
	buyTotal := buy.Units.Mul(buy.PricePln)
	if _, err := q.UpdatePositionDelta(ctx, toDeltaParams(buyPos)); err != nil {
		return fmt.Errorf("rebalancing: apply buy delta for %s: %w", buy.Ticker, err)
	}
	if _, err := q.InsertAssetPurchase(ctx, sqlcgen.InsertAssetPurchaseParams{
		SessionID: nil, Portfolio: sqlcgen.PortfolioEnum(buyPos.portfolio), Ticker: buy.Ticker,
		AccountType: sqlcgen.AccountTypeEnum(buy.AccountType), UnitsDelta: pgnum.FromDecimal(buy.Units),
		PricePln: pgnum.FromDecimal(buy.PricePln), TotalPln: pgnum.FromDecimal(buyTotal),
		PurchasedAt: now, TradeType: sqlcgen.TradeTypeEnumREBALANCINGREINVESTBUY,
	}); err != nil {
		return fmt.Errorf("rebalancing: audit buy leg for %s: %w", buy.Ticker, err)
	}
	if sold.AccountType != position.AccountTypeREGULAR ||
		(buy.AccountType != position.AccountTypeIKE && buy.AccountType != position.AccountTypeIKZE) {
		return nil
	}
	comment := fmt.Sprintf("Auto from rebalancing transfer: sold %s (REGULAR) to fund %s (%s)",
		sold.Ticker, buy.Ticker, buy.AccountType)
	if _, err := q.InsertDeposit(ctx, sqlcgen.InsertDepositParams{
		DepositDate: today, Amount: pgnum.FromDecimal(buyTotal.Round(4)), Currency: sqlcgen.CurrencyEnumPLN,
		Exchange: sqlcgen.ExchangeEnum(buyPos.exchange), AccountType: sqlcgen.AccountTypeEnum(buy.AccountType),
		Portfolio: sqlcgen.PortfolioEnum(buyPos.portfolio), Comment: &comment,
		CapitalSource: sqlcgen.CapitalSourceEnumINTERNALTRANSFER,
	}); err != nil {
		return fmt.Errorf("rebalancing: auto-deposit for %s: %w", buy.Ticker, err)
	}
	return nil
}

// transferResult lists the sold ticker then each bought ticker, once each.
func transferResult(req TransferRequest) TransferResult {
	affected := make([]string, 0, 1+len(req.Buys))
	affected = append(affected, req.Sold.Ticker)
	for _, buy := range req.Buys {
		affected = append(affected, buy.Ticker)
	}
	return TransferResult{
		AffectedTickers: distinct(affected),
		Message: fmt.Sprintf("Rebalancing transfer completed: sold %s, bought %d asset(s).",
			req.Sold.Ticker, len(req.Buys)),
	}
}

// costMembersByAsset groups the loaded rows into cost-bookkeeping members by asset_id; each member
// points at its row's own PositionUnits so a cost written through it is what UpdatePositionDelta
// persists for that row.
func costMembersByAsset(byKey map[positionKey]*row) map[int64][]*position.CostMember {
	members := make(map[int64][]*position.CostMember)
	for _, r := range byKey {
		members[r.assetID] = append(members[r.assetID], &position.CostMember{
			ID: r.id, AssetID: r.assetID, YahooTicker: r.yahooTicker, Units: &r.units,
		})
	}
	for _, ms := range members { // byKey is a map: fix the order so cost writes are deterministic
		sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
	}
	return members
}

// planTransferCosts runs the costbasis kernel over the sold asset and every asset a buy leg
// touches (rows as they stand BEFORE any delta of this transfer), copies the new costs onto the
// in-memory rows and returns the members whose cost changed, sold asset first, then buy assets in
// leg order (deterministic write order).
func planTransferCosts(members map[int64][]*position.CostMember, prices map[string]decimal.Decimal,
	soldPos *row, req TransferRequest, byKey map[positionKey]*row) []*position.CostMember {
	legs := make([]costbasis.Leg, 0, len(req.Buys))
	for _, buy := range req.Buys {
		buyPos := byKey[positionKey{ticker: buy.Ticker, portfolio: req.Portfolio}]
		legs = append(legs, costbasis.Leg{PositionID: buyPos.id, AssetID: buyPos.assetID, Units: buy.Units, PricePln: buy.PricePln})
	}
	return costmember.PlanTransfer(members, prices, soldPos.assetID, costbasis.Transfer{
		Sold: costbasis.Sale{PositionID: soldPos.id, Units: req.Sold.Units, PricePln: req.Sold.PricePln},
		Buys: legs,
	})
}

func ptrOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// currentUnitsFor is the direct port of RebalancingTransferService.getCurrentUnits.
func currentUnitsFor(u position.PositionUnits, accountType, regularCurrency string) decimal.Decimal {
	switch accountType {
	case position.AccountTypeIKE:
		return u.UnitsIke
	case position.AccountTypeIKZE:
		return u.UnitsIkze
	default: // REGULAR
		bucket := regularCurrency
		if bucket == "" {
			bucket = u.PriceCurrency
		}
		switch bucket {
		case position.CurrencyEUR:
			return u.UnitsRegularEur
		case position.CurrencyUSD:
			return u.UnitsRegularUsd
		default:
			return u.UnitsRegularPln
		}
	}
}

func distinct(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
