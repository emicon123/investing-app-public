import { apiClient } from "@common/app-core";
import {
  benchmarkHistoryListResponseSchema,
  fxRateListResponseSchema,
  priceListResponseSchema,
  linearRegressionResponseSchema,
  macroStateResponseSchema,
  macroStateHistoryListResponseSchema,
  tradeChartTickersResponseSchema,
  tradeChartResponseSchema,
  type BenchmarkHistoryListResponse,
  type FxRateListResponse,
  type PriceListResponse,
  type LinearRegressionResponse,
  type MacroStateResponse,
  type MacroStateHistoryListResponse,
  type TradeChartTickersResponse,
  type TradeChartResponse,
} from "../schemas/index.js";

/**
 * Cached prices with their `fetchedAt` (the portfolio header's "Ceny z …", task 170). Reads the
 * price cache only; refreshing is `refreshPrices`.
 */
export async function getPrices(params?: { tickers?: string }): Promise<PriceListResponse> {
  const r = await apiClient.get("/market-data/prices", { params });
  return priceListResponseSchema.parse(r.data);
}

export async function getFxRates(params?: {
  forceRefresh?: boolean;
}): Promise<FxRateListResponse> {
  const r = await apiClient.get("/market-data/fx-rates", { params });
  return fxRateListResponseSchema.parse(r.data);
}

export async function getLinearRegression(params: {
  ticker: string;
  interval?: string;
  fromDate?: string;
  toDate?: string;
  since?: number;
}): Promise<LinearRegressionResponse> {
  const r = await apiClient.get("/market-data/linear-regression", { params });
  return linearRegressionResponseSchema.parse(r.data);
}

export interface PriceRefreshResult {
  total: number;
  succeeded: number;
  failures: { ticker: string; reason: string }[];
}

export async function refreshPrices(): Promise<PriceRefreshResult> {
  const r = await apiClient.post("/market-data/prices/refresh");
  return r.data as PriceRefreshResult;
}

/**
 * Read-only macro liquidity de-risk state (ADR-042). Reads the persisted
 * `macro_state` row — it does NOT trigger a FRED/ECB/BoE fetch.
 */
export async function getMacroState(): Promise<MacroStateResponse> {
  const r = await apiClient.get("/market-data/macro-state");
  return macroStateResponseSchema.parse(r.data);
}

/**
 * Force a full macro recompute: re-fetches every upstream series (FRED / ECB /
 * BoE / BoJ / chinadata.live, plus the ADR-068 China-M2 tip scrapes), re-derives
 * NETLIQ + GLOBAL_M2_USD, and rewrites `macro_state` — then returns the new row.
 *
 * This is the write-side counterpart to {@link getMacroState}, which only reads
 * the stored row. Slow by nature (a dozen upstream HTTP calls), so it is only
 * ever triggered by an explicit user action or the scheduled job.
 */
export async function refreshMacroState(): Promise<MacroStateResponse> {
  // Overrides the client's 60s default. ~14 sequential upstream fetches: ~11s on an idle Pi,
  // but ~40s observed when it collides with the scheduled price refresh. Must stay <= the
  // matching `proxy_read_timeout` in infra/nginx/nginx.conf, or nginx 504s first.
  const r = await apiClient.post("/market-data/macro-state/refresh", null, {
    timeout: 180_000,
  });
  return macroStateResponseSchema.parse(r.data);
}

/**
 * Full blend-factor history (ADR-069): every persisted `macro_state` row, ordered by
 * `asOfDate` ascending. Read-only and cheap — reads the persisted rows only, does NOT
 * trigger a FRED/ECB/BoE/BoJ/chinadata.live fetch. Backs the Settings page's blend-factor
 * history chart, merged client-side with the frozen pre-live backtest series.
 */
export async function getMacroStateHistory(): Promise<MacroStateHistoryListResponse> {
  const r = await apiClient.get("/market-data/macro-state/history");
  return macroStateHistoryListResponseSchema.parse(r.data);
}

/** The four reference symbols the blend-chart overlay (ADR-070, ADR-072) supports. */
export type BenchmarkSymbol = "BTC-USD" | "^NDX" | "GC=F" | "^HSI";

/**
 * Weekly closes of a reference index (BTC-USD, ^NDX, GC=F, ^HSI) for the Settings blend-chart
 * overlay — the frontend overlays their log-scaled, [0,1]-normalized value on the
 * blend-factor history. Read-only; server-cached for 12h. Unsupported symbols are a 400.
 */
export async function getBenchmarkHistory(params: {
  symbol: BenchmarkSymbol;
}): Promise<BenchmarkHistoryListResponse> {
  const r = await apiClient.get("/market-data/benchmark-history", { params });
  return benchmarkHistoryListResponseSchema.parse(r.data);
}

/**
 * Ticker picker backing list for the trade-history chart (ADR-057). Built from
 * every ticker ever present in asset_purchases, joined to positions for display
 * metadata — never triggers a Yahoo Finance call.
 */
export async function getTradeChartTickers(): Promise<TradeChartTickersResponse> {
  const r = await apiClient.get("/market-data/trade-chart/tickers");
  return tradeChartTickersResponseSchema.parse(r.data);
}

/**
 * Raw close-price series (no regression fit) plus every buy/sell marker for
 * one ticker, aggregated across portfolio/accountType (ADR-057).
 */
export async function getTradeChart(params: {
  ticker: string;
  interval?: string;
  fromDate?: string;
  toDate?: string;
}): Promise<TradeChartResponse> {
  const r = await apiClient.get("/market-data/trade-chart", { params });
  return tradeChartResponseSchema.parse(r.data);
}
