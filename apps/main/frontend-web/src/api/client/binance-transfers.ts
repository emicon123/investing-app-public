import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  binanceTransfersListResponseSchema,
  binanceTransferResponseSchema,
  type BinanceTransfersListResponse,
  type BinanceTransferResponse,
  type CreateBinanceTransferRequest,
} from "../schemas/index.js";

export async function listBinanceTransfers(params?: {
  year?: number;
}): Promise<BinanceTransfersListResponse> {
  const r = await apiClient.get("/binance-transfers", { params });
  return binanceTransfersListResponseSchema.parse(r.data);
}

export async function createBinanceTransfer(
  body: CreateBinanceTransferRequest,
  options?: IdempotencyOptions,
): Promise<BinanceTransferResponse> {
  const r = await apiClient.post("/binance-transfers", body, {
    headers: idempotencyHeaders(options?.idempotencyKey),
  });
  return binanceTransferResponseSchema.parse(r.data);
}

export async function deleteBinanceTransfer(id: number): Promise<void> {
  await apiClient.delete(`/binance-transfers/${id}`);
}
