import { apiClient } from "@common/app-core";
import { taxReturnListResponseSchema, type TaxReturnListResponse } from "../schemas/index.js";

export async function listTaxReturns(params?: {
  page?: number;
  size?: number;
}): Promise<TaxReturnListResponse> {
  const r = await apiClient.get("/tax-returns", { params });
  return taxReturnListResponseSchema.parse(r.data);
}
