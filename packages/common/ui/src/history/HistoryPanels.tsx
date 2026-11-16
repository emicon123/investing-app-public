import { useMemo, type CSSProperties } from "react";
import { format, parseISO } from "date-fns";
import { pl } from "date-fns/locale";
import {
  Area,
  ComposedChart,
  Line,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  type TooltipContentProps,
} from "recharts";
import { ASSET_PALETTE, blendToColor, formatPln } from "@common/app-core";
import { HISTORY_CHART_COLORS as COLOR } from "../lib/historyChartColors.js";
import { HistoryRangeSlider } from "./HistoryRangeSlider.js";
import { ProfitBand } from "./ProfitBand.js";
import type {
  BandPolygon,
  BrushSelection,
  ChartRow,
  RangeWindow,
  Visibility,
} from "./historyChartModel.js";

export interface GradientStop {
  offset: string;
  color: string;
}

export interface HistoryTopPanelProps {
  rows: ChartRow[];
  range: RangeWindow;
  visible: Visibility;
  syncId: string;
  plnDomain: number[];
  assetTickers: string[];
  safeKeyMap: Map<string, string>;
  colorMap: Map<string, string>;
  hoveredTicker: string | null;
  showBlend: boolean;
  blendGradientId: string;
  blendGradientStops: GradientStop[];
  bandPolygons: BandPolygon[];
  onAssetHover: (ticker: string | null) => void;
}

export interface HistoryBottomPanelProps {
  rows: ChartRow[];
  range: RangeWindow;
  visible: Visibility;
  syncId: string;
  pctDomain: number[];
  startIndex: number;
  endIndex: number;
  onRangeChange: (selection: BrushSelection) => void;
}

const CHART_MARGIN = { top: 8, right: 16, bottom: 0, left: 8 };

const TOOLTIP_CURSOR = {
  stroke: "var(--muted-foreground)",
  strokeDasharray: "3 3",
  strokeOpacity: 0.6,
};

const EMPTY_COLOR_MAP = new Map<string, string>();
const IGNORE_ASSET_HOVER = () => undefined;

const formatPct = (value: number) => `${(value * 100).toFixed(1)}%`;

function formatMaybePct(value: number | null | undefined): string {
  return value != null && Number.isFinite(value) ? formatPct(value) : "—";
}

function formatMaybePln(value: number | null | undefined): string {
  return value != null && Number.isFinite(value) ? formatPln(value, 0) : "—";
}

