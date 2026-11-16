import { Brush, ComposedChart, ResponsiveContainer } from "recharts";

import type { IndexRange } from "@/hooks/useRangeZoom";

const BRUSH_HEIGHT = 40;

// The Brush prints the start/end labels just outside its handles, so the (otherwise empty)
// chart reserves room for them left and right.
const LABEL_GUTTER = 56;
const CHART_MARGIN = { top: 0, right: LABEL_GUTTER, bottom: 0, left: LABEL_GUTTER };

interface TimeRangeBrushProps {
  /** The FULL series — the Brush indexes into it, not into the zoomed slice. */
  rows: { ts: number }[];
  onChange: (range: IndexRange) => void;
  formatTs: (ts: number) => string;
}

/**
 * Standalone range-selection bar. recharts 3 feeds a `Brush` its data and layout from the
 * surrounding chart's store, so a Brush placed straight in a `ResponsiveContainer` renders
 * nothing — it needs to live inside a chart. This one carries no series or axes, only the
 * Brush, and reports the selected index window through `onChange`.
 *
 * Remount it (change its `key`) to reset the selection to the full range.
 */
export function TimeRangeBrush({ rows, onChange, formatTs }: TimeRangeBrushProps) {
  return (
    <ResponsiveContainer width="100%" height={BRUSH_HEIGHT}>
      <ComposedChart data={rows} margin={CHART_MARGIN}>
        <Brush
          dataKey="ts"
          height={BRUSH_HEIGHT}
          travellerWidth={10}
          fill="var(--background)"
          stroke="var(--border)"
          tickFormatter={formatTs}
          onChange={onChange}
          ariaLabel="Zakres wykresu"
        />
      </ComposedChart>
    </ResponsiveContainer>
  );
}
