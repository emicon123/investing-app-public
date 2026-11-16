import { exposureGroupOf, formatCurrency } from "@common/app-core";
import type { PortfolioAssetLike, PortfolioSubassetLike } from "../types.js";
import {
  STATUS_LABEL,
  STATUS_SEVERITY,
  deviationStatus,
  effectiveTargetPct,
  formatRebalanceFigure,
  groupMeterStatus,
  type RebalanceStatus,
} from "./rebalanceStatus.js";
import { computeTargetMeter, targetMeterLabels, type TargetMeterGeometry, type TargetMeterLabels } from "./targetMeter.js";
import {
  assetClassLabel,
  displayTicker,
  formatPositionCount,
  formatPp1,
  formatPricePln,
  formatShare,
  formatSignedPct,
  formatSignedZl,
  formatZl,
} from "./portfolioFormat.js";

/**
 * The positions view model (task 170, design doc §3.3, §4, §5.3, §6): every figure the grouped
 * table, ribbon, chips and footer show, computed once from the app's assets. Pure; the components
 * only render it (Law of Demeter: rows never reach into `asset.computed`).
 */

type AnyAsset = PortfolioAssetLike<PortfolioSubassetLike>;

export type ProfitTone = "gain" | "loss" | "flat" | "none";

export interface ProfitFigures {
  /** Group profit FRACTION, null = unknown. */
  profitPct: number | null;
  /** P/L in PLN, null = unknown. */
  plPln: number | null;
  /** Cost basis in PLN, null = unknown. */
  costPln: number | null;
  /** "+34,2%" or "—". */
  profitLabel: string;
  /** "+12 563 zł", or "brak kosztu" / "brak jednostek". */
  plLabel: string;
  tone: ProfitTone;
}

export interface PositionRowModel<TAsset> {
  asset: TAsset;
  id: number;
  name: string;
  exposure: string;
  assetClass: string;
  /** Primary subasset's ticker (else the asset key), the icon resolver's input. */
  iconTicker: string;
  /** "BTC • Krypto". */
  caption: string;
  price: string;
  entry: string;
  /** Native-currency price and entry, the cell's tooltip; undefined for a PLN primary. */
  priceTitle: string | undefined;
  valuePln: number;
  value: string;
  shareLabel: string;
  currentPct: number;
  effectivePct: number | null;
  basePct: number | null;
  status: RebalanceStatus;
  /** The "Rebalansowanie" figure: SELL amount or p.p. deviation. */
  figure: string;
  meter: TargetMeterGeometry;
  meterLabels: TargetMeterLabels;
  profit: ProfitFigures;
}

export interface GroupSummary {
  status: RebalanceStatus;
  label: string;
  /** "×9"; null for OK. */
  countLabel: string | null;
}

export interface PositionGroupModel<TAsset> {
  exposure: string;
  label: string;
  color: string;
  /** Every row of the group, value descending. */
  rows: PositionRowModel<TAsset>[];
  valuePln: number;
  value: string;
  currentPct: number;
  shareLabel: string;
  /** Σ effective targets; null when no row has a target. */
  effectivePct: number | null;
  meter: TargetMeterGeometry;
  meterLabels: TargetMeterLabels;
  profit: ProfitFigures;
  summary: GroupSummary;
}

export type RibbonDeltaTone = "over" | "under" | "flat";

export interface RibbonSegment {
  exposure: string;
  label: string;
  color: string;
  /** Width of the "Udział" segment, % of the bar. */
  currentWidthPct: number;
  /** Width of the "Cel" segment, normalised so the bar always fills. */
  targetWidthPct: number;
  currentLabel: string;
  targetLabel: string;
  delta: string;
  deltaTone: RibbonDeltaTone;
  title: string;
}

export type TargetSumTone = "ok" | "near" | "off";

export interface TargetSum {
  /** Σ base targets over the whole book, rounded to 1 decimal. */
  value: number;
  label: string;
  tone: TargetSumTone;
}

export interface PositionsModel<TAsset> {
  rows: PositionRowModel<TAsset>[];
  groups: PositionGroupModel<TAsset>[];
  totalPln: number;
  /** Some asset's effective target differs from its base target (main MAIN under the macro gate). */
  hasEffectiveOverride: boolean;
  sellCount: number;
  ribbon: RibbonSegment[];
  targetSum: TargetSum;
}

