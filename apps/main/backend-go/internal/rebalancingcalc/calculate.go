package rebalancingcalc

import (
	"context"

	"github.com/shopspring/decimal"

	"investing-app/backend-go/internal/settings"
)

var cashBufferPct = decimal.RequireFromString("0.05")

// Calculate is the cluster's top-level orchestrator (ported from the Java
// RebalancingService.calculate, ADR-094). Read-only; must be called with the caller's own tx-scoped
// Queries value (see architect-output §4). cashOverride is MAIN-only (the request's cashOnHandPln);
// nil for RETIREMENT, or when MAIN should use the live cash_positions snapshot instead of an
// override.
func Calculate(ctx context.Context, q Queries, prices PriceSource, portfolio string, cashOverride *decimal.Decimal) (Result, error) {
	computation, err := computeValues(ctx, q, prices, portfolio)
	if err != nil {
		return Result{}, err
	}
	// computeValues already fetched these; reuse them instead of fetching a second time.
	fxRates := computation.fxRates
	pricePlnByTicker := computation.pricePlnByTicker

	totalPortfolioValuePln := decimal.Zero
	for _, v := range computation.valuePln {
		totalPortfolioValuePln = totalPortfolioValuePln.Add(v)
	}

	var (
		cashOnHandPln     decimal.Decimal
		deployableCashPln decimal.Decimal
		buyList           []BuyListItem
	)

	if portfolio == PortfolioMAIN {
		var grossCashPln decimal.Decimal
		if cashOverride != nil {
			grossCashPln = *cashOverride
		} else {
			grossCashPln, err = loadCashSnapshotTotal(ctx, q)
			if err != nil {
				return Result{}, err
			}
		}
		retirementContribution, err := settings.Get(ctx, q, settings.DefRetirementMonthlyContributionPln, settings.RetirementMonthlyContributionFallback, false)
		if err != nil {
			return Result{}, err
		}
		cashOnHandPln = grossCashPln.Sub(retirementContribution)
		if cashOnHandPln.Sign() < 0 {
			cashOnHandPln = decimal.Zero
		}
		buffer := totalPortfolioValuePln.Mul(cashBufferPct)
		deployableCashPln = cashOnHandPln.Sub(buffer).DivRound(decimal.NewFromInt(1).Add(cashBufferPct), 10)

		targetOverrides, err := LoadMainTargetOverrides(ctx, q)
		if err != nil {
			return Result{}, err
		}
		buyList, err = calculateBuyList(deployableCashPln, computation.tradablePositions, computation.valuePln,
			fxRates, pricePlnByTicker, totalPortfolioValuePln, portfolioConfig{targetOverrides: targetOverrides, convertChfNative: true})
		if err != nil {
			return Result{}, err
		}
	} else {
		monthlyContribution, err := settings.Get(ctx, q, settings.DefRetirementMonthlyContributionPln, settings.RetirementMonthlyContributionFallback, false)
		if err != nil {
			return Result{}, err
		}
		cashOnHandPln = monthlyContribution
		deployableCashPln = monthlyContribution
		buyList, err = calculateBuyList(monthlyContribution, computation.tradablePositions, computation.valuePln,
			fxRates, pricePlnByTicker, totalPortfolioValuePln, portfolioConfig{convertChfNative: false})
		if err != nil {
			return Result{}, err
		}
	}

	return Result{
		Portfolio:              portfolio,
		CashOnHandPln:          cashOnHandPln,
		DeployableCashPln:      deployableCashPln.Round(4),
		TotalPortfolioValuePln: totalPortfolioValuePln.Round(4),
		BuyList:                buyList,
	}, nil
}
