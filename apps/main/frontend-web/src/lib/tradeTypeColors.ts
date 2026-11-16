import type { TradeType } from "../api/index.js";
import { EXPOSURE_COLORS, FALLBACK_PALETTE } from "./chartColors";

export const TRADE_TYPE_COLORS: Record<TradeType, string> = {
  DCA_BUY: EXPOSURE_COLORS.CASH,
  MIGRATION_BUY: "oklch(0.66 0.15 165)",
  REBALANCING_REINVEST_BUY: "oklch(0.62 0.18 250)",
  MIGRATION_SELL: FALLBACK_PALETTE[6]!,
  REBALANCING_TRIM_SELL: "oklch(0.66 0.19 45)",
  SEED_IMPORT: "oklch(0.55 0.01 280)",
};

export const TRADE_TYPE_LABELS: Record<TradeType, string> = {
  DCA_BUY: "Regularny zakup (DCA)",
  MIGRATION_BUY: "Zakup migracyjny",
  REBALANCING_REINVEST_BUY: "Reinwestycja (rebalancing)",
  MIGRATION_SELL: "Sprzedaż migracyjna",
  REBALANCING_TRIM_SELL: "Przycięcie pozycji (rebalancing)",
  SEED_IMPORT: "Import początkowy",
};

/** Buy/sell/neutral, driven purely off unitsDelta's sign — same convention asset_purchases uses. */
export function isBuy(unitsDelta: number): boolean {
  return unitsDelta >= 0;
}

const MIN_DOT_RADIUS = 4;
const MAX_DOT_RADIUS = 16;

/**
 * Dot radius for a trade marker, scaled so the dot's *area* (not radius) is
 * proportional to trade size — a straight radius-linear scale would let the
 * largest trades visually dwarf everything else (Saylor-chart reference).
 */
export function tradeDotRadius(absTotalPln: number, maxAbsTotalPln: number): number {
  if (!(maxAbsTotalPln > 0) || !(absTotalPln >= 0)) return MIN_DOT_RADIUS;
  const ratio = Math.min(1, absTotalPln / maxAbsTotalPln);
  return MIN_DOT_RADIUS + (MAX_DOT_RADIUS - MIN_DOT_RADIUS) * Math.sqrt(ratio);
}
