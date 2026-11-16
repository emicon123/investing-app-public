import { apiClient } from "@common/app-core";
import {
  newsListResponseSchema,
  newsDatesListResponseSchema,
  type NewsListResponse,
  type NewsDatesListResponse,
} from "../schemas/index.js";

export async function listNews(params?: { date?: string }): Promise<NewsListResponse> {
  const r = await apiClient.get("/news", { params });
  return newsListResponseSchema.parse(r.data);
}

export async function listNewsDates(): Promise<NewsDatesListResponse> {
  const r = await apiClient.get("/news/dates");
  return newsDatesListResponseSchema.parse(r.data);
}