const EFFECTIVE_OVERRIDE_MIN_DIFF = 0.05;
const RIBBON_DELTA_FLAT_PP = 0.5;
const plCompare = (a: string, b: string): number => a.localeCompare(b, "pl");

export function primaryOf<TSub extends PortfolioSubassetLike>(asset: PortfolioAssetLike<TSub>): TSub | undefined {
  return asset.subassets.find((s) => s.isPrimary) ?? asset.subassets[0];
}

/** Case-insensitive match on the asset's key and name and every subasset's ticker and name ("IGLN" finds Gold). */
export function matchesAssetQuery(asset: AnyAsset, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (q === "") return true;
  const hit = (text: string): boolean => text.toLowerCase().includes(q);
  return hit(asset.key) || hit(asset.name) || asset.subassets.some((s) => hit(s.ticker) || hit(s.name));
}

function toneOf(x: number): ProfitTone {
  if (x > 0) return "gain";
  if (x < 0) return "loss";
  return "flat";
}

const UNKNOWN_PROFIT = { profitPct: null, plPln: null, costPln: null, profitLabel: "—", tone: "none" } as const;

/** Row P/L: cost = value / (1 + profit), defined only for a known profit above −100 %. */
function rowProfit(valuePln: number, profitPct: number | null): ProfitFigures {
  if (profitPct == null) {
    return { ...UNKNOWN_PROFIT, plLabel: valuePln === 0 ? "brak jednostek" : "brak kosztu" };
  }
  const known = profitPct > -1;
  const costPln = known ? valuePln / (1 + profitPct) : null;
  const plPln = costPln != null ? valuePln - costPln : null;
  return {
    profitPct,
    plPln,
    costPln,
    profitLabel: formatSignedPct(profitPct, 1),
    plLabel: plPln != null ? formatSignedZl(plPln) : "—",
    tone: toneOf(profitPct),
  };
}

/** Group P/L % = Σ P/L ÷ Σ cost over the rows whose cost is known (design doc §14 D5). */
function groupProfit<TAsset>(rows: PositionRowModel<TAsset>[]): ProfitFigures {
  let plPln = 0;
  let costPln = 0;
  let known = 0;
  for (const row of rows) {
    if (row.profit.plPln != null && row.profit.costPln != null) {
      plPln += row.profit.plPln;
      costPln += row.profit.costPln;
      known += 1;
    }
  }
  if (known === 0 || costPln <= 0) return { ...UNKNOWN_PROFIT, plLabel: "brak kosztu" };
  const profitPct = plPln / costPln;
  return {
    profitPct,
    plPln,
    costPln,
    profitLabel: formatSignedPct(profitPct, 1),
    plLabel: formatSignedZl(plPln),
    tone: toneOf(plPln),
  };
}

function priceTitleOf(asset: AnyAsset, currency: string): string | undefined {
  if (currency === "PLN") return undefined;
  const parts: string[] = [];
  const priceNative = asset.computed?.priceNative;
  const entryNative = asset.computed?.avgBuyPriceNative;
  if (priceNative != null) parts.push(`Cena ${formatCurrency(priceNative, currency)}`);
  if (entryNative != null) parts.push(`wejście ≈ ${formatCurrency(entryNative, currency)} (po dzisiejszym kursie)`);
  return parts.length > 0 ? parts.join(" · ") : undefined;
}

function buildRow<TAsset extends AnyAsset>(asset: TAsset, totalPln: number): PositionRowModel<TAsset> {
  const computed = asset.computed;
  const primary = primaryOf(asset);
  const valuePln = computed?.valuePln ?? 0;
  const currentPct = computed?.currentPct ?? 0;
  const effectivePct = effectiveTargetPct(asset);
  const basePct = asset.targetAllocationPct;
  const status = deviationStatus(computed?.deviationPct);
  const pricePln = computed?.pricePln;
  const entryPln = computed?.avgBuyPricePln;
  const iconTicker = primary?.ticker ?? asset.key;

  return {
    asset,
    id: asset.id,
    name: asset.name,
    exposure: asset.exposure,
    assetClass: asset.assetClass,
    iconTicker,
    caption: `${displayTicker(iconTicker)} • ${assetClassLabel(asset.assetClass)}`,
    price: pricePln != null ? formatPricePln(pricePln) : "—",
    entry: entryPln != null ? `wejście ${formatPricePln(entryPln)}` : "brak wejścia",
    priceTitle: priceTitleOf(asset, primary?.priceCurrency ?? "PLN"),
    valuePln,
    value: formatZl(valuePln),
    shareLabel: `${formatShare(currentPct)} portfela`,
    currentPct,
    effectivePct,
    basePct,
    status,
    figure: formatRebalanceFigure({ status, valuePln, currentPct, effectivePct, totalPln }),
    meter: computeTargetMeter({ currentPct, effectivePct, basePct, status }),
    meterLabels: targetMeterLabels(currentPct, effectivePct, basePct),
    profit: rowProfit(valuePln, computed?.profitPct ?? null),
  };
}

