import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { sliceRange, useRangeZoom } from "./useRangeZoom.js";

const makeRows = (n: number) => Array.from({ length: n }, (_, i) => ({ ts: i }));

describe("sliceRange", () => {
  const rows = makeRows(10);

  it("returns the same array when nothing is zoomed", () => {
    expect(sliceRange(rows, null)).toBe(rows);
  });

  it("returns the same array when the range spans every row", () => {
    expect(sliceRange(rows, { startIndex: 0, endIndex: 9 })).toBe(rows);
    expect(sliceRange(rows, { startIndex: -3, endIndex: 40 })).toBe(rows);
  });

  it("slices inclusively on both ends", () => {
    expect(sliceRange(rows, { startIndex: 2, endIndex: 4 }).map((r) => r.ts)).toEqual([2, 3, 4]);
    expect(sliceRange(rows, { startIndex: 5, endIndex: 5 }).map((r) => r.ts)).toEqual([5]);
  });

  it("clamps an out-of-bounds end and ignores an inverted range", () => {
    expect(sliceRange(rows, { startIndex: 7, endIndex: 99 }).map((r) => r.ts)).toEqual([7, 8, 9]);
    expect(sliceRange(rows, { startIndex: 6, endIndex: 2 })).toBe(rows);
  });
});

describe("useRangeZoom", () => {
  it("starts on the full range", () => {
    const rows = makeRows(10);
    const { result } = renderHook(() => useRangeZoom(rows));
    expect(result.current.visibleRows).toBe(rows);
    expect(result.current.isZoomed).toBe(false);
  });

  it("narrows visibleRows to the Brush range (the Brush -> chart wiring)", () => {
    const rows = makeRows(10);
    const { result } = renderHook(() => useRangeZoom(rows));

    act(() => result.current.onBrushChange({ startIndex: 3, endIndex: 6 }));

    expect(result.current.visibleRows.map((r) => r.ts)).toEqual([3, 4, 5, 6]);
    expect(result.current.isZoomed).toBe(true);
  });

  it("is not 'zoomed' when the Brush is dragged back to both ends", () => {
    const rows = makeRows(10);
    const { result } = renderHook(() => useRangeZoom(rows));

    act(() => result.current.onBrushChange({ startIndex: 3, endIndex: 6 }));
    act(() => result.current.onBrushChange({ startIndex: 0, endIndex: 9 }));

    expect(result.current.visibleRows).toBe(rows);
    expect(result.current.isZoomed).toBe(false);
  });

  it("reset restores the full range and remounts the Brush (new key)", () => {
    const rows = makeRows(10);
    const { result } = renderHook(() => useRangeZoom(rows));
    const keyBefore = result.current.brushKey;

    act(() => result.current.onBrushChange({ startIndex: 3, endIndex: 6 }));
    act(() => result.current.reset());

    expect(result.current.visibleRows).toBe(rows);
    expect(result.current.isZoomed).toBe(false);
    expect(result.current.brushKey).not.toBe(keyBefore);
  });

  it("drops the zoom, and remounts the Brush, when the rows array is replaced", () => {
    const { result, rerender } = renderHook(({ rows }) => useRangeZoom(rows), {
      initialProps: { rows: makeRows(10) },
    });
    const keyBefore = result.current.brushKey;
    act(() => result.current.onBrushChange({ startIndex: 3, endIndex: 6 }));
    expect(result.current.isZoomed).toBe(true);

    const nextRows = makeRows(12);
    rerender({ rows: nextRows });

    expect(result.current.visibleRows).toBe(nextRows);
    expect(result.current.isZoomed).toBe(false);
    expect(result.current.brushKey).not.toBe(keyBefore);
  });

  it("keeps the zoom across re-renders that pass the same rows array", () => {
    const rows = makeRows(10);
    const { result, rerender } = renderHook(({ r }) => useRangeZoom(r), { initialProps: { r: rows } });
    act(() => result.current.onBrushChange({ startIndex: 3, endIndex: 6 }));

    rerender({ r: rows });

    expect(result.current.visibleRows.map((r) => r.ts)).toEqual([3, 4, 5, 6]);
  });
});
