import { cn } from "../lib/utils.js";
import { STATUS_LABEL, type RebalanceStatus } from "./model/rebalanceStatus.js";
import { FOCUS_RING } from "./tableLayout.js";

/**
 * "Rebalansowanie": dot + label + figure, no filled pill (task 170 rule 7). Coloured text only for
 * "Sprzedaj". A SELL row becomes a button only when the container passes `onSell` (main: edit mode
 * only; portfolioB/portfolioC: always) — it opens the existing manual transfer dialog, nothing sells on its
 * own (ADR-045 / ADR-048).
 */

export const STATUS_DOT: Record<RebalanceStatus, string> = {
  SELL: "bg-loss shadow-[0_0_0_3px_rgba(244,63,94,0.18)]",
  OVER: "bg-status-over shadow-[0_0_0_3px_rgba(232,176,75,0.16)]",
  UNDER: "bg-status-under shadow-[0_0_0_3px_rgba(106,168,240,0.16)]",
  OK: "bg-status-ok shadow-[0_0_0_3px_rgba(125,139,161,0.14)]",
};

export const STATUS_LABEL_TONE: Record<RebalanceStatus, string> = {
  SELL: "text-loss-text",
  OVER: "text-text-1",
  UNDER: "text-text-1",
  OK: "text-text-2",
};

export interface StatusCellProps {
  status: RebalanceStatus;
  figure: string;
  assetName: string;
  onSell?: () => void;
}

function StatusContent({ status, figure }: Pick<StatusCellProps, "status" | "figure">) {
  return (
    <>
      <span aria-hidden="true" className={cn("size-[7px] shrink-0 rounded-full", STATUS_DOT[status])} />
      <span className={STATUS_LABEL_TONE[status]}>{STATUS_LABEL[status]}</span>
      <span className={cn("text-xs whitespace-nowrap", status === "SELL" ? "text-text-soft" : "text-text-caption")}>
        {figure}
      </span>
    </>
  );
}

export function StatusCell({ status, figure, assetName, onSell }: StatusCellProps) {
  if (status === "SELL" && onSell) {
    return (
      <button
        type="button"
        onClick={onSell}
        aria-label={`Sprzedaj ${assetName}: ${figure}`}
        className={cn(
          "-mx-1.5 inline-flex min-h-8 items-center gap-2 rounded-md px-1.5 text-[13px] transition-colors hover:bg-loss/10 motion-reduce:transition-none pointer-coarse:min-h-11",
          FOCUS_RING,
        )}
      >
        <StatusContent status={status} figure={figure} />
      </button>
    );
  }
  return (
    <div className="flex items-center gap-2 text-[13px]">
      <StatusContent status={status} figure={figure} />
    </div>
  );
}