function sumOrNull(values: (number | null)[]): number | null {
  const known = values.filter((v): v is number => v != null);
  return known.length > 0 ? known.reduce((a, b) => a + b, 0) : null;
}

function groupSummary<TAsset>(rows: PositionRowModel<TAsset>[]): GroupSummary {
  const status = STATUS_SEVERITY.find((s) => rows.some((r) => r.status === s)) ?? "OK";
  const count = rows.filter((r) => r.status === status).length;
  return { status, label: STATUS_LABEL[status], countLabel: status === "OK" ? null : `×${count}` };
}

function buildGroup<TAsset>(exposure: string, rows: PositionRowModel<TAsset>[]): PositionGroupModel<TAsset> {
  const spec = exposureGroupOf(exposure);
  const valuePln = rows.reduce((a, r) => a + r.valuePln, 0);
  const currentPct = rows.reduce((a, r) => a + r.currentPct, 0);
  const effectivePct = sumOrNull(rows.map((r) => r.effectivePct));
  const basePct = sumOrNull(rows.map((r) => r.basePct));
  const status = effectivePct != null ? groupMeterStatus(currentPct, effectivePct) : "OK";
  return {
    exposure,
    label: spec.label,
    color: spec.color,
    rows,
    valuePln,
    value: formatZl(valuePln),
    currentPct,
    shareLabel: `${formatShare(currentPct)} portfela`,
    effectivePct,
    meter: computeTargetMeter({ currentPct, effectivePct, basePct, status }),
    meterLabels: targetMeterLabels(currentPct, effectivePct, basePct),
    profit: groupProfit(rows),
    summary: groupSummary(rows),
  };
}

function byValueThenName<TAsset>(a: PositionRowModel<TAsset>, b: PositionRowModel<TAsset>): number {
  return b.valuePln - a.valuePln || plCompare(a.name, b.name);
}

function byValueThenLabel<TAsset>(a: PositionGroupModel<TAsset>, b: PositionGroupModel<TAsset>): number {
  return b.valuePln - a.valuePln || plCompare(a.label, b.label);
}

function ribbonDeltaTone(delta: number): RibbonDeltaTone {
  if (delta > RIBBON_DELTA_FLAT_PP) return "over";
  if (delta < -RIBBON_DELTA_FLAT_PP) return "under";
  return "flat";
}

function buildRibbon<TAsset>(groups: PositionGroupModel<TAsset>[]): RibbonSegment[] {
  const targetTotal = groups.reduce((a, g) => a + (g.effectivePct ?? 0), 0);
  return groups.map((g) => {
    const target = g.effectivePct ?? 0;
    const delta = g.currentPct - target;
    return {
      exposure: g.exposure,
      label: g.label,
      color: g.color,
      currentWidthPct: g.currentPct,
      targetWidthPct: targetTotal > 0 ? (target / targetTotal) * 100 : 0,
      currentLabel: formatShare(g.currentPct),
      targetLabel: `cel ${formatShare(target)}`,
      delta: formatPp1(delta),
      deltaTone: ribbonDeltaTone(delta),
      title: `${g.label}: ${formatShare(g.currentPct)}, cel ${formatShare(target)}`,
    };
  });
}

/** Footer: Σ base targets over the WHOLE book, never the filtered rows (design doc §5.3, §14 D4). */
function buildTargetSum(assets: AnyAsset[], hasEffectiveOverride: boolean): TargetSum {
  const value = Math.round(assets.reduce((a, x) => a + (x.targetAllocationPct ?? 0), 0) * 10) / 10;
  const diff = Math.abs(value - 100);
  const tone: TargetSumTone = diff < 0.1 ? "ok" : diff < 2 ? "near" : "off";
  const prefix = hasEffectiveOverride ? "Suma celów bazowych" : "Suma celów";
  return { value, tone, label: `${prefix} ${formatShare(value)}${tone === "ok" ? " ✓" : ""}` };
}

