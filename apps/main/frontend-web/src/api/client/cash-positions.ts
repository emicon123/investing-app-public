import { apiClient } from "@common/app-core";
import {
  cashPositionListResponseSchema,
  type CashPositionListResponse,
  type UpsertCashPositionRequest,
} from "../schemas/index.js";

export async function getCashPositions(): Promise<CashPositionListResponse> {
  const r = await apiClient.get("/cash-positions");
  return cashPositionListResponseSchema.parse(r.data);
}

export async function createCashPosition(
  body: UpsertCashPositionRequest,
): Promise<CashPositionListResponse> {
  const r = await apiClient.post("/cash-positions", body);
  return cashPositionListResponseSchema.parse(r.data);
}
