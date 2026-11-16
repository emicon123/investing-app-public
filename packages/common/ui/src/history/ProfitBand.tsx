import { useId } from "react";
import {
  usePlotArea,
  useXAxisScale,
  useYAxisScale,
  type ScaleFunction,
} from "recharts";
import { HISTORY_CHART_COLORS } from "../lib/historyChartColors.js";
import type { BandPolygon, Point } from "./historyChartModel.js";

type Props = {
  polygons: BandPolygon[];
  enabled: boolean;
};

function isOrdered(points: Point[]): boolean {
  return points.every((point, index) => index === 0 || point.ts >= points[index - 1]!.ts);
}

function toPixels(
  points: Point[],
  xScale: ScaleFunction,
  yScale: ScaleFunction
): Array<{ x: number; y: number }> | null {
  const pixels: Array<{ x: number; y: number }> = [];
  for (const point of points) {
    const x = xScale(point.ts);
    const y = yScale(point.value);
    if (x == null || y == null || !Number.isFinite(x) || !Number.isFinite(y)) {
      return null;
    }
    pixels.push({ x, y });
  }
  return pixels;
}

function polygonPath(
  polygon: BandPolygon,
  xScale: ScaleFunction,
  yScale: ScaleFunction
): string | null {
  if (polygon.valueLine.length < 2) return null;
  if (polygon.valueLine.length !== polygon.baseline.length) return null;
  if (!isOrdered(polygon.valueLine) || !isOrdered(polygon.baseline)) return null;

  const valuePixels = toPixels(polygon.valueLine, xScale, yScale);
  const baselinePixels = toPixels(polygon.baseline, xScale, yScale);
  if (!valuePixels || !baselinePixels) return null;

  const round = (value: number) => Math.round(value * 100) / 100;
  const forward = valuePixels
    .map((point, index) => `${index === 0 ? "M" : "L"}${round(point.x)} ${round(point.y)}`)
    .join(" ");
  const backward = baselinePixels
    .slice()
    .reverse()
    .map((point) => `L${round(point.x)} ${round(point.y)}`)
    .join(" ");
  return `${forward} ${backward} Z`;
}

export function ProfitBand({ polygons, enabled }: Props) {
  const plotArea = usePlotArea();
  const xScale = useXAxisScale();
  const yScale = useYAxisScale("pln");
  const reactId = useId();
  const clipId = `history-profit-band-clip-${reactId.replace(/:/g, "")}`;

  if (!enabled || !plotArea || !xScale || !yScale) return null;

  const gain: string[] = [];
  const loss: string[] = [];
  for (const polygon of polygons) {
    const path = polygonPath(polygon, xScale, yScale);
    if (!path) continue;
    if (polygon.sign === 1) gain.push(path);
    else loss.push(path);
  }
  if (gain.length === 0 && loss.length === 0) return null;

  return (
    <>
      <defs>
        <clipPath id={clipId}>
          <rect
            x={plotArea.x}
            y={plotArea.y}
            width={plotArea.width}
            height={plotArea.height}
          />
        </clipPath>
      </defs>
      <g aria-hidden="true" clipPath={`url(#${clipId})`}>
        {gain.length > 0 && (
          <path d={gain.join(" ")} fill={HISTORY_CHART_COLORS.profitFill} stroke="none" />
        )}
        {loss.length > 0 && (
          <path d={loss.join(" ")} fill={HISTORY_CHART_COLORS.lossFill} stroke="none" />
        )}
      </g>
    </>
  );
}