export function buildPositionsModel<TAsset extends AnyAsset>(assets: readonly TAsset[]): PositionsModel<TAsset> {
  const totalPln = assets.reduce((a, x) => a + (x.computed?.valuePln ?? 0), 0);
  const rows = assets.map((a) => buildRow(a, totalPln)).sort(byValueThenName);

  const byExposure = new Map<string, PositionRowModel<TAsset>[]>();
  for (const row of rows) {
    const bucket = byExposure.get(row.exposure);
    if (bucket) bucket.push(row);
    else byExposure.set(row.exposure, [row]);
  }
  const groups = [...byExposure].map(([exposure, groupRows]) => buildGroup(exposure, groupRows)).sort(byValueThenLabel);

  const hasEffectiveOverride = rows.some(
    (r) => Math.abs((r.basePct ?? 0) - (r.effectivePct ?? 0)) > EFFECTIVE_OVERRIDE_MIN_DIFF,
  );

  return {
    rows,
    groups,
    totalPln,
    hasEffectiveOverride,
    sellCount: rows.filter((r) => r.status === "SELL").length,
    ribbon: buildRibbon(groups),
    targetSum: buildTargetSum([...assets], hasEffectiveOverride),
  };
}

// ── Filtering (design doc §6) ─────────────────────────────────────────────────────────────

/** "all", "sell", or one exposure value (exposure enums are upper-case, so they never collide). */
export type PositionsFilter = string;
export const FILTER_ALL = "all";
export const FILTER_SELL = "sell";

export interface FilterChip {
  id: PositionsFilter;
  label: string;
  count: number;
  /** Swatch colour; null for "Wszystkie". */
  color: string | null;
  /** Round swatch (the "Sprzedaj" dot) instead of a square one. */
  round: boolean;
}

export function buildFilterChips<TAsset>(model: PositionsModel<TAsset>): FilterChip[] {
  return [
    { id: FILTER_ALL, label: "Wszystkie", count: model.rows.length, color: null, round: false },
    ...model.groups.map((g) => ({ id: g.exposure, label: g.label, count: g.rows.length, color: g.color, round: false })),
    { id: FILTER_SELL, label: "Sprzedaj", count: model.sellCount, color: "var(--loss)", round: true },
  ];
}

export interface VisibleGroup<TAsset> {
  group: PositionGroupModel<TAsset>;
  /** Rows matching the filter and query (all rows while browsing). */
  rows: PositionRowModel<TAsset>[];
  /** "13 pozycji", or "2 z 13 pozycji" while filtering hides some. */
  countLabel: string;
}

export interface FilteredPositions<TAsset> {
  isFiltering: boolean;
  groups: VisibleGroup<TAsset>[];
}

export function isFilteringActive(filter: PositionsFilter, query: string): boolean {
  return filter !== FILTER_ALL || query.trim() !== "";
}

function rowMatchesFilter<TAsset>(row: PositionRowModel<TAsset>, filter: PositionsFilter): boolean {
  if (filter === FILTER_ALL) return true;
  if (filter === FILTER_SELL) return row.status === "SELL";
  return row.exposure === filter;
}

/** Chip AND query; while filtering, groups without a match are hidden. */
export function applyPositionsFilter<TAsset extends AnyAsset>(
  model: PositionsModel<TAsset>,
  filter: PositionsFilter,
  query: string,
): FilteredPositions<TAsset> {
  const isFiltering = isFilteringActive(filter, query);
  const groups: VisibleGroup<TAsset>[] = [];
  for (const group of model.groups) {
    const rows = group.rows.filter((r) => rowMatchesFilter(r, filter) && matchesAssetQuery(r.asset, query));
    if (isFiltering && rows.length === 0) continue;
    const total = group.rows.length;
    const prefix = isFiltering && rows.length !== total ? `${rows.length} z ` : "";
    groups.push({ group, rows, countLabel: `${prefix}${formatPositionCount(total)}` });
  }
  return { isFiltering, groups };
}
