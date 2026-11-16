import { useCallback, useId, useMemo, useState } from "react";
import { Check } from "lucide-react";
import { ASSET_PALETTE, EXPOSURE_BASE, blendToColor } from "@common/app-core";
import { HISTORY_CHART_COLORS as COLOR } from "./lib/historyChartColors.js";
import {
  PRESET_KEYS,
  activePreset,
  attachBlend,
  brushIndices,
  buildChartRows,
  buildProfitBandPolygons,
  fullRange,
  pctDomain as computePctDomain,
  plnDomain as computePlnDomain,
  presetWindow,
  resolveBrushWindow,
  snapWindow,
  type BrushSelection,
  type PresetKey,
  type RangeWindow,
  type Visibility,
} from "./history/historyChartModel.js";
import {
  HistoryBottomPanel,
  HistoryTopPanel,
  type GradientStop,
} from "./history/HistoryPanels.js";
import type {
  BlendPointLike,
  HistoriaEntryLike,
  WallPositionLike,
} from "./history/historyChartTypes.js";

export type {
  BlendPointLike,
  HistoriaEntryLike,
  WallPositionLike,
} from "./history/historyChartTypes.js";

type Props = {
  data: HistoriaEntryLike[];
  height?: number | string;
  positions?: WallPositionLike[];
  blendPoints?: BlendPointLike[];
};

const DEFAULT_VISIBILITY: Visibility = {
  portfolioValue: true,
  totalDeposits: true,
  inflationAdjusted: true,
  profitBand: true,
  assetStack: false,
  blend: false,
  returnPct: true,
  realReturn: false,
  inflationCum: true,
  annualized: false,
  xirr: false,
};

const EMPTY_POSITIONS: WallPositionLike[] = [];
const EMPTY_BLEND_POINTS: BlendPointLike[] = [];

