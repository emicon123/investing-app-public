import { useId } from "react";
import { cn } from "../lib/utils.js";
import type { RibbonDeltaTone, RibbonSegment } from "./model/positionsModel.js";

/**
 * "Alokacja według ekspozycji": the share bar ("Udział"), a thinner faded target bar ("Cel",
 * normalised so it always fills) and a legend per group with share, target and the p.p. delta.
 * Colour never carries identity alone: every segment has a `title` and the legend is the text
 * equivalent. Segment widths and group colours are data, hence inline styles.
 */

const DELTA_TONE: Record<RibbonDeltaTone, string> = {
  over: "text-status-over-text",
  under: "text-status-under-text",
  flat: "text-text-2",
};

const width = (pct: number): string => `${pct.toFixed(2)}%`;

export interface AllocationRibbonProps {
  segments: readonly RibbonSegment[];
  /** main MAIN under the macro gate: say that the targets are the effective ones. */
  hasEffectiveOverride: boolean;
}

export function AllocationRibbon({ segments, hasEffectiveOverride }: AllocationRibbonProps) {
  const headingId = useId();
  if (segments.length === 0) return null;

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3.5 px-0.5 pt-1 pb-1.5">
      <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2">
        <h2 id={headingId} className="text-[15px] font-semibold">
          Alokacja według ekspozycji
        </h2>
        {hasEffectiveOverride && (
          <span className="text-xs text-text-caption">
            Cele efektywne po korekcie reżimu makro; bazowe w tabeli poniżej
          </span>
        )}
      </div>

      <div className="grid grid-cols-[52px_minmax(0,1fr)] items-center gap-x-3 gap-y-2">
        <span className="text-xs text-text-2">Udział</span>
        <div className="flex h-3.5 gap-0.5 overflow-hidden rounded">
          {segments.map((s) => (
            <div
              key={s.exposure}
              title={s.title}
              className="min-w-0.5"
              style={{ width: width(s.currentWidthPct), background: s.color }}
            />
          ))}
        </div>
        <span className="text-xs text-text-2">Cel</span>
        <div aria-hidden="true" className="flex h-1.5 gap-0.5 overflow-hidden rounded-[3px] opacity-50">
          {segments.map((s) => (
            <div key={s.exposure} className="min-w-0.5" style={{ width: width(s.targetWidthPct), background: s.color }} />
          ))}
        </div>
      </div>

      <ul className="grid grid-cols-[repeat(auto-fit,minmax(190px,1fr))] gap-x-5 gap-y-2.5 pl-16 max-[620px]:pl-0">
        {segments.map((s) => (
          <li key={s.exposure} className="flex min-w-0 flex-col gap-[3px]">
            <span className="flex items-center gap-2 text-[13px] text-text-soft">
              <span aria-hidden="true" className="size-[9px] rounded-[2px]" style={{ background: s.color }} />
              {s.label}
            </span>
            <span className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-[13px]">
              <span className="text-base font-medium text-text-1">{s.currentLabel}</span>
              <span className="text-text-caption">{s.targetLabel}</span>
              <span className={cn("font-medium", DELTA_TONE[s.deltaTone])}>{s.delta}</span>
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
