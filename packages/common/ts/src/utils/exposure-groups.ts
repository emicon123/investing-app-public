import { EXPOSURE_LABELS_PL } from "./chart-colors.js";

/**
 * One colour per exposure group, shared by all three apps (task 170, design doc
 * `docs/architecture/diagrams/170-portfolio-redesign.md` §4.2). The portfolio ribbon, filter chips,
 * group swatches and the allocation treemap all read this table, so a group has one colour
 * everywhere.
 *
 * Literal hexes, not CSS custom properties: nivo's treemap writes SVG presentation attributes,
 * where `var()` does not resolve. Labels reuse `EXPOSURE_LABELS_PL`.
 *
 * Owner hexes: CRYPTO, METALS, EQUITY/STOCKS (both "Akcje"), BONDS, EXPERIMENTS. Architect hexes:
 * COMMODITIES (bronze) and CURRENCY (magenta); CASH and QUANTUM (main, unused) share them since
 * neither ever co-occurs with its twin in one app. A new exposure value is one entry here (OCP).
 */
const EXPOSURE_GROUP_COLORS: Readonly<Record<string, string>> = {
  CRYPTO: "#9D8CF8",
  METALS: "#D8B45C",
  EQUITY: "#4DB6C3",
  STOCKS: "#4DB6C3",
  BONDS: "#8297BE",
  EXPERIMENTS: "#6B7588",
  COMMODITIES: "#956725",
  CURRENCY: "#9B46B4",
  CASH: "#9B46B4",
  QUANTUM: "#956725",
};

/** Colour of an exposure value this table does not know yet. */
export const EXPOSURE_FALLBACK_COLOR = "#6B7588";

export interface ExposureGroupSpec {
  /** The raw exposure enum value, e.g. "CRYPTO". */
  exposure: string;
  /** Polish label, e.g. "Krypto"; the raw value when unknown. */
  label: string;
  /** Literal hex colour. */
  color: string;
}

export const EXPOSURE_GROUPS: Readonly<Record<string, ExposureGroupSpec>> = Object.fromEntries(
  Object.entries(EXPOSURE_GROUP_COLORS).map(([exposure, color]) => [
    exposure,
    { exposure, label: EXPOSURE_LABELS_PL[exposure] ?? exposure, color },
  ]),
);

export function exposureGroupOf(exposure: string | null | undefined): ExposureGroupSpec {
  const key = exposure ?? "";
  return (
    EXPOSURE_GROUPS[key] ?? {
      exposure: key,
      label: EXPOSURE_LABELS_PL[key] ?? key,
      color: EXPOSURE_FALLBACK_COLOR,
    }
  );
}
