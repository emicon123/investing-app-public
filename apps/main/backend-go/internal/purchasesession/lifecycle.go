package purchasesession

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/deposit"
	"investing-app/backend-go/internal/rebalancingcalc"
	"investing-app/common-go/historiaquery"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgerr"
	"investing-app/common-go/pgnum"
)

// This file is the direct port of PurchaseSessionService's session/item CRUD gated by status —
// createSession, getActiveSession, updateSessionItems, listSessions, deleteSession — plus
// createSession's OWN persistence of the new purchase_sessions/purchase_session_items rows
// (architect-output §1's refinement: this belongs here, not in execution.go, since it never
// touches positions/deposits/asset_purchases/binance_transfers). Every write here is scoped to
// this package's own tables only, via the CHEAP lifecycleTx (architect-output §4).

// ErrPlannedSessionExists mirrors PlanSessionAlreadyExistsException (409, PLANNED_SESSION_EXISTS).
func errPlannedSessionExists(existingID int64) *httpx.AppError {
	return &httpx.AppError{Status: 409, Code: "PLANNED_SESSION_EXISTS",
		Message: fmt.Sprintf("A PLANNED purchase session already exists (id=%d)", existingID)}
}

// errSessionAlreadyDone mirrors SessionAlreadyDoneException (409, SESSION_ALREADY_DONE).
func errSessionAlreadyDone(id int64) *httpx.AppError {
	return &httpx.AppError{Status: 409, Code: httpx.CodeSessionAlreadyDone,
		Message: fmt.Sprintf("Purchase session %d is already DONE", id)}
}

// CreateSession is the direct port of PurchaseSessionService.createSession: inside one
// lifecycle transaction it rejects a second PLANNED session, plans the session (planSession) and
// persists it with its items (insertPlannedSession).
func (s *Service) CreateSession(ctx context.Context, in CreateSessionInput) (SessionDetail, error) {
	var detail SessionDetail
	var newSessionID int64
	var newDepositPlnByBucket map[groupKey]decimal.Decimal
	err := s.lifecycleTx(ctx, func(q Queries) error {
		if err := rejectIfPlannedSessionExists(ctx, q); err != nil {
			return err
		}
		plan, err := s.planSession(ctx, q, in)
		if err != nil {
			return err
		}
		sessionRow, savedItems, err := insertPlannedSession(ctx, q, in, plan)
		if err != nil {
			return err
		}

		// ADR-065 §5: remember the authoritative new-deposit figure for this session's id — captured
		// here (session id now exists) but only actually stashed in the cache after this whole
		// transaction commits (below), so a rollback never leaves an orphaned cache entry.
		newSessionID = sessionRow.ID
		newDepositPlnByBucket = plan.result.NewDepositPlnByBucket

		detail = decodeSession(sessionRow)
		detail.Items = savedItems
		detail.CurrencyBreakdown = plan.breakdown
		detail.RetirementLimitsRemaining = plan.limits
		detail.MainLimitsRemaining = plan.limits
		return nil
	})
	if err != nil {
		return SessionDetail{}, err
	}
	s.cachePut(newSessionID, newDepositPlnByBucket)
	return detail, nil
}

// rejectIfPlannedSessionExists is the at-most-one-PLANNED-session guard (409).
func rejectIfPlannedSessionExists(ctx context.Context, q Queries) error {
	exists, err := q.ExistsPlannedSession(ctx)
	if err != nil {
		return fmt.Errorf("purchasesession: check existing planned session: %w", err)
	}
	if !exists {
		return nil
	}
	existing, err := q.GetPlannedSession(ctx)
	if err != nil {
		return fmt.Errorf("purchasesession: read existing planned session: %w", err)
	}
	return errPlannedSessionExists(existing.ID)
}

// sessionBuyLists is both portfolios' rebalancing output as a session uses it: the included
// portfolios' buy lists and budgets.
type sessionBuyLists struct {
	retirementList, mainList           []rebalancingcalc.BuyListItem
	retirementBudgetPln, mainBudgetPln decimal.Decimal
}