function parseOklch(s: string): [number, number, number] | null {
  const m = s.match(/oklch\(\s*([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)\s*\)/);
  if (!m) return null;
  return [parseFloat(m[1]!), parseFloat(m[2]!), parseFloat(m[3]!)];
}
function oklchToString([l, c, h]: [number, number, number]): string {
  return `oklch(${l.toFixed(3)} ${c.toFixed(3)} ${h.toFixed(1)})`;
}
const CRYPTO_LIGHT = "oklch(0.90 0.11 95)";
const CRYPTO_DARK = "oklch(0.46 0.13 32)";

function lerpOklch(a: string, b: string, t: number): string {
  const pa = parseOklch(a);
  const pb = parseOklch(b);
  if (!pa || !pb) return a;
  const l = pa[0] + (pb[0] - pa[0]) * t;
  const c = pa[1] + (pb[1] - pa[1]) * t;
  let h = pa[2] + (pb[2] - pa[2]) * t;
  h = ((h % 360) + 360) % 360;
  return oklchToString([l, c, h]);
}
function cryptoShade(index: number, total: number): string {
  if (total <= 1) return EXPOSURE_BASE.CRYPTO!;
  const t = index / (total - 1);
  return lerpOklch(CRYPTO_DARK, CRYPTO_LIGHT, t);
}
function shadeForExposure(base: string, index: number, total: number): string {
  const parsed = parseOklch(base);
  if (!parsed || total <= 1) return base;
  const [l, c, h] = parsed;
  const step = 0.07;
  const offset = (index - (total - 1) / 2) * step;
  const newL = Math.max(0.38, Math.min(0.72, l + offset));
  let newC = c;
  if (total > 3) {
    const chromaStep = 0.018;
    newC = Math.max(0.08, Math.min(0.24, c + (index % 2 === 0 ? -chromaStep : chromaStep)));
  }
  return oklchToString([newL, newC, h]);
}

function tickerSafeKey(ticker: string, used: Set<string>): string {
  const base = `a_${ticker.replace(/[^A-Za-z0-9]/g, "_")}`;
  if (!used.has(base)) return base;
  let i = 1;
  while (used.has(`${base}_${i}`)) i++;
  return `${base}_${i}`;
}

function isNumber(v: number | undefined): v is number {
  return v != null;
}

function buildGradientStops(blends: number[]): GradientStop[] {
  if (blends.length === 0) return [];
  if (blends.length === 1) {
    const color = blendToColor(blends[0] ?? 0);
    return [
      { offset: "0%", color },
      { offset: "100%", color },
    ];
  }
  return blends.map((blend, i) => ({
    offset: `${(i / (blends.length - 1)) * 100}%`,
    color: blendToColor(blend),
  }));
}

type LegendSwatchKind =
  | "line"
  | "dashedLine"
  | "dottedLine"
  | "band"
  | "gradient"
  | "stacked";

interface LegendToggle {
  key: string;
  label: string;
  color: string;
  enabled: boolean;
  onToggle: () => void;
  swatch: LegendSwatchKind;
}

const BLEND_LEGEND_GRADIENT = [0, 0.25, 0.5, 0.75, 1].map(blendToColor).join(", ");

function LegendSwatch({
  kind,
  color,
  enabled,
}: {
  kind: LegendSwatchKind;
  color: string;
  enabled: boolean;
}) {
  const opacity = enabled ? 1 : 0.4;
  if (kind === "gradient") {
    return (
      <span
        className="inline-block h-2.5 w-8 shrink-0 rounded-full"
        style={{ background: `linear-gradient(to right, ${BLEND_LEGEND_GRADIENT})`, opacity }}
        aria-hidden="true"
      />
    );
  }
  if (kind === "stacked") {
    return (
      <span
        className="inline-flex h-2.5 w-5 shrink-0 overflow-hidden rounded-sm"
        style={{ opacity }}
        aria-hidden="true"
      >
        <span style={{ background: ASSET_PALETTE[0], flex: 1 }} />
        <span style={{ background: ASSET_PALETTE[1], flex: 1 }} />
        <span style={{ background: ASSET_PALETTE[2], flex: 1 }} />
      </span>
    );
  }
  if (kind === "band") {
    return (
      <span
        className="inline-flex h-2.5 w-5 shrink-0 flex-col overflow-hidden rounded-sm"
        style={{ opacity }}
        aria-hidden="true"
      >
        <span style={{ background: COLOR.profit, flex: 1 }} />
        <span style={{ background: COLOR.loss, flex: 1 }} />
      </span>
    );
  }
  return (
    <svg width="20" height="10" aria-hidden="true" className="shrink-0">
      <line
        x1="0"
        y1="5"
        x2="20"
        y2="5"
        stroke={color}
        strokeWidth={2}
        strokeLinecap="round"
        strokeDasharray={
          kind === "dashedLine" ? "5 4" : kind === "dottedLine" ? "2 3" : undefined
        }
        opacity={opacity}
      />
    </svg>
  );
}

function PortfolioValueLegend({ toggles }: { toggles: LegendToggle[] }) {
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5 text-xs text-foreground/75">
      {toggles.map((t) => (
        <button
          key={t.key}
          type="button"
          onClick={t.onToggle}
          aria-pressed={t.enabled}
          title={t.enabled ? `Ukryj ${t.label}` : `Pokaż ${t.label}`}
          className="flex items-center gap-1.5 rounded transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span
            className={`inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded border transition-colors ${
              t.enabled
                ? "border-foreground/50 bg-foreground/10 text-foreground"
                : "border-border bg-background/50 text-muted-foreground"
            }`}
          >
            {t.enabled && <Check className="size-3" strokeWidth={3} aria-hidden="true" />}
          </span>
          <LegendSwatch kind={t.swatch} color={t.color} enabled={t.enabled} />
          <span className={t.enabled ? undefined : "opacity-40"}>{t.label}</span>
        </button>
      ))}
    </div>
  );
}

interface PresetBarProps {
  active: PresetKey | null;
  disabled: boolean;
  onSelect: (key: PresetKey) => void;
}

