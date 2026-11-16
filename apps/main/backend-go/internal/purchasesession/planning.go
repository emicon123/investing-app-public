package purchasesession

import (
	"fmt"
	"sort"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/position"
	"investing-app/backend-go/internal/rebalancingcalc"
)

// This file is ported from the Java PurchasePlanningService (ADR-094) — pure functions over plain
// Go values, ZERO DB dependency (confirmed: no sqlcgen/pgx/pgtype import anywhere in this file).
// Converts pre-computed rebalancing buy lists into concrete purchase session items, applying
// IKE/IKZE account priority and the generalized crypto-donor waterfall (ADR-061/065) for crypto
// assets that have an IKE/IKZE-eligible wrapper subasset.
//
// Task 156 (Asset / Subasset model) re-expression — the MATH is unchanged, only the
// vocabulary: the old parent/companion string match (companion_parent_ticker) is now group
// membership by the asset_id FK, so a buy-list item is an Asset's primary subasset, a
// "companion" is a wrapper subasset of the same asset, and donors are subassets of assets
// whose asset_class is CRYPTO. See docs/architecture/diagrams/156-asset-subasset-model.md §7
// (row-by-row table) — group->asset is a bijection, so cardinalities and values feeding the
// waterfall are identical (asserted by TestComputePlan_AssetBijection_* below).
//
// Manual-sell-only invariant (architect-output §3 item 3): the ONLY sells this file can ever
// emit are the IKE/IKZE crypto-donor migration sells in applyCryptoSellStep/findBestCryptoDonor
// below — an account/tax-capacity optimization for a BUY, never a portfolio-rebalancing sell.
// Every donor sell is immediately paired with a compensating buy restoring the exact same crypto
// exposure elsewhere (reconcileOwedCompensatingBuys), and the donor set is filtered to
// subassets of asset_class=CRYPTO assets holding a live IKE/IKZE balance (seedSellableCryptoPln),
// never a rebalancing-target-driven selection. See TestFindBestCryptoDonor_* below for the
// explicit reviewer-checkpoint tests this invariant demands.

// sellKey identifies one (portfolio, account, subasset ticker) balance/row — the same key shape
// the companion-keyed maps had; tickers are unchanged by the task-156 migration, so the merge/
// netting behavior and the psi_session_portfolio_ticker_account_uq collision profile are
// identical.
type sellKey struct {
	portfolio, accountType, subassetTicker string
}

// owedKey identifies the asset owed a compensating buy (donor waterfall reconciliation):
// (portfolio, asset id) replaces the old (portfolio, parent ticker) — one entry per group
// before and after.
type owedKey struct {
	portfolio string
	assetID   int64
}

type groupKey struct {
	portfolio, accountType string
}

// ComputePlanResult is the direct port of PurchasePlanningService.ComputePlanResult.
type ComputePlanResult struct {
	Items                 []*SessionItem
	NewDepositPlnByBucket map[groupKey]decimal.Decimal
}

// planState is ComputePlan's run-wide state, shared by reference by both portfolio passes
// (RETIREMENT then MAIN) exactly as the Java service threaded it (ADR-061 §1): the read-only
// book and prices, plus the mutable statutory room left, the decrementing donor
// balances, the booked subasset rows, the owed compensating buys, the new-deposit figures and the
// planned items. The donor maps are keyed by portfolio, so sharing them across the passes has no
// cross-portfolio effect beyond ADR-065's fallback tier.
type planState struct {
	allPositions     []rebalancingcalc.PositionRow
	pricePlnByTicker map[string]decimal.Decimal

	ikeLeft, ikzeLeft      decimal.Decimal
	sellableCryptoPln      map[sellKey]decimal.Decimal
	subassetRowsByKey      map[sellKey]*SessionItem
	owedCompensatingBuyPln map[owedKey]decimal.Decimal
	newDepositPlnByBucket  map[groupKey]decimal.Decimal
	result                 []*SessionItem
}

