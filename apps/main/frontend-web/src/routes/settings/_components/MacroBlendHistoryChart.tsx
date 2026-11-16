import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Maximize } from "lucide-react";
import { getMacroStateHistory, getBenchmarkHistory, type BenchmarkSymbol, type BenchmarkHistoryPoint, type MacroStateHistoryPoint, type MacroZone } from "../../../api/index.js";
import {
  ComposedChart,
  Line,
  ReferenceArea,
  ReferenceLine,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  type DotItemDotProps,
  type TooltipContentProps,
} from "recharts";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { blendToColor } from "@common/app-core";
import { queryKeys } from "@/lib/queryKeys";
import { MACRO_BLEND_BACKTEST, type MacroBlendBacktestPoint } from "@/lib/macroBlendBacktest";
import { BENCHMARK_COLORS, REGIME_FILL, REGIME_SWATCH } from "@/lib/chartColors";
import { computeZigzagRegimes, type PriceRegimeSegment } from "@/lib/priceRegimes";
import { TimeRangeBrush } from "@/components/charts/TimeRangeBrush";
import { useRangeZoom } from "@/hooks/useRangeZoom";
import { formatZone } from "./MacroStatePanel";

// ── data merge (ADR-069 §4) ─────────────────────────────────────────────────

// Backtest points strictly before this date render dashed; live macro_state
// rows from this date on render solid. There is a real one-month data gap
// (no backtest point after 2026-05, no live row before 2026-07-01) — rendered
// as an actual break, not bridged, by keeping the two series on separate
// dataKeys (see BlendHistoryRow) rather than one continuous line.
const LIVE_CUTOVER = "2026-07-01";

interface BlendHistoryRow {
  ts: number;
  backtestBlend?: number;
  liveBlend?: number;
  liveZone?: MacroZone | null;
}

function toTs(isoDate: string): number {
  return new Date(`${isoDate.slice(0, 10)}T00:00:00Z`).getTime();
}

function buildBacktestRows(points: MacroBlendBacktestPoint[]): BlendHistoryRow[] {
  return points
    .filter((p) => p.date < LIVE_CUTOVER)
    .map((p) => ({ ts: toTs(p.date), backtestBlend: p.blend }));
}

function buildLiveRows(points: MacroStateHistoryPoint[]): BlendHistoryRow[] {
  return points
    .filter((p) => p.asOfDate >= LIVE_CUTOVER && p.blendFactor !== null)
    .map((p) => ({ ts: toTs(p.asOfDate), liveBlend: p.blendFactor ?? undefined, liveZone: p.zone }));
}

/**
 * The two series never share a timestamp (the backtest ends before the live
 * cutover), so merging is a plain concat + sort — no per-date reconciliation
 * needed, unlike trade-history's buildRows which merges two series that DO
 * overlap in time.
 */
function buildRows(
  backtest: MacroBlendBacktestPoint[],
  live: MacroStateHistoryPoint[]
): BlendHistoryRow[] {
  return [...buildBacktestRows(backtest), ...buildLiveRows(live)].sort((a, b) => a.ts - b.ts);
}

function isNumber(v: number | undefined): v is number {
  return v != null;
}

// ── optional price overlays (BTC / Nasdaq 100 / Gold / Hang Seng, ADR-070) ──

// The overlay series hold RAW weekly closes. They are plotted on a dedicated
// right-hand Y axis with `scale="log"` (YAxis id "price"), so the user reads the
// actual price (BTC $..., Nasdaq ~$…) at a glance while the log scale keeps the
// multi-decade range on one screen. The left axis stays the blend [0,1] scale. The
// axis domain is recomputed from the *visible* window, so the overlay refits when
// the fullscreen view is zoomed via the Brush or the backtest segment is hidden.
interface OverlayPoint {
  ts: number;
  close: number;
}

interface OverlaySeries {
  id: string;
  name: string;
  color: string;
  points: OverlayPoint[];
}

/** Blend row extended with per-overlay raw prices (keyed by overlay id). */
type ChartRow = BlendHistoryRow & Record<string, unknown>;

/** Legend/UI spec for one overlay — reuses the on/off pattern of the backtest toggle. */
interface OverlayToggle {
  id: string;
  name: string;
  color: string;
  enabled: boolean;
  onToggle: () => void;
}

/**
 * Legend/UI spec for one price series' regime-shading toggle (ADR-072 §4),
 * generalized across BTC/NDX the same way {@link OverlayToggle} generalizes
 * the price-line overlays. Only meaningful once that series' own price
 * overlay is on — `disabled` renders it inert (not interactable, no bands)
 * rather than hiding it outright, so the legend layout doesn't jump when the
 * user toggles the underlying overlay.
 */
