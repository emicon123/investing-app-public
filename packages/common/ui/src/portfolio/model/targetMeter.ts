import type { RebalanceStatus } from "./rebalanceStatus.js";
import { formatShare } from "./portfolioFormat.js";

/**
 * TargetMeter geometry (task 170 rule 6, owner-reviewed; design doc §5.1).
 *
 * - per-row scale `s = max(current, effective, 0.01) × 1.18`; the base target never stretches it
 * - neutral fill up to `min(current, effective)`; past the marker the excess is coral (SELL),
 *   amber (OVER) or neutral; short of the marker a blue "brakuje do celu" gap (UNDER) or a faint one
 * - solid marker = effective target; dashed tick = base target, only when base > 0, base ≤ s and
 *   |base − effective| > 0.05
 * - no effective target: a plain fill to `current`, nothing else
 *
 * Every position is an unrounded percentage of the bar's width (0–100).
 */

export type MeterExcessTone = "sell" | "over" | "neutral";
export type MeterGapTone = "under" | "faint";

export interface MeterSegment<TTone> {
  leftPct: number;
  widthPct: number;
  tone: TTone;
}

export interface TargetMeterGeometry {
  scale: number;
  fillPct: number;
  excess: MeterSegment<MeterExcessTone> | null;
  gap: MeterSegment<MeterGapTone> | null;
  markerPct: number | null;
  baseTickPct: number | null;
}

export interface TargetMeterInput {
  currentPct: number;
  effectivePct: number | null;
  basePct: number | null;
  status: RebalanceStatus;
}

const SCALE_HEADROOM = 1.18;
const MIN_SCALE_PCT = 0.01;
const BASE_TICK_MIN_DIFF = 0.05;

function excessTone(status: RebalanceStatus): MeterExcessTone {
  if (status === "SELL") return "sell";
  if (status === "OVER") return "over";
  return "neutral";
}

export function computeTargetMeter({ currentPct, effectivePct, basePct, status }: TargetMeterInput): TargetMeterGeometry {
  const current = Math.max(currentPct, 0);
  const scale = Math.max(current, effectivePct ?? 0, MIN_SCALE_PCT) * SCALE_HEADROOM;
  const at = (x: number): number => Math.min(Math.max(x / scale, 0), 1) * 100;

  if (effectivePct == null) {
    return { scale, fillPct: at(current), excess: null, gap: null, markerPct: null, baseTickPct: null };
  }

  const showBase =
    basePct != null && basePct > 0 && basePct <= scale && Math.abs(basePct - effectivePct) > BASE_TICK_MIN_DIFF;

  return {
    scale,
    fillPct: at(Math.min(current, effectivePct)),
    excess:
      current > effectivePct
        ? { leftPct: at(effectivePct), widthPct: at(current - effectivePct), tone: excessTone(status) }
        : null,
    gap:
      current < effectivePct
        ? { leftPct: at(current), widthPct: at(effectivePct - current), tone: status === "UNDER" ? "under" : "faint" }
        : null,
    markerPct: at(effectivePct),
    baseTickPct: showBase ? at(basePct) : null,
  };
}

export interface TargetMeterLabels {
  /** Above the bar, left: "25,4%". */
  current: string;
  /** Above the bar, right: "cel 13,9%" ("cel —" without a target). */
  target: string;
  /** The cell's tooltip; the only place the base target is named. */
  title: string;
}

export function targetMeterLabels(currentPct: number, effectivePct: number | null, basePct: number | null): TargetMeterLabels {
  const current = formatShare(currentPct);
  if (effectivePct == null) {
    return { current, target: "cel —", title: `Udział ${current}, brak celu` };
  }
  const effective = formatShare(effectivePct);
  const title =
    basePct != null && Math.abs(basePct - effectivePct) > BASE_TICK_MIN_DIFF
      ? `Udział ${current}, cel efektywny ${effective}, cel bazowy ${formatShare(basePct)}`
      : `Udział ${current}, cel ${effective}`;
  return { current, target: `cel ${effective}`, title };
}
