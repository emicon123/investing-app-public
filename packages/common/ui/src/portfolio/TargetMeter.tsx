import type { ReactNode } from "react";
import { cn } from "../lib/utils.js";
import type { MeterExcessTone, MeterGapTone, TargetMeterGeometry, TargetMeterLabels } from "./model/targetMeter.js";

/**
 * "Udział wobec celu": the current share against the effective target in one bar (task 170 rule
 * 6; geometry from `computeTargetMeter`). The base target is named only in the tooltip (`title`).
 * Replaces the old "Cel %" and "Aktual. %" columns.
 */

const EXCESS_TONE: Record<MeterExcessTone, string> = {
  sell: "bg-loss",
  over: "bg-status-over",
  neutral: "bg-meter-fill",
};

const GAP_TONE: Record<MeterGapTone, string> = {
  under: "bg-status-under-fill",
  faint: "bg-meter-faint",
};

const SIZE = {
  row: {
    root: "gap-1.5",
    current: "font-medium",
    track: "h-1.5 rounded-[3px] bg-meter-track",
    fill: "rounded-l-[3px]",
    marker: "-top-1 h-3.5",
    base: "-top-[3px] h-3",
  },
  group: {
    root: "gap-[7px]",
    current: "font-semibold",
    track: "h-2 rounded-[4px] bg-white/7",
    fill: "rounded-l-[4px]",
    marker: "-top-1 h-4",
    base: "-top-[3px] h-3.5",
  },
} as const;

/** Positions are model-computed percentages of the bar, so they can only be inline styles. */
const pct = (v: number): string => `${v.toFixed(2)}%`;

export interface TargetMeterProps {
  geometry: TargetMeterGeometry;
  labels: TargetMeterLabels;
  size?: "row" | "group";
  /** Rendered after "cel X%", e.g. the target-edit pencil. */
  targetSlot?: ReactNode;
}

export function TargetMeter({ geometry, labels, size = "row", targetSlot }: TargetMeterProps) {
  const s = SIZE[size];
  const { fillPct, excess, gap, markerPct, baseTickPct } = geometry;
  return (
    <div className={cn("flex min-w-0 flex-col pr-2", s.root)} title={labels.title}>
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className={cn("text-text-1", s.current)}>{labels.current}</span>
        <span className="flex items-center gap-1 whitespace-nowrap text-text-2">
          {labels.target}
          {targetSlot}
        </span>
      </div>
      <div aria-hidden="true" data-testid="target-meter-bar" className={cn("relative", s.track)}>
        <span className={cn("absolute inset-y-0 left-0 bg-meter-fill", s.fill)} style={{ width: pct(fillPct) }} />
        {excess && (
          <span
            data-tone={excess.tone}
            className={cn("absolute inset-y-0", EXCESS_TONE[excess.tone])}
            style={{ left: pct(excess.leftPct), width: pct(excess.widthPct) }}
          />
        )}
        {gap && (
          <span
            data-tone={gap.tone}
            className={cn("absolute inset-y-0", GAP_TONE[gap.tone])}
            style={{ left: pct(gap.leftPct), width: pct(gap.widthPct) }}
          />
        )}
        {markerPct != null && (
          <span
            data-testid="target-meter-marker"
            className={cn("absolute -ml-px w-0.5 rounded-[1px] bg-meter-marker", s.marker)}
            style={{ left: pct(markerPct) }}
          />
        )}
        {baseTickPct != null && (
          <span
            data-testid="target-meter-base"
            className={cn("absolute w-0 border-l border-dashed border-meter-base", s.base)}
            style={{ left: pct(baseTickPct) }}
          />
        )}
      </div>
    </div>
  );
}
