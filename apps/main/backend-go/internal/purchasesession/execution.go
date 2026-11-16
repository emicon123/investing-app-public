package purchasesession

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/position"
	"investing-app/common-go/costbasis"
	"investing-app/common-go/costmember"
	"investing-app/common-go/historiaquery"
	"investing-app/common-go/pgnum"
)

// This file is EXACTLY completeSession's body — the FOR-UPDATE-gated, money-critical
// transaction touching positions/deposits/asset_purchases/binance_transfers (architect-output
// §1's refinement: nothing from createSession lives here). The GetSessionForUpdate call below
// MUST be the literal first statement inside the transaction — architect-output §4 calls this
// "the single highest-risk line in the entire phase to get wrong." See
// TestCompleteSession_ConcurrentDoubleSubmit_ExactlyOneSucceeds in integration_test.go, the
// mandatory concurrency test this line exists to satisfy.

type positionKey struct {
	ticker, portfolio string
}

type positionRow struct {
	id          int64
	assetID     int64
	yahooTicker *string
	units       position.PositionUnits
}

// CompleteSession is the direct port of PurchaseSessionService.completeSession. Wraps the whole
// body in the (separate, money-critical) executionTx.
func (s *Service) CompleteSession(ctx context.Context, sessionID int64) (CompleteResult, error) {
	var result CompleteResult
	err := s.executionTx(ctx, func(q Queries) error {
		out, err := completeSessionTx(ctx, q, sessionID, s.cachePeek)
		if err != nil {
			return err
		}
		result = out
		return nil
	})
	if err != nil {
		return CompleteResult{}, err
	}
	// The cache entry is only evicted now, after the transaction above has actually committed —
	// a rollback (returned as err above) leaves it in place so a retry still sees it.
	s.cacheTake(sessionID)
	return result, nil
}

// completeSessionTx is the pure transactional body, taking the tx-scoped Queries value and a
// non-destructive cachePeek closure (so unit tests can exercise this without the full
// Service/sync.Mutex plumbing; peeking rather than taking keeps this function safe to call
// inside a transaction that might still roll back). GetSessionForUpdate is the FIRST statement —
// do not reorder any read/write above it (loadCompletionState issues it).
func completeSessionTx(ctx context.Context, q Queries, sessionID int64, cachePeek func(int64) (map[groupKey]decimal.Decimal, bool)) (CompleteResult, error) {
	st, err := loadCompletionState(ctx, q, sessionID)
	if err != nil {
		return CompleteResult{}, err
	}

	// Step 1 - resolve every applicable item (units, price, value, position). Nothing is written
	// yet: an item whose position was deleted after the session was created fails the whole
	// completion loudly before any audit row or position update exists.
	trades, err := resolveTrades(st.items, st.positions, sessionID)
	if err != nil {
		return CompleteResult{}, err
	}

	// Step 2 - asset-level average cost (task 161a-2, design 161 §2). The whole session is netted
	// PER ASSET by the costbasis kernel from the pre-session state (asset rows + price_cache prices
	// read above through this same transaction - DB only): a donor/migration sell and a
	// (compensating) buy of the SAME asset are a swap and carry cost; only a buy excess enters at
	// market and only a sale excess scales the cost. The new costs are set on the in-memory rows,
	// so the unit writes below persist them together with the units; siblings that no item writes
	// are persisted after the loop, all inside this transaction.
	changedCosts := planSessionCosts(trades, st.members, st.prices)

	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}

	// Step 3 - audit rows and unit deltas, in item order.
	updatedPositionIDs, err := applyTrades(ctx, q, sessionID, trades, now)
	if err != nil {
		return CompleteResult{}, err
	}
	if err := position.PersistCosts(ctx, q, changedCosts, updatedPositionIDs...); err != nil {
		return CompleteResult{}, fmt.Errorf("purchasesession: %w", err)
	}

	if _, err := q.UpdateSessionStatusDone(ctx, sqlcgen.UpdateSessionStatusDoneParams{
		ID: sessionID, DoneAt: now,
	}); err != nil {
		return CompleteResult{}, fmt.Errorf("purchasesession: mark session %d done: %w", sessionID, err)
	}

	// ── Auto-deposit + binance-transfer side-ledger (ADR-033/035/059/065) ──
	newDepositPlnByBucket, hasCache := cachePeek(sessionID)
	depositSums, err := sumSessionDeposits(st.items, st.positions, newDepositPlnByBucket, hasCache)
	if err != nil {
		return CompleteResult{}, err
	}

	today := pgtype.Date{Time: dateOnly(historiaquery.NowInWarsaw()), Valid: true}
	depositsCreated, err := writeSessionDeposits(ctx, q, sessionID, depositSums, today)
	if err != nil {
		return CompleteResult{}, err
	}
	if err := writeBinanceTransfer(ctx, q, sessionID, depositSums, today); err != nil {
		return CompleteResult{}, err
	}

	return CompleteResult{
		SessionID: sessionID, Status: StatusDONE, PurchasesWritten: len(trades),
		UpdatedPositionIds: updatedPositionIDs, DepositsCreated: depositsCreated,
	}, nil
}