// sessionPlan is a computed, not yet persisted session: budgets, statutory limits, the planned
// items and new-deposit figures, the book they were planned on and the currency breakdown.
type sessionPlan struct {
	retirementBudgetPln, mainBudgetPln decimal.Decimal
	limits                             AccountLimits
	allPositions                       []rebalancingcalc.PositionRow
	positionByTicker                   map[string]rebalancingcalc.PositionRow
	result                             ComputePlanResult
	breakdown                          CurrencyBreakdown
}

// calculateBuyLists runs the rebalancing calculation for the included portfolios only and keeps
// their buy lists and budgets. (Until task 192 both portfolios were always computed, ADR-065 §4,
// only to feed buy-list deviations to the donor ranking; the donor order is fixed now.)
func (s *Service) calculateBuyLists(ctx context.Context, q Queries, in CreateSessionInput) (sessionBuyLists, error) {
	out := sessionBuyLists{retirementBudgetPln: decimal.Zero, mainBudgetPln: decimal.Zero}
	if in.PortfoliosIncluded == PortfoliosIncludedBOTH || in.PortfoliosIncluded == PortfoliosIncludedRETIREMENT {
		result, err := rebalancingcalc.Calculate(ctx, q, s.prices, rebalancingcalc.PortfolioRETIREMENT, nil)
		if err != nil {
			return sessionBuyLists{}, err
		}
		out.retirementList, out.retirementBudgetPln = result.BuyList, result.DeployableCashPln
	}
	if in.PortfoliosIncluded == PortfoliosIncludedBOTH || in.PortfoliosIncluded == PortfoliosIncludedMAIN {
		result, err := rebalancingcalc.Calculate(ctx, q, s.prices, rebalancingcalc.PortfolioMAIN, in.CashOverridePln)
		if err != nil {
			return sessionBuyLists{}, err
		}
		out.mainList, out.mainBudgetPln = result.BuyList, result.DeployableCashPln
	}
	return out, nil
}

// planSession computes the session: buy lists, this year's IKE/IKZE room, the live book and
// prices, ComputePlan, and the currency breakdown of the planned items.
func (s *Service) planSession(ctx context.Context, q Queries, in CreateSessionInput) (sessionPlan, error) {
	lists, err := s.calculateBuyLists(ctx, q, in)
	if err != nil {
		return sessionPlan{}, err
	}

	year := historiaquery.NowInWarsaw().Year()
	limits, err := computeLimits(ctx, q, year)
	if err != nil {
		return sessionPlan{}, err
	}

	positionRows, err := q.ListAllPositions(ctx)
	if err != nil {
		return sessionPlan{}, fmt.Errorf("purchasesession: list positions: %w", err)
	}
	assetRows, err := q.ListAllAssets(ctx)
	if err != nil {
		return sessionPlan{}, fmt.Errorf("purchasesession: list assets: %w", err)
	}
	allPositions, err := rebalancingcalc.DecodeAllPositions(positionRows, assetRows)
	if err != nil {
		return sessionPlan{}, err
	}
	pricePlnMap, err := s.prices.PricePlnByYahooTicker(ctx)
	if err != nil {
		return sessionPlan{}, fmt.Errorf("purchasesession: load prices: %w", err)
	}

	planResult, err := ComputePlan(lists.retirementList, lists.mainList, limits.IkeRemainingPln, limits.IkzeRemainingPln,
		pricePlnMap, allPositions)
	if err != nil {
		return sessionPlan{}, err
	}

	fx, err := getLatestFxRates(ctx, s.prices)
	if err != nil {
		return sessionPlan{}, err
	}
	positionByTicker := indexPositionsByTicker(allPositions)
	breakdown, err := computeBreakdown(planResult.Items, positionByTicker, fx.eur, fx.usd)
	if err != nil {
		return sessionPlan{}, err
	}

	return sessionPlan{
		retirementBudgetPln: lists.retirementBudgetPln, mainBudgetPln: lists.mainBudgetPln, limits: limits,
		allPositions: allPositions, positionByTicker: positionByTicker, result: planResult, breakdown: breakdown,
	}, nil
}