function PresetBar({ active, disabled, onSelect }: PresetBarProps) {
  return (
    <div
      role="group"
      aria-label="Zakres czasu"
      className="flex shrink-0 items-center gap-1 pb-1"
    >
      {PRESET_KEYS.map((key) => {
        const isActive = active === key;
        return (
          <button
            key={key}
            type="button"
            disabled={disabled}
            aria-disabled={disabled}
            aria-pressed={isActive}
            onClick={() => onSelect(key)}
            className={`rounded px-2 py-1 text-xs font-medium tabular-nums transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-40 ${
              isActive
                ? "bg-foreground/10 text-foreground"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {key}
          </button>
        );
      })}
    </div>
  );
}

export function PortfolioValueLine({
  data,
  height = 560,
  positions = EMPTY_POSITIONS,
  blendPoints = EMPTY_BLEND_POINTS,
}: Props) {
  const [viewRange, setViewRange] = useState<RangeWindow | null>(null);
  const [visible, setVisible] = useState<Visibility>(DEFAULT_VISIBILITY);
  const [hoveredTicker, setHoveredTicker] = useState<string | null>(null);

  const hasBlend = useMemo(
    () => blendPoints.some((p) => p.blendFactor != null),
    [blendPoints]
  );

  const assetTickers = useMemo(() => {
    const totals = new Map<string, number>();
    for (const e of data) {
      if (!e.positionValues) continue;
      for (const [k, v] of Object.entries(e.positionValues)) {
        if (Math.abs(v) < 0.5) continue;
        totals.set(k, (totals.get(k) ?? 0) + Math.abs(v));
      }
    }
    return [...totals.entries()].sort((a, b) => b[1] - a[1]).map(([k]) => k);
  }, [data]);

  const { safeKeyMap, colorMap } = useMemo(() => {
    const used = new Set<string>();
    const map = new Map<string, string>();
    const colors = new Map<string, string>();
    const tickerExposure = new Map<string, string>();
    // Task 156: history keys are SUBASSET tickers — each takes its asset's exposure.
    // The optional parent `ticker` branch is compatibility only.
    for (const a of positions) {
      if (a.ticker !== undefined) tickerExposure.set(a.ticker, a.exposure);
      for (const s of a.subassets ?? []) {
        tickerExposure.set(s.ticker, a.exposure);
      }
    }
    const byExposure = new Map<string, string[]>();
    for (const ticker of assetTickers) {
      const exp = tickerExposure.get(ticker) ?? "__FALLBACK__";
      byExposure.set(exp, [...(byExposure.get(exp) ?? []), ticker]);
    }
    let fallbackIdx = 0;
    for (const [exposure, tickers] of byExposure.entries()) {
      if (exposure === "CRYPTO") {
        tickers.forEach((ticker, idx) => {
          const safe = tickerSafeKey(ticker, used);
          used.add(safe);
          map.set(ticker, safe);
          colors.set(safe, cryptoShade(idx, tickers.length));
        });
        continue;
      }
      const base = EXPOSURE_BASE[exposure] ?? null;
      if (base) {
        tickers.forEach((ticker, idx) => {
          const safe = tickerSafeKey(ticker, used);
          used.add(safe);
          map.set(ticker, safe);
          colors.set(safe, shadeForExposure(base, idx, tickers.length));
        });
      } else {
        tickers.forEach((ticker) => {
          const safe = tickerSafeKey(ticker, used);
          used.add(safe);
          map.set(ticker, safe);
          colors.set(safe, ASSET_PALETTE[fallbackIdx % ASSET_PALETTE.length] ?? ASSET_PALETTE[0]!);
          fallbackIdx++;
        });
      }
    }
    return { safeKeyMap: map, colorMap: colors };
  }, [assetTickers, positions]);

  const rows = useMemo(
    () => attachBlend(buildChartRows(data, assetTickers, safeKeyMap), blendPoints),
    [data, assetTickers, safeKeyMap, blendPoints]
  );

  const hasInflation = useMemo(
    () => data.some((e) => e.inflationAdjustedDeposits != null),
    [data]
  );

  const full = useMemo(() => fullRange(rows), [rows]);
  const effectiveRange = useMemo(() => {
    if (!viewRange) return full;
    return snapWindow(rows, viewRange.startTs, viewRange.endTs) ?? full;
  }, [rows, viewRange, full]);

  const brush = useMemo(() => brushIndices(rows, effectiveRange), [rows, effectiveRange]);
  const active = useMemo(() => activePreset(rows, viewRange), [rows, viewRange]);

  const effectiveVisible = useMemo<Visibility>(() => {
    const withInflation = hasInflation
      ? visible
      : {
          ...visible,
          inflationAdjusted: false,
          realReturn: false,
          inflationCum: false,
          annualized: false,
        };
    return { ...withInflation, blend: hasBlend && visible.blend };
  }, [visible, hasInflation, hasBlend]);

  const plnAxisDomain = useMemo(
    () => computePlnDomain(rows, effectiveRange, effectiveVisible, assetTickers, safeKeyMap),
    [rows, effectiveRange, effectiveVisible, assetTickers, safeKeyMap]
  );
  const pctAxisDomain = useMemo(
    () => computePctDomain(rows, effectiveRange, effectiveVisible),
    [rows, effectiveRange, effectiveVisible]
  );
  const bandPolygons = useMemo(
    () => buildProfitBandPolygons(rows, effectiveRange),
    [rows, effectiveRange]
  );

  const blendValues = useMemo(
    () => rows.map((r) => r.blendFactor).filter(isNumber),
    [rows]
  );
  const blendGradientStops = useMemo(() => buildGradientStops(blendValues), [blendValues]);
  const reactId = useId();
  const idSuffix = reactId.replace(/:/g, "");
  const blendGradientId = `portfolio-value-blend-gradient-${idSuffix}`;
  const syncId = `history-sync-${idSuffix}`;

  const toggle = useCallback((key: keyof Visibility) => {
    setVisible((prev) => ({ ...prev, [key]: !prev[key] }));
    if (key === "assetStack") setHoveredTicker(null);
  }, []);

  const handlePreset = useCallback(
    (key: PresetKey) => {
      if (key === "MAX") {
        setViewRange(null);
        return;
      }
      const next = presetWindow(key, rows);
      if (next) setViewRange(next);
    },
    [rows]
  );

  const handleRangeChange = useCallback(
    (selection: BrushSelection) => {
      if (rows.length < 2 || !full) return;
      const current = brushIndices(rows, effectiveRange);
      if (
        selection.startIndex === current.startIndex &&
        selection.endIndex === current.endIndex
      ) {
        return;
      }
      const next = resolveBrushWindow(rows, effectiveRange, selection);
      if (!next) return;
      if (next.startTs === effectiveRange?.startTs && next.endTs === effectiveRange?.endTs) {
        return;
      }
      const isFull = next.startTs === full.startTs && next.endTs === full.endTs;
      setViewRange(isFull ? null : next);
    },
    [rows, effectiveRange, full]
  );

  const handleAssetHover = useCallback((ticker: string | null) => {
    setHoveredTicker(ticker);
  }, []);

  const topToggles: LegendToggle[] = [
    {
      key: "portfolioValue",
      label: "Wartość portfela",
      color: COLOR.portfolioValue,
      enabled: visible.portfolioValue,
      onToggle: () => toggle("portfolioValue"),
      swatch: "line",
    },
    {
      key: "totalDeposits",
      label: "Wpłacony kapitał",
      color: COLOR.deposits,
      enabled: visible.totalDeposits,
      onToggle: () => toggle("totalDeposits"),
      swatch: "line",
    },
    ...(hasInflation
      ? [
          {
            key: "inflationAdjusted",
            label: "Wpłaty + inflacja",
            color: COLOR.inflationAdjusted,
            enabled: visible.inflationAdjusted,
            onToggle: () => toggle("inflationAdjusted"),
            swatch: "dashedLine" as const,
          },
        ]
      : []),
    {
      key: "profitBand",
      label: "Zysk/strata (nominalny)",
      color: COLOR.profit,
      enabled: visible.profitBand,
      onToggle: () => toggle("profitBand"),
      swatch: "band",
    },
    {
      key: "assetStack",
      label: "Wartość aktywów",
      color: ASSET_PALETTE[0]!,
      enabled: visible.assetStack,
      onToggle: () => toggle("assetStack"),
      swatch: "stacked",
    },
    ...(hasBlend
      ? [
          {
            key: "blend",
            label: "Blend (makro)",
            color: "currentColor",
            enabled: visible.blend,
            onToggle: () => toggle("blend"),
            swatch: "gradient" as const,
          },
        ]
      : []),
  ];

  const bottomToggles: LegendToggle[] = [
    {
      key: "returnPct",
      label: "Stopa zwrotu",
      color: COLOR.performance,
      enabled: visible.returnPct,
      onToggle: () => toggle("returnPct"),
      swatch: "line",
    },
    ...(hasInflation
      ? [
          {
            key: "realReturn",
            label: "Realny zwrot",
            color: COLOR.realReturn,
            enabled: visible.realReturn,
            onToggle: () => toggle("realReturn"),
            swatch: "line" as const,
          },
          {
            key: "inflationCum",
            label: "Inflacja na wpłaconym kapitale",
            color: COLOR.inflationAdjusted,
            enabled: visible.inflationCum,
            onToggle: () => toggle("inflationCum"),
            swatch: "dashedLine" as const,
          },
          {
            key: "annualized",
            label: "Inflacja CPI (śr. roczna)",
            color: COLOR.annualized,
            enabled: visible.annualized,
            onToggle: () => toggle("annualized"),
            swatch: "dottedLine" as const,
          },
        ]
      : []),
    {
      key: "xirr",
      label: "XIRR (rocznie)",
      color: COLOR.realReturn,
      enabled: visible.xirr,
      onToggle: () => toggle("xirr"),
      swatch: "dottedLine",
    },
  ];

  if (data.length === 0 || !effectiveRange) {
    return (
      <div className="flex min-h-0 flex-col" style={{ height }}>
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          Brak danych historycznych.
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-col" style={{ height }}>
      <PresetBar active={active} disabled={rows.length < 2} onSelect={handlePreset} />

      <div className="min-h-0 flex-[3]">
        <HistoryTopPanel
          rows={rows}
          range={effectiveRange}
          visible={effectiveVisible}
          syncId={syncId}
          plnDomain={plnAxisDomain}
          assetTickers={assetTickers}
          safeKeyMap={safeKeyMap}
          colorMap={colorMap}
          hoveredTicker={hoveredTicker}
          showBlend={effectiveVisible.blend}
          blendGradientId={blendGradientId}
          blendGradientStops={blendGradientStops}
          bandPolygons={bandPolygons}
          onAssetHover={handleAssetHover}
        />
      </div>

      <div className="shrink-0 pt-1.5">
        <PortfolioValueLegend toggles={topToggles} />
      </div>

      {visible.assetStack && assetTickers.length > 0 && (
        <div className="flex shrink-0 flex-wrap gap-2 pt-1.5">
          {assetTickers.map((ticker) => {
            const safe = safeKeyMap.get(ticker);
            const color = (safe ? colorMap.get(safe) : undefined) ?? ASSET_PALETTE[0]!;
            const isHovered = hoveredTicker === ticker;
            return (
              <button
                key={ticker}
                type="button"
                onMouseEnter={() => handleAssetHover(ticker)}
                onMouseLeave={() => handleAssetHover(null)}
                onFocus={() => handleAssetHover(ticker)}
                onBlur={() => handleAssetHover(null)}
                className={`flex items-center gap-1 rounded border px-2 py-1 text-xs transition-colors ${
                  isHovered
                    ? "border-foreground/20 bg-muted"
                    : "border-transparent bg-transparent hover:bg-muted/50"
                }`}
              >
                <span
                  className="inline-block h-2.5 w-2.5 shrink-0 rounded-sm border border-border/30"
                  style={{ background: color }}
                  aria-hidden="true"
                />
                <span className="font-mono text-xs">{ticker}</span>
              </button>
            );
          })}
        </div>
      )}

      <div className="min-h-0 flex-[2]">
        <HistoryBottomPanel
          rows={rows}
          range={effectiveRange}
          visible={effectiveVisible}
          syncId={syncId}
          pctDomain={pctAxisDomain}
          startIndex={brush.startIndex}
          endIndex={brush.endIndex}
          onRangeChange={handleRangeChange}
        />
      </div>

      <div className="shrink-0 pt-1.5">
        <PortfolioValueLegend toggles={bottomToggles} />
      </div>
    </div>
  );
}