// completionState is what a completion reads before it writes: the session's items in completion
// order, every positions row by (ticker, portfolio), the same rows as cost-bookkeeping members by
// asset_id, and the price_cache PLN prices.
type completionState struct {
	items     []sqlcgen.PurchaseSessionItem
	positions map[positionKey]*positionRow
	members   map[int64][]*position.CostMember
	prices    map[string]decimal.Decimal
}

// loadCompletionState locks the session row FIRST (GetSessionForUpdate — a concurrent
// double-submit blocks here, then sees DONE), rejects a session that is not PLANNED, then reads
// its items, the positions and price_cache.
func loadCompletionState(ctx context.Context, q Queries, sessionID int64) (*completionState, error) {
	sessionRow, err := q.GetSessionForUpdate(ctx, sessionID)
	if err != nil {
		return nil, notFoundOrErr("Purchase session", sessionID, err)
	}
	if !CanComplete(string(sessionRow.Status)) {
		return nil, errSessionAlreadyDone(sessionID)
	}

	items, err := q.ListSessionItemsOrderedForCompletion(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("purchasesession: list items for completion: %w", err)
	}

	positionRows, err := q.ListAllPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("purchasesession: list positions: %w", err)
	}
	positionMap, assetMembers, err := decodePositionMap(positionRows)
	if err != nil {
		return nil, err
	}
	// Task 161a-2: prices for the asset-level cost bookkeeping come from price_cache through the
	// SAME transaction-scoped Queries — DB only, never a network call inside this transaction.
	priceRows, err := q.GetAllPriceCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("purchasesession: read price cache: %w", err)
	}
	prices, err := position.PricePlnMap(priceRows)
	if err != nil {
		return nil, err
	}
	return &completionState{items: items, positions: positionMap, members: assetMembers, prices: prices}, nil
}

// applyTrades writes each trade's asset_purchases audit row and applies its unit delta (units and
// the planned cost in one UpdatePositionDelta), in item order. Returns the written position ids,
// first-write order, each once.
func applyTrades(ctx context.Context, q Queries, sessionID int64, trades []sessionTrade, now pgtype.Timestamptz) ([]int64, error) {
	var updatedPositionIDs []int64
	seenPositionIDs := map[int64]bool{}
	for _, tr := range trades {
		item, pos := tr.item, tr.pos
		totalPln := tr.units.Mul(tr.pricePln)

		if _, err := q.InsertAssetPurchase(ctx, sqlcgen.InsertAssetPurchaseParams{
			SessionID: &sessionID, Portfolio: item.Portfolio, Ticker: item.Ticker, AccountType: item.AccountType,
			UnitsDelta: pgnum.FromDecimal(tr.units), PricePln: pgnum.FromDecimal(tr.pricePln),
			TotalPln: pgnum.FromDecimal(totalPln), PurchasedAt: now, TradeType: classifyTradeType(item),
		}); err != nil {
			return nil, fmt.Errorf("purchasesession: insert asset purchase for %s: %w", item.Ticker, err)
		}

		position.ApplyDelta(&pos.units, string(item.AccountType), tr.units)
		if _, err := q.UpdatePositionDelta(ctx, toDeltaParams(pos)); err != nil {
			return nil, fmt.Errorf("purchasesession: apply delta for %s: %w", item.Ticker, err)
		}
		if !seenPositionIDs[pos.id] {
			seenPositionIDs[pos.id] = true
			updatedPositionIDs = append(updatedPositionIDs, pos.id)
		}
	}
	return updatedPositionIDs, nil
}

// depositKey is one auto-deposit group: (portfolio, account type, exchange).
type depositKey struct {
	portfolio, accountType, exchange string
}

