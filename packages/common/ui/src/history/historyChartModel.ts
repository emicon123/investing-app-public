import { format, parseISO, startOfYear, subMonths, subYears } from "date-fns";
import { pl } from "date-fns/locale";
import type { BlendPointLike, HistoriaEntryLike } from "./historyChartTypes.js";

export type ChartRow = Omit<
  HistoriaEntryLike,
  | "xirr"
  | "inflationAdjustedDeposits"
  | "realReturnPct"
  | "annualizedInflationPct"
  | "positionValues"
> & {
  ts: number;
  xLabel: string;
  inflationCumPct: number | null;
  xirr: number | null;
  inflationAdjustedDeposits: number | null;
  realReturnPct: number | null;
  annualizedInflationPct: number | null;
  positionValues: Record<string, number>;
  blendFactor?: number;
  [key: string]: unknown;
};

export interface RangeWindow {
  startTs: number;
  endTs: number;
}

export const PRESET_KEYS = ["1M", "3M", "YTD", "1R", "3R", "MAX"] as const;
export type PresetKey = (typeof PRESET_KEYS)[number];

export interface Visibility {
  portfolioValue: boolean;
  totalDeposits: boolean;
  inflationAdjusted: boolean;
  profitBand: boolean;
  assetStack: boolean;
  blend: boolean;
  returnPct: boolean;
  realReturn: boolean;
  inflationCum: boolean;
  annualized: boolean;
  xirr: boolean;
}

export interface Point {
  ts: number;
  value: number;
}

export interface BandPolygon {
  sign: 1 | -1;
  valueLine: Point[];
  baseline: Point[];
}

export interface BrushSelection {
  startIndex: number;
  endIndex: number;
}

const DAY_MS = 24 * 60 * 60 * 1000;
const HALF_DAY_MS = DAY_MS / 2;
const MIN_WINDOW_MS = 5 * DAY_MS;
const MIN_PCT_SPAN = 0.02;
const SIGN_EPSILON = 1e-9;

export function computeInflationCumPct(
  entry: Pick<HistoriaEntryLike, "inflationAdjustedDeposits" | "totalDeposits">
): number | null {
  const adjusted = entry.inflationAdjustedDeposits;
  if (adjusted == null || !Number.isFinite(adjusted)) return null;
  if (!(entry.totalDeposits > 0)) return null;
  return adjusted / entry.totalDeposits - 1;
}

export function buildChartRows(
  data: HistoriaEntryLike[],
  assetTickers: string[],
  safeKeyMap: Map<string, string>
): ChartRow[] {
  const rows = data.map((entry) => {
    const date = parseISO(entry.snapshotDate);
    const row: ChartRow = {
      ...entry,
      ts: date.getTime(),
      xLabel: format(date, "d MMM yy", { locale: pl }),
      inflationCumPct: computeInflationCumPct(entry),
      xirr: entry.xirr ?? null,
      inflationAdjustedDeposits: entry.inflationAdjustedDeposits ?? null,
      realReturnPct: entry.realReturnPct ?? null,
      annualizedInflationPct: entry.annualizedInflationPct ?? null,
      positionValues: entry.positionValues ?? {},
    };
    for (const ticker of assetTickers) {
      const safe = safeKeyMap.get(ticker);
      if (safe) row[safe] = entry.positionValues?.[ticker] ?? 0;
    }
    return row;
  });
  return rows.sort((a, b) => a.ts - b.ts);
}

export function attachBlend(
  rows: ChartRow[],
  blendPoints: BlendPointLike[]
): ChartRow[] {
  const points = blendPoints
    .filter(
      (p): p is BlendPointLike & { blendFactor: number } =>
        p.blendFactor != null
    )
    .slice()
    .sort((a, b) => a.asOfDate.localeCompare(b.asOfDate));
  if (points.length === 0 || rows.length === 0) return rows;

  let i = 0;
  let lastBlend: number | undefined;
  return rows.map((row) => {
    let next = points[i];
    while (next != null && next.asOfDate <= row.snapshotDate) {
      lastBlend = next.blendFactor;
      i++;
      next = points[i];
    }
    return lastBlend != null ? { ...row, blendFactor: lastBlend } : row;
  });
}

export function fullRange(rows: ChartRow[]): RangeWindow | null {
  if (rows.length === 0) return null;
  const first = rows[0]!;
  const last = rows[rows.length - 1]!;
  if (rows.length === 1) {
    return { startTs: first.ts - HALF_DAY_MS, endTs: first.ts + HALF_DAY_MS };
  }
  return { startTs: first.ts, endTs: last.ts };
}

