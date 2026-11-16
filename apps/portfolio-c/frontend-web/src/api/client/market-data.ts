import { apiClient } from "@common/app-core";
import {
  priceListResponseSchema,
  fxRateListResponseSchema,
  type PriceListResponse,
  type FxRateListResponse,
} from "../schemas/index.js";

export async function getPrices(params?: {
  tickers?: string;
  forceRefresh?: boolean;
}): Promise<PriceListResponse> {
  const r = await apiClient.get("/market-data/prices", { params });
  return priceListResponseSchema.parse(r.data);
}

export async function getFxRates(params?: {
  forceRefresh?: boolean;
}): Promise<FxRateListResponse> {
  const r = await apiClient.get("/market-data/fx-rates", { params });
  return fxRateListResponseSchema.parse(r.data);
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
