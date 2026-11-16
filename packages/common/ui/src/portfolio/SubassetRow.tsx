import { CrownIcon, PencilIcon, Trash2Icon } from "lucide-react";
import { formatCurrency } from "@common/app-core";
import { cn } from "../lib/utils.js";
import { formatPricePln, formatShare, formatZl } from "./model/portfolioFormat.js";
import { COARSE_HIT_AREA, FOCUS_RING, ICON_BUTTON, rowGrid } from "./tableLayout.js";
import type { PortfolioSubassetLike, PositionsTableSlots } from "./types.js";

/**
 * One concrete holding under an expanded asset (task 156 Asset/Subasset): ticker + "Główna", name,
 * price (native price below for a non-PLN instrument), value and its share of the asset, and the
 * app's account split (`renderAccountCell`). Edit mode adds "Ustaw jako główną" (non-primary rows),
 * main's per-row edit pencil, and delete.
 */

const CAPTION = "text-xs whitespace-nowrap text-text-caption";

export interface SubassetRowProps<TSub extends PortfolioSubassetLike> {
  id: string;
  sub: TSub;
  assetClass: string;
  /** The asset's value, for "X% aktywa". */
  assetValuePln: number;
  editMode: boolean;
  slots: Pick<
    PositionsTableSlots<unknown, TSub>,
    "onEditSubClick" | "onDeleteSubClick" | "onSetPrimaryClick" | "settingPrimaryId" | "renderAccountCell"
  >;
}

export function SubassetRow<TSub extends PortfolioSubassetLike>({
  id,
  sub,
  assetClass,
  assetValuePln,
  editMode,
  slots,
}: SubassetRowProps<TSub>) {
  const { onEditSubClick, onDeleteSubClick, onSetPrimaryClick, settingPrimaryId, renderAccountCell } = slots;
  const valuePln = sub.valuePln ?? null;
  const priceNative = sub.priceCurrency !== "PLN" ? (sub.priceNative ?? null) : null;

  return (
    <div
      id={id}
      role="row"
      className={cn(rowGrid(editMode), "min-h-12 border-t border-hairline-soft bg-white/[0.015] py-1.5 pr-5 pl-[78px] text-[13px]")}
    >
      <div role="cell" className="flex min-w-0 flex-col">
        <span className="flex items-center gap-1.5">
          <span className={cn("font-mono text-xs", sub.isPrimary ? "font-medium text-text-1" : "text-text-2")}>
            {sub.ticker}
          </span>
          {sub.isPrimary && (
            <span className="rounded border border-hairline-strong px-1 text-[10px] leading-4 text-text-2">Główna</span>
          )}
        </span>
        <span className="truncate text-xs text-text-caption">{sub.name}</span>
      </div>

      <div role="cell" className="flex flex-col items-end gap-px text-right">
        <span>{sub.pricePln != null ? formatPricePln(sub.pricePln) : "—"}</span>
        {priceNative != null && <span className={CAPTION}>{formatCurrency(priceNative, sub.priceCurrency)}</span>}
      </div>

      <div role="cell" className="flex flex-col items-end gap-px text-right">
        <span>{valuePln != null ? formatZl(valuePln) : "—"}</span>
        {valuePln != null && assetValuePln > 0 && (
          <span className={CAPTION}>{formatShare((valuePln / assetValuePln) * 100)} aktywa</span>
        )}
      </div>

      <div role="cell" className="col-span-3 min-w-0">
        {renderAccountCell(sub, assetClass)}
      </div>

      {editMode && (
        <div role="cell" className="flex items-center justify-end gap-1">
          {onSetPrimaryClick && !sub.isPrimary && (
            <button
              type="button"
              aria-label={`Ustaw ${sub.ticker} jako główną`}
              title="Ustaw jako główną"
              disabled={settingPrimaryId === sub.id}
              onClick={() => onSetPrimaryClick(sub)}
              className={cn(ICON_BUTTON, COARSE_HIT_AREA, FOCUS_RING)}
            >
              <CrownIcon className="size-3.5" aria-hidden="true" />
            </button>
          )}
          {onEditSubClick && (
            <button
              type="button"
              aria-label={`Edytuj instrument ${sub.ticker}`}
              onClick={() => onEditSubClick(sub)}
              className={cn(ICON_BUTTON, COARSE_HIT_AREA, FOCUS_RING)}
            >
              <PencilIcon className="size-3.5" aria-hidden="true" />
            </button>
          )}
          {onDeleteSubClick && (
            <button
              type="button"
              aria-label={`Usuń instrument ${sub.ticker}`}
              onClick={() => onDeleteSubClick(sub)}
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
