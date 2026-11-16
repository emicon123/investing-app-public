import { apiClient, idempotencyHeaders, type IdempotencyOptions } from "@common/app-core";
import {
  depositListResponseSchema,
  depositResponseSchema,
  xirrResponseSchema,
  accountCapUsageResponseSchema,
  accountCapAdjustmentResponseSchema,
  type DepositListResponse,
  type DepositResponse,
  type CreateDepositRequest,
  type XirrResponse,
  type AccountCapUsageResponse,
  type AccountCapAdjustmentRequest,
  type AccountCapAdjustmentResponse,
} from "../schemas/index.js";
import type { PortfolioType, AccountType } from "../schemas/index.js";

export async function listDeposits(params?: {
  portfolio?: PortfolioType;
  accountType?: AccountType;
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

export async function computeXirr(params?: {
  portfolio?: PortfolioType;
}): Promise<XirrResponse> {
  const r = await apiClient.get("/deposits/xirr", { params });
  return xirrResponseSchema.parse(r.data);
}

export async function getAccountCapUsage(
  year: number,
): Promise<AccountCapUsageResponse> {
  const r = await apiClient.get("/deposits/account-cap-usage", {
    params: { year },
  });
  return accountCapUsageResponseSchema.parse(r.data);
}

/**
 * Corrects the IKE/IKZE remaining limit for a given (accountType, year) — ADR-058.
 * `remainingPln` is the real remaining limit the user is asserting (e.g. read off a broker
 * statement), not a signed delta; the backend solves for the internal correction that produces
 * it. Idempotent — calling again for the same key replaces the correction. Returns the
 * freshly-recomputed AccountCapUsageItem. `accountType` must be "IKE" or "IKZE" (the backend
 * rejects "REGULAR" with 400).
 */
export async function updateAccountCapAdjustment(
  body: AccountCapAdjustmentRequest,
): Promise<AccountCapAdjustmentResponse> {
  const r = await apiClient.put("/deposits/account-cap-usage/adjustment", body);
  return accountCapAdjustmentResponseSchema.parse(r.data);
}