interface RegimeToggle {
  id: string;
  /** Legend label, e.g. "Cykle BTC (byk/niedźwiedź)". */
  name: string;
  /** The underlying overlay's display name — used only for the disabled-state tooltip. */
  overlayName: string;
  enabled: boolean;
  disabled: boolean;
  onToggle: () => void;
}

const OVERLAY_DEFS: Record<BenchmarkSymbol, { id: string; name: string; color: string }> = {
  "BTC-USD": { id: "btc", name: "BTC", color: BENCHMARK_COLORS["BTC-USD"]! },
  "^NDX": { id: "ndx", name: "Nasdaq 100", color: BENCHMARK_COLORS["^NDX"]! },
  "GC=F": { id: "gold", name: "Złoto", color: BENCHMARK_COLORS["GC=F"]! },
  "^HSI": { id: "hsi", name: "Hang Seng", color: BENCHMARK_COLORS["^HSI"]! },
};

function buildOverlayPoints(points: BenchmarkHistoryPoint[]): OverlayPoint[] {
  return points
    .map((p) => ({
      ts: new Date(`${p.date.slice(0, 10)}T00:00:00Z`).getTime(),
      close: p.close,
    }))
    .filter((p) => Number.isFinite(p.close) && p.close > 0);
}

// ── price-regime shading (ADR-072, generalized to any overlay symbol) ──────



// Per-symbol zigzag reversal threshold, passed explicitly to computeZigzagRegimes
// instead of relying on its own thresholdPct=0.2 default (frontend/priceRegimes.ts is
// unchanged). A single global 20% doesn't fit assets of very different volatility: BTC
// routinely pulls back 20%+ *within* an ongoing bull run (20% was too sensitive/noisy),
// while Gold is much lower-volatility, so 20% is a rare/major move (too insensitive —
// barely ever fired). BTC/NDX/GC=F values confirmed directly by the user; ^HSI's 25% is
// a starting default (not yet user-confirmed) — a middle value between NDX's 20%
// (developed-market convention) and BTC's 30% (crypto-tuned), reflecting that Hong
// Kong/China equities run more volatile than developed markets but not as extreme as
// crypto.
const REGIME_THRESHOLD: Record<BenchmarkSymbol, number> = {
  "BTC-USD": 0.3,
  "^NDX": 0.2,
  "GC=F": 0.1,
  "^HSI": 0.25,
};

// ── gradient stroke (dataviz skill: per-point <stop>, one gradient per segment) ──

interface GradientStop {
  offset: string;
  color: string;
}

/**
 * One `<stop>` per data point, offset = fractional position along the series
 * (index/(n-1)) — fine given roughly-even time spacing — colored by that
 * point's own blend value via {@link blendToColor}. Recharts has no built-in
 * per-point stroke color, so the `<Line>` instead points its `stroke` at a
 * `url(#...)` SVG gradient built from these stops (see MacroBlendHistoryChartInner).
 * Built once per line segment (backtest vs live) because each renders as its
 * own `<path>` with its own bounding box — an `objectBoundingBox` gradient
 * defined against one segment's points would map incorrectly onto the other.
 */
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

// ── axis formatting ─────────────────────────────────────────────────────────

function formatAxisTick(ts: number): string {
  return new Date(ts).toLocaleDateString("pl-PL", { year: "numeric", month: "short", timeZone: "UTC" });
}

function formatDateOnly(ts: number): string {
  return new Date(ts).toLocaleDateString("pl-PL", { year: "numeric", month: "short", day: "numeric", timeZone: "UTC" });
}

/** Compact right-axis price label, e.g. 15 → "15", 42100 → "42.1k", 1000000 → "1M". */
function formatPriceAxisTick(v: number): string {
  return Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 })
    .format(v)
    .toLowerCase();
}

/** Full price in the tooltip, e.g. 42100 → "42,100". */
function formatPrice(v: number): string {
  return v.toLocaleString("en-US", { maximumFractionDigits: 0 });
}

/**
 * Explicit tick timestamps taken every ~nth row. Passed to `<XAxis ticks=…>`
 * instead of formatting recharts' generated ticks by index — recharts
 * generates far fewer ticks than there are rows (a number axis picks "nice"
 * yearly values), so an index-based formatter (old everyNthTick) matched only
 * tick 0 and the axis showed just the starting date.
 */
function everyNthTickTs(rows: BlendHistoryRow[], targetTicks: number): number[] {
  const step = Math.max(1, Math.floor(rows.length / targetTicks));
  return rows.filter((_, i) => i % step === 0).map((r) => r.ts);
}

const Y_TICKS = [0, 0.25, 0.5, 0.75, 1];
function formatBlendTick(v: number): string {
  return v.toFixed(2);
}

