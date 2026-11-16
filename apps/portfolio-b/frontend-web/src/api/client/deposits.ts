import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  depositListResponseSchema,
  depositResponseSchema,
  xirrResponseSchema,
  type DepositListResponse,
  type DepositResponse,
  type CreateDepositRequest,
  type XirrResponse,
} from "../schemas/index.js";

export async function listDeposits(params?: {
  year?: number;
  page?: number;
  size?: number;
}): Promise<DepositListResponse> {
  const r = await apiClient.get("/deposits", { params });
  return depositListResponseSchema.parse(r.data);
}

export async function createDeposit(
  body: CreateDepositRequest,
  options?: IdempotencyOptions,
): Promise<DepositResponse> {
  const r = await apiClient.post("/deposits", body, {
    headers: idempotencyHeaders(options?.idempotencyKey),
  });
  return depositResponseSchema.parse(r.data);
}

export async function computeXirr(): Promise<XirrResponse> {
  const r = await apiClient.get("/deposits/xirr");
  return xirrResponseSchema.parse(r.data);
}
