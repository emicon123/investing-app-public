// investing-app's oklch chart tokens (FALLBACK_PALETTE, ASSET_PALETTE, EXPOSURE_BASE,
// BENCHMARK_COLORS, REGIME_FILL/SWATCH, REGRESSION_BAND_COLORS) plus the shared Polish exposure
// labels. The portfolio/treemap exposure-group colours live in `exposure-groups.ts` (task 170,
// ADR-106); the old portfolioB/portfolioC neon EXPOSURE_COLORS/ASSET_CLASS_COLORS palette had no consumer
// left and was removed. The history-chart palette (CSS-var based) lives in
// packages/common/ui/src/lib/historyChartColors.ts.

export const FALLBACK_PALETTE = [
  "oklch(0.58 0.24 295)",
  "oklch(0.68 0.18 148)",
  "oklch(0.48 0.22 322)",
  "oklch(0.62 0.17 145)",
  "oklch(0.62 0.015 260)",
  "oklch(0.62 0.16 75)",
  "oklch(0.60 0.20 20)",
  "oklch(0.50 0.18 310)",
];

export const ASSET_PALETTE = [
  "oklch(0.58 0.24 295)",
  "oklch(0.68 0.18 148)",
  "oklch(0.48 0.22 322)",
  "oklch(0.62 0.17 145)",
  "oklch(0.62 0.015 260)",
  "oklch(0.62 0.16 75)",
  "oklch(0.60 0.20 20)",
  "oklch(0.55 0.14 130)",
  "oklch(0.50 0.18 310)",
  "oklch(0.65 0.15 250)",
  "oklch(0.62 0.20 350)",
  "oklch(0.58 0.18 180)",
];

export const EXPOSURE_BASE: Record<string, string> = {
  CRYPTO: "oklch(0.62 0.16 75)",
  METALS: "oklch(0.62 0.015 260)",
  CASH: "oklch(0.68 0.18 148)",
  QUANTUM: "oklch(0.58 0.24 295)",
  BONDS: "oklch(0.50 0.16 265)",
  EXPERIMENTS: "oklch(0.48 0.22 322)",
  EQUITY: "oklch(0.62 0.17 145)",
  STOCKS: "oklch(0.62 0.17 145)",
  COMMODITIES: "oklch(0.43 0.12 45)",
  CURRENCY: "oklch(0.68 0.18 148)",
};

// Polish display labels for exposure enum values — unified superset across all 3 apps
// (unlike the color tokens above, which are deliberately split per app; see
// docs/architecture/12-portfolio-bpps-and-shared-packages.md §2.1). EQUITY/STOCKS both map
// to "Akcje" since investing-app uses EQUITY while portfolioB/portfolioC use STOCKS for the same concept.
export const EXPOSURE_LABELS_PL: Record<string, string> = {
  CRYPTO: "Krypto",
  METALS: "Metale",
  CASH: "Gotówka",
  QUANTUM: "Quantum",
  BONDS: "Obligacje",
  EXPERIMENTS: "Eksperymenty",
  EQUITY: "Akcje",
  STOCKS: "Akcje",
  COMMODITIES: "Surowce",
  CURRENCY: "Waluta",
};

export const BENCHMARK_COLORS: Record<string, string> = {
  "BTC-USD": "oklch(0.62 0.16 75)",
  "^NDX": "oklch(0.76 0.12 235)",
  "GC=F": "oklch(0.70 0.13 85)",
  "^HSI": "oklch(0.60 0.20 20)",
};

export const REGIME_FILL: Record<string, string> = {
  bear: "oklch(0.55 0.16 25 / 0.16)",
  bull: "oklch(0.65 0.15 145 / 0.16)",
};

export const REGIME_SWATCH: Record<string, string> = {
  bear: "oklch(0.55 0.16 25 / 0.9)",
  bull: "oklch(0.65 0.15 145 / 0.9)",
};

export const REGRESSION_BAND_COLORS = {
  sd1: "#22c55e",
  sd2: "#eab308",
  sd3: "#f97316",
  sd4: "#ef4444",
  trend: "#f97316",
  price: "#3b82f6",
} as const;

export function labelForExposure(exposure: string | undefined, fallback: string = exposure ?? ""): string {
  if (exposure && exposure in EXPOSURE_LABELS_PL) {
    return EXPOSURE_LABELS_PL[exposure]!;
  }
  return fallback;
}
