import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  positionTransferResponseSchema,
  type PositionTransferRequest,
  type PositionTransferResponse,
} from "../schemas/index.js";

export async function executePositionTransfer(
  request: PositionTransferRequest,
  options?: IdempotencyOptions,
): Promise<PositionTransferResponse> {
  const r = await apiClient.post("/position-transfers", request, {
    headers: idempotencyHeaders(options?.idempotencyKey),
  });
  return positionTransferResponseSchema.parse(r.data);
}