// ── tooltip ──────────────────────────────────────────────────────────────────

type BlendPayloadEntry = {
  dataKey?: string | number;
  value: number | null;
  payload: BlendHistoryRow;
};

function MacroBlendTooltip({
  active,
  payload,
  overlays,
}: TooltipContentProps & { overlays: OverlaySeries[] }) {
  if (!active || !payload?.length) return null;
  const entries = payload as unknown as BlendPayloadEntry[];

  // The blend lines are the primary subject — prefer them over any overlay line
  // that happens to share the hovered point's timestamp.
  const entry =
    entries.find(
      (e) =>
        e.value != null &&
        (e.dataKey === "backtestBlend" || e.dataKey === "liveBlend")
    ) ?? entries.find((e) => e.value != null);
  if (!entry) return null;

  const row = entry.payload;
  const isLive = row.liveBlend != null;
  const blend = isLive ? row.liveBlend : row.backtestBlend;

  const overlayValues = overlays.flatMap((ov) => {
    const e = entries.find((x) => x.dataKey === ov.id && x.value != null);
    if (!e) return [];
    return [{ ov, value: e.value as number }];
  });

  return (
    <div className="min-w-[160px] space-y-1 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
      <p className="text-sm font-semibold">{formatDateOnly(row.ts)}</p>
      <p className="text-muted-foreground">{isLive ? "Dane na żywo" : "Backtest"}</p>
      <p className="flex items-center gap-1.5">
        <span
          className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
          style={{ background: blendToColor(blend ?? 0) }}
          aria-hidden="true"
        />
        <span>blend {blend?.toFixed(2)}</span>
      </p>
      {isLive && row.liveZone && (
        <p className="text-muted-foreground">strefa {formatZone(row.liveZone)}</p>
      )}
      {overlayValues.map(({ ov, value }) => (
        <p key={ov.id} className="flex items-center gap-1.5">
          <span
            className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
            style={{ background: ov.color }}
            aria-hidden="true"
          />
          <span>
            {ov.name} ${formatPrice(value)}
          </span>
        </p>
      ))}
    </div>
  );
}

// ── legend ───────────────────────────────────────────────────────────────────

// Sampled (not just the two poles) so the swatch matches the OKLab-interpolated
// midpoint the line itself uses, rather than a naive 2-stop browser gradient
// that would render a muddier, differently-colored middle.
const LEGEND_SCALE_STOPS = [0, 0.25, 0.5, 0.75, 1].map(blendToColor).join(", ");

function MacroBlendLegend({
  showBacktest,
  onToggleBacktest,
  overlays,
  regimeToggles,
}: {
  showBacktest: boolean;
  onToggleBacktest: () => void;
  overlays: OverlayToggle[];
  regimeToggles: RegimeToggle[];
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-xs text-muted-foreground">
      <button
        type="button"
        onClick={onToggleBacktest}
        aria-pressed={showBacktest}
        title={showBacktest ? "Ukryj backtest historyczny" : "Pokaż backtest historyczny"}
        className="flex items-center gap-1.5 rounded transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded border border-border bg-background/50">
          {showBacktest && <Check className="size-3" />}
        </span>
        <svg width="20" height="10" aria-hidden="true" className="shrink-0">
          <line x1="0" y1="5" x2="20" y2="5" stroke="currentColor" strokeWidth={2} strokeDasharray="5 4" strokeLinecap="round" opacity={showBacktest ? 1 : 0.4} />
        </svg>
        Backtest (do 06.2026)
      </button>
      <span className="flex items-center gap-1.5">
        <svg width="20" height="10" aria-hidden="true">
          <line x1="0" y1="5" x2="20" y2="5" stroke="currentColor" strokeWidth={2} strokeLinecap="round" />
        </svg>
        Dane na żywo (od 07.2026)
      </span>
      {overlays.map((ov) => (
        <button
          key={ov.id}
          type="button"
          onClick={ov.onToggle}
          aria-pressed={ov.enabled}
          title={ov.enabled ? `Ukryj ${ov.name}` : `Pokaż ${ov.name}`}
          className="flex items-center gap-1.5 rounded transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded border border-border bg-background/50">
            {ov.enabled && <Check className="size-3" />}
          </span>
          <svg width="20" height="10" aria-hidden="true" className="shrink-0">
            <line x1="0" y1="5" x2="20" y2="5" stroke={ov.color} strokeWidth={2} strokeLinecap="round" opacity={ov.enabled ? 1 : 0.4} />
          </svg>
          {ov.name}
        </button>
      ))}
      {regimeToggles.map((rt) => (
        <button
          key={rt.id}
          type="button"
          onClick={rt.disabled ? undefined : rt.onToggle}
          disabled={rt.disabled}
          aria-pressed={rt.enabled}
          title={
            rt.disabled
              ? `Włącz ${rt.overlayName}, aby pokazać cykle byka/niedźwiedzia`
              : rt.enabled
                ? `Ukryj ${rt.name}`
                : `Pokaż ${rt.name}`
          }
          className="flex items-center gap-1.5 rounded transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:text-muted-foreground"
        >
          <span className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded border border-border bg-background/50">
            {rt.enabled && <Check className="size-3" />}
          </span>
          <span className="flex h-2.5 w-5 shrink-0 overflow-hidden rounded-full" aria-hidden="true">
            <span className="h-full w-1/2" style={{ background: REGIME_SWATCH.bear }} />
            <span className="h-full w-1/2" style={{ background: REGIME_SWATCH.bull }} />
          </span>
          {rt.name}
        </button>
      ))}
      <span className="flex items-center gap-2">
        <span
          className="inline-block h-2.5 w-16 rounded-full"
          style={{ background: `linear-gradient(to right, ${LEGEND_SCALE_STOPS})` }}
          aria-hidden="true"
        />
        risk-on ↔ risk-off (kolor = wartość blend)
      </span>
    </div>
  );
}

