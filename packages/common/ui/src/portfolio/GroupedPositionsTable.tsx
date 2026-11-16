import { Fragment, useId } from "react";
import { cn } from "../lib/utils.js";
import { AssetRow } from "./AssetRow.js";
import { GroupHeaderRow } from "./GroupHeaderRow.js";
import { SubassetRow } from "./SubassetRow.js";
import type { TargetSum, TargetSumTone } from "./model/positionsModel.js";
import { FOCUS_RING, rowGrid } from "./tableLayout.js";
import type { PortfolioAssetLike, PortfolioSubassetLike, PositionsTableSlots } from "./types.js";
import type { PositionsView } from "./usePositionsView.js";

/**
 * The positions table grouped by exposure (task 170 §5, design doc §10–§11): CSS-grid rows with
 * table roles, a sticky column header and sticky group headers (they stick to the viewport, which
 * is why no ancestor may be a scroll container; below ~1160 px the table scrolls sideways in its
 * own box and the headers stick inside it instead), and a footer with the target sum and the
 * meter legend. Edit mode (any row-action callback present) adds an actions column.
 */

const TARGET_SUM_TONE: Record<TargetSumTone, string> = {
  ok: "text-text-caption",
  near: "text-status-over-text",
  off: "text-loss-text",
};

function TargetAllocationSum({ sum }: { sum: TargetSum }) {
  return <span className={TARGET_SUM_TONE[sum.tone]}>{sum.label}</span>;
}

function LegendItem({ swatch, label }: { swatch: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span aria-hidden="true" className={swatch} />
      {label}
    </span>
  );
}

function MeterLegend({ showBase }: { showBase: boolean }) {
  return (
    <div className="flex flex-wrap items-center gap-x-[18px] gap-y-2">
      <LegendItem swatch="h-1.5 w-4 rounded-[3px] bg-meter-fill" label="udział" />
      <LegendItem swatch="h-3 w-0.5 rounded-[1px] bg-meter-marker" label={showBase ? "cel efektywny" : "cel"} />
      {showBase && <LegendItem swatch="h-3 w-0 border-l border-dashed border-meter-base" label="cel bazowy" />}
      <LegendItem swatch="h-1.5 w-4 rounded-[3px] bg-status-over" label="nadwyżka" />
      <LegendItem swatch="h-1.5 w-4 rounded-[3px] bg-loss" label="do sprzedaży" />
      <LegendItem swatch="h-1.5 w-4 rounded-[3px] bg-status-under-fill" label="brakuje do celu" />
    </div>
  );
}

function NoResults({ query, onClear }: { query: string; onClear: () => void }) {
  const trimmed = query.trim();
  return (
    <div role="row" className="border-t border-white/6">
      <div role="cell" className="flex flex-col items-center gap-3 px-4 py-12 text-center text-text-2">
        <span>
          {trimmed !== ""
            ? `Żadna pozycja nie pasuje do „${trimmed}” w wybranym filtrze.`
            : "Żadna pozycja nie pasuje do wybranego filtra."}
        </span>
        <button
          type="button"
          onClick={onClear}
          className={cn(
            "min-h-9 rounded-lg border border-white/12 px-3.5 text-[13px] font-medium text-text-1 transition-colors hover:bg-white/6 motion-reduce:transition-none pointer-coarse:min-h-11",
            FOCUS_RING,
          )}
        >
          Wyczyść filtry
        </button>
      </div>
    </div>
  );
}

const COLUMN_HEADERS: readonly { label: string; numeric: boolean }[] = [
  { label: "Aktywo", numeric: false },
  { label: "Cena / śr. wejście", numeric: true },
  { label: "Wartość / udział", numeric: true },
  { label: "Udział wobec celu", numeric: false },
  { label: "Zysk / strata", numeric: true },
  { label: "Rebalansowanie", numeric: false },
];

export interface GroupedPositionsTableProps<TAsset extends PortfolioAssetLike<TSub>, TSub extends PortfolioSubassetLike> {
  view: PositionsView<TAsset>;
  table: PositionsTableSlots<TAsset, TSub>;
  /** Names the book, e.g. "Pozycje portfela Main pogrupowane według ekspozycji". */
  ariaLabel: string;
}

export function GroupedPositionsTable<TAsset extends PortfolioAssetLike<TSub>, TSub extends PortfolioSubassetLike>({
  view,
  table,
  ariaLabel,
}: GroupedPositionsTableProps<TAsset, TSub>) {
  const idPrefix = useId();
  const editMode =
    table.onEditClick != null ||
    table.onDeleteClick != null ||
    table.onEditSubClick != null ||
    table.onDeleteSubClick != null ||
    table.onSetPrimaryClick != null;
  const { model, filtered } = view;
  const noResults = filtered.isFiltering && filtered.groups.length === 0;
  const subId = (subassetId: number): string => `${idPrefix}-sub-${subassetId}`;

  return (
    <section aria-label="Pozycje" className="overflow-clip rounded-[14px] border border-hairline bg-surface-table">
      <div className="max-[1160px]:overflow-x-auto">
        <div role="table" aria-label={ariaLabel} className={editMode ? "min-w-[1200px]" : "min-w-[1080px]"}>
          <div role="rowgroup" className="sticky top-[var(--app-sticky-top,0px)] z-[3] bg-surface-table max-[1160px]:top-0">
            <div
              role="row"
              className={cn(rowGrid(editMode), "min-h-10 border-b border-hairline px-5 text-xs font-medium text-text-caption")}
            >
              {COLUMN_HEADERS.map((h) => (
                <div key={h.label} role="columnheader" className={cn(h.numeric && "text-right")}>
                  {h.label}
                </div>
              ))}
              {editMode && (
                <div role="columnheader" className="text-right">
                  <span className="sr-only">Akcje</span>
                </div>
              )}
            </div>
          </div>

          {filtered.groups.map((visible) => {
            const exposure = visible.group.exposure;
            const rowsId = `${idPrefix}-group-${exposure}`;
            const open = view.isGroupOpen(exposure);
            return (
              <div key={exposure} role="rowgroup" id={rowsId}>
                <GroupHeaderRow
                  view={visible}
                  open={open}
                  onToggle={() => view.toggleGroup(exposure)}
                  rowsId={rowsId}
                  editMode={editMode}
                />
                {open &&
                  visible.rows.map((row) => {
                    const expanded = view.isAssetExpanded(row.id);
                    return (
                      <Fragment key={row.id}>
                        <AssetRow
                          row={row}
                          editMode={editMode}
                          expanded={expanded}
                          onToggleExpand={view.toggleAsset}
                          subRowIds={row.asset.subassets.map((s) => subId(s.id)).join(" ")}
                          slots={table}
                        />
                        {expanded &&
                          row.asset.subassets.map((sub) => (
                            <SubassetRow
                              key={sub.id}
                              id={subId(sub.id)}
                              sub={sub}
                              assetClass={row.assetClass}
                              assetValuePln={row.valuePln}
                              editMode={editMode}
                              slots={table}
                            />
                          ))}
                      </Fragment>
                    );
                  })}
              </div>
            );
          })}

          {noResults && <NoResults query={view.query} onClear={view.clearFilters} />}
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2.5 border-t border-hairline px-5 py-3.5 text-xs text-text-caption">
        <TargetAllocationSum sum={model.targetSum} />
        <MeterLegend showBase={model.hasEffectiveOverride} />
      </div>
    </section>
  );
}