export function snapWindow(
  rows: ChartRow[],
  startTs: number,
  endTs: number
): RangeWindow | null {
  if (rows.length === 0) return null;
  const dataMinTs = rows[0]!.ts;
  const dataMaxTs = rows[rows.length - 1]!.ts;
  const lower = Math.max(startTs, dataMinTs);
  const upper = Math.min(endTs, dataMaxTs);
  if (lower > upper) return null;

  const start = rows.find((row) => row.ts >= lower);
  let end: ChartRow | undefined;
  for (let i = rows.length - 1; i >= 0; i--) {
    if (rows[i]!.ts <= upper) {
      end = rows[i];
      break;
    }
  }
  if (!start || !end || start.ts > end.ts) return null;
  return { startTs: start.ts, endTs: end.ts };
}

export function presetWindow(
  key: PresetKey,
  rows: ChartRow[]
): RangeWindow | null {
  if (key === "MAX") return null;
  const last = rows[rows.length - 1];
  if (!last) return null;
  const anchor = parseISO(last.snapshotDate);
  const rawStart =
    key === "1M"
      ? subMonths(anchor, 1)
      : key === "3M"
        ? subMonths(anchor, 3)
        : key === "YTD"
          ? startOfYear(anchor)
          : key === "1R"
            ? subYears(anchor, 1)
            : subYears(anchor, 3);
  const window = snapWindow(rows, rawStart.getTime(), last.ts);
  if (!window || window.startTs >= window.endTs) return null;
  return window;
}

export function activePreset(
  rows: ChartRow[],
  viewRange: RangeWindow | null
): PresetKey | null {
  if (viewRange == null) return "MAX";
  const target = snapWindow(rows, viewRange.startTs, viewRange.endTs);
  if (!target) return "MAX";
  for (const key of PRESET_KEYS) {
    if (key === "MAX") continue;
    const window = presetWindow(key, rows);
    if (window && window.startTs === target.startTs && window.endTs === target.endTs) {
      return key;
    }
  }
  return null;
}

export function brushIndices(
  rows: ChartRow[],
  range: RangeWindow | null
): BrushSelection {
  const lastIndex = Math.max(rows.length - 1, 0);
  if (rows.length === 0) return { startIndex: 0, endIndex: 0 };
  if (!range) return { startIndex: 0, endIndex: lastIndex };

  let startIndex = rows.findIndex((row) => row.ts >= range.startTs);
  if (startIndex < 0) startIndex = 0;
  let endIndex = 0;
  for (let i = rows.length - 1; i >= 0; i--) {
    if (rows[i]!.ts <= range.endTs) {
      endIndex = i;
      break;
    }
  }
  if (startIndex > endIndex) return { startIndex: 0, endIndex: lastIndex };
  return { startIndex, endIndex };
}

function minWindowMs(rows: ChartRow[]): number {
  const span = rows[rows.length - 1]!.ts - rows[0]!.ts;
  if (span <= 0) return MIN_WINDOW_MS;
  return Math.min(MIN_WINDOW_MS, span / 2);
}

function firstIndexAtOrAfter(rows: ChartRow[], ts: number): number {
  return rows.findIndex((row) => row.ts >= ts);
}

function lastIndexAtOrBefore(rows: ChartRow[], ts: number): number {
  for (let i = rows.length - 1; i >= 0; i--) {
    if (rows[i]!.ts <= ts) return i;
  }
  return -1;
}