// ── chart ────────────────────────────────────────────────────────────────────

// When the fullscreen view is zoomed in far enough (≤ this many visible rows),
// individual per-point dots are rendered so the raw data is visible.
const DOT_THRESHOLD = 60;

/**
 * A per-point dot colored by that point's own blend value — used instead of
 * recharts' single-color dot when the fullscreen view is zoomed in.
 */
function BlendDot(props: DotItemDotProps) {
  // recharts types the datum as `any`; the chart is fed BlendHistoryRow[] (chartRows).
  const row = props.payload as BlendHistoryRow | undefined;
  const blend = row?.liveBlend ?? row?.backtestBlend;
  return (
    <circle
      key={props.key}
      cx={props.cx}
      cy={props.cy}
      r={2.5}
      fill={blendToColor(blend ?? 0)}
    />
  );
}

/**
 * Attaches each enabled overlay's RAW close to each row, keyed by the overlay id (the
 * right-hand log axis does the scaling). Overlay points are now weekly (ADR-070 fetches an
 * explicit weekly Yahoo range, see BenchmarkHistoryService), so each row is matched to the
 * latest overlay close on or before its own timestamp — a plain two-pointer merge, since both
 * `rows` and `ov.points` are sorted ascending by ts. That "carry forward" behaves like an
 * as-of-date lookup and, unlike the old exact calendar-month bucket join against monthly data,
 * leaves no gaps: every row from the overlay's start date onward gets a value, which is what
 * keeps the overlay line solid instead of breaking into disconnected segments.
 */
function buildChartRows(rows: BlendHistoryRow[], overlays: OverlaySeries[]): ChartRow[] {
  if (overlays.length === 0 || rows.length === 0) return rows.map((r) => ({ ...r }));

  const attached: ChartRow[] = rows.map((r) => ({ ...r }));
  for (const ov of overlays) {
    let i = 0;
    let lastClose: number | undefined;
    for (const r of attached) {
      let next = ov.points[i];
      while (next != null && next.ts <= r.ts) {
        lastClose = next.close;
        i++;
        next = ov.points[i];
      }
      if (lastClose != null) r[ov.id] = lastClose;
    }
  }
  return attached;
}

