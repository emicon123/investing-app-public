import { z } from "zod";
import {
  currencySchema,
  fxRateSourceSchema,
  portfolioTypeSchema,
  accountTypeSchema,
  tradeTypeSchema,
} from "./enums.js";
import { apiResponseSchema, metaSchema } from "@common/app-core";

/** `GET /market-data/prices` (cache-only, never triggers an upstream fetch). Mirrors PriceEntry. */
export const priceEntrySchema = z.object({
  ticker: z.string(),
  price: z.number(),
  currency: currencySchema,
  pricePln: z.number().nullish().transform((v) => v ?? null),
  fetchedAt: z.string().datetime({ offset: true }),
});

export const priceListResponseSchema = apiResponseSchema(z.array(priceEntrySchema));

export type PriceEntry = z.infer<typeof priceEntrySchema>;
export type PriceListResponse = z.infer<typeof priceListResponseSchema>;

export const fxRateEntrySchema = z.object({
  fromCurrency: currencySchema,
  rateToPln: z.number(),
  source: fxRateSourceSchema,
  fetchedAt: z.string().datetime({ offset: true }),
});

export const fxRateListResponseSchema = apiResponseSchema(z.array(fxRateEntrySchema));

export type FxRateEntry = z.infer<typeof fxRateEntrySchema>;
export type FxRateListResponse = z.infer<typeof fxRateListResponseSchema>;

export const regressionPointSchema = z.object({
  date: z.string(),
  count: z.number(),
  close: z.number().nullable().optional().transform((v) => v ?? null),
  high: z.number().nullable().optional().transform((v) => v ?? null),
  low: z.number().nullable().optional().transform((v) => v ?? null),
  logN: z.number().nullable().optional().transform((v) => v ?? null),
  trendline: z.number(),
  lnTrendline: z.number(),
  residual: z.number().nullable().optional().transform((v) => v ?? null),
  sd1plus: z.number(),
  sd2plus: z.number(),
  sd1minus: z.number(),
  sd2minus: z.number(),
  sd3plus: z.number(),
  sd3minus: z.number(),
  sd4plus: z.number(),
  sd4minus: z.number(),
});

export const linearRegressionResponseSchema = z.object({
  ticker: z.string(),
  interval: z.string(),
  slope: z.number(),
  intercept: z.number(),
  stdDev: z.number(),
  dataPoints: z.array(regressionPointSchema),
  meta: z.object({ requestId: z.string(), timestamp: z.string() }),
});

export type RegressionPoint = z.infer<typeof regressionPointSchema>;
export type LinearRegressionResponse = z.infer<typeof linearRegressionResponseSchema>;

// --- Macro liquidity de-risk state (ADR-042, continuous blend since ADR-044) ---
// Mirrors MacroState / MacroComponentStatus / MacroStateResponse in docs/openapi.yaml.
// This package is hand-authored (not codegen'd), so these are kept in sync manually.

/**
 * Macro source codes the backend can emit, mirroring the `macro_observations_source_check`
 * constraint. `TRADINGEC`/`YCHARTS` are ADR-068's scraped China-M2 tip sources.
 */
export const MACRO_SERIES_SOURCES = [
  "FRED",
  "ECB",
  "BOE",
  "BOJ",
  "CHINADATA",
  "TRADINGEC",
  "PBC",
  "YCHARTS",
  "MANUAL",
  "DERIVED",
] as const;

/**
 * Deliberately permissive: a source code this package has not heard of yet must NOT fail the
 * whole `/market-data/macro-state` parse. This list is a hand-synced mirror of a backend
 * constraint, and when ADR-068 added `TRADINGEC` server-side without updating it here, the
 * strict `z.enum` rejected the response and the entire macro panel rendered "Błąd ładowania
 * stanu makro" — even though the data behind it was perfectly good. `source` is display
 * metadata (rendered as-is, plus one `=== "MANUAL"` comparison), so an unrecognised code should
 * degrade to showing that code, not to blanking the panel. Keep {@link MACRO_SERIES_SOURCES}
 * up to date for documentation and autocomplete; correctness no longer depends on it.
 */
export const macroSeriesSourceSchema = z.enum(MACRO_SERIES_SOURCES).or(z.string());

export const macroComponentStatusSchema = z.object({
  seriesCode: z.string(),
  source: macroSeriesSourceSchema,
  latestObservationDate: z.string(),
  latestValue: z.number(),
  stale: z.boolean(),
});

export const macroZoneSchema = z.enum([
  "RISK_ON",
  "MID_ON",
  "NEUTRAL",
  "MID_OFF",
  "RISK_OFF",
]);

export const macroStateSchema = z.object({
  asOfDate: z.string(),
  blendFactor: z.number().nullable().optional().transform((v) => v ?? null),
  zone: macroZoneSchema.nullable().optional().transform((v) => v ?? null),
  compositeZ: z.number().nullable().optional().transform((v) => v ?? null),
  smoothedZ: z.number().nullable().optional().transform((v) => v ?? null),
  riskOff: z.boolean(),
  deriskEnabled: z.boolean(),
  netLiqLatestUsdMln: z.number().nullable().optional().transform((v) => v ?? null),
  netLiqYoy: z.number().nullable().optional().transform((v) => v ?? null),
  netLiqYoyAccel: z.number().nullable().optional().transform((v) => v ?? null),
  netLiqRiskOff: z.boolean().nullable().optional().transform((v) => v ?? null),
  globalM2LatestUsdMln: z.number().nullable().optional().transform((v) => v ?? null),
  globalM2Yoy: z.number().nullable().optional().transform((v) => v ?? null),
  globalM2YoyAccel: z.number().nullable().optional().transform((v) => v ?? null),
  globalM2RiskOff: z.boolean().nullable().optional().transform((v) => v ?? null),
  globalM2Stale: z.boolean(),
  components: z.array(macroComponentStatusSchema),
  computedAt: z.string(),
});

