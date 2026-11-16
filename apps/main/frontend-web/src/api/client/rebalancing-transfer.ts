import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  rebalancingTransferResponseSchema,
  type RebalancingTransferRequest,
  type RebalancingTransferResponse,
} from "../schemas/index.js";

export async function createRebalancingTransfer(
  body: RebalancingTransferRequest,
  options?: IdempotencyOptions,
): Promise<RebalancingTransferResponse> {
  const r = await apiClient.post("/rebalancing-transfers", body, {
    headers: idempotencyHeaders(options?.idempotencyKey),
  });
  return rebalancingTransferResponseSchema.parse(r.data);
}