// insertPlannedSession writes the PLANNED purchase_sessions row and its items, returning the row
// and the saved items with their price currency and exchange filled in.
func insertPlannedSession(ctx context.Context, q Queries, in CreateSessionInput, plan sessionPlan) (sqlcgen.PurchaseSession, []*SessionItem, error) {
	sessionRow, err := q.InsertPurchaseSession(ctx, sqlcgen.InsertPurchaseSessionParams{
		Status: sqlcgen.SessionStatusEnumPLANNED, PlannedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		Notes: in.Notes, RetirementBudgetPln: pgnum.FromDecimal(maxZero(plan.retirementBudgetPln)),
		MainBudgetPln: pgnum.FromDecimal(maxZero(plan.mainBudgetPln)), PortfoliosIncluded: sqlcgen.PortfoliosIncludedEnum(in.PortfoliosIncluded),
	})
	if err != nil {
		return sqlcgen.PurchaseSession{}, nil, fmt.Errorf("purchasesession: insert session: %w", err)
	}

	savedItems := make([]*SessionItem, 0, len(plan.result.Items))
	for _, item := range plan.result.Items {
		row, err := q.InsertPurchaseSessionItem(ctx, toInsertItemParams(sessionRow.ID, item))
		if err != nil {
			return sqlcgen.PurchaseSession{}, nil, fmt.Errorf("purchasesession: insert session item %s: %w", item.Ticker, err)
		}
		saved, err := decodeItem(row)
		if err != nil {
			return sqlcgen.PurchaseSession{}, nil, err
		}
		savedItems = append(savedItems, saved)
	}
	enrichPriceCurrencies(savedItems, plan.positionByTicker, plan.allPositions)
	return sessionRow, savedItems, nil
}

// GetActiveSession is the direct port of PurchaseSessionService.getActiveSession.
func (s *Service) GetActiveSession(ctx context.Context) (SessionDetail, error) {
	sessionRow, err := s.queries.GetPlannedSession(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionDetail{}, httpx.NewNotFoundError("No active purchase session")
		}
		return SessionDetail{}, fmt.Errorf("purchasesession: read active session: %w", err)
	}
	return s.buildDetail(ctx, s.queries, sessionRow)
}

// UpdateSessionItems replaces the planned items of a session (ported from the Java
// PurchaseSessionService.updateSessionItems).
//
// The negative-value rejection below is one of the two functions architect-output §3 flags for
// closest manual-sell-only scrutiny (item 5): ported VERBATIM from the Java guard — the one place a
// negative number could otherwise sneak into completeSession's write path outside the two
// sanctioned mechanisms (manual rebalancing transfer, IKE/IKZE crypto donor).
func (s *Service) UpdateSessionItems(ctx context.Context, sessionID int64, entries []UpdateItemEntry) (SessionDetail, error) {
	var detail SessionDetail
	err := s.lifecycleTx(ctx, func(q Queries) error {
		sessionRow, err := q.GetSessionByID(ctx, sessionID)
		if err != nil {
			return notFoundOrErr("Purchase session", sessionID, err)
		}
		if !CanUpdate(string(sessionRow.Status)) {
			return errSessionAlreadyDone(sessionID)
		}

		existingRows, err := q.ListSessionItemsBySessionID(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("purchasesession: list items for session %d: %w", sessionID, err)
		}
		itemsByID := map[int64]sqlcgen.PurchaseSessionItem{}
		for _, r := range existingRows {
			itemsByID[r.ID] = r
		}

		for _, entry := range entries {
			row, ok := itemsByID[entry.ID]
			if !ok {
				return httpx.NewNotFoundError(fmt.Sprintf("Session item %d not found in session %d", entry.ID, sessionID))
			}
			if !row.IsMigrationSell {
				if entry.ActualUnits != nil && entry.ActualUnits.Sign() < 0 {
					return httpx.NewValidationError("", fmt.Sprintf(
						"actualUnits must not be negative for item %d (%s) — only migration-sell items may be negative", entry.ID, row.Ticker))
				}
				if entry.ActualValuePln != nil && entry.ActualValuePln.Sign() < 0 {
					return httpx.NewValidationError("", fmt.Sprintf(
						"actualValuePln must not be negative for item %d (%s) — only migration-sell items may be negative", entry.ID, row.Ticker))
				}
			}
			if _, err := q.UpdateSessionItemActuals(ctx, sqlcgen.UpdateSessionItemActualsParams{
				ID: entry.ID, ActualUnits: pgnum.FromDecimalPtr(entry.ActualUnits), ActualValuePln: pgnum.FromDecimalPtr(entry.ActualValuePln),
			}); err != nil {
				return fmt.Errorf("purchasesession: update item %d: %w", entry.ID, err)
			}
		}

		out, err := s.buildDetail(ctx, q, sessionRow)
		if err != nil {
			return err
		}
		detail = out
		return nil
	})
	return detail, err
}

