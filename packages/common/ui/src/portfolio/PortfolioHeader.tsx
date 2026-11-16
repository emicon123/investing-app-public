import type { ReactNode } from "react";
import { ChartLineIcon, ChartPieIcon, PencilIcon, PlusIcon } from "lucide-react";
import { cn } from "../lib/utils.js";
import { formatPricesAsOf } from "./model/portfolioFormat.js";
import { FOCUS_RING } from "./tableLayout.js";
import type { PortfolioHeaderActions } from "./types.js";

/**
 * Page header: title + book tag, "Ceny z …" and the app's status slot (main: the bot badge), and
 * the actions. "Odśwież ceny" lives only in the app top bar (`AppLayout`), not here. There is deliberately no "Rebalansuj" button: selling stays
 * manual, row by row (ADR-045 / ADR-048).
 */

const ACTION_BUTTON = cn(
  "inline-flex min-h-10 items-center gap-2 rounded-[9px] border px-3.5 text-[13px] font-medium transition-colors motion-reduce:transition-none pointer-coarse:min-h-11",
  FOCUS_RING,
);
const OUTLINE = "border-hairline-strong text-text-soft hover:bg-white/6 hover:text-text-1";

export interface PortfolioHeaderProps {
  title: string;
  bookTag?: string;
  pricesAsOf: string | null;
  status?: ReactNode;
  actions: PortfolioHeaderActions;
}

export function PortfolioHeader({ title, bookTag, pricesAsOf, status, actions }: PortfolioHeaderProps) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-x-6 gap-y-4">
      <div className="flex min-w-0 flex-col gap-2.5">
        <div className="flex items-center gap-2.5">
          <h1 className="text-[26px] leading-[1.1] font-semibold tracking-[-0.015em]">{title}</h1>
          {bookTag && (
            <span className="rounded-md border border-hairline-strong px-2 py-0.5 text-xs font-medium text-text-2">
              {bookTag}
            </span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 text-[13px] text-text-2">
          {pricesAsOf && <span>{formatPricesAsOf(pricesAsOf)}</span>}
          {status}
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        <button type="button" onClick={actions.onOpenHistory} className={cn(ACTION_BUTTON, OUTLINE)}>
          <ChartLineIcon aria-hidden="true" className="size-4" />
          Historia
        </button>
        <button type="button" onClick={actions.onOpenTreemap} className={cn(ACTION_BUTTON, OUTLINE)}>
          <ChartPieIcon aria-hidden="true" className="size-4" />
          Mapa alokacji
        </button>
        <button
          type="button"
          aria-pressed={actions.editMode}
          onClick={actions.onToggleEdit}
          className={cn(ACTION_BUTTON, actions.editMode ? "border-primary/50 bg-accent text-text-1" : OUTLINE)}
        >
          <PencilIcon aria-hidden="true" className="size-4" />
          {actions.editMode ? "Wyjdź z edycji" : "Edytuj portfel"}
        </button>
        <button
          type="button"
          onClick={actions.onAdd}
          className={cn(ACTION_BUTTON, "border-primary bg-primary font-semibold text-primary-foreground hover:bg-primary/90")}
        >
          <PlusIcon aria-hidden="true" className="size-4" />
          Dodaj pozycję
        </button>
      </div>
    </header>
  );
}
