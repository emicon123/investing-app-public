import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  cryptoTradeListResponseSchema,
  cryptoTradeResponseSchema,
  cryptoTaxSummaryResponseSchema,
  type CryptoTradeListResponse,
  type CryptoTradeResponse,
  type CryptoTaxSummaryResponse,
  type CreateCryptoTradeRequest,
} from "../schemas/index.js";
import type { Exchange, TradeSide } from "../schemas/index.js";

export async function listCryptoTrades(params?: {
  ticker?: string;
  year?: number;
  exchange?: Exchange;
  side?: TradeSide;
  page?: number;
  size?: number;
}): Promise<CryptoTradeListResponse> {
  const r = await apiClient.get("/crypto-trades", { params });
  return cryptoTradeListResponseSchema.parse(r.data);
}

export async function createCryptoTrade(
  body: CreateCryptoTradeRequest,
  options?: IdempotencyOptions,
): Promise<CryptoTradeResponse> {
  const r = await apiClient.post("/crypto-trades", body, {
    headers: idempotencyHeaders(options?.idempotencyKey),
  });
  return cryptoTradeResponseSchema.parse(r.data);
}

export async function getCryptoTaxSummary(
  year: number,
): Promise<CryptoTaxSummaryResponse> {
  const r = await apiClient.get("/crypto-trades/tax-summary", {
    params: { year },
  });
  return cryptoTaxSummaryResponseSchema.parse(r.data);
}