function MacroBlendHistoryChartInner({
  rows,
  instanceId,
  height = 320,
  targetTicks = 10,
  showDots = false,
  overlays = [],
  regimes = [],
}: {
  rows: BlendHistoryRow[];
  /** Suffix for the SVG gradient ids — each mounted chart needs unique ids. */
  instanceId: string;
  /** Forwarded to recharts' ResponsiveContainer, which accepts pixels or a percentage only. */
  height?: number | `${number}%`;
  targetTicks?: number;
  showDots?: boolean;
  overlays?: OverlaySeries[];
  /**
   * Bull/bear background bands (ADR-072) — rendered behind the Lines. Flat
   * across all enabled overlay symbols (BTC and/or NDX and/or Gold and/or Hang
   * Seng); the caller just concatenates each symbol's segments since exact-timestamp collisions
   * between two different price series are practically impossible, so no
   * "source" tag is needed to tell them apart.
   */
  regimes?: PriceRegimeSegment[];
}) {
  const chartRows = useMemo(() => buildChartRows(rows, overlays), [rows, overlays]);
  const backtestBlends = useMemo(() => rows.map((r) => r.backtestBlend).filter(isNumber), [rows]);
  const liveBlends = useMemo(() => rows.map((r) => r.liveBlend).filter(isNumber), [rows]);
  const backtestStops = useMemo(() => buildGradientStops(backtestBlends), [backtestBlends]);
  const liveStops = useMemo(() => buildGradientStops(liveBlends), [liveBlends]);
  const tickTs = useMemo(() => everyNthTickTs(rows, targetTicks), [rows, targetTicks]);
  const backtestGradientId = `macro-blend-backtest-gradient-${instanceId}`;
  const liveGradientId = `macro-blend-live-gradient-${instanceId}`;
  const dot = showDots ? BlendDot : false;

  // Right-hand log axis domain over the visible window — recomputed so the price
  // overlay refits when the Brush zooms or the backtest segment is hidden. Padding is
  // applied multiplicatively (a fixed 5% factor), not by subtracting/adding a linear
  // amount — on a log axis a linear pad computed from (max - min) is dominated by
  // whichever overlay has the widest raw-price range (BTC: ~$3 in 2011 to six figures
  // today) and barely nudges the other (Nasdaq 100: low thousands to ~25k), so the
  // smaller series hugs the bottom/top of the plot instead of both getting equal
  // visual breathing room.
  const priceDomain = useMemo<[number, number] | undefined>(() => {
    const values = overlays.flatMap((ov) =>
      chartRows
        .map((r) => r[ov.id])
        .filter((v): v is number => typeof v === "number" && v > 0)
    );
    if (values.length === 0) return undefined;
    const min = Math.min(...values);
    const max = Math.max(...values);
    const padFactor = 1.05;
    return [min / padFactor, max * padFactor];
  }, [overlays, chartRows]);

  return (
    <ResponsiveContainer width="100%" height={height}>
      <ComposedChart data={chartRows} margin={{ top: 8, right: 4, bottom: 4, left: 8 }}>
        <defs>
          <linearGradient id={backtestGradientId} x1="0" y1="0" x2="1" y2="0">
            {backtestStops.map((s, i) => (
              <stop key={i} offset={s.offset} stopColor={s.color} />
            ))}
          </linearGradient>
          <linearGradient id={liveGradientId} x1="0" y1="0" x2="1" y2="0">
            {liveStops.map((s, i) => (
              <stop key={i} offset={s.offset} stopColor={s.color} />
            ))}
          </linearGradient>
        </defs>

        {/* Bull/bear regime bands (ADR-072) — rendered before the Lines so they
            paint behind the blend line and any price overlay. fillOpacity={1}
            overrides recharts' own ReferenceArea default (0.5), which would
            otherwise multiply the oklch alpha above down to near-invisible.
            ifOverflow="hidden" (not the default "discard") so a segment that
            only partially overlaps the currently visible/zoomed window still
            renders its visible slice instead of disappearing outright. */}
        {regimes.map((seg) => (
          <ReferenceArea
            key={`${seg.type}-${seg.startTs}`}
            x1={seg.startTs}
            x2={seg.endTs}
            fill={REGIME_FILL[seg.type]}
            fillOpacity={1}
            stroke="none"
            ifOverflow="hidden"
          />
        ))}

        <XAxis
          type="number"
          dataKey="ts"
          domain={["dataMin", "dataMax"]}
          ticks={tickTs}
          tickFormatter={formatAxisTick}
          tick={{ fontSize: 10 }}
          interval="preserveStartEnd"
          minTickGap={40}
        />
        <YAxis
          type="number"
          domain={[0, 1]}
          ticks={Y_TICKS}
          tickFormatter={formatBlendTick}
          tick={{ fontSize: 10 }}
          width={40}
        />
        {priceDomain && (
          <YAxis
            yAxisId="price"
            orientation="right"
            type="number"
            scale="log"
            domain={priceDomain}
            tickFormatter={formatPriceAxisTick}
            tick={{ fontSize: 10 }}
            width={44}
          />
        )}
        <ReferenceLine y={0.5} stroke="var(--border)" strokeWidth={1} />
        <Tooltip
          content={(props) => <MacroBlendTooltip {...props} overlays={overlays} />}
        />

        <Line
          dataKey="backtestBlend"
          name="Backtest"
          stroke={`url(#${backtestGradientId})`}
          strokeWidth={2}
          strokeDasharray="5 4"
          strokeLinecap="round"
          strokeLinejoin="round"
          dot={dot}
          activeDot={{ r: 4 }}
          isAnimationActive={false}
        />
        <Line
          dataKey="liveBlend"
          name="Live"
          stroke={`url(#${liveGradientId})`}
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
          dot={dot}
          activeDot={{ r: 4 }}
          isAnimationActive={false}
        />
        {overlays.map((ov) => (
          <Line
            key={ov.id}
            yAxisId="price"
            dataKey={ov.id}
            name={ov.name}
            stroke={ov.color}
            strokeWidth={1.5}
            strokeLinecap="round"
            strokeLinejoin="round"
            dot={false}
            activeDot={{ r: 3 }}
            isAnimationActive={false}
          />
        ))}
      </ComposedChart>
    </ResponsiveContainer>
  );
}

