import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import { format } from "date-fns";
import { pl } from "date-fns/locale";
import type { BrushSelection, ChartRow } from "./historyChartModel.js";

interface HistoryRangeSliderProps {
  rows: ChartRow[];
  startIndex: number;
  endIndex: number;
  onChange: (selection: BrushSelection) => void;
}

type DragState =
  | { kind: "start" | "end"; pointerId: number }
  | {
      kind: "slide";
      pointerId: number;
      originX: number;
      startIndex: number;
      endIndex: number;
    }
  | null;

const ACCENT = "var(--history-performance)";
const HANDLE_CLASS =
  "absolute top-1/2 z-10 h-5 w-3 -translate-x-1/2 -translate-y-1/2 rounded-sm border-2 bg-background shadow-sm outline-none before:absolute before:-inset-2.5 before:content-[''] focus-visible:ring-2 focus-visible:ring-ring";

function formatRangeDate(row: ChartRow | undefined): string {
  if (!row) return "—";
  return format(new Date(row.ts), "d MMM yy", { locale: pl });
}

export function HistoryRangeSlider({
  rows,
  startIndex,
  endIndex,
  onChange,
}: HistoryRangeSliderProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<DragState>(null);
  const lastIndex = Math.max(rows.length - 1, 0);
  const disabled = lastIndex < 1;

  const percent = (index: number) => (lastIndex <= 0 ? 0 : (index / lastIndex) * 100);

  const sparkPath = useMemo(() => {
    if (rows.length < 2) return null;
    let min = Infinity;
    let max = -Infinity;
    for (const row of rows) {
      const value = row.portfolioValue;
      if (!Number.isFinite(value)) continue;
      if (value < min) min = value;
      if (value > max) max = value;
    }
    if (!Number.isFinite(min) || !Number.isFinite(max)) return null;
    const span = max - min || 1;
    const step = 100 / (rows.length - 1);
    const parts: string[] = [];
    let open = false;
    rows.forEach((row, index) => {
      const value = row.portfolioValue;
      if (!Number.isFinite(value)) {
        open = false;
        return;
      }
      const x = index * step;
      const y = 96 - ((value - min) / span) * 92;
      parts.push(`${open ? "L" : "M"}${x.toFixed(2)} ${y.toFixed(2)}`);
      open = true;
    });
    return parts.length > 1 ? parts.join(" ") : null;
  }, [rows]);

  const indexAt = useCallback(
    (clientX: number) => {
      const el = trackRef.current;
      if (!el || lastIndex <= 0) return 0;
      const rect = el.getBoundingClientRect();
      if (rect.width <= 0) return 0;
      const ratio = (clientX - rect.left) / rect.width;
      return Math.min(lastIndex, Math.max(0, Math.round(ratio * lastIndex)));
    },
    [lastIndex]
  );

  const emit = useCallback(
    (nextStart: number, nextEnd: number) => {
      if (nextStart >= nextEnd) return;
      if (nextStart === startIndex && nextEnd === endIndex) return;
      onChange({ startIndex: nextStart, endIndex: nextEnd });
    },
    [endIndex, onChange, startIndex]
  );

  const handlePointerDown =
    (kind: "start" | "end") => (event: PointerEvent<HTMLDivElement>) => {
      if (disabled) return;
      event.preventDefault();
      event.stopPropagation();
      event.currentTarget.setPointerCapture(event.pointerId);
      setDrag({ kind, pointerId: event.pointerId });
    };

  const handleSlidePointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (disabled) return;
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    setDrag({
      kind: "slide",
      pointerId: event.pointerId,
      originX: event.clientX,
      startIndex,
      endIndex,
    });
  };

  const handleTrackPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (disabled || event.target !== event.currentTarget) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    const index = indexAt(event.clientX);
    const moveStart = Math.abs(index - startIndex) <= Math.abs(index - endIndex);
    if (moveStart) {
      emit(Math.min(index, endIndex - 1), endIndex);
    } else {
      emit(startIndex, Math.max(index, startIndex + 1));
    }
    setDrag({ kind: moveStart ? "start" : "end", pointerId: event.pointerId });
  };

  const handlePointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!drag || event.pointerId !== drag.pointerId) return;
    if (drag.kind === "slide") {
      const el = trackRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      if (rect.width <= 0) return;
      const delta = Math.round(((event.clientX - drag.originX) / rect.width) * lastIndex);
      const shift = Math.min(
        Math.max(delta, -drag.startIndex),
        lastIndex - drag.endIndex
      );
      emit(drag.startIndex + shift, drag.endIndex + shift);
      return;
    }
    const index = indexAt(event.clientX);
    if (drag.kind === "start") {
      emit(Math.min(index, endIndex - 1), endIndex);
    } else {
      emit(startIndex, Math.max(index, startIndex + 1));
    }
  };

  const handlePointerEnd = (event: PointerEvent<HTMLDivElement>) => {
    if (drag && event.pointerId === drag.pointerId) setDrag(null);
  };

  const handleKeyDown =
    (kind: "start" | "end") => (event: KeyboardEvent<HTMLDivElement>) => {
      if (disabled) return;
      let next: number;
      if (event.key === "Home") {
        next = kind === "start" ? 0 : startIndex + 1;
      } else if (event.key === "End") {
        next = kind === "start" ? endIndex - 1 : lastIndex;
      } else {
        const delta =
          event.key === "ArrowLeft" || event.key === "ArrowDown"
            ? -1
            : event.key === "ArrowRight" || event.key === "ArrowUp"
              ? 1
              : 0;
        if (delta === 0) return;
        next = kind === "start" ? startIndex + delta : endIndex + delta;
      }
      event.preventDefault();
      if (kind === "start") {
        emit(Math.max(Math.min(next, endIndex - 1), 0), endIndex);
      } else {
        emit(startIndex, Math.min(Math.max(next, startIndex + 1), lastIndex));
      }
    };

  const selectionLeft = percent(startIndex);
  const selectionWidth = Math.max(percent(endIndex) - selectionLeft, 0.5);

  return (
    <div className="flex shrink-0 items-center gap-2 px-2 pt-1" role="group" aria-label="Suwak zakresu">
      <span className="w-14 shrink-0 text-right text-[10px] tabular-nums text-foreground/70">
        {formatRangeDate(rows[startIndex])}
      </span>
      <div
        ref={trackRef}
        className={`relative h-8 flex-1 touch-none select-none ${disabled ? "opacity-50" : ""}`}
        onPointerDown={handleTrackPointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerEnd}
        onPointerCancel={handlePointerEnd}
        onLostPointerCapture={handlePointerEnd}
      >
        <div className="pointer-events-none absolute inset-x-0 top-1/2 h-px -translate-y-1/2 bg-border" />
        <div
          className="pointer-events-none absolute top-1/2 h-1 -translate-y-1/2 rounded-full"
          style={{
            left: `${selectionLeft}%`,
            width: `${selectionWidth}%`,
            background: ACCENT,
            opacity: 0.45,
          }}
        />
        <div
          className="absolute top-1/2 h-5 -translate-y-1/2 cursor-move before:absolute before:-inset-y-2 before:inset-x-0 before:content-['']"
          style={{
            left: `${selectionLeft}%`,
            width: `${selectionWidth}%`,
            background: ACCENT,
            opacity: 0.06,
          }}
          onPointerDown={handleSlidePointerDown}
          aria-hidden="true"
        />
        {sparkPath && (
          <svg
            className="pointer-events-none absolute inset-x-0 top-1/2 h-6 w-full -translate-y-1/2"
            viewBox="0 0 100 100"
            preserveAspectRatio="none"
            aria-hidden="true"
          >
            <path
              d={sparkPath}
              fill="none"
              stroke="var(--history-sparkline)"
              strokeWidth={1.25}
              strokeLinecap="round"
              strokeLinejoin="round"
              vectorEffect="non-scaling-stroke"
            />
          </svg>
        )}
        <div
          role="slider"
          tabIndex={disabled ? -1 : 0}
          aria-label="Początek zakresu"
          aria-valuemin={0}
          aria-valuemax={Math.max(endIndex - 1, 0)}
          aria-valuenow={startIndex}
          aria-valuetext={formatRangeDate(rows[startIndex])}
          className={HANDLE_CLASS}
          style={{ left: `${selectionLeft}%`, borderColor: ACCENT }}
          onPointerDown={handlePointerDown("start")}
          onKeyDown={handleKeyDown("start")}
        />
        <div
          role="slider"
          tabIndex={disabled ? -1 : 0}
          aria-label="Koniec zakresu"
          aria-valuemin={Math.min(startIndex + 1, lastIndex)}
          aria-valuemax={lastIndex}
          aria-valuenow={endIndex}
          aria-valuetext={formatRangeDate(rows[endIndex])}
          className={HANDLE_CLASS}
          style={{ left: `${percent(endIndex)}%`, borderColor: ACCENT }}
          onPointerDown={handlePointerDown("end")}
          onKeyDown={handleKeyDown("end")}
        />
      </div>
      <span className="w-14 shrink-0 text-left text-[10px] tabular-nums text-foreground/70">
        {formatRangeDate(rows[endIndex])}
      </span>
    </div>
  );
}