// buyTarget is one buy-list item resolved against the book: the item's own subasset in its
// portfolio (the asset's primary, the default buy destination and the donor-exclusion
// recipient), its price, and where its IKE/IKZE leg lands.
type buyTarget struct {
	item            rebalancingcalc.BuyListItem
	portfolio       string
	recipient       *rebalancingcalc.PositionRow
	pricePln        decimal.Decimal
	wrapper         *rebalancingcalc.PositionRow
	useWrapper      bool
	ikeIkzeEligible bool
}

// ComputePlan is the direct port of PurchasePlanningService.computePlan. The buy lists are
// ticker-oriented by contract (one item per asset, ticker = its primary); donors are ranked by the
// fixed cryptoDonorOrder, not by any buy-list figure.
func ComputePlan(
	retirementList, mainList []rebalancingcalc.BuyListItem,
	ikeRemaining, ikzeRemaining decimal.Decimal,
	pricePlnByTicker map[string]decimal.Decimal,
	allPositions []rebalancingcalc.PositionRow,
) (ComputePlanResult, error) {
	st := &planState{
		allPositions: allPositions, pricePlnByTicker: pricePlnByTicker,
		ikeLeft: ikeRemaining, ikzeLeft: ikzeRemaining,
		sellableCryptoPln:      map[sellKey]decimal.Decimal{},
		subassetRowsByKey:      map[sellKey]*SessionItem{},
		owedCompensatingBuyPln: map[owedKey]decimal.Decimal{},
		newDepositPlnByBucket:  map[groupKey]decimal.Decimal{},
	}

	seedSellableCryptoPln(allPositions, pricePlnByTicker, st.sellableCryptoPln)

	if len(retirementList) > 0 {
		if err := processPortfolio(st, retirementList, "RETIREMENT"); err != nil {
			return ComputePlanResult{}, err
		}
	}
	if len(mainList) > 0 {
		if err := processPortfolio(st, mainList, "MAIN"); err != nil {
			return ComputePlanResult{}, err
		}
	}

	if err := reconcileOwedCompensatingBuys(st.owedCompensatingBuyPln, allPositions, pricePlnByTicker, &st.result); err != nil {
		return ComputePlanResult{}, err
	}

	return ComputePlanResult{Items: st.result, NewDepositPlnByBucket: st.newDepositPlnByBucket}, nil
}

// isCryptoAsset is the donor/substitution predicate (design §5/§7): the ASSET's own
// asset_class, backfilled from the old parent row's asset_class — load-bearing for XRP
// (CRYPTO-class, EXPERIMENTS-exposure), and stable across a primary change.
func isCryptoAsset(p rebalancingcalc.PositionRow) bool { return p.Asset.Class == "CRYPTO" }

// seedSellableCryptoPln is the direct port of PurchasePlanningService.seedSellableCryptoPln.
// Only subassets of CRYPTO-class assets are ever sellable donors: their live IKE/IKZE unit
// buckets x the same price map. (Old rows: companions whose parent was CRYPTO — the same rows;
// a primary with no IKE/IKZE units seeds a zero balance that findCryptoDonorCandidate skips.)
func seedSellableCryptoPln(allPositions []rebalancingcalc.PositionRow, pricePlnByTicker map[string]decimal.Decimal, sellableCryptoPln map[sellKey]decimal.Decimal) {
	for _, p := range allPositions {
		if !isCryptoAsset(p) || p.YahooTicker == nil {
			continue
		}
		subassetPricePln, ok := pricePlnByTicker[*p.YahooTicker]
		if !ok || subassetPricePln.Sign() <= 0 {
			continue // soft-skip: an unpriced donor just can't be sold this session
		}
		sellableCryptoPln[sellKey{p.Portfolio, position.AccountTypeIKE, p.Ticker}] = p.UnitsIke.Mul(subassetPricePln)
		sellableCryptoPln[sellKey{p.Portfolio, position.AccountTypeIKZE, p.Ticker}] = p.UnitsIkze.Mul(subassetPricePln)
	}
}