// ── fullscreen dialog (zoom via Brush range selection) ───────────────────────

interface MacroBlendDialogProps {
  rows: BlendHistoryRow[];
  showBacktest: boolean;
  onToggleBacktest: () => void;
  overlays: OverlaySeries[];
  overlayToggles: OverlayToggle[];
  regimes: PriceRegimeSegment[];
  regimeToggles: RegimeToggle[];
}

/**
 * Everything inside the dialog that depends on the zoom. It is only mounted while the dialog
 * is open (Radix unmounts closed content), so zoom state resets by itself on every reopen.
 */
function MacroBlendDialogBody({
  rows,
  showBacktest,
  onToggleBacktest,
  overlays,
  overlayToggles,
  regimes,
  regimeToggles,
}: MacroBlendDialogProps) {
  const zoom = useRangeZoom(rows);

  return (
    <>
      <DialogHeader className="shrink-0 pr-8">
        <DialogTitle>Historia współczynnika blend — pełny ekran</DialogTitle>
        <DialogDescription>
          Przeciągnij krawędzie zaznaczenia na pasku pod wykresem, aby przybliżyć wybrany zakres
          dat; przeciągnij sam zaznaczony obszar, aby go przesunąć. Przycisk „Pełny zakres”
          przywraca cały przebieg.
        </DialogDescription>
      </DialogHeader>

      <div className="min-h-0 flex-1">
        <MacroBlendHistoryChartInner
          rows={zoom.visibleRows}
          instanceId="fullscreen"
          height="100%"
          targetTicks={16}
          showDots={zoom.visibleRows.length <= DOT_THRESHOLD}
          overlays={overlays}
          regimes={regimes}
        />
      </div>

      <div className="flex shrink-0 items-center gap-2 px-2">
        <div className="min-w-0 flex-1">
          <TimeRangeBrush
            key={zoom.brushKey}
            rows={rows}
            onChange={zoom.onBrushChange}
            formatTs={formatAxisTick}
          />
        </div>
        <Button variant="outline" size="sm" onClick={zoom.reset} disabled={!zoom.isZoomed}>
          Pełny zakres
        </Button>
      </div>

      <div className="shrink-0 px-4 pb-4">
        <MacroBlendLegend
          showBacktest={showBacktest}
          onToggleBacktest={onToggleBacktest}
          overlays={overlayToggles}
          regimeToggles={regimeToggles}
        />
      </div>
    </>
  );
}

function MacroBlendHistoryDialog({
  open,
  onOpenChange,
  ...body
}: MacroBlendDialogProps & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[95vw] h-[92vh] flex flex-col">
        <MacroBlendDialogBody {...body} />
      </DialogContent>
    </Dialog>
  );
}

/**
 * Each symbol's regime bands are derived from that symbol's own overlay series, so they're only
 * meaningful while that overlay is on — turning an overlay off drops its bands too (they stay off
 * when the overlay comes back) rather than leaving them orphaned with no visible line (ADR-072 §4).
 */
function toggleOverlay(
  overlayOn: boolean,
  setOverlayOn: (on: boolean) => void,
  setRegimesOn: (on: boolean) => void,
) {
  if (overlayOn) setRegimesOn(false);
  setOverlayOn(!overlayOn);
}

// ── page-level panel ─────────────────────────────────────────────────────────

