// Package deposit serves deposits, account-cap usage and XIRR (ported from the Java
// DepositService/AccountCapUsageService/XirrService, ADR-094 / doc 13 §7). It owns `deposits`
// (shared InsertDeposit, architect-output §2) and `account_cap_adjustment` exclusively.
package deposit

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"investing-app/backend-go/db/sqlcgen"
	"investing-app/backend-go/internal/settings"
	"investing-app/common-go/httpx"
	"investing-app/common-go/pgnum"
)

var hundred = decimal.NewFromInt(100)

// CapUsageQueries is the narrow slice this file's ComputeCapUsage needs — a subset of the
// package's own Queries interface, exported so internal/purchasesession (which needs to call
// ComputeCapUsage during createSession — diagram doc 150 §3's "cross-package call into
// internal/deposit, read-only, safe regardless of which package owns deposits writes") can
// declare it in its own narrow Queries interface via Go's structural typing, without importing
// this package's full write surface.
type CapUsageQueries interface {
	settings.SettingReader
	SumDepositAmountByAccountTypeAndYear(ctx context.Context, arg sqlcgen.SumDepositAmountByAccountTypeAndYearParams) (pgtype.Numeric, error)
	GetAccountCapAdjustment(ctx context.Context, arg sqlcgen.GetAccountCapAdjustmentParams) (sqlcgen.AccountCapAdjustment, error)
}

// CapUsage is the direct port of AccountCapUsageService.CapUsage.
type CapUsage struct {
	TotalDepositedPln decimal.Decimal
	AnnualCapPln      decimal.Decimal
	RemainingPln      decimal.Decimal // UNCLAMPED — caller applies .max(0) if the figure feeds a budget
	UtilizationPct    decimal.Decimal
}

// ComputeCapUsage is the direct port of AccountCapUsageService.computeCapUsage — shared by
// DepositService.getAccountCapUsage and PurchaseSessionService.computeLimits (now
// internal/purchasesession's own createSession, via this exported function).
func ComputeCapUsage(ctx context.Context, q CapUsageQueries, accountType string, year int) (CapUsage, error) {
	annualCap, err := annualCapFor(ctx, q, accountType)
	if err != nil {
		return CapUsage{}, err
	}
	sumRow, err := q.SumDepositAmountByAccountTypeAndYear(ctx, sqlcgen.SumDepositAmountByAccountTypeAndYearParams{
		AccountType: sqlcgen.AccountTypeEnum(accountType), Year: int32(year),
	})
	if err != nil {
		return CapUsage{}, fmt.Errorf("deposit: sum deposits for %s/%d: %w", accountType, year, err)
	}
	totalDeposited, err := pgnum.ToDecimal(sumRow)
	if err != nil {
		return CapUsage{}, fmt.Errorf("deposit: decode deposit sum: %w", err)
	}

	adjustment, err := currentAdjustmentPln(ctx, q, accountType, year)
	if err != nil {
		return CapUsage{}, err
	}

	effectiveUsed := totalDeposited.Sub(adjustment)
	remaining := annualCap.Sub(effectiveUsed)
	utilization := decimal.Zero
	if annualCap.Sign() != 0 {
		utilization = effectiveUsed.Mul(hundred).DivRound(annualCap, 4)
	}
	return CapUsage{TotalDepositedPln: totalDeposited, AnnualCapPln: annualCap, RemainingPln: remaining, UtilizationPct: utilization}, nil
}

func annualCapFor(ctx context.Context, q CapUsageQueries, accountType string) (decimal.Decimal, error) {
	switch accountType {
	case "IKE":
		return settings.Get(ctx, q, settings.DefIkeAnnualLimitPln, decimal.Zero, true)
	case "IKZE":
		return settings.Get(ctx, q, settings.DefIkzeAnnualLimitPln, decimal.Zero, true)
	default:
		return decimal.Zero, fmt.Errorf("deposit: REGULAR has no statutory annual cap; ComputeCapUsage only supports IKE/IKZE")
	}
}

func currentAdjustmentPln(ctx context.Context, q CapUsageQueries, accountType string, year int) (decimal.Decimal, error) {
	row, err := q.GetAccountCapAdjustment(ctx, sqlcgen.GetAccountCapAdjustmentParams{
		AccountType: sqlcgen.AccountTypeEnum(accountType), Year: int32(year),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, nil
		}
		return decimal.Zero, fmt.Errorf("deposit: read account_cap_adjustment for %s/%d: %w", accountType, year, err)
	}
	return pgnum.ToDecimal(row.AdjustmentPln)
}

// UpsertAdjustment is the direct port of AccountCapUsageService.upsertAdjustment.
func UpsertAdjustment(ctx context.Context, q Queries, accountType string, year int, remainingPln decimal.Decimal) (CapUsage, error) {
	if accountType != "IKE" && accountType != "IKZE" {
		return CapUsage{}, httpx.NewValidationError("accountType", fmt.Sprintf("accountType must be IKE or IKZE (REGULAR has no statutory annual cap), got %s", accountType))
	}
	annualCap, err := annualCapFor(ctx, q, accountType)
	if err != nil {
		return CapUsage{}, err
	}
	sumRow, err := q.SumDepositAmountByAccountTypeAndYear(ctx, sqlcgen.SumDepositAmountByAccountTypeAndYearParams{
		AccountType: sqlcgen.AccountTypeEnum(accountType), Year: int32(year),
	})
	if err != nil {
		return CapUsage{}, fmt.Errorf("deposit: sum deposits for %s/%d: %w", accountType, year, err)
	}
	totalDeposited, err := pgnum.ToDecimal(sumRow)
	if err != nil {
		return CapUsage{}, fmt.Errorf("deposit: decode deposit sum: %w", err)
	}
	newAdjustment := remainingPln.Sub(annualCap).Add(totalDeposited)

	if _, err := q.UpsertAccountCapAdjustment(ctx, sqlcgen.UpsertAccountCapAdjustmentParams{
		AccountType: sqlcgen.AccountTypeEnum(accountType), Year: int32(year), AdjustmentPln: pgnum.FromDecimal(newAdjustment),
	}); err != nil {
		return CapUsage{}, fmt.Errorf("deposit: upsert account_cap_adjustment: %w", err)
	}

	effectiveUsed := annualCap.Sub(remainingPln)
	utilization := decimal.Zero
	if annualCap.Sign() != 0 {
		utilization = effectiveUsed.Mul(hundred).DivRound(annualCap, 4)
	}
	return CapUsage{TotalDepositedPln: totalDeposited, AnnualCapPln: annualCap, RemainingPln: remainingPln, UtilizationPct: utilization}, nil
}
