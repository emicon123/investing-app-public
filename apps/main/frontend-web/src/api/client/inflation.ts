import { apiClient } from "@common/app-core";
import { inflationListResponseSchema, type InflationListResponse } from "../schemas/index.js";

export async function listInflation(params?: {
  year?: number;
  page?: number;
  size?: number;
}): Promise<InflationListResponse> {
  const r = await apiClient.get("/inflation", { params });
  return inflationListResponseSchema.parse(r.data);
}
