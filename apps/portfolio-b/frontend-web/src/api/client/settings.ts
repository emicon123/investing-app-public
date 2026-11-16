import { apiClient } from "@common/app-core";
import {
  settingsListResponseSchema,
  settingResponseSchema,
  type SettingsListResponse,
  type SettingResponse,
  type UpsertSettingRequest,
} from "../schemas/index.js";

export async function listSettings(): Promise<SettingsListResponse> {
  const r = await apiClient.get("/settings");
  return settingsListResponseSchema.parse(r.data);
}

export async function updateSetting(
  key: string,
  body: UpsertSettingRequest,
): Promise<SettingResponse> {
  const r = await apiClient.put(`/settings/${encodeURIComponent(key)}`, body);
  return settingResponseSchema.parse(r.data);
}
