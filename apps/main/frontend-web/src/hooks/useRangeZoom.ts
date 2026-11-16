import { useCallback, useMemo, useState } from "react";

/** Inclusive row-index window, as reported by recharts' `Brush` (`onChange`). */
export interface IndexRange {
  startIndex: number;
  endIndex: number;
}

interface ZoomState<T> {
  /** The rows array the range's indices refer to. */
  source: T[];
  range: IndexRange | null;
  /** Bumped whenever zoom is dropped, so the Brush (which keeps its own handles) remounts too. */
  epoch: number;
}

/**
 * Rows inside `range` (inclusive). Returns `rows` itself — not a copy — when nothing is
 * zoomed or the range spans every row, so callers can use reference equality to detect it.
 */
export function sliceRange<T>(rows: T[], range: IndexRange | null): T[] {
  if (range === null) return rows;
  const start = Math.max(0, range.startIndex);
  const end = Math.min(rows.length - 1, range.endIndex);
  if (end < start || (start === 0 && end === rows.length - 1)) return rows;
  return rows.slice(start, end + 1);
}

export interface RangeZoom<T> {
  /** Rows the zoomed chart should draw. */
  visibleRows: T[];
  isZoomed: boolean;
  /** Feed to the Brush's `onChange`. */
  onBrushChange: (range: IndexRange) => void;
  /** Back to the full range. */
  reset: () => void;
  /** Use as the Brush's `key`: it changes whenever zoom is reset, remounting the Brush. */
  brushKey: number;
}

/**
 * Zoom state for a chart driven by a separate range Brush: the Brush is drawn over the full
 * `rows`, its selected index range decides which slice the main chart shows.
 *
 * Zoom is dropped when `rows` is replaced (live refetch, series toggled): the old indices
 * would silently select a different slice of the new array.
 */
export function useRangeZoom<T>(rows: T[]): RangeZoom<T> {
  const [state, setState] = useState<ZoomState<T>>({ source: rows, range: null, epoch: 0 });

  // Adjusting state while rendering (React docs: "storing information from previous renders")
  // rather than in an effect, so the stale range is never painted against the new rows.
  let current = state;
  if (state.source !== rows) {
    current = { source: rows, range: null, epoch: state.epoch + 1 };
    setState(current);
  }

  const onBrushChange = useCallback((range: IndexRange) => {
    setState((prev) => ({ ...prev, range }));
  }, []);

  const reset = useCallback(() => {
    setState((prev) => ({ ...prev, range: null, epoch: prev.epoch + 1 }));
  }, []);

  const visibleRows = useMemo(() => sliceRange(rows, current.range), [rows, current.range]);

  return {
    visibleRows,
    isZoomed: visibleRows !== rows,
    onBrushChange,
    reset,
    brushKey: current.epoch,
  };
}
