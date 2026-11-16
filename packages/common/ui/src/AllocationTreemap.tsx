import { useMemo } from "react";
import { ResponsiveTreeMap } from "@nivo/treemap";
import { exposureGroupOf, formatPln, labelForExposure } from "@common/app-core";

/**
 * Structural subset of each app's own generated `Position` type — enough for
 * the treemap to render without importing any app-specific schema. `exposure`
 * is generic since each app's `Exposure` enum has a different literal value
 * set; the group colour is injected via `colorForExposure`.
 */
export interface PositionLike<E extends string = string> {
  ticker: string;
  name: string;
  groupName?: string | null;
  exposure: E;
  computed?: { valuePln?: number | null } | null;
}

type LeafNode = {
  id: string;
  name: string;
  value: number;
  color: string;
};

type GroupNode = {
  id: string;
  name: string;
  color: string;
  pct: number;
  children: LeafNode[];
};

type RootNode = {
  id: string;
  children: GroupNode[];
};

function parseOklch(s: string): [number, number, number] | null {
  const m = s.match(/oklch\(\s*([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)\s*\)/);
  if (!m) return null;
  return [parseFloat(m[1]!), parseFloat(m[2]!), parseFloat(m[3]!)];
}
function oklchToString([l, c, h]: [number, number, number]): string {
  return `oklch(${l.toFixed(3)} ${c.toFixed(3)} ${h.toFixed(1)})`;
}
function parseHex(s: string): [number, number, number] | null {
  const m = s.match(/^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i);
  if (!m) return null;
  return [parseInt(m[1]!, 16), parseInt(m[2]!, 16), parseInt(m[3]!, 16)];
}
/** Lighter (offset > 0, towards white) or darker (towards black) sRGB shade of a hex colour. */
function shadeHex([r, g, b]: [number, number, number], offset: number): string {
  const target = offset > 0 ? 255 : 0;
  const t = Math.min(Math.abs(offset), 0.45);
  const mix = (c: number) => Math.round(c + (target - c) * t).toString(16).padStart(2, "0");
  return `#${mix(r)}${mix(g)}${mix(b)}`;
}
/** Relative luminance of a hex colour (WCAG). */
function luminance([r, g, b]: [number, number, number]): number {
  const lin = (c: number) => {
    const v = c / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}
/** Dark text on light tiles, light text on dark ones (task 170: group colours include gold). */
function readableLabelColor(fill: string): string {
  const hex = parseHex(fill);
  const light = hex ? luminance(hex) > 0.3 : (parseOklch(fill)?.[0] ?? 0) > 0.7;
  return light ? "#0B0F17" : "#E7ECF3";
}
/** Leaf `idx` of `total` in a group: shades around the group colour (hex or oklch). */
function shadeGeneric(base: string, idx: number, total: number): string {
  if (total <= 1) return base;
  const hex = parseHex(base);
  if (hex) return shadeHex(hex, (idx - (total - 1) / 2) * 0.06);
  const parsed = parseOklch(base);
  if (!parsed) return base;
  const [l, c, h] = parsed;
  const step = 0.07;
  const offset = (idx - (total - 1) / 2) * step;
  const newL = Math.max(0.38, Math.min(0.72, l + offset));
  let newC = c;
  if (total > 3) {
    const chromaStep = 0.018;
    newC = Math.max(0.08, Math.min(0.24, c + (idx % 2 === 0 ? -chromaStep : chromaStep)));
  }
  return oklchToString([newL, newC, h]);
}

function buildTree<E extends string>(
  positions: PositionLike<E>[],
  colorForExposure: (exposure: E | undefined, fallbackIndex: number) => string,
): RootNode {
  const total = positions.reduce((s, p) => s + (p.computed?.valuePln ?? 0), 0);

  const byExposure = new Map<E, PositionLike<E>[]>();
  for (const p of positions) {
    if (!p.computed?.valuePln || p.computed.valuePln <= 0) continue;
    const key = p.exposure;
    byExposure.set(key, [...(byExposure.get(key) ?? []), p]);
  }

  const children: GroupNode[] = Array.from(byExposure.entries()).map(
    ([exposure, items], idx) => {
      const color = colorForExposure(items[0]?.exposure, idx);
      const groupValue = items.reduce((s, p) => s + (p.computed!.valuePln! ?? 0), 0);
      const sorted = [...items].sort((a, b) => (b.computed?.valuePln ?? 0) - (a.computed?.valuePln ?? 0));
      return {
        id: exposure,
        name: labelForExposure(exposure),
        color,
        pct: total > 0 ? (groupValue / total) * 100 : 0,
        children: sorted.map((p, leafIdx) => {
          const leafColor = shadeGeneric(color, leafIdx, sorted.length);
          return {
            id: p.ticker,
            name: p.groupName ?? p.name,
            value: p.computed!.valuePln!,
            pct: total > 0 ? (p.computed!.valuePln! / total) * 100 : 0,
            color: leafColor,
          };
        }),
      };
    },
  );

  return { id: "root", children };
}

type NodeData = {
  id: string;
  name: string;
  value: number;
  pct?: number;
  color: string;
};

function NivoTooltip({ node }: { node: { data: NodeData; value: number } }) {
  return (
    <div className="rounded-md border bg-popover px-3 py-2 text-sm shadow-md text-popover-foreground">
      <p className="font-semibold">{node.data.name}</p>
      <p>{formatPln(node.value, 0)}</p>
      {node.data.pct != null && (
        <p className="text-muted-foreground">{node.data.pct.toFixed(1)}% portfela</p>
      )}
    </div>
  );
}

export function AllocationTreemap<E extends string>({
  positions,
  height = 420,
  colorForExposure,
}: {
  positions: PositionLike<E>[];
  height?: number | string;
  colorForExposure: (exposure: E | undefined, fallbackIndex: number) => string;
}) {
  const data = useMemo(
    () => buildTree(positions, colorForExposure),
    [positions, colorForExposure],
  );

  if (!data.children.length) {
    return (
      <p className="text-muted-foreground text-sm py-8 text-center">
        Brak danych do wyświetlenia.
      </p>
    );
  }

  const h = typeof height === "number" ? `${height}px` : height;

  return (
    <div style={{ height: h, width: "100%" }}>
      <ResponsiveTreeMap
        data={data}
        identity="id"
        value="value"
        valueFormat={(v) => formatPln(v, 0)}
        tile="squarify"
        innerPadding={3}
        outerPadding={6}
        colors={(node) => (node.data as unknown as NodeData).color ?? "#888"}
        colorBy="id"
        enableParentLabel
        parentLabelPosition="top"
        parentLabelPadding={6}
        parentLabelTextColor="#E7ECF3"
        labelSkipSize={12}
        orientLabel
        label={(node) => (node.data as { name?: string }).name ?? node.id}
        labelTextColor={(node) => readableLabelColor((node.data as unknown as NodeData).color ?? "#888888")}
        theme={{
          labels: { text: { fontSize: 14, fontWeight: 700 } },
        }}
        borderWidth={1}
        borderColor={{ from: "color", modifiers: [["darker", 0.8]] }}
        tooltip={({ node }) => <NivoTooltip node={node as never} />}
      />
    </div>
  );
}

/** The leaf shape every app's Asset satisfies (task 156: one leaf per asset). */
export interface TreemapAssetLike {
  key: string;
  name: string;
  exposure: string;
  computed?: { valuePln?: number | null } | null;
}

/** Colour of an exposure group: the same `EXPOSURE_GROUPS` hex as the portfolio ribbon (task 170). */
function exposureGroupColor(exposure: string | undefined): string {
  return exposureGroupOf(exposure).color;
}

/**
 * "Mapa alokacji" of all three apps: one leaf per asset (its `key` is the leaf id, its
 * `computed.valuePln` is Σ subassets), grouped and coloured by exposure group so a group has one
 * colour everywhere (ADR-106 §5).
 */
export function AssetAllocationTreemap({ assets, height }: { assets: readonly TreemapAssetLike[]; height?: number | string }) {
  // Memoized — the treemap rebuilds its tree whenever this array's identity changes.
  const leaves = useMemo(
    () => assets.map((a) => ({ ticker: a.key, name: a.name, exposure: a.exposure, computed: a.computed })),
    [assets],
  );
  return <AllocationTreemap positions={leaves} height={height} colorForExposure={exposureGroupColor} />;
}