export function resolveBrushWindow(
  rows: ChartRow[],
  currentRange: RangeWindow | null,
  selection: BrushSelection
): RangeWindow | null {
  const rowCount = rows.length;
  if (rowCount < 2) return currentRange;

  const { startIndex, endIndex } = selection;
  if (!Number.isInteger(startIndex) || !Number.isInteger(endIndex)) return currentRange;
  if (startIndex < 0 || endIndex < 0) return currentRange;
  if (startIndex >= rowCount || endIndex >= rowCount) return currentRange;
  if (startIndex > endIndex) return currentRange;

  const current = brushIndices(rows, currentRange);
  const startChanged = startIndex !== current.startIndex;
  const endChanged = endIndex !== current.endIndex;
  if (!startChanged && !endChanged) return currentRange;
  if (startIndex === endIndex) return currentRange;

  const startRow = rows[startIndex]!;
  const endRow = rows[endIndex]!;
  if (endRow.ts - startRow.ts >= minWindowMs(rows)) {
    return { startTs: startRow.ts, endTs: endRow.ts };
  }

  if (startChanged && endChanged) {
    return { startTs: startRow.ts, endTs: endRow.ts };
  }

  if (startChanged) {
    const movedEnd = firstIndexAtOrAfter(rows, startRow.ts + minWindowMs(rows));
    if (movedEnd >= 0 && movedEnd < rowCount && movedEnd > startIndex) {
      return { startTs: startRow.ts, endTs: rows[movedEnd]!.ts };
    }
    const clampedStart = lastIndexAtOrBefore(rows, endRow.ts - minWindowMs(rows));
    if (clampedStart >= 0 && clampedStart < endIndex) {
      return { startTs: rows[clampedStart]!.ts, endTs: endRow.ts };
    }
    return currentRange;
  }

  const movedStart = lastIndexAtOrBefore(rows, endRow.ts - minWindowMs(rows));
  if (movedStart >= 0 && movedStart < endIndex) {
    return { startTs: rows[movedStart]!.ts, endTs: endRow.ts };
  }
  const clampedEnd = firstIndexAtOrAfter(rows, startRow.ts + minWindowMs(rows));
  if (clampedEnd >= 0 && clampedEnd < rowCount && clampedEnd > startIndex) {
    return { startTs: startRow.ts, endTs: rows[clampedEnd]!.ts };
  }
  return currentRange;
}

export function windowRows(
  rows: ChartRow[],
  range: RangeWindow | null,
  padding = 0
): ChartRow[] {
  if (!range) return rows;
  if (rows.length === 0) return rows;

  let start = rows.findIndex((row) => row.ts >= range.startTs);
  if (start < 0) start = rows.length;
  let end = -1;
  for (let i = rows.length - 1; i >= 0; i--) {
    if (rows[i]!.ts <= range.endTs) {
      end = i;
      break;
    }
  }
  start = Math.max(0, start - padding);
  end = Math.min(rows.length - 1, end + padding);
  if (start > end) return [];
  return rows.slice(start, end + 1);
}

function niceStep(raw: number): number {
  if (!(raw > 0) || !Number.isFinite(raw)) return 1;
  const exponent = Math.floor(Math.log10(raw));
  const fraction = raw / 10 ** exponent;
  const nice = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10;
  return nice * 10 ** exponent;
}

export function niceDomain(min: number, max: number): [number, number] {
  if (!Number.isFinite(min) || !Number.isFinite(max)) return [0, 1];
  let lower = min;
  let upper = max;
  if (lower === upper) {
    const delta = Math.max(1, Math.abs(lower) * 0.05);
    lower -= delta;
    upper += delta;
  }
  const step = niceStep((upper - lower) / 4);
  return [Math.floor(lower / step) * step, Math.ceil(upper / step) * step];
}

function paddedDomain(values: number[], padRatio: number): [number, number] {
  if (values.length === 0) return [0, 1];
  let min = Math.min(...values);
  let max = Math.max(...values);
  if (!Number.isFinite(min) || !Number.isFinite(max)) return [0, 1];
  if (min === max) {
    const delta = Math.max(1, Math.abs(min) * 0.05);
    min -= delta;
    max += delta;
  }
  const pad = (max - min) * padRatio;
  return niceDomain(min - pad, max + pad);
}

export function plnDomain(
  rows: ChartRow[],
  range: RangeWindow | null,
  visible: Visibility,
  assetTickers: string[],
  safeKeyMap: Map<string, string>
): number[] {
  const scoped = windowRows(rows, range);
  const includeValue = visible.portfolioValue || visible.profitBand;
  const includeDeposits = visible.totalDeposits || visible.profitBand;
  const values: number[] = [];

  for (const row of scoped) {
    if (includeValue && Number.isFinite(row.portfolioValue)) {
      values.push(row.portfolioValue);
    }
    if (includeDeposits && Number.isFinite(row.totalDeposits)) {
      values.push(row.totalDeposits);
    }
    if (
      visible.inflationAdjusted &&
      row.inflationAdjustedDeposits != null &&
      Number.isFinite(row.inflationAdjustedDeposits)
    ) {
      values.push(row.inflationAdjustedDeposits);
    }
    if (visible.assetStack) {
      let cumulative = 0;
      for (const ticker of assetTickers) {
        const safe = safeKeyMap.get(ticker);
        const value = safe ? row[safe] : 0;
        if (typeof value === "number" && Number.isFinite(value)) cumulative += value;
      }
      values.push(cumulative);
    }
  }
  return paddedDomain(values, 0.06);
}