// sumSessionDeposits sums the new capital per deposit group (ADR-033/035/059/065): every item
// with a value adds it (a migration sell subtracts its proceeds), except IKE/IKZE items when
// createSession's cached new-deposit figures exist — those groups take the cached figures
// instead (XTB).
func sumSessionDeposits(items []sqlcgen.PurchaseSessionItem, positionMap map[positionKey]*positionRow,
	newDepositPlnByBucket map[groupKey]decimal.Decimal, hasCache bool) (map[depositKey]decimal.Decimal, error) {
	depositSums := map[depositKey]decimal.Decimal{}

	for _, item := range items {
		// NOTE the reversed priority vs. the units/value loop above: plannedValuePln (capital
		// committed) is preferred over actualValuePln (post-fee/post-conversion executed amount)
		// here (ADR-055).
		effectiveValuePln, hasValue, err := coalesceNumeric(item.PlannedValuePln, item.ActualValuePln)
		if err != nil {
			return nil, err
		}
		if !hasValue {
			continue
		}
		accountType := string(item.AccountType)
		directTrackedGroup := hasCache && (accountType == position.AccountTypeIKE || accountType == position.AccountTypeIKZE)
		if directTrackedGroup {
			continue
		}

		exchange := "XTB"
		if pos, ok := positionMap[positionKey{ticker: item.Ticker, portfolio: string(item.Portfolio)}]; ok {
			exchange = pos.units.Exchange
		}
		key := depositKey{portfolio: string(item.Portfolio), accountType: accountType, exchange: exchange}

		if item.IsMigrationSell {
			depositSums[key] = depositSums[key].Sub(effectiveValuePln.Abs())
			continue
		}
		effectiveUnits, hasUnits, err := coalesceNumeric(item.ActualUnits, item.PlannedUnits)
		if err != nil {
			return nil, err
		}
		if !hasUnits || effectiveUnits.Sign() <= 0 {
			continue
		}
		depositSums[key] = depositSums[key].Add(effectiveValuePln)
	}

	if hasCache {
		for gk, v := range newDepositPlnByBucket {
			if v.Sign() <= 0 {
				continue
			}
			key := depositKey{portfolio: gk.portfolio, accountType: gk.accountType, exchange: position.ExchangeXTB}
			depositSums[key] = depositSums[key].Add(v)
		}
	}
	return depositSums, nil
}

// writeSessionDeposits writes one EXTERNAL auto-deposit per group with new capital. Returns how
// many it wrote.
func writeSessionDeposits(ctx context.Context, q Queries, sessionID int64, depositSums map[depositKey]decimal.Decimal, today pgtype.Date) (int, error) {
	depositsCreated := 0
	for _, key := range sortedDepositKeys(depositSums) {
		value := depositSums[key]
		if value.Sign() <= 0 {
			continue // group fully offset by migration-sell proceeds — no new capital
		}
		comment := fmt.Sprintf("Auto from session #%d", sessionID)
		if _, err := q.InsertDeposit(ctx, sqlcgen.InsertDepositParams{
			Portfolio: sqlcgen.PortfolioEnum(key.portfolio), AccountType: sqlcgen.AccountTypeEnum(key.accountType),
			Exchange: sqlcgen.ExchangeEnum(key.exchange), Amount: pgnum.FromDecimal(value.Round(4)),
			Currency: sqlcgen.CurrencyEnumPLN, DepositDate: today, Comment: &comment,
			CapitalSource: sqlcgen.CapitalSourceEnumEXTERNAL,
		}); err != nil {
			return 0, fmt.Errorf("purchasesession: auto-deposit for %s/%s: %w", key.portfolio, key.accountType, err)
		}
		depositsCreated++
	}
	return depositsCreated, nil
}

// sortedDepositKeys orders the deposit groups by portfolio, account type, exchange, so the
// auto-deposits are inserted (and get their ids) in the same order on every completion instead
// of in Go map order.
func sortedDepositKeys(sums map[depositKey]decimal.Decimal) []depositKey {
	keys := slices.Collect(maps.Keys(sums))
	slices.SortFunc(keys, func(a, b depositKey) int {
		return cmp.Or(cmp.Compare(a.portfolio, b.portfolio), cmp.Compare(a.accountType, b.accountType), cmp.Compare(a.exchange, b.exchange))
	})
	return keys
}

