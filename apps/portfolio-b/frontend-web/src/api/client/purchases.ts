import { apiClient } from "@common/app-core";
import {
  purchaseHistoryResponseSchema,
  type PurchaseHistoryResponse,
} from "../schemas/index.js";

export async function getPurchaseHistory(): Promise<PurchaseHistoryResponse> {
  const r = await apiClient.get("/purchases/history");
  return purchaseHistoryResponseSchema.parse(r.data);
}