// processPortfolio is the direct port of PurchasePlanningService.processPortfolio: every item of
// one portfolio's buy list, in order, through the five-step waterfall.
func processPortfolio(st *planState, buyList []rebalancingcalc.BuyListItem, portfolio string) error {
	for _, item := range buyList {
		target, ok := resolveBuyTarget(st, item, portfolio)
		if !ok {
			continue
		}
		if err := planBuyItem(st, target); err != nil {
			return err
		}
	}
	return nil
}

// resolveBuyTarget resolves item within THIS portfolio (BTC-USD exists in both). ok is false for
// an item the plan skips: no position for its ticker, or no positive price.
//
// IKE/IKZE leg: a CRYPTO asset with an IKE/IKZE-eligible wrapper subasset is bought via that
// wrapper (substitution, §8.1 steps 1-4); every other asset via its primary, and it is
// IKE/IKZE-eligible iff that leg's subasset is on XTB (eligibility derived from exchange, no flag
// — evaluated on the leg's actual subasset).
func resolveBuyTarget(st *planState, item rebalancingcalc.BuyListItem, portfolio string) (buyTarget, bool) {
	pos := findPositionByTicker(item.Ticker, portfolio, st.allPositions)
	if pos == nil {
		return buyTarget{}, false // Position not found for ticker — skipped
	}
	pricePln, ok := priceFor(*pos, st.pricePlnByTicker)
	if !ok || pricePln.Sign() <= 0 {
		return buyTarget{}, false
	}
	wrapper := findWrapperSubasset(*pos, st.allPositions)
	useWrapper := wrapper != nil && isCryptoAsset(*pos)
	ikeIkzeEligible := pos.Exchange == "XTB"
	if useWrapper {
		ikeIkzeEligible = wrapper.Exchange == "XTB"
	}
	return buyTarget{item: item, portfolio: portfolio, recipient: pos, pricePln: pricePln,
		wrapper: wrapper, useWrapper: useWrapper, ikeIkzeEligible: ikeIkzeEligible}, true
}

// planBuyItem runs one item through the waterfall (ADR-060/061/065): IKE new-deposit, IKZE
// new-deposit, then — only once BOTH statutory limits are exhausted — an IKE and an IKZE crypto
// donor sell, and the true remainder as a plain REGULAR buy.
func planBuyItem(st *planState, t buyTarget) error {
	remaining := t.item.AllocatedPln
	var ikeRowRef, ikzeRowRef *SessionItem
	var err error

	// Step 1: IKE new-deposit.
	if t.ikeIkzeEligible && st.ikeLeft.Sign() > 0 {
		ikeFillPln := decimal.Min(st.ikeLeft, remaining)
		st.ikeLeft = st.ikeLeft.Sub(ikeFillPln)
		remaining = remaining.Sub(ikeFillPln)
		if ikeRowRef, err = bookNewDeposit(st, t, position.AccountTypeIKE, ikeFillPln); err != nil {
			return err
		}
	}

	// Step 2: IKZE new-deposit.
	if t.ikeIkzeEligible && remaining.Sign() > 0 && st.ikzeLeft.Sign() > 0 {
		ikzeFillPln := decimal.Min(st.ikzeLeft, remaining)
		st.ikzeLeft = st.ikzeLeft.Sub(ikzeFillPln)
		remaining = remaining.Sub(ikzeFillPln)
		if ikzeRowRef, err = bookNewDeposit(st, t, position.AccountTypeIKZE, ikzeFillPln); err != nil {
			return err
		}
	}

	bothLimitsExhausted := st.ikeLeft.Sign() <= 0 && st.ikzeLeft.Sign() <= 0

	// Step 3: IKE crypto-sell.
	if t.ikeIkzeEligible && remaining.Sign() > 0 && bothLimitsExhausted {
		if remaining, err = applyCryptoSellStep(st, t, position.AccountTypeIKE, ikeRowRef, remaining); err != nil {
			return err
		}
	}

	// Step 4: IKZE crypto-sell.
	if t.ikeIkzeEligible && remaining.Sign() > 0 && bothLimitsExhausted {
		if remaining, err = applyCryptoSellStep(st, t, position.AccountTypeIKZE, ikzeRowRef, remaining); err != nil {
			return err
		}
	}

	// Step 5: true remainder -> plain REGULAR purchase of the item's own ticker (the
	// asset's primary subasset — the default buy destination).
	if remaining.Sign() > 0 {
		st.result = append(st.result, plainItem(t.item.Ticker, t.portfolio, position.AccountTypeREGULAR, remaining, unitsForFill(t.item, remaining, t.pricePln), t.pricePln))
	}
	return nil
}

