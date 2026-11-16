import { ChevronRightIcon } from "lucide-react";
import { cn } from "../lib/utils.js";
import { PROFIT_TONE } from "./AssetRow.js";
import { STATUS_DOT, STATUS_LABEL_TONE } from "./StatusCell.js";
import { TargetMeter } from "./TargetMeter.js";
import type { VisibleGroup } from "./model/positionsModel.js";
import { rowGrid } from "./tableLayout.js";

/**
 * An exposure group's header: the whole row is one toggle button (`::after` covers it and carries
 * the focus outline). Value, share, meter and P/L always describe the WHOLE group, even while a
 * filter hides some of its rows. Sticky below the column header on wide screens.
 */

export interface GroupHeaderRowProps<TAsset> {
  view: VisibleGroup<TAsset>;
  open: boolean;
  onToggle: () => void;
  /** id of the rows this header expands. */
  rowsId: string;
  editMode: boolean;
}

export function GroupHeaderRow<TAsset>({ view, open, onToggle, rowsId, editMode }: GroupHeaderRowProps<TAsset>) {
  const { group, countLabel } = view;
  const { summary } = group;

  return (
    <div
      role="row"
      className={cn(
        rowGrid(editMode),
        "sticky top-[calc(var(--app-sticky-top,0px)+41px)] z-[2] min-h-[62px] border-t border-white/6 bg-surface-group px-5 transition-colors hover:bg-surface-group-hover motion-reduce:transition-none",
        "max-[1160px]:top-[41px]",
      )}
    >
      <div role="rowheader" className="flex min-w-0 items-center gap-3">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={rowsId}
          onClick={onToggle}
          className={cn(
            "inline-flex items-center gap-2.5 text-[15px] font-semibold text-text-1",
            "after:absolute after:inset-0 after:content-[''] focus-visible:outline-none",
            "focus-visible:after:outline-2 focus-visible:after:-outline-offset-2 focus-visible:after:outline-ring",
          )}
        >
          <ChevronRightIcon
            aria-hidden="true"
            className={cn("size-4 text-text-2 transition-transform motion-reduce:transition-none", open && "rotate-90")}
          />
          {/* Literal group colour from EXPOSURE_GROUPS (data, not a token). */}
          <span aria-hidden="true" className="size-2.5 rounded-[3px]" style={{ background: group.color }} />
          {group.label}
        </button>
        <span className="text-xs whitespace-nowrap text-text-caption">{countLabel}</span>
      </div>

      <div role="cell" />

      <div role="cell" className="flex flex-col items-end gap-0.5 text-right">
        <span className="text-[15px] font-semibold">{group.value}</span>
        <span className="text-xs text-text-caption">{group.shareLabel}</span>
      </div>

      <div role="cell" className="min-w-0">
        <TargetMeter geometry={group.meter} labels={group.meterLabels} size="group" />
      </div>

      <div role="cell" className="flex flex-col items-end gap-0.5 text-right">
        <span className={cn("text-[15px] font-semibold", PROFIT_TONE[group.profit.tone])}>{group.profit.profitLabel}</span>
        <span className="text-xs text-text-caption">{group.profit.plLabel}</span>
      </div>

      <div role="cell" className="flex items-center gap-2 text-[13px]">
        <span aria-hidden="true" className={cn("size-[7px] shrink-0 rounded-full", STATUS_DOT[summary.status])} />
        <span className={STATUS_LABEL_TONE[summary.status]}>{summary.label}</span>
        {summary.countLabel && <span className="text-xs text-text-caption">{summary.countLabel}</span>}
      </div>

      {editMode && <div role="cell" />}
    </div>
  );
}
