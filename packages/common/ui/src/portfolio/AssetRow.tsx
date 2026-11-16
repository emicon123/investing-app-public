import { ChevronRightIcon, PencilIcon, Trash2Icon } from "lucide-react";
import { cn } from "../lib/utils.js";
import { AssetIcon } from "./AssetIcon.js";
import { StatusCell } from "./StatusCell.js";
import { TargetEditPopover } from "./TargetEditPopover.js";
import { TargetMeter } from "./TargetMeter.js";
import type { PositionRowModel, ProfitTone } from "./model/positionsModel.js";
import { COARSE_HIT_AREA, FOCUS_RING, ICON_BUTTON, rowGrid } from "./tableLayout.js";
import type { PortfolioAssetLike, PortfolioSubassetLike, PositionsTableSlots } from "./types.js";

/** One asset: icon + name, price / entry, value / share, TargetMeter, P/L, rebalancing status. */

export const PROFIT_TONE: Record<ProfitTone, string> = {
  gain: "text-gain-text",
  loss: "text-loss-text",
  flat: "text-text-2",
  none: "text-text-caption",
};

const CAPTION = "text-xs whitespace-nowrap text-text-caption";
const NUMERIC_CELL = "flex flex-col items-end gap-px text-right";

export interface AssetRowProps<TAsset extends PortfolioAssetLike<TSub>, TSub extends PortfolioSubassetLike> {
  row: PositionRowModel<TAsset>;
  editMode: boolean;
  expanded: boolean;
  onToggleExpand: (assetId: number) => void;
  /** Space-separated ids of the rendered sub-rows (for `aria-controls`). */
  subRowIds: string;
  slots: PositionsTableSlots<TAsset, TSub>;
}

export function AssetRow<TAsset extends PortfolioAssetLike<TSub>, TSub extends PortfolioSubassetLike>({
  row,
  editMode,
  expanded,
  onToggleExpand,
  subRowIds,
  slots,
}: AssetRowProps<TAsset, TSub>) {
  const { asset } = row;
  const { onSellClick, onEditClick, onDeleteClick, targetEdit } = slots;

  return (
    <div
      role="row"
      className={cn(
        rowGrid(editMode),
        "group/row relative min-h-[58px] border-t border-hairline-soft py-1.5 pr-5 pl-[46px] transition-colors hover:bg-white/[0.025] motion-reduce:transition-none",
      )}
    >
      <div role="cell" className="flex min-w-0 items-center gap-3">
        {asset.subassets.length > 0 && (
          <button
            type="button"
            aria-expanded={expanded}
            aria-controls={expanded ? subRowIds : undefined}
            aria-label={`Pokaż instrumenty: ${row.name}`}
            onClick={() => onToggleExpand(row.id)}
            className={cn(ICON_BUTTON, "absolute left-3 size-6", COARSE_HIT_AREA, FOCUS_RING)}
          >
            <ChevronRightIcon
              aria-hidden="true"
              className={cn("size-4 transition-transform motion-reduce:transition-none", expanded && "rotate-90")}
            />
          </button>
        )}
        <AssetIcon ticker={row.iconTicker} assetClass={row.assetClass} />
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-semibold">{row.name}</span>
          <span className={CAPTION}>{row.caption}</span>
        </div>
      </div>

      <div role="cell" className={NUMERIC_CELL} title={row.priceTitle}>
        <span>{row.price}</span>
        <span className={CAPTION}>{row.entry}</span>
      </div>

      <div role="cell" className={NUMERIC_CELL}>
        <span className="font-medium">{row.value}</span>
        <span className={CAPTION}>{row.shareLabel}</span>
      </div>

      <div role="cell" className="min-w-0">
        <TargetMeter
          geometry={row.meter}
          labels={row.meterLabels}
          targetSlot={<TargetEditPopover asset={asset} {...targetEdit} />}
        />
      </div>

      <div role="cell" className={NUMERIC_CELL}>
        <span className={cn("font-medium", PROFIT_TONE[row.profit.tone])}>{row.profit.profitLabel}</span>
        <span className={CAPTION}>{row.profit.plLabel}</span>
      </div>

      <div role="cell">
        <StatusCell
          status={row.status}
          figure={row.figure}
          assetName={row.name}
          onSell={onSellClick ? () => onSellClick(asset) : undefined}
        />
      </div>

      {editMode && (
        <div role="cell" className="flex items-center justify-end gap-1">
          {onEditClick && (
            <button
              type="button"
              aria-label={`Edytuj instrument ${row.name}`}
              onClick={() => onEditClick(asset)}
              className={cn(ICON_BUTTON, COARSE_HIT_AREA, FOCUS_RING)}
            >
              <PencilIcon className="size-3.5" aria-hidden="true" />
            </button>
          )}
          {onDeleteClick && (
            <button
              type="button"
              aria-label={`Usuń aktywo ${row.name}`}
              onClick={() => onDeleteClick(asset)}
              className={cn(ICON_BUTTON, "text-loss-text hover:bg-loss/10 hover:text-loss-text", COARSE_HIT_AREA, FOCUS_RING)}
            >
              <Trash2Icon className="size-3.5" aria-hidden="true" />
            </button>
          )}
        </div>
      )}
    </div>
  );
}