// bookNewDeposit records fillPln of new statutory money for (portfolio, accountType) and books
// the buy: on the wrapper's subasset row (merged per key) or as a new item on the item's own
// ticker. Returns the booked row.
func bookNewDeposit(st *planState, t buyTarget, accountType string, fillPln decimal.Decimal) (*SessionItem, error) {
	bucket := groupKey{t.portfolio, accountType}
	st.newDepositPlnByBucket[bucket] = st.newDepositPlnByBucket[bucket].Add(fillPln)
	if t.useWrapper {
		wrapperPricePln, err := resolveSubassetPricePln(*t.wrapper, st.pricePlnByTicker)
		if err != nil {
			return nil, err
		}
		return st.upsertSubassetRow(sellKey{t.portfolio, accountType, t.wrapper.Ticker}, accountType, fillPln, wrapperPricePln), nil
	}
	row := ikeIkzeBuyItem(t.item, t.portfolio, accountType, t.useWrapper, t.wrapper, t.pricePln, st.pricePlnByTicker, fillPln)
	st.result = append(st.result, row)
	return row, nil
}

// applyCryptoSellStep is the direct port of PurchasePlanningService.applyCryptoSellStep: sells
// up to remaining from the best IKE/IKZE crypto donor (findBestCryptoDonor), books the freed room
// as this item's buy in the same account (wrapper row, the item's existing row from the
// new-deposit step, or a new row) and owes the donor asset a compensating buy. Returns what is
// left of remaining.
func applyCryptoSellStep(st *planState, t buyTarget, accountType string, rowRef *SessionItem, remaining decimal.Decimal) (decimal.Decimal, error) {
	donor := findBestCryptoDonor(t.portfolio, accountType, st.allPositions, st.sellableCryptoPln, t.recipient.Asset.ID)
	if donor == nil {
		return remaining, nil
	}

	donorKey := sellKey{donor.Portfolio, accountType, donor.Ticker}
	donorBalance := st.sellableCryptoPln[donorKey]
	sellPln := decimal.Min(remaining, donorBalance)
	st.sellableCryptoPln[donorKey] = donorBalance.Sub(sellPln)

	donorPricePln := decimal.Zero
	if donor.YahooTicker != nil {
		donorPricePln = st.pricePlnByTicker[*donor.YahooTicker]
	}
	st.upsertSubassetRow(donorKey, accountType, sellPln.Neg(), donorPricePln)

	switch {
	case t.useWrapper:
		wrapperPricePln, err := resolveSubassetPricePln(*t.wrapper, st.pricePlnByTicker)
		if err != nil {
			return decimal.Decimal{}, err
		}
		st.upsertSubassetRow(sellKey{t.portfolio, accountType, t.wrapper.Ticker}, accountType, sellPln, wrapperPricePln)
	case rowRef != nil:
		mergeSellPlnIntoRow(rowRef, sellPln)
	default:
		st.result = append(st.result, ikeIkzeBuyItem(t.item, t.portfolio, accountType, t.useWrapper, t.wrapper, t.pricePln, st.pricePlnByTicker, sellPln))
	}

	if donor.Asset.ID != 0 {
		k := owedKey{donor.Portfolio, donor.Asset.ID}
		st.owedCompensatingBuyPln[k] = st.owedCompensatingBuyPln[k].Add(sellPln)
	}

	return remaining.Sub(sellPln), nil
}

