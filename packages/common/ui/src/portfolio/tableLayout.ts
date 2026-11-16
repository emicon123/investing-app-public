/**
 * Grid and interaction classes shared by every row of the grouped positions table (design doc §10,
 * §11). Full literal class strings so Tailwind's scanner sees them.
 */

/** The six columns: Aktywo, Cena / śr. wejście, Wartość / udział, Udział wobec celu, Zysk / strata, Rebalansowanie. */
export const ROW_GRID =
  "grid grid-cols-[minmax(230px,2.2fr)_minmax(130px,1fr)_minmax(130px,1fr)_minmax(230px,1.7fr)_minmax(120px,0.9fr)_minmax(170px,1fr)] items-center gap-x-6";

/** Edit mode appends a seventh column for the row actions. */
export const ROW_GRID_EDIT =
  "grid grid-cols-[minmax(230px,2.2fr)_minmax(130px,1fr)_minmax(130px,1fr)_minmax(230px,1.7fr)_minmax(120px,0.9fr)_minmax(170px,1fr)_minmax(112px,auto)] items-center gap-x-6";

export function rowGrid(editMode: boolean): string {
  return editMode ? ROW_GRID_EDIT : ROW_GRID;
}

/** Visible keyboard focus in the app's accent. */
export const FOCUS_RING = "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring";

/** A 44 × 44 hit area on coarse pointers for a small icon button (needs `relative`). */
export const COARSE_HIT_AREA =
  "pointer-coarse:before:absolute pointer-coarse:before:top-1/2 pointer-coarse:before:left-1/2 pointer-coarse:before:size-11 pointer-coarse:before:-translate-x-1/2 pointer-coarse:before:-translate-y-1/2 pointer-coarse:before:content-['']";

/** A small square icon button (row actions, chevron, pencil). */
export const ICON_BUTTON =
  "relative inline-flex size-7 shrink-0 items-center justify-center rounded-md text-text-2 transition-colors hover:bg-white/6 hover:text-text-1 disabled:pointer-events-none disabled:opacity-50 motion-reduce:transition-none";
