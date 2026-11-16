import { formatSignedPct, formatSignedZl, formatZl } from "./portfolioFormat.js";

/**
 * The KPI card's valuation sparkline and change line (task 170, design doc §3.3). Pure; the
 * component draws the returned SVG paths.
 */

export interface SparklineHistoryPoint {
  /** "YYYY-MM-DD". */
  snapshotDate: string;
  portfolioValue: number;
}

export interface SparklineModel {
  /** SVG path of the line in a 240 × 56 box. */
  line: string;
  /** The line closed down to the bottom edge, for the faint fill. */
  area: string;
  /** Vertical position of the last point, % of the box height (for the end dot). */
  endYPct: number;
  delta: number;
  /** "−27 800 zł (−9,8%)". */
  changeLabel: string;
  /** "w 30 dni". */
  rangeLabel: string;
  tone: "gain" | "loss";
  /** "Wycena w ostatnich 30 dniach: od 136 984 zł do 121 344 zł". */
  ariaLabel: string;
}

export const SPARKLINE_WIDTH = 240;
export const SPARKLINE_HEIGHT = 56;
const PADDING_Y = 6;
const DAY_MS = 24 * 60 * 60 * 1000;

const pad2 = (n: number): string => String(n).padStart(2, "0");

/** Local calendar date as "YYYY-MM-DD". */
export function isoLocalDate(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

/**
 * Points from `today − days` on, ascending; the live valuation replaces today's snapshot or is
 * appended after the last one.
 */
export function sparklineSeries(
  history: readonly SparklineHistoryPoint[],
  liveValue: number | null,
  days: number,
  today: Date,
): number[] {
  const todayIso = isoLocalDate(today);
  const fromIso = isoLocalDate(new Date(today.getTime() - days * DAY_MS));
  const points = history
    .filter((p) => p.snapshotDate >= fromIso && p.snapshotDate <= todayIso)
    .sort((a, b) => a.snapshotDate.localeCompare(b.snapshotDate))
    .map((p) => ({ date: p.snapshotDate, value: p.portfolioValue }));
  if (liveValue != null) {
    const last = points.at(-1);
    if (last?.date === todayIso) last.value = liveValue;
    else points.push({ date: todayIso, value: liveValue });
  }
  return points.map((p) => p.value);
}

/** Null when fewer than 2 points are available (no sparkline, no change line). */
export function buildSparkline(
  history: readonly SparklineHistoryPoint[],
  liveValue: number | null,
  days: number,
  today: Date,
): SparklineModel | null {
  const series = sparklineSeries(history, liveValue, days, today);
  const first = series[0];
  const last = series.at(-1);
  if (series.length < 2 || first === undefined || last === undefined) return null;

  const min = Math.min(...series);
  const max = Math.max(...series);
  const range = max - min || 1;
  const coords = series.map((v, i) => ({
    x: (i / (series.length - 1)) * SPARKLINE_WIDTH,
    y: PADDING_Y + (1 - (v - min) / range) * (SPARKLINE_HEIGHT - 2 * PADDING_Y),
  }));
  const line = coords.map((c, i) => `${i === 0 ? "M" : "L"}${c.x.toFixed(1)} ${c.y.toFixed(1)}`).join(" ");
  const endY = coords.at(-1)?.y ?? SPARKLINE_HEIGHT / 2;
  const delta = last - first;
  const deltaPct = first !== 0 ? delta / first : 0;

  return {
    line,
    area: `${line} L${SPARKLINE_WIDTH} ${SPARKLINE_HEIGHT} L0 ${SPARKLINE_HEIGHT} Z`,
    endYPct: (endY / SPARKLINE_HEIGHT) * 100,
    delta,
    changeLabel: `${formatSignedZl(delta)} (${formatSignedPct(deltaPct, 1)})`,
    rangeLabel: `w ${days} dni`,
    tone: last < first ? "loss" : "gain",
    ariaLabel: `Wycena w ostatnich ${days} dniach: od ${formatZl(first)} do ${formatZl(last)}`,
  };
}