// upsertSubassetRow is the direct port of PurchasePlanningService.upsertCompanionRow, keyed
// (portfolio, account, subasset ticker) — the same key shape as before.
func (st *planState) upsertSubassetRow(key sellKey, accountType string, signedDeltaPln, tickerPricePln decimal.Decimal) *SessionItem {
	existing, hasExisting := st.subassetRowsByKey[key]
	priorSigned := decimal.Zero
	if hasExisting {
		if existing.IsMigrationSell {
			priorSigned = valueOrZero(existing.PlannedValuePln).Neg()
		} else {
			priorSigned = valueOrZero(existing.PlannedValuePln)
		}
		removeByIdentity(&st.result, existing)
		delete(st.subassetRowsByKey, key)
	}

	newSigned := priorSigned.Add(signedDeltaPln)
	if newSigned.IsZero() {
		return nil // fully netted — nothing to book
	}

	absPln := newSigned.Abs()
	var units decimal.Decimal
	if tickerPricePln.Sign() > 0 {
		units = absPln.DivRound(tickerPricePln, 8)
	}
	var row *SessionItem
	if newSigned.Sign() < 0 {
		row = migrationSellItem(key.subassetTicker, key.portfolio, accountType, absPln, units.Neg(), tickerPricePln)
	} else {
		row = plainItem(key.subassetTicker, key.portfolio, accountType, absPln, units, tickerPricePln)
	}
	st.subassetRowsByKey[key] = row
	st.result = append(st.result, row)
	return row
}

func removeByIdentity(result *[]*SessionItem, target *SessionItem) {
	out := (*result)[:0]
	for _, r := range *result {
		if r != target {
			out = append(out, r)
		}
	}
	*result = out
}

// mergeSellPlnIntoRow is the direct port of PurchasePlanningService.mergeSellPlnIntoRow.
func mergeSellPlnIntoRow(row *SessionItem, sellPln decimal.Decimal) {
	newTotal := valueOrZero(row.PlannedValuePln).Add(sellPln)
	row.PlannedValuePln = &newTotal
	if row.PricePlnAtPlan != nil && row.PricePlnAtPlan.Sign() > 0 {
		units := newTotal.DivRound(*row.PricePlnAtPlan, 8)
		row.PlannedUnits = &units
	}
}

// reconcileOwedCompensatingBuys is the direct port of
// PurchasePlanningService.reconcileOwedCompensatingBuys: every donor sell is paired with a
// REGULAR compensating buy of the donor ASSET's primary subasset (old: the parent row — the
// same ticker under the migration's primary = old main row rule), merged into an existing
// REGULAR row for it when there is one.
func reconcileOwedCompensatingBuys(owedCompensatingBuyPln map[owedKey]decimal.Decimal, allPositions []rebalancingcalc.PositionRow, pricePlnByTicker map[string]decimal.Decimal, result *[]*SessionItem) error {
	// Deterministic order (portfolio, primary ticker): the original map iteration order was
	// arbitrary, so any fixed order is behavior-compatible — and it makes the planned item order
	// stable across runs.
	type owedEntry struct {
		key     owedKey
		owed    decimal.Decimal
		primary *rebalancingcalc.PositionRow
	}
	var entries []owedEntry
	for key, owed := range owedCompensatingBuyPln {
		if owed.Sign() <= 0 {
			continue
		}
		primary := findPrimarySubasset(key.assetID, key.portfolio, allPositions)
		if primary == nil {
			continue
		}
		entries = append(entries, owedEntry{key, owed, primary})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].key.portfolio != entries[j].key.portfolio {
			return entries[i].key.portfolio < entries[j].key.portfolio
		}
		return entries[i].primary.Ticker < entries[j].primary.Ticker
	})
	for _, e := range entries {
		key, owed, primary := e.key, e.owed, e.primary
		var existing *SessionItem
		for _, r := range *result {
			if r.Portfolio == key.portfolio && r.Ticker == primary.Ticker && r.AccountType == position.AccountTypeREGULAR && !r.IsMigrationSell {
				existing = r
				break
			}
		}
		if existing != nil {
			newTotal := valueOrZero(existing.PlannedValuePln).Add(owed)
			existing.PlannedValuePln = &newTotal
			if existing.PricePlnAtPlan != nil && existing.PricePlnAtPlan.Sign() > 0 {
				units := newTotal.DivRound(*existing.PricePlnAtPlan, 8)
				existing.PlannedUnits = &units
			}
			existing.IsMigrationBuy = true
			continue
		}

		if primary.YahooTicker == nil {
			continue
		}
		price, ok := pricePlnByTicker[*primary.YahooTicker]
		if !ok || price.Sign() <= 0 {
			continue
		}
		units := owed.DivRound(price, 8)
		*result = append(*result, migrationBuyItem(primary.Ticker, key.portfolio, position.AccountTypeREGULAR, owed, units, price))
	}
	return nil
}

