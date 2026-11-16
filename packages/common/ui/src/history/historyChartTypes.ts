/**
 * Structural contract for the shared history chart. Deliberately app-agnostic:
 * every field is satisfied by all three apps' Zod-inferred types without
 * importing anything from an app's source tree.
 */
export interface HistoriaEntryLike {
  snapshotDate: string;
  totalDeposits: number;
  portfolioValue: number;
  xirr?: number | null;
  returnPct: number;
  profitPln: number;
  positionValues?: Record<string, number> | null;
  inflationAdjustedDeposits?: number | null;
  realReturnPct?: number | null;
  annualizedInflationPct?: number | null;
  /** portfolioC only; ignored by the chart. String, not a literal union. */
  portfolio?: string;
}

/**
 * One row of the positions list, used only to map a snapshot's per-ticker
 * `positionValues` to an exposure colour. Current apps pass ASSETS (no own
 * `ticker`; their concrete tickers are the `subassets`).
 */
export interface WallPositionLike {
  /** Legacy pre-task-156 main shape; no current producer. */
  ticker?: string;
  exposure: string;
  subassets?: { ticker: string }[] | null;
}

export interface BlendPointLike {
  asOfDate: string;
  blendFactor?: number | null;
}
