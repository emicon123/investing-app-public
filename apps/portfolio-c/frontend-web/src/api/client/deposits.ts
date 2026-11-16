import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  depositListResponseSchema,
  depositResponseSchema,
  xirrResponseSchema,
  type DepositListResponse,
  type DepositResponse,
  type CreateDepositRequest,
  type XirrResponse,
  type Portfolio,
} from "../schemas/index.js";

export async function listDeposits(params: {
  portfolio: Portfolio;
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

export async function computeXirr(portfolio: Portfolio): Promise<XirrResponse> {
  const r = await apiClient.get("/deposits/xirr", { params: { portfolio } });
  return xirrResponseSchema.parse(r.data);
}