// writeBinanceTransfer books the session's SESSION-sourced binance_transfers side-ledger row:
// the sum of the BINANCE groups' new capital, when positive.
func writeBinanceTransfer(ctx context.Context, q Queries, sessionID int64, depositSums map[depositKey]decimal.Decimal, today pgtype.Date) error {
	binanceTotalPln := decimal.Zero
	for key, value := range depositSums {
		if key.exchange == position.ExchangeBINANCE && value.Sign() > 0 {
			binanceTotalPln = binanceTotalPln.Add(value)
		}
	}
	if binanceTotalPln.Sign() <= 0 {
		return nil
	}
	comment := fmt.Sprintf("Auto from session #%d", sessionID)
	if _, err := q.InsertBinanceTransferSession(ctx, sqlcgen.InsertBinanceTransferSessionParams{
		TransferDate: today, AmountPln: pgnum.FromDecimal(binanceTotalPln.Round(2)), SessionID: &sessionID, Comment: &comment,
	}); err != nil {
		return fmt.Errorf("purchasesession: binance transfer side-ledger: %w", err)
	}
	return nil
}

// sessionTrade is one applicable session item resolved against its position: signed units (buy
// > 0, sale < 0), execution price and, when the item carries one, the executed PLN value.
type sessionTrade struct {
	item     sqlcgen.PurchaseSessionItem
	pos      *positionRow
	units    decimal.Decimal
	pricePln decimal.Decimal
	value    decimal.Decimal
	hasValue bool
}

// resolveTrades keeps exactly the items completeSession applies (non-zero units, a usable price)
// and looks each one's position up; a missing position fails the completion.
func resolveTrades(items []sqlcgen.PurchaseSessionItem, positionMap map[positionKey]*positionRow, sessionID int64) ([]sessionTrade, error) {
	var trades []sessionTrade
	for _, item := range items {
		effectiveUnits, hasUnits, err := coalesceNumeric(item.ActualUnits, item.PlannedUnits)
		if err != nil {
			return nil, err
		}
		if !hasUnits || effectiveUnits.IsZero() {
			continue
		}
		effectiveValuePln, hasValue, err := coalesceNumeric(item.ActualValuePln, item.PlannedValuePln)
		if err != nil {
			return nil, err
		}

		var pricePln decimal.Decimal
		switch {
		case hasValue && effectiveUnits.Abs().Sign() > 0:
			pricePln = effectiveValuePln.DivRound(effectiveUnits.Abs(), 8)
		case item.PricePlnAtPlan.Valid:
			pricePln, err = pgnum.ToDecimal(item.PricePlnAtPlan)
			if err != nil {
				return nil, err
			}
		default:
			continue // no price available for this item; skip
		}
		if pricePln.Sign() <= 0 {
			continue
		}

		pos, ok := positionMap[positionKey{ticker: item.Ticker, portfolio: string(item.Portfolio)}]
		if !ok {
			return nil, fmt.Errorf(
				"purchasesession: no position for %s/%s while completing session %d (deleted after the session was created?)",
				item.Ticker, item.Portfolio, sessionID)
		}
		trades = append(trades, sessionTrade{
			item: item, pos: pos, units: effectiveUnits, pricePln: pricePln, value: effectiveValuePln, hasValue: hasValue,
		})
	}
	return trades, nil
}

// planSessionCosts asks the costbasis kernel for the new recorded costs of every asset the
// session's trades touch (rows = the in-memory state BEFORE any unit delta of the session),
// writes them onto the members' in-memory PositionUnits and returns the members whose cost
// changed. A buy counts only when the item carries a value (as before: the paid amount is
// unknown otherwise); a sale is valued at its executed value when present, else units x price.
func planSessionCosts(trades []sessionTrade, members map[int64][]*position.CostMember, prices map[string]decimal.Decimal) []*position.CostMember {
	var sales []costbasis.Sale
	var buys []costbasis.Leg
	var order []int64
	seen := map[int64]bool{}
	for _, tr := range trades {
		if !seen[tr.pos.assetID] {
			seen[tr.pos.assetID] = true
			order = append(order, tr.pos.assetID)
		}
		var exact *decimal.Decimal
		if tr.hasValue {
			v := tr.value.Abs()
			exact = &v
		}
		if tr.units.Sign() > 0 {
			if exact == nil {
				continue
			}
			buys = append(buys, costbasis.Leg{PositionID: tr.pos.id, AssetID: tr.pos.assetID, Units: tr.units, PricePln: tr.pricePln, ValuePln: exact})
			continue
		}
		sales = append(sales, costbasis.Sale{PositionID: tr.pos.id, Units: tr.units.Abs(), PricePln: tr.pricePln, ValuePln: exact})
	}
	if len(sales) == 0 && len(buys) == 0 {
		return nil
	}

	before := make(map[int64][]costbasis.Row, len(order))
	for _, assetID := range order {
		before[assetID] = costmember.Rows(members[assetID], prices)
	}
	after := costbasis.PlanTrades(before, sales, buys)
	var changed []*position.CostMember
	for _, assetID := range order {
		changed = append(changed, costmember.Apply(members[assetID], before[assetID], after[assetID])...)
	}
	return changed
}