// subassetsOfSameAsset returns the rows sharing p's asset (same asset id, same portfolio) in
// allPositions order (ORDER BY ticker). A row not joined to any asset (id 0 — never on real
// data, the FK forbids it) belongs to no group.
func subassetsOfSameAsset(p rebalancingcalc.PositionRow, allPositions []rebalancingcalc.PositionRow) []*rebalancingcalc.PositionRow {
	if p.Asset.ID == 0 {
		return nil
	}
	var out []*rebalancingcalc.PositionRow
	for i := range allPositions {
		q := &allPositions[i]
		if q.Asset.ID == p.Asset.ID && q.Portfolio == p.Portfolio {
			out = append(out, q)
		}
	}
	return out
}

// findWrapperSubasset is the re-expression of findCompanion (§7 row 1): the IKE/IKZE-eligible
// wrapper of p's asset — the first (ticker order) XTB-listed subasset that is not itself the
// raw coin (row asset_class != CRYPTO: an ETN/ETF wrapping it). "Wrapper" is deliberately not
// "non-primary": after the owner re-heads a crypto asset onto its ETN (set-primary), the
// primary IS the wrapper, so IKE/IKZE buys and the donor waterfall keep landing on the ETN
// instead of the raw XTB coin row. For every current group (primary = the old main row, the
// wrappers are XTB ETF rows) this returns the very row findCompanion returned.
func findWrapperSubasset(p rebalancingcalc.PositionRow, allPositions []rebalancingcalc.PositionRow) *rebalancingcalc.PositionRow {
	for _, q := range subassetsOfSameAsset(p, allPositions) {
		if q.Exchange == "XTB" && q.AssetClass != "CRYPTO" {
			return q
		}
	}
	return nil
}

// findPrimarySubasset returns the asset's primary subasset (the default buy destination).
func findPrimarySubasset(assetID int64, portfolio string, allPositions []rebalancingcalc.PositionRow) *rebalancingcalc.PositionRow {
	if assetID == 0 {
		return nil
	}
	for i := range allPositions {
		p := &allPositions[i]
		if p.Asset.ID == assetID && p.Portfolio == portfolio && p.IsPrimary {
			return p
		}
	}
	return nil
}

func findPositionByTicker(ticker, portfolio string, allPositions []rebalancingcalc.PositionRow) *rebalancingcalc.PositionRow {
	if ticker == "" {
		return nil
	}
	for i := range allPositions {
		p := &allPositions[i]
		if p.Ticker == ticker && p.Portfolio == portfolio {
			return p
		}
	}
	return nil
}

