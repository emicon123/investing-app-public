import type { PortfolioAssetLike } from "../types.js";
import { formatKiloZl, formatPp, formatZl } from "./portfolioFormat.js";

/**
 * Rebalancing status of one position (task 170 rule 7, design doc §5). Display only: nothing here
 * is sent to the backend and nothing sells on its own (manual-sell-only, ADR-045 / ADR-048).
 */
export type RebalanceStatus = "SELL" | "OVER" | "UNDER" | "OK";

/** Thresholds unchanged (ADR-048 §7.5): > +50 % SELL, > +5 % OVER, < −5 % UNDER; null → OK. */
export function deviationStatus(deviationPct: number | null | undefined): RebalanceStatus {
  if (deviationPct == null) return "OK";
  if (deviationPct > 0.5) return "SELL";
  if (deviationPct > 0.05) return "OVER";
  if (deviationPct < -0.05) return "UNDER";
  return "OK";
}

export const STATUS_LABEL: Readonly<Record<RebalanceStatus, string>> = {
  SELL: "Sprzedaj",
  OVER: "Za dużo",
  UNDER: "Za mało",
  OK: "OK",
};

/** Severity order for a group's summary: the most severe row status wins. */
export const STATUS_SEVERITY: readonly RebalanceStatus[] = ["SELL", "OVER", "UNDER", "OK"];

/**
 * The target the status is computed against: main's macro-blended target when the server sends
 * one, else the raw target (always the raw one in portfolioB/portfolioC, design doc §3.2).
 */
export function effectiveTargetPct(asset: Pick<PortfolioAssetLike, "computed" | "targetAllocationPct">): number | null {
  return asset.computed?.effectiveTargetAllocationPct ?? asset.targetAllocationPct;
}

/**
 * PLN to sell to bring a position back to exactly 1.0× its effective target (ADR-048 §7.5),
 * floored at 0. The ONE trim formula: the SELL figure in the table and the transfer dialog's
 * pre-fill (`PositionTransferDialog.computeSuggestedSellUnits`) both call it.
 */
export function computeTrimExcessPln(valuePln: number, effectivePct: number, totalPln: number): number {
  return Math.max(0, valuePln - (effectivePct / 100) * totalPln);
}

export interface RebalanceFigureInput {
  status: RebalanceStatus;
  valuePln: number;
  currentPct: number;
  effectivePct: number | null;
  totalPln: number;
}

/**
 * The "Rebalansowanie" figure. It never shows a percentage (it was read as a return):
 * SELL → the amount to trim ("~14,4 tys. zł", or "całość, 402 zł" for a zero target);
 * otherwise the deviation in percentage points ("−14,8 p.p."); no target → "brak celu".
 */
export function formatRebalanceFigure({
  status,
  valuePln,
  currentPct,
  effectivePct,
  totalPln,
}: RebalanceFigureInput): string {
  if (effectivePct == null) return "brak celu";
  if (status === "SELL") {
    if (effectivePct === 0) return `całość, ${formatZl(valuePln)}`;
    return `~${formatKiloZl(computeTrimExcessPln(valuePln, effectivePct, totalPln))}`;
  }
  return formatPp(currentPct - effectivePct);
}

/** A group's meter status: never SELL (selling is a per-asset decision). */
export function groupMeterStatus(currentPct: number, effectivePct: number): Exclude<RebalanceStatus, "SELL"> {
  const deviation = effectivePct > 0 ? currentPct / effectivePct - 1 : 1;
  if (deviation > 0.05) return "OVER";
  if (deviation < -0.05) return "UNDER";
  return "OK";
}