// DeleteSession is the direct port of PurchaseSessionService.deleteSession.
func (s *Service) DeleteSession(ctx context.Context, id int64) error {
	return s.lifecycleTx(ctx, func(q Queries) error {
		sessionRow, err := q.GetSessionByID(ctx, id)
		if err != nil {
			return notFoundOrErr("Purchase session", id, err)
		}
		if !CanDelete(string(sessionRow.Status)) {
			return errSessionAlreadyDone(id)
		}
		if err := q.DeleteSessionItemsBySessionID(ctx, id); err != nil {
			return fmt.Errorf("purchasesession: delete items for session %d: %w", id, err)
		}
		if err := q.DeleteSessionByID(ctx, id); err != nil {
			return fmt.Errorf("purchasesession: delete session %d: %w", id, err)
		}
		// ADR-065 §5: evict the cached new-deposit figure so a discarded PLANNED session never
		// leaves an orphaned entry.
		s.cacheEvict(id)
		return nil
	})
}

// ListSessions is the direct port of PurchaseSessionService.listSessions.
func (s *Service) ListSessions(ctx context.Context, page, size int) ([]SessionSummary, int64, error) {
	rows, err := s.queries.ListSessionsByStatusPaged(ctx, sqlcgen.ListSessionsByStatusPagedParams{
		Status: sqlcgen.SessionStatusEnumDONE, Limit: int32(size), Offset: int32(page * size),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("purchasesession: list sessions: %w", err)
	}
	total, err := s.queries.CountSessionsByStatus(ctx, sqlcgen.SessionStatusEnumDONE)
	if err != nil {
		return nil, 0, fmt.Errorf("purchasesession: count sessions: %w", err)
	}
	totalsByID, err := s.itemTotalsBySession(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	out := make([]SessionSummary, 0, len(rows))
	for _, r := range rows {
		// A session absent from the totals has no items, so no actual total either.
		totals := totalsByID[r.ID]
		summary := decodeSummary(r, int(totals.ItemCount))
		if totals.HasActual {
			totalActual, err := pgnum.ToDecimal(totals.TotalActualPln)
			if err != nil {
				return nil, 0, err
			}
			if totalActual.Sign() != 0 {
				summary.TotalActualPln = &totalActual
			}
		}
		out = append(out, summary)
	}
	return out, total, nil
}

// itemTotalsBySession reads the item count and actual-value total of every listed session in one
// query (F3.5: was one item read per session), keyed by session id.
func (s *Service) itemTotalsBySession(ctx context.Context, sessions []sqlcgen.PurchaseSession) (map[int64]sqlcgen.ListSessionItemTotalsRow, error) {
	ids := make([]int64, len(sessions))
	for i, r := range sessions {
		ids[i] = r.ID
	}
	rows, err := s.queries.ListSessionItemTotals(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("purchasesession: list item totals: %w", err)
	}
	out := make(map[int64]sqlcgen.ListSessionItemTotalsRow, len(rows))
	for _, r := range rows {
		out[r.SessionID] = r
	}
	return out, nil
}

// ── shared helpers ──────────────────────────────────────────────────────────

// buildDetail takes an explicit Queries value rather than always reading via s.queries — callers
// still inside an open transaction (e.g. UpdateSessionItems) must pass their own tx-scoped q so
// this reads the write they just made instead of a stale pre-commit snapshot from a separate
// connection.
func (s *Service) buildDetail(ctx context.Context, q Queries, sessionRow sqlcgen.PurchaseSession) (SessionDetail, error) {
	itemRows, err := q.ListSessionItemsBySessionID(ctx, sessionRow.ID)
	if err != nil {
		return SessionDetail{}, fmt.Errorf("purchasesession: list items for session %d: %w", sessionRow.ID, err)
	}
	items := make([]*SessionItem, 0, len(itemRows))
	for _, r := range itemRows {
		it, err := decodeItem(r)
		if err != nil {
			return SessionDetail{}, err
		}
		items = append(items, it)
	}

	// Task 173: history read — a session (past or open) keeps naming soft-deleted assets.
	positionRows, err := q.ListAllPositionsIncludingDeleted(ctx)
	if err != nil {
		return SessionDetail{}, fmt.Errorf("purchasesession: list positions: %w", err)
	}
	assetRows, err := q.ListAllAssetsIncludingDeleted(ctx)
	if err != nil {
		return SessionDetail{}, fmt.Errorf("purchasesession: list assets: %w", err)
	}
	allPositions, err := rebalancingcalc.DecodeAllPositions(positionRows, assetRows)
	if err != nil {
		return SessionDetail{}, err
	}
	positionByTicker := indexPositionsByTicker(allPositions)
	enrichPriceCurrencies(items, positionByTicker, allPositions)

	fx, err := getLatestFxRates(ctx, s.prices)
	if err != nil {
		return SessionDetail{}, err
	}
	breakdown, err := computeBreakdown(items, positionByTicker, fx.eur, fx.usd)
	if err != nil {
		return SessionDetail{}, err
	}

	limits, err := computeLimits(ctx, q, historiaquery.NowInWarsaw().Year())
	if err != nil {
		return SessionDetail{}, err
	}

	detail := decodeSession(sessionRow)
	detail.Items = items
	detail.CurrencyBreakdown = breakdown
	detail.RetirementLimitsRemaining = limits
	detail.MainLimitsRemaining = limits
	return detail, nil
}

// computeLimits is the direct port of PurchaseSessionService.computeLimits — ikeRemainingPln/
// ikzeRemainingPln are floored at ZERO (differs from AccountCapUsageItem.remainingPln), since
// this figure directly caps how much of a session's budget planning.ComputePlan may allocate.
func computeLimits(ctx context.Context, q deposit.CapUsageQueries, year int) (AccountLimits, error) {
	ike, err := deposit.ComputeCapUsage(ctx, q, "IKE", year)
	if err != nil {
		return AccountLimits{}, err
	}
	ikze, err := deposit.ComputeCapUsage(ctx, q, "IKZE", year)
	if err != nil {
		return AccountLimits{}, err
	}
	return AccountLimits{
		IkeUsedPln: ike.TotalDepositedPln, IkeRemainingPln: maxZero(ike.RemainingPln),
		IkzeUsedPln: ikze.TotalDepositedPln, IkzeRemainingPln: maxZero(ikze.RemainingPln),
		IkeAnnualLimitPln: ike.AnnualCapPln, IkzeAnnualLimitPln: ikze.AnnualCapPln,
	}, nil
}

type fxSnapshot struct{ eur, usd decimal.Decimal }

func getLatestFxRates(ctx context.Context, prices PriceSource) (fxSnapshot, error) {
	rates, err := prices.AllRatesToPln(ctx)
	if err != nil {
		return fxSnapshot{}, fmt.Errorf("purchasesession: load fx rates: %w", err)
	}
	return fxSnapshot{eur: rates["EUR"], usd: rates["USD"]}, nil
}

func indexPositionsByTicker(rows []rebalancingcalc.PositionRow) map[string]rebalancingcalc.PositionRow {
	out := make(map[string]rebalancingcalc.PositionRow, len(rows))
	for _, p := range rows {
		if _, exists := out[p.Ticker]; !exists {
			out[p.Ticker] = p
		}
	}
	return out
}

// enrichPriceCurrencies fills each item's price currency (ported from the Java
// enrichPriceCurrencies), plus an Exchange lookup the original DTO never carried (see handler.go's
// doc comment on the generated PurchaseSessionItemPlanned type for why).
func enrichPriceCurrencies(items []*SessionItem, positionByTicker map[string]rebalancingcalc.PositionRow, allPositions []rebalancingcalc.PositionRow) {
	for _, item := range items {
		pos, ok := positionByTicker[item.Ticker]
		if !ok {
			continue
		}
		// A non-primary subasset item reports its asset's PRIMARY's price currency (ported
		// bug-for-bug from the old companion -> parent lookup; primary = old main row).
		currency := pos.PriceCurrency
		if !pos.IsPrimary {
			if primary := findPrimarySubasset(pos.Asset.ID, pos.Portfolio, allPositions); primary != nil {
				currency = primary.PriceCurrency
			}
		}
		item.PriceCurrency = currency
		item.Exchange = pos.Exchange
	}
}

// computeBreakdown is the direct port of PurchaseSessionService.computeBreakdown.
func computeBreakdown(items []*SessionItem, positionByTicker map[string]rebalancingcalc.PositionRow, eurRate, usdRate decimal.Decimal) (CurrencyBreakdown, error) {
	var xtbPln, xtbEur, xtbEurPln, xtbUsd, xtbUsdPln, binanceEur, binanceEurPln decimal.Decimal

	for _, item := range items {
		pos, ok := positionByTicker[item.Ticker]
		if !ok {
			continue
		}
		if item.PlannedValuePln == nil {
			continue
		}
		valuePln := *item.PlannedValuePln
		if item.IsMigrationSell {
			valuePln = valuePln.Neg()
		}

		exchange := pos.Exchange
		purchaseCurrency := pos.PurchaseCurrency
		if item.AccountType != "IKE" && item.AccountType != "IKZE" && pos.AssetClass == "CRYPTO" {
			exchange = "BINANCE"
		}

		switch exchange {
		case "XTB":
			if item.AccountType == "IKE" || item.AccountType == "IKZE" {
				xtbPln = xtbPln.Add(valuePln)
			} else {
				switch purchaseCurrency {
				case "EUR":
					rate, err := requireRate(eurRate, "EUR")
					if err != nil {
						return CurrencyBreakdown{}, err
					}
					xtbEur = xtbEur.Add(valuePln.DivRound(rate, 4))
					xtbEurPln = xtbEurPln.Add(valuePln)
				case "USD":
					rate, err := requireRate(usdRate, "USD")
					if err != nil {
						return CurrencyBreakdown{}, err
					}
					xtbUsd = xtbUsd.Add(valuePln.DivRound(rate, 4))
					xtbUsdPln = xtbUsdPln.Add(valuePln)
				default:
					xtbPln = xtbPln.Add(valuePln)
				}
			}
		case "BINANCE":
			rate, err := requireRate(eurRate, "EUR")
			if err != nil {
				return CurrencyBreakdown{}, err
			}
			binanceEur = binanceEur.Add(valuePln.DivRound(rate, 4))
			binanceEurPln = binanceEurPln.Add(valuePln)
		}
	}

	return CurrencyBreakdown{
		XtbPln: xtbPln, XtbEur: xtbEur, XtbEurPln: xtbEurPln, XtbUsd: xtbUsd, XtbUsdPln: xtbUsdPln,
		BinanceEur: binanceEur, BinanceEurPln: binanceEurPln, UsdPlnRate: usdRate, EurPlnRate: eurRate,
	}, nil
}

func requireRate(rate decimal.Decimal, currency string) (decimal.Decimal, error) {
	if rate.Sign() <= 0 {
		return decimal.Zero, httpx.NewServiceUnavailableError("FX rate unavailable for currency: " + currency)
	}
	return rate, nil
}

func maxZero(d decimal.Decimal) decimal.Decimal {
	if d.Sign() < 0 {
		return decimal.Zero
	}
	return d
}

func notFoundOrErr(entity string, id int64, err error) error {
	return pgerr.NotFoundOr(err, fmt.Sprintf("%s %d not found", entity, id), fmt.Sprintf("purchasesession: read %s %d", entity, id))
}

// ── decode/encode between sqlcgen rows and this package's own types ────────

func decodeSession(r sqlcgen.PurchaseSession) SessionDetail {
	retirementBudget := pgnum.ToDecimalOrZero(r.RetirementBudgetPln)
	mainBudget := pgnum.ToDecimalOrZero(r.MainBudgetPln)
	var doneAt *time.Time
	if r.DoneAt.Valid {
		t := r.DoneAt.Time
		doneAt = &t
	}
	return SessionDetail{
		ID: r.ID, Status: string(r.Status), PlannedAt: r.PlannedAt.Time, DoneAt: doneAt, Notes: r.Notes,
		RetirementBudgetPln: retirementBudget, MainBudgetPln: mainBudget, PortfoliosIncluded: string(r.PortfoliosIncluded),
	}
}

func decodeSummary(r sqlcgen.PurchaseSession, itemCount int) SessionSummary {
	var doneAt *time.Time
	if r.DoneAt.Valid {
		t := r.DoneAt.Time
		doneAt = &t
	}
	return SessionSummary{
		ID: r.ID, Status: string(r.Status), PlannedAt: r.PlannedAt.Time, DoneAt: doneAt,
		PortfoliosIncluded: string(r.PortfoliosIncluded), RetirementBudgetPln: pgnum.ToDecimalOrZero(r.RetirementBudgetPln),
		MainBudgetPln: pgnum.ToDecimalOrZero(r.MainBudgetPln), ItemCount: itemCount,
	}
}

func decodeItem(r sqlcgen.PurchaseSessionItem) (*SessionItem, error) {
	item := &SessionItem{
		ID: r.ID, SessionID: r.SessionID, Portfolio: string(r.Portfolio), Ticker: r.Ticker,
		AccountType: string(r.AccountType), IsMigrationSell: r.IsMigrationSell, IsMigrationBuy: r.IsMigrationBuy,
	}
	if r.PlannedUnits.Valid {
		v, err := pgnum.ToDecimal(r.PlannedUnits)
		if err != nil {
			return nil, err
		}
		item.PlannedUnits = &v
	}
	if r.PlannedValuePln.Valid {
		v, err := pgnum.ToDecimal(r.PlannedValuePln)
		if err != nil {
			return nil, err
		}
		item.PlannedValuePln = &v
	}
	if r.ActualUnits.Valid {
		v, err := pgnum.ToDecimal(r.ActualUnits)
		if err != nil {
			return nil, err
		}
		item.ActualUnits = &v
	}
	if r.ActualValuePln.Valid {
		v, err := pgnum.ToDecimal(r.ActualValuePln)
		if err != nil {
			return nil, err
		}
		item.ActualValuePln = &v
	}
	if r.PricePlnAtPlan.Valid {
		v, err := pgnum.ToDecimal(r.PricePlnAtPlan)
		if err != nil {
			return nil, err
		}
		item.PricePlnAtPlan = &v
	}
	return item, nil
}

func toInsertItemParams(sessionID int64, item *SessionItem) sqlcgen.InsertPurchaseSessionItemParams {
	return sqlcgen.InsertPurchaseSessionItemParams{
		SessionID: sessionID, Portfolio: sqlcgen.PortfolioEnum(item.Portfolio), Ticker: item.Ticker,
		AccountType: sqlcgen.AccountTypeEnum(item.AccountType), PlannedUnits: pgnum.FromDecimalPtr(item.PlannedUnits),
		PlannedValuePln: pgnum.FromDecimalPtr(item.PlannedValuePln), ActualUnits: pgnum.FromDecimalPtr(item.ActualUnits),
		ActualValuePln: pgnum.FromDecimalPtr(item.ActualValuePln), PricePlnAtPlan: pgnum.FromDecimalPtr(item.PricePlnAtPlan),
		IsMigrationSell: item.IsMigrationSell, IsMigrationBuy: item.IsMigrationBuy,
	}
}