// cryptoDonorOrder is the owner's fixed donor order (2026-11-09, task 192, ADR-061 §3): when a
// crypto wrapper must leave IKE/IKZE to free room, BTC's ETN goes first, then ETH, SOL, XRP. A
// wrapper not listed (for example ETNVIRLINK.PL) ranks after all of them, alphabetically by
// ticker. No target, deviation or price input takes part, so the choice never reacts to a position
// being over target (ADR-045 manual-sell-only).
var cryptoDonorOrder = []string{"PL.ETNVIRBTCP", "PL.ETNVIRETH", "PL.ETNVIRSOL", "PL.ETNVIRXRP"}

// cryptoDonorRank is the ticker's place in cryptoDonorOrder; unlisted tickers share the rank after
// the last listed one.
func cryptoDonorRank(ticker string) int {
	for i, listed := range cryptoDonorOrder {
		if ticker == listed {
			return i
		}
	}
	return len(cryptoDonorOrder)
}

// findBestCryptoDonor is the port of PurchasePlanningService.findBestCryptoDonor with the fixed
// cryptoDonorOrder — two-tier, self-portfolio-first-then-cross-portfolio-fallback (ADR-061/065).
// This is one of the two functions architect-output §3 flags for the closest manual-sell-only
// scrutiny: donor selection never triggers from a position being over-target alone — see
// TestFindBestCryptoDonor_* below. excludeAssetID is the recipient asset (tier-1 self-donation
// exclusion = the recipient asset's own subassets).
func findBestCryptoDonor(portfolio, accountType string, allPositions []rebalancingcalc.PositionRow, sellableCryptoPln map[sellKey]decimal.Decimal, excludeAssetID int64) *rebalancingcalc.PositionRow {
	tier1 := findCryptoDonorCandidate(portfolio, accountType, allPositions, sellableCryptoPln, excludeAssetID)
	if tier1 != nil {
		return tier1
	}
	// ADR-065 §1/§3: cross-portfolio fallback tier — no self-donation exclusion (0 disables it, see
	// findCryptoDonorCandidate).
	otherPortfolio := "RETIREMENT"
	if portfolio == "RETIREMENT" {
		otherPortfolio = "MAIN"
	}
	return findCryptoDonorCandidate(otherPortfolio, accountType, allPositions, sellableCryptoPln, 0)
}

// findCryptoDonorCandidate picks the best donor in one portfolio/account tier: among the
// subassets with a sellable balance in the account, the first in cryptoDonorOrder (unlisted ones
// by ticker). excludeAssetID == 0 means "no self-donation exclusion" (tier 2's rule, ADR-065 §3) —
// safe because a real asset id is never 0.
func findCryptoDonorCandidate(candidatePortfolio, accountType string, allPositions []rebalancingcalc.PositionRow, sellableCryptoPln map[sellKey]decimal.Decimal, excludeAssetID int64) *rebalancingcalc.PositionRow {
	var best *rebalancingcalc.PositionRow
	for i := range allPositions {
		p := &allPositions[i]
		if p.Portfolio != candidatePortfolio {
			continue
		}
		bal, ok := sellableCryptoPln[sellKey{candidatePortfolio, accountType, p.Ticker}]
		if !ok || bal.Sign() <= 0 {
			continue
		}
		if excludeAssetID != 0 && p.Asset.ID == excludeAssetID {
			continue
		}
		if best == nil || donorBefore(p.Ticker, best.Ticker) {
			best = p
		}
	}
	return best
}

// donorBefore reports whether ticker a is a better donor than b: lower rank first, equal ranks
// (unlisted cryptos) alphabetically.
func donorBefore(a, b string) bool {
	if ra, rb := cryptoDonorRank(a), cryptoDonorRank(b); ra != rb {
		return ra < rb
	}
	return a < b
}

// ── Item builders — direct ports of PurchasePlanningService's private builders ──