export function MacroBlendHistoryChart() {
  const { data, isPending, isError } = useQuery({
    queryKey: queryKeys.macroState.history,
    queryFn: getMacroStateHistory,
    staleTime: 5 * 60_000,
  });

  const [showBacktest, setShowBacktest] = useState(true);
  const [showBtc, setShowBtc] = useState(false);
  const [showNdx, setShowNdx] = useState(false);
  const [showGold, setShowGold] = useState(false);
  const [showHsi, setShowHsi] = useState(false);
  const [showBtcRegimes, setShowBtcRegimes] = useState(false);
  const [showNdxRegimes, setShowNdxRegimes] = useState(false);
  const [showGoldRegimes, setShowGoldRegimes] = useState(false);
  const [showHsiRegimes, setShowHsiRegimes] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);

  const livePoints = useMemo(() => data?.data ?? [], [data]);

  const btcQuery = useQuery({
    queryKey: queryKeys.macroState.benchmarkHistory("BTC-USD"),
    queryFn: () => getBenchmarkHistory({ symbol: "BTC-USD" }),
    enabled: showBtc,
    staleTime: 12 * 60 * 60_000,
  });
  const ndxQuery = useQuery({
    queryKey: queryKeys.macroState.benchmarkHistory("^NDX"),
    queryFn: () => getBenchmarkHistory({ symbol: "^NDX" }),
    enabled: showNdx,
    staleTime: 12 * 60 * 60_000,
  });
  const goldQuery = useQuery({
    queryKey: queryKeys.macroState.benchmarkHistory("GC=F"),
    queryFn: () => getBenchmarkHistory({ symbol: "GC=F" }),
    enabled: showGold,
    staleTime: 12 * 60 * 60_000,
  });
  const hsiQuery = useQuery({
    queryKey: queryKeys.macroState.benchmarkHistory("^HSI"),
    queryFn: () => getBenchmarkHistory({ symbol: "^HSI" }),
    enabled: showHsi,
    staleTime: 12 * 60 * 60_000,
  });

  // The backtest half is a static import, so it's always available
  // immediately — the chart renders it right away and the live/solid
  // portion fills in once the query resolves, rather than blocking the
  // whole card behind isPending.
  const rows = useMemo(() => buildRows(MACRO_BLEND_BACKTEST, livePoints), [livePoints]);

  // Hiding the backtest removes backtest-only rows entirely, so the live
  // series refits the whole plot area instead of hugging the right edge.
  const baseRows = useMemo(
    () => (showBacktest ? rows : rows.filter((r) => r.liveBlend != null)),
    [rows, showBacktest]
  );

  const overlays = useMemo<OverlaySeries[]>(() => {
    const items: OverlaySeries[] = [];
    if (showBtc && btcQuery.data?.data) {
      items.push({ ...OVERLAY_DEFS["BTC-USD"], points: buildOverlayPoints(btcQuery.data.data) });
    }
    if (showNdx && ndxQuery.data?.data) {
      items.push({ ...OVERLAY_DEFS["^NDX"], points: buildOverlayPoints(ndxQuery.data.data) });
    }
    if (showGold && goldQuery.data?.data) {
      items.push({ ...OVERLAY_DEFS["GC=F"], points: buildOverlayPoints(goldQuery.data.data) });
    }
    if (showHsi && hsiQuery.data?.data) {
      items.push({ ...OVERLAY_DEFS["^HSI"], points: buildOverlayPoints(hsiQuery.data.data) });
    }
    return items;
  }, [showBtc, btcQuery.data, showNdx, ndxQuery.data, showGold, goldQuery.data, showHsi, hsiQuery.data]);

  const overlayToggles: OverlayToggle[] = [
    {
      ...OVERLAY_DEFS["BTC-USD"],
      enabled: showBtc,
      onToggle: () => toggleOverlay(showBtc, setShowBtc, setShowBtcRegimes),
    },
    {
      ...OVERLAY_DEFS["^NDX"],
      enabled: showNdx,
      onToggle: () => toggleOverlay(showNdx, setShowNdx, setShowNdxRegimes),
    },
    {
      ...OVERLAY_DEFS["GC=F"],
      enabled: showGold,
      onToggle: () => toggleOverlay(showGold, setShowGold, setShowGoldRegimes),
    },
    {
      ...OVERLAY_DEFS["^HSI"],
      enabled: showHsi,
      onToggle: () => toggleOverlay(showHsi, setShowHsi, setShowHsiRegimes),
    },
  ];

  // Computed over each symbol's full fetched series (not clipped to the
  // visible window) — recharts clips each ReferenceArea to the active axis
  // domain at render time, so segments outside the current zoom simply don't
  // paint rather than needing to be re-derived per zoom level (ADR-072 §6).
  // Each symbol passes its own REGIME_THRESHOLD explicitly (ADR-072 amendment) —
  // computeZigzagRegimes's own thresholdPct=0.2 default is not used here.
  const btcRegimes = useMemo<PriceRegimeSegment[]>(() => {
    if (!showBtc || !showBtcRegimes || !btcQuery.data?.data) return [];
    return computeZigzagRegimes(buildOverlayPoints(btcQuery.data.data), REGIME_THRESHOLD["BTC-USD"]);
  }, [showBtc, showBtcRegimes, btcQuery.data]);
  const ndxRegimes = useMemo<PriceRegimeSegment[]>(() => {
    if (!showNdx || !showNdxRegimes || !ndxQuery.data?.data) return [];
    return computeZigzagRegimes(buildOverlayPoints(ndxQuery.data.data), REGIME_THRESHOLD["^NDX"]);
  }, [showNdx, showNdxRegimes, ndxQuery.data]);
  const goldRegimes = useMemo<PriceRegimeSegment[]>(() => {
    if (!showGold || !showGoldRegimes || !goldQuery.data?.data) return [];
    return computeZigzagRegimes(buildOverlayPoints(goldQuery.data.data), REGIME_THRESHOLD["GC=F"]);
  }, [showGold, showGoldRegimes, goldQuery.data]);
  const hsiRegimes = useMemo<PriceRegimeSegment[]>(() => {
    if (!showHsi || !showHsiRegimes || !hsiQuery.data?.data) return [];
    return computeZigzagRegimes(buildOverlayPoints(hsiQuery.data.data), REGIME_THRESHOLD["^HSI"]);
  }, [showHsi, showHsiRegimes, hsiQuery.data]);

  // Flat and unsourced — all toggles can be on simultaneously (bands may
  // visually overlap, same as the price overlays already can be), and an
  // exact-timestamp collision between two different price series is
  // practically impossible, so no per-segment "which symbol" tag is needed.
  const regimes = useMemo<PriceRegimeSegment[]>(
    () => [...btcRegimes, ...ndxRegimes, ...goldRegimes, ...hsiRegimes],
    [btcRegimes, ndxRegimes, goldRegimes, hsiRegimes]
  );

  const regimeToggles: RegimeToggle[] = [
    {
      id: OVERLAY_DEFS["BTC-USD"].id,
      name: "Cykle BTC (byk/niedźwiedź)",
      overlayName: OVERLAY_DEFS["BTC-USD"].name,
      enabled: showBtcRegimes,
      disabled: !showBtc,
      onToggle: () => setShowBtcRegimes((s) => !s),
    },
    {
      id: OVERLAY_DEFS["^NDX"].id,
      name: "Cykle NASDAQ (byk/niedźwiedź)",
      overlayName: OVERLAY_DEFS["^NDX"].name,
      enabled: showNdxRegimes,
      disabled: !showNdx,
      onToggle: () => setShowNdxRegimes((s) => !s),
    },
    {
      id: OVERLAY_DEFS["GC=F"].id,
      name: "Cykle Złota (byk/niedźwiedź)",
      overlayName: OVERLAY_DEFS["GC=F"].name,
      enabled: showGoldRegimes,
      disabled: !showGold,
      onToggle: () => setShowGoldRegimes((s) => !s),
    },
    {
      id: OVERLAY_DEFS["^HSI"].id,
      name: "Cykle Hang Seng (byk/niedźwiedź)",
      overlayName: OVERLAY_DEFS["^HSI"].name,
      enabled: showHsiRegimes,
      disabled: !showHsi,
      onToggle: () => setShowHsiRegimes((s) => !s),
    },
  ];

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-4 pb-2">
        <div className="space-y-1">
          <CardTitle className="text-base">Historia współczynnika blend (de-risk makro)</CardTitle>
          <p className="text-sm text-muted-foreground">
            Backtest (2011–06.2026, linia przerywana) połączony z danymi na żywo (od 07.2026, linia
            ciągła) — zob. legenda. Kolor linii odzwierciedla wartość blendu w danym punkcie (0 =
            risk-off, 1 = risk-on). Włącz BTC / Nasdaq 100 / Złoto / Hang Seng, by porównać ich
            cenę (prawa oś, skala logarytmiczna) z przebiegiem de-risk.
          </p>
        </div>
        <Button
          variant="outline"
          size="icon"
          onClick={() => setDialogOpen(true)}
          aria-label="Powiększ wykres blend do pełnego ekranu"
          title="Pełny ekran / zoom"
          className="shrink-0"
        >
          <Maximize className="size-4" />
        </Button>
      </CardHeader>
      <CardContent className="space-y-3 pl-0 pr-2">
        {isPending && (
          <p className="px-4 text-xs text-muted-foreground">
            Ładowanie danych na żywo (od 07.2026)…
          </p>
        )}
        {isError && (
          <p role="alert" className="px-4 text-xs text-destructive">
            Błąd ładowania danych na żywo — widoczny jest tylko backtest historyczny.
          </p>
        )}

        <MacroBlendHistoryChartInner
          rows={baseRows}
          instanceId="card"
          overlays={overlays}
          regimes={regimes}
        />

        <div className="px-4">
          <MacroBlendLegend
            showBacktest={showBacktest}
            onToggleBacktest={() => setShowBacktest((s) => !s)}
            overlays={overlayToggles}
            regimeToggles={regimeToggles}
          />
        </div>
      </CardContent>

      <MacroBlendHistoryDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        rows={baseRows}
        showBacktest={showBacktest}
        onToggleBacktest={() => setShowBacktest((s) => !s)}
        overlays={overlays}
        overlayToggles={overlayToggles}
        regimes={regimes}
        regimeToggles={regimeToggles}
      />
    </Card>
  );
}
