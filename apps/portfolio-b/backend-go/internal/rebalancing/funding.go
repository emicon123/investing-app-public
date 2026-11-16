package rebalancing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	accountcashkernel "investing-app/common-go/accountcash"
	"investing-app/common-go/rebalancecalc"
	"investing-app/common-go/siblingcore"
	screbalancing "investing-app/common-go/siblingcore/rebalancing"
	"portfolio-b/backend-go/db/sqlcgen"
	"portfolio-b/backend-go/internal/accountcash"
	"portfolio-b/backend-go/internal/book"
	"portfolio-b/backend-go/internal/store"
)

// Funding is portfolioB's rebalancing funding policy (ADR-113, the one Strategy siblingcore leaves
// per app): Calculate deploys min(monthly_contribution_pln, Σ IKE/IKZE cash) and splits each buy
// over the four accounts by a cash waterfall; Confirm deducts what was paid from each IKE/IKZE
// account and records the monthly contribution as the deposit.
type Funding struct{}

// NewFunding returns portfolioB's funding policy.
func NewFunding() Funding { return Funding{} }

var _ screbalancing.Funding[*store.Queries] = Funding{}

// accounts is the waterfall order: IKE_TATA → IKE_MAMA → IKZE_TATA → IKZE_MAMA, each with its
// account_cash_snapshot key. INNE holds no cash and is never bought into by the waterfall.
var accounts = []struct {
	bucket siblingcore.Bucket
	key    string
}{
	{book.IkeTata, accountcash.KeyIkeTata}, {book.IkeMama, accountcash.KeyIkeMama},
	{book.IkzeTata, accountcash.KeyIkzeTata}, {book.IkzeMama, accountcash.KeyIkzeMama},
}

func cashService(q *store.Queries) *accountcashkernel.Service {
	return accountcash.NewService(accountcash.NewStore(q.SQL()))
}

// Budget: the deployable cash is the monthly contribution capped by the cash on the four
// accounts.
func (Funding) Budget(ctx context.Context, q *store.Queries, _ siblingcore.Portfolio, _ *decimal.Decimal) (screbalancing.Budget, error) {
	snap, err := cashService(q).GetCurrent(ctx, "")
	if err != nil {
		return screbalancing.Budget{}, err
	}
	total := decimal.Zero
	balances := make(map[string]decimal.Decimal, len(accounts))
	for _, a := range accounts {
		total = total.Add(snap.Balances[a.key])
		balances[string(a.bucket)] = snap.Balances[a.key]
	}
	contribution, err := monthlyContributionPln(ctx, q.SQL())
	if err != nil {
		return screbalancing.Budget{}, err
	}
	return screbalancing.Budget{
		DeployablePln: decimal.Min(contribution, total), CashOnHandPln: &total,
		Allocate: accountWaterfall, Balances: balances,
	}, nil
}

// Check: portfolioB's confirm has no request-level funding field.
func (Funding) Check(screbalancing.ConfirmRequest) error { return nil }

// Settle deducts what each IKE/IKZE account paid (INNE is never cash-funded) and returns the
// monthly contribution, recorded as this session's deposit.
func (Funding) Settle(ctx context.Context, q *store.Queries, _ screbalancing.ConfirmRequest, paid map[siblingcore.Bucket]decimal.Decimal) (decimal.Decimal, error) {
	deduct := make(map[string]decimal.Decimal, len(accounts))
	for _, a := range accounts {
		deduct[a.key] = paid[a.bucket]
	}
	if _, err := cashService(q).Deduct(ctx, "", deduct); err != nil {
		return decimal.Zero, fmt.Errorf("rebalancing: deduct account cash: %w", err)
	}
	return monthlyContributionPln(ctx, q.SQL())
}

// accountWaterfall is portfolioB's allocateToAccounts step: it fills IKE_TATA → IKE_MAMA →
// IKZE_TATA → IKZE_MAMA in that fixed order until each buy item's allocated PLN is exhausted or
// all account cash is spent (a negative balance counts as zero).
func accountWaterfall(buyList []rebalancecalc.BuyListItem, balances map[string]decimal.Decimal) []rebalancecalc.BuyListItem {
	remaining := make([]decimal.Decimal, len(accounts))
	for i, a := range accounts {
		remaining[i] = decimal.Max(balances[string(a.bucket)], decimal.Zero)
	}
	result := make([]rebalancecalc.BuyListItem, len(buyList))
	for idx, item := range buyList {
		needed := item.AllocatedPln
		var allocs []rebalancecalc.AccountAlloc
		for i, a := range accounts {
			if needed.Sign() <= 0 {
				break
			}
			if remaining[i].Sign() <= 0 {
				continue
			}
			take := decimal.Min(remaining[i], needed)
			remaining[i] = remaining[i].Sub(take)
			needed = needed.Sub(take)
			units := decimal.Zero
			if item.UnitsToBuy.Sign() > 0 && item.AllocatedPln.Sign() > 0 {
				units = item.UnitsToBuy.Mul(take).DivRound(item.AllocatedPln, 8)
			}
			allocs = append(allocs, rebalancecalc.AccountAlloc{AccountType: string(a.bucket), AllocPln: take.Round(4), UnitsToBuy: units.Round(8)})
		}
		item.AccountAllocations = allocs
		result[idx] = item
	}
	return result
}

const monthlyContributionKey = "monthly_contribution_pln"

// monthlyContributionFallback is used when the setting is absent or unparseable.
var monthlyContributionFallback = decimal.RequireFromString("4000")

func monthlyContributionPln(ctx context.Context, q *sqlcgen.Queries) (decimal.Decimal, error) {
	row, err := q.GetSettingByKey(ctx, monthlyContributionKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return monthlyContributionFallback, nil
		}
		return decimal.Zero, err
	}
	v, err := decimal.NewFromString(row.Value)
	if err != nil {
		slog.Warn("rebalancing: invalid monthly_contribution_pln value, using default", "value", row.Value)
		return monthlyContributionFallback, nil
	}
	return v, nil
}
