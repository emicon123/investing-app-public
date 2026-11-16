import { apiClient } from "@common/app-core";
import {
  assetListResponseSchema,
  assetResponseSchema,
  type AssetListResponse,
  type AssetResponse,
  type CreatePositionRequest,
  type UpdatePositionRequest,
  type UpdateAssetRequest,
  type SetAssetPrimaryRequest,
} from "../schemas/index.js";
import type { PortfolioType } from "../schemas/index.js";

/** Lists ASSETS (each with its subassets) — task 156: `GET /positions` answers with assets. */
export async function listPositions(params?: {
  portfolio?: PortfolioType;
  page?: number;
  size?: number;
}): Promise<AssetListResponse> {
  const r = await apiClient.get("/positions", { params });
  return assetListResponseSchema.parse(r.data);
}

/** `{id}` is a SUBASSET row id; the response is its PARENT asset (with all its subassets). */
export async function getPosition(id: number): Promise<AssetResponse> {
  const r = await apiClient.get(`/positions/${id}`);
  return assetResponseSchema.parse(r.data);
}

/** Creates one subasset (under `assetId`, or under a new asset via `newAsset`); answers with its asset. */
export async function createPosition(
  body: CreatePositionRequest,
): Promise<AssetResponse> {
  const r = await apiClient.post("/positions", body);
  return assetResponseSchema.parse(r.data);
}

/** Edits subasset-owned fields (and re-homes a non-primary subasset via `assetId`); answers with its asset. */
export async function updatePosition(
  id: number,
  body: UpdatePositionRequest,
): Promise<AssetResponse> {
  const r = await apiClient.put(`/positions/${id}`, body);
  return assetResponseSchema.parse(r.data);
}

/**
 * Deletes one subasset. The asset's last subasset takes the asset with it; a primary subasset
 * whose asset still has siblings is refused with 409 ("set a new primary first").
 */
export async function deletePosition(id: number): Promise<void> {
  await apiClient.delete(`/positions/${id}`);
}

/**
 * Makes `subassetId` the primary subasset of asset `assetId` (replaces the retired
 * `POST /positions/{id}/promote`, ADR-088) — a pure boolean flip: no units, cost basis or
 * targets move. Involutive: setting the previous primary again undoes it exactly.
 */
export async function setPrimary(assetId: number, subassetId: number): Promise<AssetResponse> {
  const r = await apiClient.post(`/assets/${assetId}/primary`, {
    subassetId,
  } satisfies SetAssetPrimaryRequest);
  return assetResponseSchema.parse(r.data);
}

/** Edits asset-level fields — the ONLY writer of targets (explicit `null` clears one; no Σ guard). */
export async function updateAsset(
  id: number,
  body: UpdateAssetRequest,
): Promise<AssetResponse> {
  const r = await apiClient.put(`/assets/${id}`, body);
  return assetResponseSchema.parse(r.data);
}

/** Deletes a whole asset; its subassets cascade. */
export async function deleteAsset(id: number): Promise<void> {
  await apiClient.delete(`/assets/${id}`);
}
