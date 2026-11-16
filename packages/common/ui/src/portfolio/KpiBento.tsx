import { useMemo, useState, type ReactNode } from "react";
import type { PortfolioKpis } from "@common/app-core";
import { cn } from "../lib/utils.js";
import { Skeleton } from "../skeleton.js";
import { buildSparkline } from "./model/sparklineModel.js";
import { formatSignedPct, formatSignedZlNumber, formatYearsMonths, formatZlNumber } from "./model/portfolioFormat.js";
import { Sparkline } from "./Sparkline.js";

/**
 * The four KPI cards (task 170 §2, design doc §3.3): valuation with a 7/30-day sparkline, profit
 * (valuation minus deposits), invested capital and XIRR. Every number comes from
 * `GET /deposits/xirr` and `GET /history`; nothing is computed beyond differences.
 */

const RANGES = [7, 30] as const;
type RangeDays = (typeof RANGES)[number];

const CARD = "flex min-h-[140px] flex-col gap-3 rounded-[14px] border border-hairline bg-surface-card px-5 py-[18px]";
const CARD_TITLE = "text-[13px] text-text-2";
const CARD_NOTE = "text-[13px] text-text-caption";
const FIGURE_MAIN = "text-[28px] leading-[1.05] font-medium tracking-[-0.02em] whitespace-nowrap";
const FIGURE_HERO = "text-[38px] leading-none font-medium tracking-[-0.025em] whitespace-nowrap";

function Figure({ value, unit, hero = false }: { value: string; unit?: string; hero?: boolean }) {
  return (
    <span className={hero ? FIGURE_HERO : FIGURE_MAIN}>
      {value}
      {unit && (
        <span className={cn("ml-1.5 font-normal tracking-normal text-text-2", hero ? "text-xl" : "text-base")}>
          {unit}
        </span>
      )}
    </span>
  );
}

function RangeToggle({ range, onChange }: { range: RangeDays; onChange: (r: RangeDays) => void }) {
  return (
    <div role="group" aria-label="Okres zmiany wyceny" className="inline-flex gap-0.5 rounded-lg border border-hairline p-0.5">
      {RANGES.map((days) => (
        <button
          key={days}
          type="button"
          aria-pressed={range === days}
          onClick={() => onChange(days)}
          className={cn(
            "min-h-[26px] rounded-md px-2.5 text-xs font-medium transition-colors motion-reduce:transition-none pointer-coarse:min-h-11",
            "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
            range === days ? "bg-white/10 text-text-1" : "text-text-2 hover:text-text-1",
          )}
        >
          {days} dni
        </button>
      ))}
    </div>
  );
}

function ValuationCard({ kpis }: { kpis: PortfolioKpis }) {
  const [range, setRange] = useState<RangeDays>(30);
  const live = kpis.xirr?.currentValuePln ?? null;
  const spark = useMemo(() => buildSparkline(kpis.history, live, range, new Date()), [kpis.history, live, range]);

  return (
    <div className={cn(CARD, "pt-3.5")}>
      <div className="flex items-center justify-between gap-3">
        <span className={CARD_TITLE}>Wycena całkowita</span>
        <RangeToggle range={range} onChange={setRange} />
      </div>
      <div className="flex flex-1 flex-wrap items-end justify-between gap-x-5 gap-y-3">
        <div className="flex min-w-0 flex-col gap-2">
          {live != null ? <Figure value={formatZlNumber(live)} unit="zł" hero /> : <Figure value="—" hero />}
          {spark && (
            <span className="text-[13px] whitespace-nowrap text-text-2">
              <span className={cn("font-medium", spark.tone === "loss" ? "text-loss-text" : "text-gain-text")}>
                {spark.changeLabel}
              </span>{" "}
              {spark.rangeLabel}
            </span>
          )}
        </div>
        {spark && <Sparkline model={spark} />}
      </div>
    </div>
  );
}

function ReturnPill({ fraction }: { fraction: number }) {
  return (
    <span
      className={cn(
        "rounded-full border px-2 py-0.5 text-xs font-semibold",
        fraction < 0
          ? "border-loss/25 bg-loss/10 text-loss-text"
          : "border-gain/25 bg-gain/10 text-gain-text",
      )}
    >
      {formatSignedPct(fraction, 2)}
    </span>
  );
}

function StatCard({ title, figure, note }: { title: string; figure: ReactNode; note: string | null }) {
  return (
    <div className={cn(CARD, "justify-between")}>
      <span className={CARD_TITLE}>{title}</span>
      {figure}
      {note && <span className={CARD_NOTE}>{note}</span>}
    </div>
  );
}

function KpiSkeleton() {
  return (
    <div className={cn(CARD, "justify-between")}>
      <Skeleton className="h-3.5 w-28" />
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-3.5 w-32" />
    </div>
  );
}

export function KpiBento({ kpis }: { kpis: PortfolioKpis }) {
  const gridClass =
    "grid grid-cols-[minmax(0,1.6fr)_repeat(3,minmax(0,1fr))] gap-3 max-[1100px]:grid-cols-2 max-[620px]:grid-cols-1";

  if (kpis.isPending) {
    return (
      <section role="status" aria-label="Ładowanie podsumowania portfela" className={gridClass}>
        {Array.from({ length: 4 }, (_, i) => (
          <KpiSkeleton key={i} />
        ))}
      </section>
    );
  }

  const x = kpis.xirr;
  const invested = x?.totalInvestedPln ?? null;
  const current = x?.currentValuePln ?? null;
  const profit = current != null && invested != null ? current - invested : null;
  const simpleReturn = x?.simpleReturnPct ?? null;
  const years = x?.yearsElapsed ?? null;
  const xirrPct = x?.xirrPct ?? null;

  return (
    <section aria-label="Podsumowanie portfela" className={gridClass}>
      <ValuationCard kpis={kpis} />
      <StatCard
        title="Zysk / strata"
        figure={
          <div className="flex flex-wrap items-center gap-2.5">
            <Figure value={profit != null ? formatSignedZlNumber(profit) : "—"} unit={profit != null ? "zł" : undefined} />
            {simpleReturn != null && <ReturnPill fraction={simpleReturn} />}
          </div>
        }
        note="Wycena minus wpłaty"
      />
      <StatCard
        title="Wpłacony kapitał"
        figure={<Figure value={invested != null ? formatZlNumber(invested) : "—"} unit={invested != null ? "zł" : undefined} />}
        note={years != null ? `Wpłaty z ${formatYearsMonths(years)}` : null}
      />
      <StatCard
        title="XIRR"
        figure={
          <div className="flex items-baseline gap-2">
            <Figure value={xirrPct != null ? formatSignedPct(xirrPct, 2) : "—"} />
            {xirrPct != null && <span className="text-[13px] text-text-2">rocznie</span>}
          </div>
        }
        note="Zwrot liczony z datami wpłat"
      />
    </section>
  );
}
