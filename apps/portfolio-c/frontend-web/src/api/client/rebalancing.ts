import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  rebalancingResponseSchema,
  type RebalancingResponse,
  type CalculateRequest,
  type ConfirmPurchasesRequest,
} from "../schemas/index.js";

export async function calculateRebalancing(req: CalculateRequest): Promise<RebalancingResponse> {
  const r = await apiClient.post("/purchases/calculate", req);
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
