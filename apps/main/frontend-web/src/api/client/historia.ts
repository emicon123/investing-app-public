import { apiClient } from "@common/app-core";
import {
  historiaListResponseSchema,
  type HistoriaListResponse,
} from "../schemas/index.js";

export async function listHistoria(params?: {
  portfolio?: string;
  from?: string;
  to?: string;
  page?: number;
  size?: number;
}): Promise<HistoriaListResponse> {
  const r = await apiClient.get("/history", { params });
  return historiaListResponseSchema.parse(r.data);
}