function formatYAxisPln(value: number): string {
  const abs = Math.abs(value);
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (abs >= 10_000) return `${(value / 1_000).toFixed(0)}k`;
  if (abs >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return String(Math.round(value));
}

function formatYAxisPct(value: number): string {
  const absPercent = Math.abs(value * 100);
  const decimals = absPercent < 1 ? 2 : absPercent < 10 ? 1 : 0;
  return `${(value * 100).toFixed(decimals)}%`;
}

const formatDateTick = (value: unknown) =>
  format(new Date(Number(value)), "d MMM yy", { locale: pl });

interface TooltipRowProps {
  label: string;
  value: string;
  valueStyle?: CSSProperties;
}

function TooltipRow({ label, value, valueStyle }: TooltipRowProps) {
  return (
    <p className="flex justify-between gap-4 tabular-nums">
      <span className="text-muted-foreground">{label}</span>
      <span style={valueStyle}>{value}</span>
    </p>
  );
}

interface HistoryTooltipProps {
  active: boolean;
  row: ChartRow | null;
  visible: Visibility;
  showAssetBreakdown: boolean;
  showBlend: boolean;
  colorMap: Map<string, string>;
  safeKeyMap: Map<string, string>;
  hoveredTicker: string | null;
  onAssetHover: (ticker: string | null) => void;
}

function HistoryTooltip({
  active,
  row,
  visible,
  showAssetBreakdown,
  showBlend,
  colorMap,
  safeKeyMap,
  hoveredTicker,
  onAssetHover,
}: HistoryTooltipProps) {
  if (!active || !row) return null;

  const hasProfit = Number.isFinite(row.profitPln);
  const profitText = hasProfit
    ? `${row.profitPln > 0 ? "+" : ""}${formatPln(row.profitPln, 0)} (${formatMaybePct(row.returnPct)})`
    : "—";
  const profitColor = !hasProfit
    ? undefined
    : row.profitPln >= 0
      ? COLOR.profitText
      : COLOR.lossText;
  const profitStyle = profitColor ? { color: profitColor } : undefined;

  const breakdown = showAssetBreakdown
    ? Object.entries(row.positionValues)
        .filter(([, value]) => Math.abs(value) >= 0.5)
        .sort((a, b) => b[1] - a[1])
    : [];
  const total = row.portfolioValue > 0 ? row.portfolioValue : 0;

  return (
    <div className="space-y-1 rounded-md border bg-popover px-3 py-2 text-sm text-popover-foreground shadow-md">
      <p className="font-semibold">
        {format(parseISO(row.snapshotDate), "d MMMM yyyy", { locale: pl })}
      </p>
      <TooltipRow label="Wartość portfela" value={formatMaybePln(row.portfolioValue)} />
      <TooltipRow label="Wpłacony kapitał" value={formatMaybePln(row.totalDeposits)} />
      <TooltipRow label="Zysk/strata" value={profitText} valueStyle={profitStyle} />
      {visible.xirr && (
        <TooltipRow label="XIRR (rocznie)" value={formatMaybePct(row.xirr)} />
      )}
      {visible.inflationAdjusted && (
        <TooltipRow label="Wpłaty + inflacja" value={formatMaybePln(row.inflationAdjustedDeposits)} />
      )}
      {visible.inflationCum && (
        <TooltipRow
          label="Inflacja na wpłaconym kapitale"
          value={formatMaybePct(row.inflationCumPct)}
        />
      )}
      {visible.realReturn && (
        <TooltipRow label="Realny zwrot" value={formatMaybePct(row.realReturnPct)} />
      )}
      {visible.annualized && (
        <TooltipRow
          label="Inflacja CPI (śr. roczna)"
          value={formatMaybePct(row.annualizedInflationPct)}
        />
      )}
      {breakdown.length > 0 && (
        <div className="mt-1.5 border-t pt-1.5">
          <p className="mb-1 text-xs font-medium text-muted-foreground">Wartość aktywów</p>
          <div className="max-h-72 space-y-0.5 overflow-auto pr-1">
            {breakdown.map(([ticker, value]) => {
              const safe = safeKeyMap.get(ticker);
              const color = safe ? colorMap.get(safe) : undefined;
              const pct = total > 0 ? (value / total) * 100 : 0;
              const isHovered = hoveredTicker === ticker;
              return (
                <div
                  key={ticker}
                  onMouseEnter={() => onAssetHover(ticker)}
                  onMouseLeave={() => onAssetHover(null)}
                  className={`flex items-center justify-between gap-3 rounded px-1 py-0.5 text-xs tabular-nums ${
                    isHovered ? "bg-muted font-semibold" : ""
                  }`}
                >
                  <span className="flex min-w-0 items-center gap-1.5">
                    {color && (
                      <span
                        className="inline-block h-2.5 w-2.5 shrink-0 rounded-sm border border-border/30"
                        style={{ background: color }}
                        aria-hidden="true"
                      />
                    )}
                    <span className={isHovered ? "text-foreground" : "text-muted-foreground"}>
                      {ticker}
                    </span>
                  </span>
                  <span className="flex shrink-0 items-center gap-2">
                    <span>{formatPln(value, 0)}</span>
                    <span className="text-[10px] text-muted-foreground">
                      {pct.toFixed(1)}%
                    </span>
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      )}
      {showBlend && row.blendFactor != null && (
        <p className="flex items-center gap-1.5">
          <span
            className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
            style={{ background: blendToColor(row.blendFactor) }}
            aria-hidden="true"
          />
          <span>Blend (makro): {row.blendFactor.toFixed(2)}</span>
        </p>
      )}
    </div>
  );
}

export function HistoryTopPanel({
  rows,
  range,
  visible,
  syncId,
  plnDomain,
  assetTickers,
  safeKeyMap,
  colorMap,
  hoveredTicker,
  showBlend,
  blendGradientId,
  blendGradientStops,
  bandPolygons,
  onAssetHover,
}: HistoryTopPanelProps) {
  const rowByTs = useMemo(() => new Map(rows.map((row) => [row.ts, row])), [rows]);

  return (
    <ResponsiveContainer width="100%" height="100%">
      <ComposedChart
        data={rows}
        margin={CHART_MARGIN}
        syncId={syncId}
        onMouseLeave={() => onAssetHover(null)}
      >
        {showBlend && blendGradientStops.length > 0 && (
          <defs>
            <linearGradient id={blendGradientId} x1="0" y1="0" x2="1" y2="0">
              {blendGradientStops.map((stop) => (
                <stop key={stop.offset} offset={stop.offset} stopColor={stop.color} />
              ))}
            </linearGradient>
          </defs>
        )}

        <XAxis
          dataKey="ts"
          type="number"
          scale="linear"
          domain={[range.startTs, range.endTs]}
          allowDataOverflow
          tick={false}
          axisLine={false}
          tickLine={false}
          height={8}
        />
        <YAxis
          yAxisId="pln"
          domain={plnDomain}
          allowDataOverflow
          tickCount={5}
          width={64}
          tickFormatter={formatYAxisPln}
          tick={{ fontSize: 11, fill: COLOR.axisTick }}
          tickLine={false}
          axisLine={{ stroke: COLOR.axisLine }}
        />
        {showBlend && <YAxis yAxisId="blend" domain={[0, 1]} hide />}

        <Tooltip
          content={(props: TooltipContentProps) => {
            const row = rowByTs.get(Number(props.label)) ?? null;
            return (
              <HistoryTooltip
                active={props.active}
                row={row}
                visible={visible}
                showAssetBreakdown
                showBlend={showBlend}
                colorMap={colorMap}
                safeKeyMap={safeKeyMap}
                hoveredTicker={hoveredTicker}
                onAssetHover={onAssetHover}
              />
            );
          }}
          cursor={TOOLTIP_CURSOR}
        />

        {visible.assetStack &&
          assetTickers.map((ticker) => {
            const safe = safeKeyMap.get(ticker);
            if (!safe) return null;
            const isHovered = hoveredTicker === ticker;
            const isDimmed = hoveredTicker != null && !isHovered;
            return (
              <Area
                key={safe}
                yAxisId="pln"
                type="linear"
                dataKey={safe}
                name={ticker}
                stackId="assets"
                stroke={colorMap.get(safe) ?? ASSET_PALETTE[0]!}
                strokeWidth={isHovered ? 1.5 : 0.5}
                strokeOpacity={isHovered ? 1 : 0.4}
                fill={colorMap.get(safe) ?? ASSET_PALETTE[0]!}
                fillOpacity={isHovered ? 1 : isDimmed ? 0.35 : 0.9}
                isAnimationActive={false}
                onMouseEnter={() => onAssetHover(ticker)}
                onMouseLeave={() => onAssetHover(null)}
              />
            );
          })}

        <ProfitBand polygons={bandPolygons} enabled={visible.profitBand} />

        {visible.totalDeposits && (
          <Line
            yAxisId="pln"
            type="linear"
            dataKey="totalDeposits"
            name="Wpłacony kapitał"
            stroke={COLOR.deposits}
            strokeWidth={1.25}
            dot={false}
            connectNulls={false}
            isAnimationActive={false}
          />
        )}
        {visible.inflationAdjusted && (
          <Line
            yAxisId="pln"
            type="linear"
            dataKey="inflationAdjustedDeposits"
            name="Wpłaty + inflacja"
            stroke={COLOR.inflationAdjusted}
            strokeWidth={1.25}
            strokeDasharray="4 3"
            dot={false}
            connectNulls={false}
            isAnimationActive={false}
          />
        )}
        {visible.portfolioValue && (
          <Line
            yAxisId="pln"
            type="linear"
            dataKey="portfolioValue"
            name="Wartość portfela"
            stroke={COLOR.portfolioValue}
            strokeWidth={2.75}
            strokeLinecap="round"
            dot={rows.length === 1 ? { r: 3 } : false}
            activeDot={{ r: 3 }}
            connectNulls={false}
            isAnimationActive={false}
          />
        )}
        {showBlend && (
          <Line
            yAxisId="blend"
            type="monotone"
            dataKey="blendFactor"
            name="Blend (makro)"
            stroke={`url(#${blendGradientId})`}
            strokeWidth={2}
            dot={false}
            connectNulls={false}
            isAnimationActive={false}
          />
        )}
      </ComposedChart>
    </ResponsiveContainer>
  );
}

export function HistoryBottomPanel({
  rows,
  range,
  visible,
  syncId,
  pctDomain,
  startIndex,
  endIndex,
  onRangeChange,
}: HistoryBottomPanelProps) {
  const rowByTs = useMemo(() => new Map(rows.map((row) => [row.ts, row])), [rows]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="min-h-0 flex-1">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart data={rows} margin={CHART_MARGIN} syncId={syncId}>
            <XAxis
              dataKey="ts"
              type="number"
              scale="linear"
              domain={[range.startTs, range.endTs]}
              allowDataOverflow
              tickFormatter={formatDateTick}
              tick={{ fontSize: 11, fill: COLOR.axisTick }}
              minTickGap={36}
              tickLine={false}
              axisLine={{ stroke: COLOR.axisLine }}
            />
            <YAxis
              yAxisId="pct"
              domain={pctDomain}
              allowDataOverflow
              tickCount={5}
              width={64}
              tickFormatter={formatYAxisPct}
              tick={{ fontSize: 11, fill: COLOR.axisTick }}
              tickLine={false}
              axisLine={{ stroke: COLOR.axisLine }}
            />

            <Tooltip
              content={(props: TooltipContentProps) => {
                const row = rowByTs.get(Number(props.label)) ?? null;
                return (
                  <HistoryTooltip
                    active={props.active}
                    row={row}
                    visible={visible}
                    showAssetBreakdown={false}
                    showBlend={false}
                    colorMap={EMPTY_COLOR_MAP}
                    safeKeyMap={EMPTY_COLOR_MAP}
                    hoveredTicker={null}
                    onAssetHover={IGNORE_ASSET_HOVER}
                  />
                );
              }}
              cursor={TOOLTIP_CURSOR}
            />

            <ReferenceLine
              yAxisId="pct"
              y={0}
              stroke={COLOR.zero}
              strokeWidth={1.25}
            />

            {visible.returnPct && (
              <Line
                yAxisId="pct"
                type="monotone"
                dataKey="returnPct"
                name="Stopa zwrotu"
                stroke={COLOR.performance}
                strokeWidth={2.75}
                strokeLinecap="round"
                dot={false}
                activeDot={{ r: 3 }}
                connectNulls={false}
                isAnimationActive={false}
              />
            )}
            {visible.realReturn && (
              <Line
                yAxisId="pct"
                type="monotone"
                dataKey="realReturnPct"
                name="Realny zwrot"
                stroke={COLOR.realReturn}
                strokeWidth={1.5}
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            )}
            {visible.inflationCum && (
              <Line
                yAxisId="pct"
                type="monotone"
                dataKey="inflationCumPct"
                name="Inflacja na wpłaconym kapitale"
                stroke={COLOR.inflationAdjusted}
                strokeWidth={1.25}
                strokeDasharray="5 3"
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            )}
            {visible.annualized && (
              <Line
                yAxisId="pct"
                type="monotone"
                dataKey="annualizedInflationPct"
                name="Inflacja CPI (śr. roczna)"
                stroke={COLOR.annualized}
                strokeWidth={1}
                strokeDasharray="2 3"
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            )}
            {visible.xirr && (
              <Line
                yAxisId="pct"
                type="monotone"
                dataKey="xirr"
                name="XIRR (rocznie)"
                stroke={COLOR.realReturn}
                strokeWidth={1.25}
                strokeDasharray="2 3"
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            )}

          </ComposedChart>
        </ResponsiveContainer>
      </div>
      <HistoryRangeSlider
        rows={rows}
        startIndex={startIndex}
        endIndex={endIndex}
        onChange={onRangeChange}
      />
    </div>
  );
}