func ikeIkzeBuyItem(item rebalancingcalc.BuyListItem, portfolio, accountType string, useWrapper bool, wrapper *rebalancingcalc.PositionRow, primaryPricePln decimal.Decimal, pricePlnByTicker map[string]decimal.Decimal, fillPln decimal.Decimal) *SessionItem {
	if !useWrapper {
		return plainItem(item.Ticker, portfolio, accountType, fillPln, unitsForFill(item, fillPln, primaryPricePln), primaryPricePln)
	}
	wrapperPricePln, err := resolveSubassetPricePln(*wrapper, pricePlnByTicker)
	if err != nil {
		// resolveSubassetPricePln's error is only reachable when useWrapper is true and the
		// wrapper's price is genuinely unavailable — a fail-loud condition. Callers already
		// resolved a wrapper price earlier in processPortfolio's own steps before reaching a plain
		// (non-donor) buy, so this is defensive; zero units is the safe degradation if it's ever
		// hit.
		return plainItem(wrapper.Ticker, portfolio, accountType, fillPln, decimal.Zero, decimal.Zero)
	}
	units := fillPln.DivRound(wrapperPricePln, 8)
	return plainItem(wrapper.Ticker, portfolio, accountType, fillPln, units, wrapperPricePln)
}

func unitsForFill(item rebalancingcalc.BuyListItem, fillPln, pricePln decimal.Decimal) decimal.Decimal {
	if fillPln.Equal(item.AllocatedPln) {
		return item.UnitsToBuy
	}
	if pricePln.Sign() <= 0 {
		return decimal.Zero
	}
	return fillPln.DivRound(pricePln, 8)
}

func resolveSubassetPricePln(sub rebalancingcalc.PositionRow, pricePlnByTicker map[string]decimal.Decimal) (decimal.Decimal, error) {
	if sub.YahooTicker == nil {
		return decimal.Zero, fmt.Errorf("purchasesession: no price available for subasset %s", sub.Ticker)
	}
	price, ok := pricePlnByTicker[*sub.YahooTicker]
	if !ok || price.Sign() <= 0 {
		return decimal.Zero, fmt.Errorf("purchasesession: no price available for subasset %s (yahoo: %s)", sub.Ticker, *sub.YahooTicker)
	}
	return price, nil
}

func plainItem(ticker, portfolio, accountType string, valuePln, units, pricePln decimal.Decimal) *SessionItem {
	return &SessionItem{
		Portfolio: portfolio, Ticker: ticker, AccountType: accountType,
		PlannedValuePln: &valuePln, PlannedUnits: &units, PricePlnAtPlan: &pricePln,
		IsMigrationSell: false, IsMigrationBuy: false,
	}
}

func migrationSellItem(ticker, portfolio, accountType string, valuePln, unitsNegative, pricePln decimal.Decimal) *SessionItem {
	abs := unitsNegative.Abs()
	return &SessionItem{
		Portfolio: portfolio, Ticker: ticker, AccountType: accountType,
		PlannedValuePln: &valuePln, PlannedUnits: &abs, ActualUnits: &unitsNegative, PricePlnAtPlan: &pricePln,
		IsMigrationSell: true, IsMigrationBuy: false,
	}
}

func migrationBuyItem(ticker, portfolio, accountType string, valuePln, units, pricePln decimal.Decimal) *SessionItem {
	return &SessionItem{
		Portfolio: portfolio, Ticker: ticker, AccountType: accountType,
		PlannedValuePln: &valuePln, PlannedUnits: &units, PricePlnAtPlan: &pricePln,
		IsMigrationSell: false, IsMigrationBuy: true,
	}
}

func priceFor(pos rebalancingcalc.PositionRow, pricePlnByTicker map[string]decimal.Decimal) (decimal.Decimal, bool) {
	if pos.YahooTicker == nil {
		return decimal.Zero, false
	}
	v, ok := pricePlnByTicker[*pos.YahooTicker]
	return v, ok
}

func valueOrZero(v *decimal.Decimal) decimal.Decimal {
	if v == nil {
		return decimal.Zero
	}
	return *v
}
