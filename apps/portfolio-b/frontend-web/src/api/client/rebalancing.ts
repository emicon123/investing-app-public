import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  rebalancingResponseSchema,
  type RebalancingResponse,
  type ConfirmPurchasesRequest,
} from "../schemas/index.js";

export async function calculateRebalancing(): Promise<RebalancingResponse> {
  const r = await apiClient.post("/purchases/calculate", {});
  return rebalancingResponseSchema.parse(r.data);
}

export async function confirmPurchases(
  req: ConfirmPurchasesRequest,
  options?: IdempotencyOptions,
): Promise<void> {
  await apiClient.post("/purchases/confirm", req, {
    headers: idempotencyHeaders(options?.idempotencyKey),
  });
}
