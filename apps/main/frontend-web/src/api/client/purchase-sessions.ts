import { apiClient } from "@common/app-core";
import {
  purchaseSessionResponseSchema,
  purchaseSessionSummaryListResponseSchema,
  purchaseSessionCompleteResponseSchema,
  type CreatePurchaseSessionRequest,
  type PurchaseSessionResponse,
  type PurchaseSessionSummaryListResponse,
  type PurchaseSessionCompleteResponse,
  type UpdatePurchaseSessionItemEntry,
} from "../schemas/index.js";

export async function createPurchaseSession(
  body: CreatePurchaseSessionRequest,
): Promise<PurchaseSessionResponse> {
  const r = await apiClient.post("/purchase-sessions", body);
  return purchaseSessionResponseSchema.parse(r.data);
}

export async function getActivePurchaseSession(): Promise<PurchaseSessionResponse> {
  const r = await apiClient.get("/purchase-sessions/active");
  return purchaseSessionResponseSchema.parse(r.data);
}

export async function listPurchaseSessions(
  page = 0,
  size = 20,
): Promise<PurchaseSessionSummaryListResponse> {
  const r = await apiClient.get("/purchase-sessions", { params: { page, size } });
  return purchaseSessionSummaryListResponseSchema.parse(r.data);
}

export async function updatePurchaseSessionItems(
  id: number,
  items: UpdatePurchaseSessionItemEntry[],
): Promise<PurchaseSessionResponse> {
  const r = await apiClient.patch(`/purchase-sessions/${id}/items`, { items });
  return purchaseSessionResponseSchema.parse(r.data);
}

export async function completePurchaseSession(id: number): Promise<PurchaseSessionCompleteResponse> {
  const r = await apiClient.post(`/purchase-sessions/${id}/complete`);
  return purchaseSessionCompleteResponseSchema.parse(r.data);
}

export async function deletePurchaseSession(id: number): Promise<void> {
  await apiClient.delete(`/purchase-sessions/${id}`);
}