export function pctDomain(
  rows: ChartRow[],
  range: RangeWindow | null,
  visible: Visibility
): number[] {
  const scoped = windowRows(rows, range);
  const values = [0];

  for (const row of scoped) {
    if (visible.returnPct && Number.isFinite(row.returnPct)) values.push(row.returnPct);
    if (
      visible.realReturn &&
      row.realReturnPct != null &&
      Number.isFinite(row.realReturnPct)
    ) {
      values.push(row.realReturnPct);
    }
    if (
      visible.inflationCum &&
      row.inflationCumPct != null &&
      Number.isFinite(row.inflationCumPct)
    ) {
      values.push(row.inflationCumPct);
    }
    if (
      visible.annualized &&
      row.annualizedInflationPct != null &&
      Number.isFinite(row.annualizedInflationPct)
    ) {
      values.push(row.annualizedInflationPct);
    }
    if (visible.xirr && row.xirr != null && Number.isFinite(row.xirr)) {
      values.push(row.xirr);
    }
  }

  let min = Math.min(...values);
  let max = Math.max(...values);
  if (max - min < MIN_PCT_SPAN) {
    const mid = (min + max) / 2;
    min = mid - MIN_PCT_SPAN / 2;
    max = mid + MIN_PCT_SPAN / 2;
  }
  const pad = (max - min) * 0.08;
  return niceDomain(min - pad, max + pad);
}

interface BandPoint {
  ts: number;
  value: number;
  baseline: number;
}

interface WorkingPolygon {
  sign: 1 | -1;
  valueLine: Point[];
  baseline: Point[];
}

export function buildProfitBandPolygons(
  rows: ChartRow[],
  range: RangeWindow | null
): BandPolygon[] {
  const scoped = windowRows(rows, range, 1);
  if (scoped.length < 2) return [];

  const polygons: BandPolygon[] = [];
  let current: WorkingPolygon | null = null;

  const flush = () => {
    if (current && current.valueLine.length >= 2) polygons.push(current);
    current = null;
  };

  const push = (point: BandPoint) => {
    if (!current) return;
    const last = current.valueLine[current.valueLine.length - 1];
    if (last && last.ts === point.ts) return;
    current.valueLine.push({ ts: point.ts, value: point.value });
    current.baseline.push({ ts: point.ts, value: point.baseline });
  };

  const append = (sign: 1 | -1, start: BandPoint, end: BandPoint) => {
    if (!current || current.sign !== sign) {
      flush();
      current = { sign, valueLine: [], baseline: [] };
    }
    push(start);
    push(end);
  };

  for (let i = 0; i < scoped.length - 1; i++) {
    const first = scoped[i]!;
    const second = scoped[i + 1]!;
    const firstValue = first.portfolioValue;
    const firstDeposits = first.totalDeposits;
    const secondValue = second.portfolioValue;
    const secondDeposits = second.totalDeposits;
    if (
      !Number.isFinite(firstValue) ||
      !Number.isFinite(firstDeposits) ||
      !Number.isFinite(secondValue) ||
      !Number.isFinite(secondDeposits)
    ) {
      flush();
      continue;
    }

    const start: BandPoint = {
      ts: first.ts,
      value: firstValue,
      baseline: firstDeposits,
    };
    const end: BandPoint = {
      ts: second.ts,
      value: secondValue,
      baseline: secondDeposits,
    };
    const firstProfit = firstValue - firstDeposits;
    const secondProfit = secondValue - secondDeposits;

    if (
      Math.abs(firstProfit) < SIGN_EPSILON &&
      Math.abs(secondProfit) < SIGN_EPSILON
    ) {
      flush();
      continue;
    }

    if (Math.abs(firstProfit) < SIGN_EPSILON) {
      flush();
      append(secondProfit > 0 ? 1 : -1, start, end);
      continue;
    }

    if (Math.abs(secondProfit) < SIGN_EPSILON) {
      append(firstProfit > 0 ? 1 : -1, start, end);
      flush();
      continue;
    }

    if (firstProfit > 0 !== secondProfit > 0) {
      const t = firstProfit / (firstProfit - secondProfit);
      const crossing: BandPoint = {
        ts: start.ts + t * (end.ts - start.ts),
        value: start.value + t * (end.value - start.value),
        baseline: start.baseline + t * (end.baseline - start.baseline),
      };
      append(firstProfit > 0 ? 1 : -1, start, crossing);
      flush();
      append(secondProfit > 0 ? 1 : -1, crossing, end);
      continue;
    }

    append(firstProfit > 0 ? 1 : -1, start, end);
  }
  flush();
  return polygons;
}