// classifyTradeType is the direct port of PurchaseSessionService.classifyTradeType (ADR-057).
func classifyTradeType(item sqlcgen.PurchaseSessionItem) sqlcgen.TradeTypeEnum {
	if item.IsMigrationSell {
		return sqlcgen.TradeTypeEnumMIGRATIONSELL
	}
	if item.IsMigrationBuy {
		return sqlcgen.TradeTypeEnumMIGRATIONBUY
	}
	return sqlcgen.TradeTypeEnumDCABUY
}

// coalesceNumeric prefers preferred over fallback, decoding whichever is Valid. A decode error on
// either value is propagated rather than silently falling through — a corrupted numeric here must
// fail the completion loudly, not silently skip or misvalue a money-critical item.
func coalesceNumeric(preferred, fallback pgtype.Numeric) (decimal.Decimal, bool, error) {
	if preferred.Valid {
		v, err := pgnum.ToDecimal(preferred)
		if err != nil {
			return decimal.Zero, false, fmt.Errorf("purchasesession: decode numeric: %w", err)
		}
		return v, true, nil
	}
	if fallback.Valid {
		v, err := pgnum.ToDecimal(fallback)
		if err != nil {
			return decimal.Zero, false, fmt.Errorf("purchasesession: decode numeric: %w", err)
		}
		return v, true, nil
	}
	return decimal.Zero, false, nil
}

// decodePositionMap decodes every positions row and also groups them into cost-bookkeeping
// members by asset_id; each member points at its positionRow's own PositionUnits, so a cost
// written through a member is what UpdatePositionDelta persists for that row.
func decodePositionMap(rows []sqlcgen.Position) (map[positionKey]*positionRow, map[int64][]*position.CostMember, error) {
	out := make(map[positionKey]*positionRow, len(rows))
	members := make(map[int64][]*position.CostMember)
	for _, r := range rows {
		ike, err := pgnum.ToDecimal(r.UnitsIke)
		if err != nil {
			return nil, nil, fmt.Errorf("purchasesession: decode units_ike for %s: %w", r.Ticker, err)
		}
		ikze, err := pgnum.ToDecimal(r.UnitsIkze)
		if err != nil {
			return nil, nil, fmt.Errorf("purchasesession: decode units_ikze for %s: %w", r.Ticker, err)
		}
		eur, err := pgnum.ToDecimal(r.UnitsRegularEur)
		if err != nil {
			return nil, nil, fmt.Errorf("purchasesession: decode units_regular_eur for %s: %w", r.Ticker, err)
		}
		usd, err := pgnum.ToDecimal(r.UnitsRegularUsd)
		if err != nil {
			return nil, nil, fmt.Errorf("purchasesession: decode units_regular_usd for %s: %w", r.Ticker, err)
		}
		pln, err := pgnum.ToDecimal(r.UnitsRegularPln)
		if err != nil {
			return nil, nil, fmt.Errorf("purchasesession: decode units_regular_pln for %s: %w", r.Ticker, err)
		}
		// NULL-preserving: a sell on a NULL cost basis must write back NULL, not 0 (see
		// position.PositionUnits.TotalSpentPln).
		totalSpent, err := pgnum.ToDecimalPtr(r.TotalSpentPln)
		if err != nil {
			return nil, nil, fmt.Errorf("purchasesession: decode total_spent_pln for %s: %w", r.Ticker, err)
		}
		row := &positionRow{
			id: r.ID, assetID: r.AssetID, yahooTicker: r.YahooTicker,
			units: position.PositionUnits{
				Exchange: string(r.Exchange), PriceCurrency: string(r.PriceCurrency),
				UnitsIke: ike, UnitsIkze: ikze, UnitsRegularEur: eur, UnitsRegularUsd: usd, UnitsRegularPln: pln,
				TotalSpentPln: totalSpent,
			},
		}
		out[positionKey{ticker: r.Ticker, portfolio: string(r.Portfolio)}] = row
		members[r.AssetID] = append(members[r.AssetID], &position.CostMember{
			ID: r.ID, AssetID: r.AssetID, YahooTicker: r.YahooTicker, Units: &row.units,
		})
	}
	return out, members, nil
}

func toDeltaParams(r *positionRow) sqlcgen.UpdatePositionDeltaParams {
	return position.ToDeltaParams(r.id, r.units)
}