export const macroStateResponseSchema = apiResponseSchema(macroStateSchema);

export type MacroSeriesSource = z.infer<typeof macroSeriesSourceSchema>;
export type MacroComponentStatus = z.infer<typeof macroComponentStatusSchema>;
export type MacroZone = z.infer<typeof macroZoneSchema>;
export type MacroState = z.infer<typeof macroStateSchema>;
export type MacroStateResponse = z.infer<typeof macroStateResponseSchema>;

/**
 * Minimal chart-shape projection of one macro_state row (ADR-069). Deliberately smaller than
 * {@link macroStateSchema} — backs the Settings page's blend-factor history chart, not the full
 * current-state panel.
 */
export const macroStateHistoryPointSchema = z.object({
  asOfDate: z.string(),
  blendFactor: z.number().nullable().optional().transform((v) => v ?? null),
  zone: macroZoneSchema.nullable().optional().transform((v) => v ?? null),
});

export const macroStateHistoryListResponseSchema = apiResponseSchema(
  z.array(macroStateHistoryPointSchema),
);

export type MacroStateHistoryPoint = z.infer<typeof macroStateHistoryPointSchema>;
export type MacroStateHistoryListResponse = z.infer<typeof macroStateHistoryListResponseSchema>;

/**
 * One weekly close of a reference index for the blend-chart overlay (ADR-070):
 * BTC-USD, ^NDX (Nasdaq 100), GC=F (COMEX Gold futures), or ^HSI (Hang Seng).
 * `date` is a YYYY-MM-DD string, `close` the raw weekly close — the frontend
 * log-scales + normalizes it against the visible window, so no math is done
 * server-side.
 */
export const benchmarkHistoryPointSchema = z.object({
  symbol: z.string(),
  date: z.string(),
  close: z.number(),
});

export const benchmarkHistoryListResponseSchema = apiResponseSchema(
  z.array(benchmarkHistoryPointSchema),
);

export type BenchmarkHistoryPoint = z.infer<typeof benchmarkHistoryPointSchema>;
export type BenchmarkHistoryListResponse = z.infer<typeof benchmarkHistoryListResponseSchema>;

// --- Trade-History Chart (ADR-057) ---
// Mirrors TradeChart* schemas in docs/openapi/components/schemas/trade-chart.yaml.
// This package is hand-authored (not codegen'd), so these are kept in sync manually.

export const tradeChartTickerEntrySchema = z.object({
  ticker: z.string(),
  name: z.string(),
  yahooTicker: z.string(),
  priceCurrency: currencySchema,
  tradeCount: z.number(),
  firstTradeAt: z.string(),
  lastTradeAt: z.string(),
});

export const tradeChartExcludedReasonSchema = z.enum([
  "NO_POSITION_MATCH",
  "MISSING_YAHOO_TICKER",
]);

export const excludedTradeChartTickerSchema = z.object({
  ticker: z.string(),
  reason: tradeChartExcludedReasonSchema,
});

export const tradeChartTickersResponseSchema = z.object({
  data: z.array(tradeChartTickerEntrySchema),
  excluded: z.array(excludedTradeChartTickerSchema),
  meta: metaSchema,
});

export const tradeChartPricePointSchema = z.object({
  date: z.string(),
  open: z.number(),
  close: z.number(),
  high: z.number(),
  low: z.number(),
});

export const tradeChartMarkerSchema = z.object({
  id: z.number(),
  purchasedAt: z.string(),
  tradeType: tradeTypeSchema,
  portfolio: portfolioTypeSchema,
  accountType: accountTypeSchema,
  unitsDelta: z.number(),
  pricePln: z.number(),
  priceNative: z.number(),
  totalPln: z.number(),
  sessionId: z.number().nullable().optional().transform((v) => v ?? null),
});

export const tradeChartDataSchema = z.object({
  ticker: z.string(),
  yahooTicker: z.string(),
  priceCurrency: currencySchema,
  interval: z.string(),
  priceSeries: z.array(tradeChartPricePointSchema),
  trades: z.array(tradeChartMarkerSchema),
});

export const tradeChartResponseSchema = apiResponseSchema(tradeChartDataSchema);

export type TradeChartTickerEntry = z.infer<typeof tradeChartTickerEntrySchema>;
export type TradeChartExcludedReason = z.infer<typeof tradeChartExcludedReasonSchema>;
export type ExcludedTradeChartTicker = z.infer<typeof excludedTradeChartTickerSchema>;
export type TradeChartTickersResponse = z.infer<typeof tradeChartTickersResponseSchema>;
export type TradeChartPricePoint = z.infer<typeof tradeChartPricePointSchema>;
export type TradeChartMarker = z.infer<typeof tradeChartMarkerSchema>;
export type TradeChartData = z.infer<typeof tradeChartDataSchema>;
export type TradeChartResponse = z.infer<typeof tradeChartResponseSchema>;
