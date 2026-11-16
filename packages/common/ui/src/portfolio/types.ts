import type { ReactNode } from "react";
import type { PortfolioKpis } from "@common/app-core";

/**
 * Structural contracts of the task-170 portfolio view (design doc
 * `docs/architecture/diagrams/170-portfolio-redesign.md` §2.4). Every app's real `Asset` / `Subasset`
 * satisfies these without a cast; app-specific fields (account splits, effective units) reach
 * only the `renderAccountCell` slot, through the generic `TSub`.
 */

export interface PortfolioSubassetLike {
  id: number;
  isPrimary: boolean;
  ticker: string;
  name: string;
  valuePln?: number | null;
  pricePln?: number | null;
  priceNative?: number | null;
  priceCurrency: string;
}

export interface PortfolioComputedLike {
  valuePln?: number | null;
  currentPct?: number | null;
  deviationPct?: number | null;
  /** main only. Absent in portfolioB/portfolioC (design doc §3.2). */
  effectiveTargetAllocationPct?: number | null;
  pricePln?: number | null;
  priceNative?: number | null;
  /** Group profit as a FRACTION (0.1 = +10 %); null = no recorded cost. */
  profitPct?: number | null;
  avgBuyPricePln?: number | null;
  avgBuyPriceNative?: number | null;
}

export interface PortfolioAssetLike<TSub extends PortfolioSubassetLike = PortfolioSubassetLike> {
  id: number;
  key: string;
  name: string;
  assetClass: string;
  exposure: string;
  targetAllocationPct: number | null;
  computed?: PortfolioComputedLike | null;
  subassets: TSub[];
}

export interface PortfolioHeaderActions {
  editMode: boolean;
  onToggleEdit: () => void;
  onOpenHistory: () => void;
  onOpenTreemap: () => void;
  onAdd: () => void;
}

export interface TargetEditSlot {
  /** `PUT /assets/{id}`, bound per app. Only `targetAllocationPct` is ever sent. */
  update: (id: number, body: { targetAllocationPct: number | null }) => Promise<unknown>;
  /** The container's positions invalidation. */
  onSaved: () => void;
  /** Popover title, default "Cel %"; main MAIN: "Cel % (risk-on)". */
  title?: string;
}

export interface PositionsTableSlots<TAsset, TSub> {
  /** Undefined = SELL is not clickable. main passes it only in edit mode; portfolioB/portfolioC always do. */
  onSellClick?: (asset: TAsset) => void;
  /** Undefined outside edit mode. */
  onEditClick?: (asset: TAsset) => void;
  /** Undefined outside edit mode. */
  onDeleteClick?: (asset: TAsset) => void;
  /** main only (per-row pencil). */
  onEditSubClick?: (sub: TSub) => void;
  /** Undefined outside edit mode. */
  onDeleteSubClick?: (sub: TSub) => void;
  /** Undefined outside edit mode. */
  onSetPrimaryClick?: (sub: TSub) => void;
  settingPrimaryId?: number | null;
  targetEdit: TargetEditSlot;
  renderAccountCell: (sub: TSub, assetClass: string) => ReactNode;
  /** Exposure values whose group starts collapsed. main MAIN: ["EXPERIMENTS"]; all others: []. */
  defaultCollapsedExposures?: readonly string[];
}

export interface PortfolioPositionsState<TAsset> {
  data: TAsset[];
  isPending: boolean;
  isError: boolean;
}

export interface PortfolioViewProps<
  TAsset extends PortfolioAssetLike<TSub>,
  TSub extends PortfolioSubassetLike,
> {
  /** "Portfolio" in every app. */
  title: string;
  /** main: "Main" / "Emerytura"; portfolioC: the book label; portfolioB: none. */
  bookTag?: string;
  /** main MAIN: the bot status badge. */
  headerStatus?: ReactNode;
  positions: PortfolioPositionsState<TAsset>;
  kpis: PortfolioKpis;
  actions: PortfolioHeaderActions;
  table: PositionsTableSlots<TAsset, TSub>;
  /** Rendered after the table, inside the 1400 px column (main MAIN: `MacroRegimePanel`). */
  children?: ReactNode;
}
