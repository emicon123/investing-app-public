import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import { TimeRangeBrush } from "./TimeRangeBrush.js";

const ROWS = Array.from({ length: 24 }, (_, i) => ({ ts: Date.UTC(2020, i, 1) }));
const formatTs = (ts: number) => new Date(ts).toISOString().slice(0, 7);

// jsdom has no layout, so ResponsiveContainer would measure 0x0 and render nothing.
// It reads its size once from getBoundingClientRect — give it a real one.
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: 600,
    bottom: 40,
    width: 600,
    height: 40,
    toJSON: () => ({}),
  });
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe("TimeRangeBrush", () => {
  it("renders the recharts Brush (a bare Brush in a ResponsiveContainer renders nothing)", () => {
    const { container } = render(<TimeRangeBrush rows={ROWS} onChange={vi.fn()} formatTs={formatTs} />);

    expect(container.querySelector(".recharts-brush")).not.toBeNull();
    expect(container.querySelector(".recharts-brush-slide")).not.toBeNull();
    expect(container.querySelectorAll(".recharts-brush-traveller")).toHaveLength(2);
  });

  it("reports an index window when a traveller is dragged", () => {
    const onChange = vi.fn();
    const { container } = render(<TimeRangeBrush rows={ROWS} onChange={onChange} formatTs={formatTs} />);

    const traveller = container.querySelectorAll(".recharts-brush-traveller")[0];
    expect(traveller).toBeDefined();
    // Drag the start handle ~a third of the way along; recharts listens for the move on window.
    fireEvent.mouseDown(traveller as Element, { clientX: 56 });
    fireEvent.mouseMove(window, { clientX: 56 + 160 });
    fireEvent.mouseUp(window);

    expect(onChange).toHaveBeenCalled();
    const last = onChange.mock.calls.at(-1)?.[0] as { startIndex: number; endIndex: number };
    expect(last.startIndex).toBeGreaterThan(0);
    expect(last.endIndex).toBe(ROWS.length - 1);
  });
});
