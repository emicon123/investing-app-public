import { apiClient } from "@common/app-core";
import {
  purchaseHistoryResponseSchema,
  type PurchaseHistoryResponse,
  type Portfolio,
} from "../schemas/index.js";

export async function getPurchaseHistory(portfolio: Portfolio): Promise<PurchaseHistoryResponse> {
  const r = await apiClient.get("/purchases/history", { params: { portfolio } });
  return purchaseHistoryResponseSchema.parse(r.data);
}
